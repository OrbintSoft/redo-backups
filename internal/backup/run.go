// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
	"github.com/OrbintSoft/redo-backups/internal/snapshot"
)

// Backup orchestrates a full backup run. All system access goes through Runner
// and Inspector, so the orchestration is testable with fakes.
type Backup struct {
	// Runner executes the imaging pipelines.
	Runner run.Runner
	// Inspector gathers disk facts.
	Inspector *disk.Inspector
	// Clock supplies the current time; defaults to time.Now when nil.
	Clock func() time.Time
	// LogDir is where partclone log files are written; when empty a temporary
	// directory is created per run and removed afterwards.
	LogDir string
}

// Report summarizes a completed backup.
type Report struct {
	Drive          string
	ID             string
	DescriptorPath string
	Partitions     []string
}

// Run performs the backup described by cfg: it resolves the drive, refuses a
// destination that is not available (ErrDestUnavailable), gathers the MBR and
// partition table, writes the ".redo" descriptor, images each selected
// partition, and finally writes the set's completion record ("<id>.complete").
// A set already in the destination under the same id is replaced.
func (b *Backup) Run(ctx context.Context, cfg *config.Config) (*Report, error) {
	strategy, err := snapshot.For(cfg, b.Runner)
	if err != nil {
		return nil, err
	}

	drive, d, parts, err := b.resolveTarget(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Refuse an unavailable destination before writing anything, including the
	// destination directory itself.
	if err := b.checkDest(ctx, cfg, drive); err != nil {
		return nil, err
	}

	now := b.now()

	id := cfg.ID
	if id == "" {
		id = FormatID(now)
	}

	timestamp := FormatTimestamp(now)

	mbr, err := b.Inspector.MBR(ctx, drive)
	if err != nil {
		return nil, err
	}

	sfd, err := b.Inspector.PartitionTable(ctx, drive)
	if err != nil {
		return nil, err
	}

	img := BuildImage(cfg, d, parts, id, timestamp, mbr, sfd)

	// Write the descriptor before imaging, matching Redo Rescue's order.
	set, descPath, err := startSet(cfg.Dest, img)
	if err != nil {
		return nil, err
	}
	defer func() { _ = set.Close() }()

	if err := b.imageAll(ctx, strategy, cfg, set, parts, id); err != nil {
		return nil, err
	}

	// Only now, with every partition imaged, is the set known to be complete.
	if _, err := writeCompletion(set, id, parts); err != nil {
		return nil, err
	}

	report := &Report{Drive: drive, ID: id, DescriptorPath: descPath, Partitions: nil}
	for _, part := range parts {
		report.Partitions = append(report.Partitions, part.Name)
	}

	return report, nil
}

// Plan is a read-only preview of what a backup run would do.
type Plan struct {
	Drive          string
	ID             string
	DescriptorPath string
	Consistency    config.Consistency
	Partitions     []PlannedPartition
}

// PlannedPartition describes the imaging of one partition in a Plan.
type PlannedPartition struct {
	Name     string
	Source   string
	Commands []string
}

// Plan computes what Run would do using only read-only inspection: it resolves
// the drive and partitions, refuses an unavailable destination as Run does, and
// builds the imaging pipeline for each partition, without reading the MBR,
// writing the descriptor, taking snapshots, or imaging anything. It backs the
// CLI's --dry-run.
func (b *Backup) Plan(ctx context.Context, cfg *config.Config) (*Plan, error) {
	// Surface an unknown/unimplemented consistency strategy without preparing it.
	if _, err := snapshot.For(cfg, b.Runner); err != nil {
		return nil, err
	}

	drive, _, parts, err := b.resolveTarget(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := b.checkDest(ctx, cfg, drive); err != nil {
		return nil, err
	}

	id := cfg.ID
	if id == "" {
		id = FormatID(b.now())
	}

	plan := &Plan{
		Drive:          drive,
		ID:             id,
		DescriptorPath: filepath.Join(cfg.Dest, id+".redo"),
		Consistency:    cfg.Consistency,
		Partitions:     nil,
	}
	for _, part := range parts {
		logfile := filepath.Join("<tmp>", part.Name+".log")
		pl := PartitionPipeline(part, "/dev/"+part.Name, string(cfg.Compressor), cfg.SplitSize, logfile, cfg.Dest, id)

		cmds := make([]string, len(pl.Stages))
		for i, s := range pl.Stages {
			cmds[i] = s.String()
		}

		plan.Partitions = append(plan.Partitions, PlannedPartition{
			Name:     part.Name,
			Source:   "/dev/" + part.Name,
			Commands: cmds,
		})
	}

	return plan, nil
}

// imageAll images each partition in turn, first removing the chunks an earlier
// set with the same id left for it.
func (b *Backup) imageAll(
	ctx context.Context,
	strategy snapshot.Strategy,
	cfg *config.Config,
	set *os.Root,
	parts []disk.Partition,
	id string,
) error {
	logDir, cleanup, err := b.resolveLogDir()
	if err != nil {
		return err
	}
	defer cleanup()

	for _, part := range parts {
		if err := removeChunks(set, id, part.Name); err != nil {
			return err
		}

		logfile := filepath.Join(logDir, part.Name+".log")
		if err := b.imagePartition(ctx, strategy, cfg, part, logfile, id); err != nil {
			return err
		}
	}

	return nil
}

// imagePartition prepares the partition with the consistency strategy, runs the
// imaging pipeline against the prepared source, and always releases the
// preparation afterwards (even on failure).
func (b *Backup) imagePartition(
	ctx context.Context,
	strategy snapshot.Strategy,
	cfg *config.Config,
	part disk.Partition,
	logfile, id string,
) (err error) {
	prepared, err := strategy.Prepare(ctx, snapshot.Target{
		Device:     part.Name,
		Mountpoint: part.Mountpoint,
		FS:         part.FS,
	})
	if err != nil {
		return fmt.Errorf("backup: preparing %s: %w", part.Name, err)
	}
	defer func() {
		if rerr := prepared.Release(); rerr != nil && err == nil {
			err = rerr
		}
	}()

	// Drop the kernel's cached view of the device now that the strategy has
	// quiesced it, so partclone reads the device itself rather than pages that
	// may predate what was written to it. Without this the image can silently
	// contain stale (typically zeroed) regions; it is the read-side counterpart
	// of the flush the restore path already needs.
	if ferr := b.Inspector.FlushBuffers(ctx, prepared.Source); ferr != nil {
		return ferr
	}

	pl := PartitionPipeline(part, prepared.Source, string(cfg.Compressor), cfg.SplitSize, logfile, cfg.Dest, id)
	if perr := b.Runner.RunPipeline(ctx, pl.Stages); perr != nil {
		return fmt.Errorf("backup: imaging %s: %w", part.Name, perr)
	}

	return nil
}

// resolveTarget resolves the drive (auto-detecting the root drive when
// configured), gathers its layout, and selects the partitions to back up. It
// performs only read-only inspection.
func (b *Backup) resolveTarget(
	ctx context.Context,
	cfg *config.Config,
) (string, *disk.Drive, []disk.Partition, error) {
	drive, err := b.resolveDrive(ctx, cfg)
	if err != nil {
		return "", nil, nil, err
	}

	d, err := b.Inspector.Drive(ctx, drive)
	if err != nil {
		return "", nil, nil, err
	}

	parts, err := SelectPartitions(cfg, d)
	if err != nil {
		return "", nil, nil, err
	}

	if len(parts) == 0 {
		return "", nil, nil, fmt.Errorf("%w %q", errNoPartitions, drive)
	}

	return drive, d, parts, nil
}

// resolveDrive returns the kernel name of the drive cfg names: the drive
// hosting the root filesystem for "auto", the drive behind a /dev/disk/by-*/
// link, or the name itself. Unlike resolveTarget it only runs findmnt and
// lsblk, which need no root.
func (b *Backup) resolveDrive(ctx context.Context, cfg *config.Config) (string, error) {
	switch {
	case cfg.DriveAuto():
		return b.Inspector.RootDrive(ctx)
	case cfg.DriveIsPath():
		return b.Inspector.ResolveDrivePath(ctx, cfg.Drive)
	default:
		return cfg.Drive, nil
	}
}

func (b *Backup) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}

	return time.Now()
}

// resolveLogDir returns the directory for partclone logs and a cleanup function.
func (b *Backup) resolveLogDir() (string, func(), error) {
	if b.LogDir != "" {
		return b.LogDir, func() {}, nil
	}

	dir, err := os.MkdirTemp("", "redo-backup-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("backup: creating log directory: %w", err)
	}

	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

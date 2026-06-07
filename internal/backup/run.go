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

// Run performs the backup described by cfg: it resolves the drive, gathers the
// MBR and partition table, writes the ".redo" descriptor, and images each
// selected partition.
func (b *Backup) Run(ctx context.Context, cfg *config.Config) (*Report, error) {
	strategy, err := snapshot.For(cfg, b.Runner)
	if err != nil {
		return nil, err
	}

	drive := cfg.Drive
	if cfg.DriveAuto() {
		detected, err := b.Inspector.RootDrive(ctx)
		if err != nil {
			return nil, err
		}
		drive = detected
	}

	d, err := b.Inspector.Drive(ctx, drive)
	if err != nil {
		return nil, err
	}
	parts, err := SelectPartitions(cfg, d)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("backup: no partitions to back up on drive %q", drive)
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

	if err := os.MkdirAll(cfg.Dest, 0o755); err != nil {
		return nil, fmt.Errorf("backup: creating destination %s: %w", cfg.Dest, err)
	}
	// Write the descriptor before imaging, matching Redo Rescue's order.
	descPath, err := WriteDescriptor(cfg.Dest, img)
	if err != nil {
		return nil, err
	}

	logDir, cleanup, err := b.resolveLogDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	report := &Report{Drive: drive, ID: id, DescriptorPath: descPath}
	for _, part := range parts {
		logfile := filepath.Join(logDir, part.Name+".log")
		if err := b.imagePartition(ctx, strategy, cfg, part, logfile, id); err != nil {
			return nil, err
		}
		report.Partitions = append(report.Partitions, part.Name)
	}
	return report, nil
}

// imagePartition prepares the partition with the consistency strategy, runs the
// imaging pipeline against the prepared source, and always releases the
// preparation afterwards (even on failure).
func (b *Backup) imagePartition(ctx context.Context, strategy snapshot.Strategy, cfg *config.Config, part disk.Partition, logfile, id string) (err error) {
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

	pl := PartitionPipeline(part, prepared.Source, string(cfg.Compressor), cfg.SplitSize, logfile, cfg.Dest, id)
	if perr := b.Runner.RunPipeline(ctx, pl.Stages); perr != nil {
		return fmt.Errorf("backup: imaging %s: %w", part.Name, perr)
	}
	return nil
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

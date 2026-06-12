// SPDX-License-Identifier: EUPL-1.2

// Package disk inspects block devices to gather the facts a backup needs: the
// partition layout, per-device sizes, the drive's MBR/GPT header, and the
// partition table. All system access goes through a run.Runner so the logic is
// unit-testable with a fake.
package disk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/OrbintSoft/redo-backups/internal/redo"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// devNameRE matches a bare device name such as "sda", "sda1" or "nvme0n1p2"
// (no "/dev/" prefix, no path separators).
var devNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Sentinel inspection errors. Dynamic context (the device name, sizes) is added
// by wrapping these with %w, so they remain matchable with errors.Is.
var (
	errDriveNotFound  = errors.New("disk: drive not found in lsblk output")
	errNoRootSource   = errors.New("disk: could not determine root source device")
	errNoParentDisk   = errors.New("disk: device has no parent disk")
	errMBRSize        = errors.New("disk: unexpected MBR size")
	errInvalidDevName = errors.New("disk: invalid device name")
)

// Partition describes one partition discovered on a drive.
type Partition struct {
	// Name is the device name without "/dev/" (e.g. "sda1").
	Name string
	// Bytes is the partition size in bytes (blockdev --getsize64).
	Bytes int64
	// Size is the human-readable size as reported by lsblk (e.g. "127M").
	Size string
	// Type is the partition type description (lsblk PARTTYPENAME).
	Type string
	// FS is the filesystem type (lsblk FSTYPE).
	FS string
	// Label is the filesystem label (lsblk LABEL), possibly empty.
	Label string
	// Mountpoint is where the partition is currently mounted, or empty if it is
	// not mounted.
	Mountpoint string
}

// Drive describes a whole drive and its partitions.
type Drive struct {
	// Name is the device name without "/dev/" (e.g. "sda").
	Name string
	// Bytes is the whole-drive size in bytes (blockdev --getsize64).
	Bytes int64
	// Partitions are the drive's partitions, in the order lsblk reports them.
	Partitions []Partition
}

// Inspector gathers disk facts via a run.Runner.
type Inspector struct {
	Runner run.Runner
}

// New returns an Inspector backed by the given Runner.
func New(r run.Runner) *Inspector { return &Inspector{Runner: r} }

// lsblk JSON decoding types (only the fields we consume).
type lsblkOutput struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name         string        `json:"name"`
	Size         string        `json:"size"`
	FSType       string        `json:"fstype"`
	PartTypeName string        `json:"parttypename"`
	Label        string        `json:"label"`
	Mountpoint   string        `json:"mountpoint"`
	Type         string        `json:"type"`
	Children     []lsblkDevice `json:"children"`
}

// Drive returns the layout of the named drive (e.g. "sda"). Partition byte sizes
// and the whole-drive size are read with blockdev for exactness, matching Redo
// Rescue.
func (i *Inspector) Drive(ctx context.Context, name string) (*Drive, error) {
	if err := validateDevName(name); err != nil {
		return nil, err
	}

	res, err := i.Runner.Run(ctx, run.Command{
		Name: "lsblk",
		Args: []string{"-J", "-o", "NAME,SIZE,FSTYPE,PARTTYPENAME,LABEL,MOUNTPOINT,TYPE", "--", "/dev/" + name},
	})
	if err != nil {
		return nil, fmt.Errorf("disk: lsblk %s: %w", name, err)
	}

	var out lsblkOutput
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, fmt.Errorf("disk: parsing lsblk output for %s: %w", name, err)
	}

	dev := findDevice(out.BlockDevices, name)
	if dev == nil {
		return nil, fmt.Errorf("%w: %q", errDriveNotFound, name)
	}

	driveBytes, err := i.devBytes(ctx, name)
	if err != nil {
		return nil, err
	}

	drive := &Drive{Name: name, Bytes: driveBytes, Partitions: nil}

	for _, c := range dev.Children {
		if c.Type != "part" {
			continue
		}

		pbytes, err := i.devBytes(ctx, c.Name)
		if err != nil {
			return nil, err
		}

		drive.Partitions = append(drive.Partitions, Partition{
			Name:       c.Name,
			Bytes:      pbytes,
			Size:       c.Size,
			Type:       c.PartTypeName,
			FS:         c.FSType,
			Label:      c.Label,
			Mountpoint: c.Mountpoint,
		})
	}

	return drive, nil
}

// RootDrive detects the whole drive that hosts the root filesystem. It resolves
// the source device of "/" with findmnt, then asks lsblk for that device's
// parent kernel device (PKNAME), e.g. "/dev/sda2" -> "sda".
func (i *Inspector) RootDrive(ctx context.Context) (string, error) {
	src, err := i.Runner.Run(ctx, run.Command{
		Name: "findmnt", Args: []string{"-n", "-o", "SOURCE", "/"},
	})
	if err != nil {
		return "", fmt.Errorf("disk: finding root source: %w", err)
	}

	source := strings.TrimSpace(string(src.Stdout))
	if source == "" {
		return "", errNoRootSource
	}

	pk, err := i.Runner.Run(ctx, run.Command{
		Name: "lsblk", Args: []string{"-n", "-o", "PKNAME", source},
	})
	if err != nil {
		return "", fmt.Errorf("disk: finding parent of %s: %w", source, err)
	}

	name := firstLine(string(pk.Stdout))
	if name == "" {
		return "", fmt.Errorf("%w: %s", errNoParentDisk, source)
	}

	if err := validateDevName(name); err != nil {
		return "", err
	}

	return name, nil
}

// MBR returns the first redo.MBRSize bytes of the drive (MBR plus GPT primary
// area), read with dd, matching Redo Rescue's extract_mbr.
func (i *Inspector) MBR(ctx context.Context, drive string) ([]byte, error) {
	if err := validateDevName(drive); err != nil {
		return nil, err
	}

	res, err := i.Runner.Run(ctx, run.Command{
		Name: "dd",
		Args: []string{"if=/dev/" + drive, "bs=32k", "count=1"},
	})
	if err != nil {
		return nil, fmt.Errorf("disk: reading MBR of %s: %w", drive, err)
	}

	if len(res.Stdout) != redo.MBRSize {
		return nil, fmt.Errorf("%w for %s: expected %d bytes, got %d",
			errMBRSize, drive, redo.MBRSize, len(res.Stdout))
	}

	return res.Stdout, nil
}

// PartitionTable returns the `sfdisk --dump` output for the drive, matching Redo
// Rescue's extract_sfd.
func (i *Inspector) PartitionTable(ctx context.Context, drive string) ([]byte, error) {
	if err := validateDevName(drive); err != nil {
		return nil, err
	}

	res, err := i.Runner.Run(ctx, run.Command{
		Name: "sfdisk",
		Args: []string{"--dump", "/dev/" + drive},
	})
	if err != nil {
		return nil, fmt.Errorf("disk: dumping partition table of %s: %w", drive, err)
	}

	return res.Stdout, nil
}

// devBytes returns the size of a block device in bytes via blockdev --getsize64.
func (i *Inspector) devBytes(ctx context.Context, name string) (int64, error) {
	res, err := i.Runner.Run(ctx, run.Command{
		Name: "blockdev",
		Args: []string{"--getsize64", "/dev/" + name},
	})
	if err != nil {
		return 0, fmt.Errorf("disk: size of %s: %w", name, err)
	}

	s := strings.TrimSpace(string(res.Stdout))

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("disk: parsing size of %s (%q): %w", name, s, err)
	}

	return n, nil
}

// findDevice locates the device with the given name among the listed devices
// (top level only, which is where lsblk places a queried drive).
func findDevice(devs []lsblkDevice, name string) *lsblkDevice {
	for idx := range devs {
		if devs[idx].Name == name {
			return &devs[idx]
		}
	}

	return nil
}

// firstLine returns the first non-empty, trimmed line of s.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}

	return ""
}

// validateDevName guards against path traversal / shell-special characters in a
// device name. Execution does not go through a shell, but rejecting bad names
// early gives clearer errors.
func validateDevName(name string) error {
	if !devNameRE.MatchString(name) {
		return fmt.Errorf("%w %q", errInvalidDevName, name)
	}

	return nil
}

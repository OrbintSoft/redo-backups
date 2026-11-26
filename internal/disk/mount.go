// SPDX-License-Identifier: EUPL-1.2

package disk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/OrbintSoft/redo-backups/internal/run"
)

// findmntFields are the columns requested when locating the filesystem that
// holds a path.
const findmntFields = "TARGET,SOURCE,FSTYPE"

// Sentinel mount-lookup errors, wrapped with the offending path or device.
var (
	errRelativePath = errors.New("disk: path is not absolute")
	errNoFilesystem = errors.New("disk: no filesystem found for path")
)

// Mount describes the mounted filesystem that holds a path: where it is
// mounted, what is mounted there, and the block devices it is stored on.
type Mount struct {
	// Target is the filesystem's mount point (findmnt TARGET).
	Target string
	// Source is what is mounted there (findmnt SOURCE): a device path such as
	// "/dev/sdb1" or "/dev/mapper/vg-data", or a source that is not a block
	// device, such as "tmpfs" or "nas:/export". The "[/dir]" suffix findmnt adds
	// for a bind mount or a btrfs subvolume is left out.
	Source string
	// FSType is the filesystem type (findmnt FSTYPE).
	FSType string
	// Devices lists the kernel names of every block device the filesystem is
	// stored on: the source device itself and, through device-mapper, LVM, LUKS
	// or RAID, the partitions and whole drives beneath it (for example "dm-0",
	// "sda3", "sda"). It is empty when the source is not a block device.
	Devices []string
}

// StoredOn reports whether the filesystem is stored, in whole or in part, on
// the named block device (a kernel name such as "sda" or "sda2").
func (m *Mount) StoredOn(device string) bool {
	return slices.Contains(m.Devices, device)
}

// findmnt JSON decoding types (only the fields we consume).
type findmntOutput struct {
	Filesystems []findmntFilesystem `json:"filesystems"`
}

type findmntFilesystem struct {
	Target string `json:"target"`
	Source string `json:"source"`
	FSType string `json:"fstype"`
}

// MountOf returns the mounted filesystem that holds path, which must be
// absolute and must exist: findmnt resolves it on the live system, walking up
// to the mount point it lies under.
func (i *Inspector) MountOf(ctx context.Context, path string) (*Mount, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: %q", errRelativePath, path)
	}

	res, err := i.Runner.Run(ctx, run.Command{
		Name: "findmnt",
		Args: []string{"-J", "-o", findmntFields, "--nofsroot", "--target", path},
	})
	if err != nil {
		return nil, fmt.Errorf("disk: finding the filesystem of %s: %w", path, err)
	}

	var out findmntOutput
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, fmt.Errorf("disk: parsing findmnt output for %s: %w", path, err)
	}

	if len(out.Filesystems) == 0 {
		return nil, fmt.Errorf("%w: %s", errNoFilesystem, path)
	}

	fsys := out.Filesystems[0]
	mount := &Mount{Target: fsys.Target, Source: fsys.Source, FSType: fsys.FSType, Devices: nil}

	if !strings.HasPrefix(mount.Source, "/dev/") {
		return mount, nil
	}

	devices, err := i.devicesBeneath(ctx, mount.Source)
	if err != nil {
		return nil, err
	}

	mount.Devices = devices

	return mount, nil
}

// devicesBeneath lists the kernel names of a block device and of every device
// it is stored on, with `lsblk --inverse`, which walks from a device down to
// the drives beneath it. A device reached along more than one path (an LV
// spanning two partitions of one drive, say) is listed once.
func (i *Inspector) devicesBeneath(ctx context.Context, devicePath string) ([]string, error) {
	if err := validateDevPath(devicePath); err != nil {
		return nil, err
	}

	res, err := i.Runner.Run(ctx, run.Command{
		Name: cmdLsblk, Args: []string{"-l", "-n", "-s", "-o", "KNAME", "--", devicePath},
	})
	if err != nil {
		return nil, fmt.Errorf("disk: listing the devices beneath %s: %w", devicePath, err)
	}

	var names []string

	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || slices.Contains(names, name) {
			continue
		}

		if err := validateDevName(name); err != nil {
			return nil, err
		}

		names = append(names, name)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("%w: %s", errNoDeviceForPath, devicePath)
	}

	return names, nil
}

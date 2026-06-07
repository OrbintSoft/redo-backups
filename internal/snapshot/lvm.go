// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// snapshotSuffix is appended to a logical volume's name to form its temporary
// backup snapshot.
const snapshotSuffix = "_redosnap"

// LVMSnapshot images a point-in-time LVM snapshot of the target. It only applies
// when the device to image is itself a logical volume: it creates a snapshot LV,
// images the snapshot's block device, and removes the snapshot afterwards. This
// gives a consistent image with only a brief write pause, at the cost of
// requiring LVM and free space in the volume group.
type LVMSnapshot struct {
	Runner run.Runner
	// SnapshotSize is the size argument passed to lvcreate (e.g. "10G" or
	// "20%ORIGIN").
	SnapshotSize string
}

// Name returns the configuration name of the strategy.
func (*LVMSnapshot) Name() config.Consistency { return config.ConsistencyLVMSnapshot }

// Prepare creates a snapshot of the target logical volume and returns the
// snapshot's device path as the source to image.
func (s *LVMSnapshot) Prepare(ctx context.Context, t Target) (Prepared, error) {
	dev := t.DevicePath()

	vg, lv, err := s.resolveLV(ctx, dev)
	if err != nil {
		return Prepared{}, err
	}

	snapName := lv + snapshotSuffix
	origin := "/dev/" + vg + "/" + lv
	if _, err := s.Runner.Run(ctx, run.Command{
		Name: "lvcreate",
		Args: []string{"--snapshot", "--name", snapName, "--size", s.SnapshotSize, origin},
	}); err != nil {
		return Prepared{}, fmt.Errorf("snapshot: creating LVM snapshot of %s: %w", origin, err)
	}

	snapDev := "/dev/" + vg + "/" + snapName
	release := func() error {
		if _, err := s.Runner.Run(ctx, run.Command{
			Name: "lvremove", Args: []string{"--force", snapDev},
		}); err != nil {
			return fmt.Errorf("snapshot: removing LVM snapshot %s: %w", snapDev, err)
		}
		return nil
	}
	return Prepared{Source: snapDev, Release: release}, nil
}

// resolveLV returns the volume group and logical volume names backing dev, or an
// error if dev is not an LVM logical volume.
func (s *LVMSnapshot) resolveLV(ctx context.Context, dev string) (vg, lv string, err error) {
	res, err := s.Runner.Run(ctx, run.Command{
		Name: "lvs",
		Args: []string{"--noheadings", "-o", "vg_name,lv_name", "--separator", "/", dev},
	})
	if err != nil {
		return "", "", fmt.Errorf("snapshot: %s is not an LVM logical volume: %w", dev, err)
	}
	out := strings.TrimSpace(string(res.Stdout))
	slash := strings.IndexByte(out, '/')
	if slash <= 0 || slash == len(out)-1 {
		return "", "", fmt.Errorf("snapshot: could not parse lvs output %q for %s", out, dev)
	}
	return out[:slash], out[slash+1:], nil
}

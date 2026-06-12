// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// LVM provides crash-consistency for an LVM physical-volume partition.
//
// A PV partition is imaged raw (partclone.dd), exactly as Redo Rescue does, so
// the backup stays restorable from the Redo Rescue live CD. To make that raw
// image consistent, this strategy freezes every mounted filesystem that lives on
// the PV's logical volumes for the duration of imaging and thaws them again
// afterwards. The imaged device is the PV partition itself, unchanged.
//
// Note this is NOT a logical-volume-level backup: imaging logical volumes
// individually would produce images Redo Rescue cannot restore.
type LVM struct {
	Runner run.Runner
}

// Name returns the configuration name of the strategy.
func (*LVM) Name() config.Consistency { return config.ConsistencyLVM }

// Prepare freezes every mounted filesystem in the device's subtree (the LVs of
// the PV) and returns the PV partition unchanged as the source to image.
func (s *LVM) Prepare(ctx context.Context, t Target) (Prepared, error) {
	mounts, err := s.subtreeMounts(ctx, t.Device)
	if err != nil {
		return Prepared{}, err
	}

	var frozen []string

	thaw := func() error {
		var firstErr error

		for _, mp := range frozen {
			_, err := s.Runner.Run(ctx, run.Command{Name: cmdFsfreeze, Args: []string{"-u", mp}})
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("snapshot: thawing %s: %w", mp, err)
			}
		}

		return firstErr
	}

	for _, mp := range mounts {
		if _, err := s.Runner.Run(ctx, run.Command{Name: cmdFsfreeze, Args: []string{"-f", mp}}); err != nil {
			_ = thaw()

			return Prepared{}, fmt.Errorf("snapshot: freezing %s: %w", mp, err)
		}

		frozen = append(frozen, mp)
	}

	return Prepared{Source: t.DevicePath(), Release: thaw}, nil
}

// subtreeMounts returns the mountpoints of the device and all its descendants
// (for a PV partition, those are its logical volumes), via lsblk.
func (s *LVM) subtreeMounts(ctx context.Context, dev string) ([]string, error) {
	res, err := s.Runner.Run(ctx, run.Command{
		Name: "lsblk", Args: []string{"-J", "-o", "NAME,MOUNTPOINT", "/dev/" + dev},
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot: listing %s: %w", dev, err)
	}

	var out struct {
		BlockDevices []lvmNode `json:"blockdevices"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, fmt.Errorf("snapshot: parsing lsblk for %s: %w", dev, err)
	}

	var (
		mounts []string
		walk   func(nodes []lvmNode)
	)

	walk = func(nodes []lvmNode) {
		for _, n := range nodes {
			if n.Mountpoint != "" {
				mounts = append(mounts, n.Mountpoint)
			}

			walk(n.Children)
		}
	}
	walk(out.BlockDevices)

	return mounts, nil
}

// lvmNode is the subset of lsblk output the LVM strategy reads.
type lvmNode struct {
	Name       string    `json:"name"`
	Mountpoint string    `json:"mountpoint"`
	Children   []lvmNode `json:"children"`
}

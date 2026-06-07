// SPDX-License-Identifier: EUPL-1.2
//
// Package snapshot provides the live-consistency strategies a backup can use
// when imaging mounted filesystems. A Strategy prepares a partition for imaging
// (for example by freezing it or creating a snapshot) and returns the device to
// image plus a release function that undoes the preparation afterwards. See
// docs/redo-format.md for the trade-offs of each strategy.
package snapshot

import (
	"context"
	"fmt"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// Target describes the partition to be imaged.
type Target struct {
	// Device is the partition device name without "/dev/" (e.g. "sda2").
	Device string
	// Mountpoint is where the partition is mounted, or empty if not mounted.
	Mountpoint string
	// FS is the filesystem type.
	FS string
}

// DevicePath returns the absolute device path for the target (e.g. "/dev/sda2").
func (t Target) DevicePath() string { return "/dev/" + t.Device }

// Prepared is the result of preparing a target for imaging.
type Prepared struct {
	// Source is the device path that should be imaged. It equals the original
	// device for in-place strategies (none, fsfreeze) and a snapshot device for
	// snapshot strategies.
	Source string
	// Release undoes the preparation (unfreeze, remove snapshot). It is always
	// non-nil and safe to call exactly once.
	Release func() error
}

// Strategy prepares partitions for consistent imaging.
type Strategy interface {
	// Name returns the strategy's configuration name.
	Name() config.Consistency
	// Prepare readies the target for imaging.
	Prepare(ctx context.Context, t Target) (Prepared, error)
}

// For returns the Strategy selected by cfg.Consistency, using r to run any
// required commands.
func For(cfg *config.Config, r run.Runner) (Strategy, error) {
	switch cfg.Consistency {
	case config.ConsistencyNone:
		return None{}, nil
	case config.ConsistencyFsfreeze:
		return &Fsfreeze{Runner: r}, nil
	case config.ConsistencyLVM:
		return &LVM{Runner: r}, nil
	default:
		return nil, fmt.Errorf("snapshot: unknown strategy %q", cfg.Consistency)
	}
}

// noRelease is a release function that does nothing.
func noRelease() error { return nil }

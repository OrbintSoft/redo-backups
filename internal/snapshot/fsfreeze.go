// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"fmt"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// Fsfreeze freezes a mounted filesystem with `fsfreeze -f` for the duration of
// imaging and thaws it with `fsfreeze -u` afterwards, yielding a crash-consistent
// image at the cost of blocking writes while imaging runs. Unmounted targets are
// imaged directly (there is nothing to freeze).
type Fsfreeze struct {
	Runner run.Runner
}

// Name returns the configuration name of the strategy.
func (*Fsfreeze) Name() config.Consistency { return config.ConsistencyFsfreeze }

// Prepare freezes the target's filesystem if it is mounted. The returned Release
// thaws it; it is safe to call even if the freeze was skipped.
func (s *Fsfreeze) Prepare(ctx context.Context, t Target) (Prepared, error) {
	source := t.DevicePath()
	if t.Mountpoint == "" {
		// Not mounted: nothing to freeze, image directly.
		return Prepared{Source: source, Release: noRelease}, nil
	}

	if _, err := s.Runner.Run(ctx, run.Command{
		Name: "fsfreeze", Args: []string{"-f", t.Mountpoint},
	}); err != nil {
		return Prepared{}, fmt.Errorf("snapshot: freezing %s: %w", t.Mountpoint, err)
	}

	release := func() error {
		if _, err := s.Runner.Run(ctx, run.Command{
			Name: "fsfreeze", Args: []string{"-u", t.Mountpoint},
		}); err != nil {
			return fmt.Errorf("snapshot: thawing %s: %w", t.Mountpoint, err)
		}
		return nil
	}
	return Prepared{Source: source, Release: release}, nil
}

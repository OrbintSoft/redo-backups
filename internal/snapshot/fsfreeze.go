// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// cmdFsfreeze is the util-linux command used to freeze (`-f`) and thaw (`-u`)
// mounted filesystems for crash-consistent imaging.
const cmdFsfreeze = "fsfreeze"

// unfreezableFS holds filesystem types (as reported by lsblk's FSTYPE, see
// Target.FS) known not to implement the FIFREEZE ioctl; `fsfreeze -f` fails on
// them with ENOTSUP. The most common case is the vfat EFI system partition.
var unfreezableFS = map[string]bool{
	"vfat":  true,
	"exfat": true,
}

// Fsfreeze freezes a mounted filesystem with `fsfreeze -f` for the duration of
// imaging and thaws it with `fsfreeze -u` afterwards, yielding a crash-consistent
// image at the cost of blocking writes while imaging runs. Unmounted targets, and
// targets on a filesystem that doesn't support freezing (see unfreezableFS), are
// imaged directly instead.
type Fsfreeze struct {
	Runner run.Runner
}

// Name returns the configuration name of the strategy.
func (*Fsfreeze) Name() config.Consistency { return config.ConsistencyFsfreeze }

// Prepare freezes the target's filesystem if it is mounted and freezable. The
// returned Release thaws it; it is safe to call even if the freeze was skipped.
func (s *Fsfreeze) Prepare(ctx context.Context, t Target) (Prepared, error) {
	source := t.DevicePath()
	if t.Mountpoint == "" || unfreezableFS[strings.ToLower(t.FS)] {
		// Not mounted, or mounted on a filesystem that doesn't support
		// FIFREEZE: nothing to freeze, image directly.
		return Prepared{Source: source, Release: noRelease}, nil
	}

	if _, err := s.Runner.Run(ctx, run.Command{
		Name: cmdFsfreeze, Args: []string{"-f", t.Mountpoint},
	}); err != nil {
		return Prepared{}, fmt.Errorf("snapshot: freezing %s: %w", t.Mountpoint, err)
	}

	release := func() error {
		if _, err := s.Runner.Run(ctx, run.Command{
			Name: cmdFsfreeze, Args: []string{"-u", t.Mountpoint},
		}); err != nil {
			return fmt.Errorf("snapshot: thawing %s: %w", t.Mountpoint, err)
		}

		return nil
	}

	return Prepared{Source: source, Release: release}, nil
}

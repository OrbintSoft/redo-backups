// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/OrbintSoft/redo-backups/internal/config"
)

// destDirPerm restricts the backup destination directory to owner and group. It
// holds backup images that may be sensitive and is accessed only by the
// root-run tool, so it need not be world-readable.
const destDirPerm os.FileMode = 0o750

// ErrDestUnavailable reports a destination a backup must not be written to,
// such as one on the drive being backed up, which is where a destination lands
// when the backup disk is not mounted on its mount point. Unlike the other
// failures of a run, another attempt does not help until the destination is
// made available.
var ErrDestUnavailable = errors.New("backup: destination not available")

// checkDest refuses a destination whose filesystem is not the one mounted on
// the profile's dest_mount, when that is set, or is stored on the drive being
// backed up. The destination need not exist yet: the filesystem checked is the
// one holding its nearest existing ancestor, which is where it would be
// created.
func (b *Backup) checkDest(ctx context.Context, cfg *config.Config, drive string) error {
	dest := cfg.Dest

	abs, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("backup: resolving destination %s: %w", dest, err)
	}

	existing, err := nearestExisting(abs)
	if err != nil {
		return fmt.Errorf("backup: checking destination %s: %w", dest, err)
	}

	mount, err := b.Inspector.MountOf(ctx, existing)
	if err != nil {
		return fmt.Errorf("backup: checking destination %s: %w", dest, err)
	}

	if want := filepath.Clean(cfg.DestMount); cfg.DestMount != "" && mount.Target != want {
		return fmt.Errorf("%w: %s would be written to the filesystem mounted on %s, "+
			"not to the one mounted on %s (dest_mount); is the backup disk mounted?",
			ErrDestUnavailable, dest, mount.Target, want)
	}

	if mount.StoredOn(drive) {
		return fmt.Errorf("%w: %s would be written to %s (mounted on %s), which is on %s, "+
			"the drive being backed up; is the backup disk mounted?",
			ErrDestUnavailable, dest, mount.Source, mount.Target, drive)
	}

	return nil
}

// nearestExisting returns path itself when it exists, otherwise its closest
// ancestor that does. path must be absolute, so the walk ends at "/" at the
// latest.
func nearestExisting(path string) (string, error) {
	for {
		_, err := os.Stat(path)
		if err == nil {
			return path, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("backup: inspecting %s: %w", path, err)
		}

		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("backup: inspecting %s: %w", path, err)
		}

		path = parent
	}
}

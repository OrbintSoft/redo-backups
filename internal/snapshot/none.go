// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"

	"github.com/OrbintSoft/redo-backups/internal/config"
)

// None images the live device as-is, without any consistency measure. It is the
// fastest strategy and has no requirements, but is only safe for idle or
// read-mostly filesystems.
type None struct{}

// Name returns the configuration name of the strategy.
func (None) Name() config.Consistency { return config.ConsistencyNone }

// Prepare returns the original device unchanged.
func (None) Prepare(_ context.Context, t Target) (Prepared, error) {
	return Prepared{Source: t.DevicePath(), Release: noRelease}, nil
}

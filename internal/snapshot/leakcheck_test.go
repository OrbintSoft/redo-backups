// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/leakcheck"
)

// TestMain runs this package's tests under Go's goroutine leak detector. It is
// a no-op on a toolchain older than Go 1.27, which is where the goroutineleak
// profile stopped being experimental (see internal/leakcheck).
func TestMain(m *testing.M) {
	leakcheck.Check(m)
}

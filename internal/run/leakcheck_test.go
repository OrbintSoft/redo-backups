// SPDX-License-Identifier: EUPL-1.2

package run

import (
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/leakcheck"
)

// TestMain runs this package's tests under the Go 1.26 goroutine leak detector.
// It is a no-op unless the test binary is built with
// GOEXPERIMENT=goroutineleakprofile (see internal/leakcheck).
func TestMain(m *testing.M) {
	leakcheck.Check(m)
}

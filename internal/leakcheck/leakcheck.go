// SPDX-License-Identifier: EUPL-1.2

// Package leakcheck wires Go 1.26's experimental goroutine leak profile into the
// unit-test suite. A leaked goroutine is one blocked on a concurrency primitive
// (channel, sync.Mutex, sync.Cond, ...) that the garbage collector has found to
// be unreachable, so it can never be unblocked.
//
// The profile only exists when the test binary is built with
//
//	GOEXPERIMENT=goroutineleakprofile
//
// (see DEPENDENCIES.md, the `leakcheck` make target, and the CI job). Without
// that experiment pprof.Lookup("goroutineleak") returns nil and Check is a
// no-op, so an ordinary `go test ./...` is completely unaffected.
//
// Each test package opts in with a one-line TestMain:
//
//	func TestMain(m *testing.M) { leakcheck.Check(m) }
package leakcheck

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

// TestingM is the subset of *testing.M that Check needs. Declaring it as an
// interface keeps this package from importing "testing" in non-test code; a real
// *testing.M satisfies it.
type TestingM interface {
	Run() int
}

// Check runs the package's tests via m.Run and then, if the goroutine leak
// profile is available, fails the test binary when any leaked goroutines remain.
// It calls os.Exit and so never returns; use it directly as the body of
// TestMain.
func Check(m TestingM) {
	code := m.Run()
	// Only surface leaks on an otherwise-green run: a failing suite is the more
	// important signal, and a teardown still in flight could leave transient
	// goroutines that would muddy the report.
	if code == 0 {
		if report, n := leaked(); n > 0 {
			fmt.Fprintf(os.Stderr,
				"\nleakcheck: %d leaked goroutine(s) detected by the Go 1.26 goroutineleak profile:\n%s\n",
				n, report)

			code = 1
		}
	}

	os.Exit(code)
}

// leaked captures the goroutineleak profile and returns its human-readable dump
// together with the number of leaked goroutines. It returns ("", 0) when the
// experiment is disabled (the profile is absent).
func leaked() (string, int) {
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		return "", 0
	}
	// The profile is derived from GC reachability information, and WriteTo (not
	// Count) is what actually computes it; force a GC so the freshest set of
	// unreachable goroutines is considered, capture the dump, then read the
	// count it produced.
	runtime.GC()

	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return "", 0
	}

	return buf.String(), p.Count()
}

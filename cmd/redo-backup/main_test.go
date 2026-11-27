// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/backup"
)

func TestParseInterspersed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args       []string
		maxAge     string
		positional []string
	}{
		{args: []string{"nightly", "--max-age", "20d"}, maxAge: "20d", positional: []string{"nightly"}},
		{args: []string{"--max-age", "20d", "nightly"}, maxAge: "20d", positional: []string{"nightly"}},
		{args: []string{"--max-age=2w", "a", "b"}, maxAge: "2w", positional: []string{"a", "b"}},
		{args: []string{"a", "--", "--max-age", "1d"}, maxAge: "", positional: []string{"a", "--max-age", "1d"}},
		{args: nil, maxAge: "", positional: nil},
	}

	for _, tc := range cases {
		fs := flag.NewFlagSet("check", flag.ContinueOnError)
		maxAge := fs.String("max-age", "", "")

		positional, err := parseInterspersed(fs, tc.args)
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)

			continue
		}

		if *maxAge != tc.maxAge || !slices.Equal(positional, tc.positional) {
			t.Errorf("%v: max-age %q, positional %q; want %q, %q", tc.args, *maxAge, positional, tc.maxAge, tc.positional)
		}
	}
}

func TestParseInterspersedUnknownFlag(t *testing.T) {
	t.Parallel()

	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})

	if _, err := parseInterspersed(fs, []string{"nightly", "--bogus"}); err == nil {
		t.Fatal("expected an error for an unknown flag after the profile")
	}
}

type unknownError struct{}

func (unknownError) Error() string { return "findmnt failed" }

func TestCheckStatus(t *testing.T) {
	t.Parallel()

	unavailable := fmt.Errorf("wrapped: %w", backup.ErrDestUnavailable)

	cases := map[string]struct {
		report *backup.CheckReport
		err    error
		want   int
	}{
		"fresh":       {report: &backup.CheckReport{Fresh: true}, err: nil, want: checkFresh},
		"too old":     {report: &backup.CheckReport{Fresh: false}, err: nil, want: checkStale},
		"none":        {report: &backup.CheckReport{}, err: nil, want: checkStale},
		"unavailable": {report: nil, err: unavailable, want: checkUnavailable},
		"other error": {report: nil, err: unknownError{}, want: checkError},
	}

	for name, tc := range cases {
		if got := checkStatus(tc.report, tc.err); got != tc.want {
			t.Errorf("%s: checkStatus = %d, want %d", name, got, tc.want)
		}
	}
}

func TestPrintCheck(t *testing.T) {
	t.Parallel()

	taken := time.Date(2026, time.January, 5, 18, 15, 11, 0, time.UTC)
	maxAge := 20 * 24 * time.Hour

	cases := map[string]struct {
		report *backup.CheckReport
		want   string
	}{
		"fresh": {
			report: &backup.CheckReport{
				Dest: "/mnt/backup", Newest: &backup.Set{ID: "nightly", Taken: taken},
				Age: 3*24*time.Hour + 2*time.Hour, Fresh: true,
			},
			want: "fresh: backup nightly, taken Mon, 05 Jan 2026 18:15:11 +0000, is 3d2h old (max 20d)\n",
		},
		"too old, with incomplete sets": {
			report: &backup.CheckReport{
				Dest: "/mnt/backup", Newest: &backup.Set{ID: "nightly", Taken: taken},
				Age: 30 * 24 * time.Hour, Fresh: false,
				Incomplete: []backup.IncompleteSet{{ID: "a", Reason: "x"}, {ID: "b", Reason: "y"}},
			},
			want: "too old: backup nightly, taken Mon, 05 Jan 2026 18:15:11 +0000, is 30d old (max 20d)\n" +
				"ignored 2 set(s) in /mnt/backup not known to be complete\n",
		},
		"none": {
			report: &backup.CheckReport{Dest: "/mnt/backup"},
			want:   "none: no complete backup in /mnt/backup\n",
		},
	}

	for name, tc := range cases {
		var out bytes.Buffer

		printCheck(&out, tc.report, maxAge)

		if got := out.String(); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", name, got, tc.want)
		}
	}
}

func TestCmdCheckUsageErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.conf"), []byte("dest = /mnt/backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Each of these fails before the destination is looked at, so no command
	// runs; and each exits with 3, never with 1, which would offer a backup.
	cases := map[string]struct {
		args []string
		want string
	}{
		"no profile":      {args: []string{"--config-dir", dir, "--max-age", "20d"}, want: "exactly one argument"},
		"two profiles":    {args: []string{"--config-dir", dir, "p", "q", "--max-age", "20d"}, want: "exactly one argument"},
		"no max age":      {args: []string{"--config-dir", dir, "p"}, want: "requires --max-age"},
		"bad max age":     {args: []string{"--config-dir", dir, "p", "--max-age", "20"}, want: "invalid age"},
		"unknown flag":    {args: []string{"--config-dir", dir, "p", "--bogus"}, want: "-bogus"},
		"unknown profile": {args: []string{"--config-dir", dir, "nope", "--max-age", "20d"}, want: "not found"},
	}

	for name, tc := range cases {
		stdout, stderr := tempFile(t), tempFile(t)

		if got := dispatch(append([]string{"check"}, tc.args...), stdout, stderr); got != checkError {
			t.Errorf("%s: exit %d, want %d", name, got, checkError)
		}

		if msg := readAll(t, stderr); !strings.Contains(msg, tc.want) {
			t.Errorf("%s: stderr %q, want it to mention %q", name, msg, tc.want)
		}
	}
}

// tempFile returns a file that stands in for stdout or stderr.
func tempFile(t *testing.T) *os.File {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = f.Close() })

	return f
}

// readAll returns what was written to f.
func readAll(t *testing.T, f *os.File) string {
	t.Helper()

	data, err := os.ReadFile(f.Name())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	return string(data)
}

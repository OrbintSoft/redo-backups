// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// taken is when the backups made by these tests are taken.
var taken = time.Date(2026, time.January, 5, 18, 15, 11, 0, time.UTC)

// backupAt runs a backup with the given id into dest at time at.
func backupAt(t *testing.T, f *run.FakeRunner, dest, id string, at time.Time) {
	t.Helper()

	cfg := baseConfig(dest)
	cfg.ID = id
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: func() time.Time { return at }, LogDir: t.TempDir()}

	if _, err := b.Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run %s: %v", id, err)
	}
}

// checkAt runs Check on dest at time at, with a maximum age of 20 days.
func checkAt(t *testing.T, f *run.FakeRunner, dest string, at time.Time) (*CheckReport, error) {
	t.Helper()

	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: func() time.Time { return at }, LogDir: ""}

	return b.Check(context.Background(), baseConfig(dest), 20*24*time.Hour)
}

func TestCheckAge(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		after time.Duration
		fresh bool
	}{
		"fresh":                   {after: 3 * 24 * time.Hour, fresh: true},
		"just under the age":      {after: 20*24*time.Hour - time.Minute, fresh: true},
		"exactly the age":         {after: 20 * 24 * time.Hour, fresh: false},
		"too old":                 {after: 30 * 24 * time.Hour, fresh: false},
		"timestamp still to come": {after: -time.Hour, fresh: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := fakeRunner()
			dest := onBackupDisk(f, t.TempDir())
			backupAt(t, f, dest, "nightly", taken)

			report, err := checkAt(t, f, dest, taken.Add(tc.after))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}

			if report.Newest == nil || report.Newest.ID != "nightly" || !report.Newest.Taken.Equal(taken) {
				t.Fatalf("newest = %+v, want nightly taken %v", report.Newest, taken)
			}

			if report.Age != tc.after || report.Fresh != tc.fresh {
				t.Errorf("age %v fresh %v, want %v %v", report.Age, report.Fresh, tc.after, tc.fresh)
			}
		})
	}
}

func TestCheckPicksNewestByTimestamp(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := onBackupDisk(f, t.TempDir())

	// The ids sort the other way round, and the older set is written last, so
	// neither the name nor the modification time tells the newest.
	backupAt(t, f, dest, "a-newer", taken.Add(24*time.Hour))
	backupAt(t, f, dest, "b-older", taken)

	report, err := checkAt(t, f, dest, taken.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if report.Newest == nil || report.Newest.ID != "a-newer" || report.Age != 24*time.Hour {
		t.Errorf("newest = %+v, age %v; want a-newer, 24h", report.Newest, report.Age)
	}
}

func TestCheckNoBackup(t *testing.T) {
	t.Parallel()

	for name, sub := range map[string]string{"empty destination": "", "missing destination": "host"} {
		f := fakeRunner()
		mountPoint := onBackupDisk(f, t.TempDir())
		dest := filepath.Join(mountPoint, sub)

		report, err := checkAt(t, f, dest, taken)
		if err != nil {
			t.Fatalf("%s: Check: %v", name, err)
		}

		if report.Newest != nil || report.Fresh || len(report.Incomplete) != 0 {
			t.Errorf("%s: report = %+v, want no backup", name, report)
		}

		// Check only reads: a missing destination stays missing.
		if _, err := os.Stat(dest); sub != "" && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: Check created the destination", name)
		}

		if len(f.Pipelines) != 0 {
			t.Errorf("%s: Check imaged %d partition(s)", name, len(f.Pipelines))
		}
	}
}

// brokenSet damages set "s" in a destination, and names the reason Check
// should give for not counting it.
type brokenSet struct {
	breakSet func(dest string) error
	reason   string
}

// brokenSets lists the ways TestCheckIgnoresIncompleteSets damages a set.
func brokenSets() map[string]brokenSet {
	write := func(path, data string) error { return os.WriteFile(path, []byte(data), 0o600) }

	return map[string]brokenSet{
		"no record": {
			breakSet: func(dest string) error { return os.Remove(filepath.Join(dest, "s.complete")) },
			reason:   "no completion record",
		},
		"unreadable record": {
			breakSet: func(dest string) error { return write(filepath.Join(dest, "s.complete"), "{") },
			reason:   "unreadable completion record",
		},
		"record of another set": {
			breakSet: func(dest string) error {
				return write(filepath.Join(dest, "s.complete"), `{"version":1,"id":"t","files":[]}`)
			},
			reason: "another set",
		},
		"chunk missing": {
			breakSet: func(dest string) error { return os.Remove(filepath.Join(dest, "s_sda2_001.img")) },
			reason:   "s_sda2_001.img missing",
		},
		"chunk truncated": {
			breakSet: func(dest string) error { return write(filepath.Join(dest, "s_sda1_001.img"), "new") },
			reason:   "s_sda1_001.img is 3 bytes",
		},
		"descriptor replaced": {
			breakSet: func(dest string) error { return write(filepath.Join(dest, "s.redo"), "{}") },
			reason:   "s.redo is 2 bytes",
		},
		"record without the descriptor": {
			breakSet: func(dest string) error {
				return write(filepath.Join(dest, "s.complete"),
					`{"version":1,"id":"s","files":[{"name":"s_sda1_001.img","bytes":9}]}`)
			},
			reason: "does not list the descriptor",
		},
	}
}

func TestCheckIgnoresIncompleteSets(t *testing.T) {
	t.Parallel()

	cases := brokenSets()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := fakeRunner()
			dest := onBackupDisk(f, t.TempDir())

			// An older complete set must still be found behind the newer, broken one.
			backupAt(t, f, dest, "r", taken)
			backupAt(t, f, dest, "s", taken.Add(24*time.Hour))

			if err := tc.breakSet(dest); err != nil {
				t.Fatal(err)
			}

			report, err := checkAt(t, f, dest, taken.Add(48*time.Hour))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}

			if report.Newest == nil || report.Newest.ID != "r" {
				t.Errorf("newest = %+v, want r", report.Newest)
			}

			if len(report.Incomplete) != 1 || report.Incomplete[0].ID != "s" ||
				!strings.Contains(report.Incomplete[0].Reason, tc.reason) {
				t.Errorf("incomplete = %+v, want s: %q", report.Incomplete, tc.reason)
			}
		})
	}
}

func TestCheckRefusesUnavailableDest(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := t.TempDir()
	destOn(f, dest, "/", "/dev/sda2", "sda2", "sda")

	if _, err := checkAt(t, f, dest, taken); !errors.Is(err, ErrDestUnavailable) {
		t.Fatalf("err = %v, want ErrDestUnavailable", err)
	}
}

func TestCheckUnreadableSetIsAnError(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}

	f := fakeRunner()
	dest := onBackupDisk(f, t.TempDir())
	backupAt(t, f, dest, "s", taken)

	if err := os.Chmod(filepath.Join(dest, "s.complete"), 0); err != nil {
		t.Fatal(err)
	}

	// A set that cannot be read tells nothing about whether it is complete, so
	// the check fails rather than reporting no backup.
	report, err := checkAt(t, f, dest, taken)
	if err == nil || errors.Is(err, errIncomplete) {
		t.Fatalf("report %+v, err %v; want a read error", report, err)
	}
}

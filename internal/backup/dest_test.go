// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// destOn programs f so that path lies on a filesystem mounted on target from
// source, with lsblk listing devices beneath source (nothing for a source that
// is not a block device).
func destOn(f *run.FakeRunner, path, target, source string, devices ...string) {
	f.AddStdout("findmnt -J -o TARGET,SOURCE,FSTYPE --nofsroot --target "+path,
		`{"filesystems": [{"target": "`+target+`", "source": "`+source+`", "fstype": "ext4"}]}`)

	if len(devices) > 0 {
		f.AddStdout("lsblk -l -n -s -o KNAME -- "+source, strings.Join(devices, "\n")+"\n")
	}
}

// onBackupDisk programs f so that dest lies on a backup drive (sdb) separate
// from the drive being backed up (sda), the normal case, and returns dest.
func onBackupDisk(f *run.FakeRunner, dest string) string {
	destOn(f, dest, "/mnt/backup", "/dev/sdb1", "sdb1", "sdb")

	return dest
}

func TestBackupRefusesDestOnImagedDrive(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source  string
		devices []string
	}{
		// The backup disk is not mounted: the mount point is a plain directory
		// of the root filesystem, on the drive being backed up.
		"root partition": {source: "/dev/sda2", devices: []string{"sda2", "sda"}},
		"lvm on a partition": {
			source: "/dev/mapper/vg0-root", devices: []string{"dm-0", "sda3", "sda"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, op := range []string{"run", "plan"} {
				f := fakeRunner()
				mountPoint := t.TempDir()
				destOn(f, mountPoint, "/", tc.source, tc.devices...)

				dest := filepath.Join(mountPoint, "host")
				b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

				var err error
				if op == "run" {
					_, err = b.Run(context.Background(), baseConfig(dest))
				} else {
					_, err = b.Plan(context.Background(), baseConfig(dest))
				}

				if !errors.Is(err, ErrDestUnavailable) {
					t.Fatalf("%s: err = %v, want ErrDestUnavailable", op, err)
				}

				if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s: destination %s created on the drive being backed up", op, dest)
				}

				assertNothingWritten(t, f, dest)
			}
		})
	}
}

func TestBackupDestMount(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		destMount string
		target    string
		wantErr   bool
	}{
		"mounted":                    {destMount: "/mnt/backup", target: "/mnt/backup", wantErr: false},
		"mounted, trailing slash":    {destMount: "/mnt/backup/", target: "/mnt/backup", wantErr: false},
		"not mounted":                {destMount: "/mnt/backup", target: "/", wantErr: true},
		"another filesystem beneath": {destMount: "/mnt/backup", target: "/mnt/backup/other", wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, op := range []string{"run", "plan"} {
				// The filesystem is on sdc, neither the drive being backed up
				// (sda) nor anything the drive check refuses: only dest_mount
				// tells a root filesystem on another drive from the backup disk.
				f := fakeRunner()
				dest := t.TempDir()
				destOn(f, dest, tc.target, "/dev/sdc2", "sdc2", "sdc")

				cfg := baseConfig(dest)
				cfg.DestMount = tc.destMount
				b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

				var err error
				if op == "run" {
					_, err = b.Run(context.Background(), cfg)
				} else {
					_, err = b.Plan(context.Background(), cfg)
				}

				if !tc.wantErr {
					if err != nil {
						t.Fatalf("%s: %v", op, err)
					}

					continue
				}

				if !errors.Is(err, ErrDestUnavailable) {
					t.Fatalf("%s: err = %v, want ErrDestUnavailable", op, err)
				}

				assertNothingWritten(t, f, dest)
			}
		})
	}
}

func TestBackupRunCreatesMissingDestOnBackupDisk(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	mountPoint := onBackupDisk(f, t.TempDir())
	dest := filepath.Join(mountPoint, "backups", "host")
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	// The missing directories are checked through their nearest existing
	// ancestor, the mount point, and created once it passes.
	rep, err := b.Run(context.Background(), baseConfig(dest))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(rep.DescriptorPath); err != nil {
		t.Errorf("descriptor not written in the created destination: %v", err)
	}
}

func TestBackupRunAcceptsDestOffBlockDevice(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := t.TempDir()
	destOn(f, dest, "/mnt/nas", "nas:/export/backups")
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	// A network share is not stored on any local drive, so it cannot be on the
	// drive being backed up.
	if _, err := b.Run(context.Background(), baseConfig(dest)); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestBackupRunDestCheckFailure(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := t.TempDir()
	f.Responses["findmnt -J -o TARGET,SOURCE,FSTYPE --nofsroot --target "+dest] = run.FakeResponse{
		Result: run.Result{Stdout: nil, Stderr: nil, ExitCode: 1},
		Err:    errBoom,
	}
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	// A destination that cannot be checked is not imaged to, but neither is it
	// reported as unavailable: the check failed, not the destination.
	_, err := b.Run(context.Background(), baseConfig(dest))
	if !errors.Is(err, errBoom) || errors.Is(err, ErrDestUnavailable) {
		t.Fatalf("err = %v, want the findmnt failure only", err)
	}

	assertNothingWritten(t, f, dest)
}

func TestNearestExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for path, want := range map[string]string{
		dir:                                dir,
		filepath.Join(dir, "a"):            dir,
		filepath.Join(dir, "a", "b", "c"):  dir,
		string(filepath.Separator):         string(filepath.Separator),
		filepath.Join(dir, "a", "..", "b"): dir,
	} {
		got, err := nearestExisting(filepath.Clean(path))
		if err != nil {
			t.Errorf("nearestExisting(%q): %v", path, err)

			continue
		}

		if got != want {
			t.Errorf("nearestExisting(%q) = %q, want %q", path, got, want)
		}
	}
}

// assertNothingWritten fails the test if a run reached past the destination
// check: the destination created, the MBR read, or any partition imaged.
func assertNothingWritten(t *testing.T, f *run.FakeRunner, dest string) {
	t.Helper()

	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		if entries, _ := os.ReadDir(dest); len(entries) > 0 {
			t.Errorf("files written to %s: %v", dest, entries)
		}
	}

	if slices.Contains(f.CommandLines(), "dd if=/dev/sda bs=32k count=1") {
		t.Error("MBR read despite the refused destination")
	}

	if len(f.Pipelines) != 0 {
		t.Errorf("%d partition(s) imaged despite the refused destination", len(f.Pipelines))
	}
}

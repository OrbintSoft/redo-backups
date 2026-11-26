// SPDX-License-Identifier: EUPL-1.2

package disk

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/run"
)

// findmntKey is the command line MountOf runs for path.
func findmntKey(path string) string {
	return "findmnt -J -o TARGET,SOURCE,FSTYPE --nofsroot --target " + path
}

// findmntJSON is findmnt's JSON output for a single filesystem.
func findmntJSON(target, source, fstype string) string {
	return `{"filesystems": [{"target": "` + target + `", "source": "` + source +
		`", "fstype": "` + fstype + `"}]}`
}

// lsblkInverseKey is the command line devicesBeneath runs for devicePath.
func lsblkInverseKey(devicePath string) string {
	return "lsblk -l -n -s -o KNAME -- " + devicePath
}

func TestMountOfBlockDevices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		source  string
		lsblk   string
		devices []string
	}{
		{
			name:    "partition",
			source:  "/dev/sdb1",
			lsblk:   "sdb1\nsdb\n",
			devices: []string{"sdb1", "sdb"},
		},
		{
			name:    "lvm on a partition",
			source:  "/dev/mapper/vg0-data",
			lsblk:   "dm-0\nsda3\nsda\n",
			devices: []string{"dm-0", "sda3", "sda"},
		},
		{
			name:    "raid across two drives",
			source:  "/dev/md0",
			lsblk:   "md0\nsda1\nsda\nsdb1\nsdb\n",
			devices: []string{"md0", "sda1", "sda", "sdb1", "sdb"},
		},
		{
			// An LV spanning two partitions of one drive reaches it twice.
			name:    "drive reached twice",
			source:  "/dev/mapper/vg0-data",
			lsblk:   "dm-0\nsda3\nsda\nsda4\nsda\n",
			devices: []string{"dm-0", "sda3", "sda", "sda4"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := run.NewFakeRunner()
			f.AddStdout(findmntKey("/mnt/backup/host"), findmntJSON("/mnt/backup", tc.source, "ext4"))
			f.AddStdout(lsblkInverseKey(tc.source), tc.lsblk)

			mount, err := New(f).MountOf(context.Background(), "/mnt/backup/host")
			if err != nil {
				t.Fatalf("MountOf: %v", err)
			}

			if mount.Target != "/mnt/backup" || mount.Source != tc.source || mount.FSType != "ext4" {
				t.Errorf("mount = %+v", mount)
			}

			if !slices.Equal(mount.Devices, tc.devices) {
				t.Errorf("devices = %v, want %v", mount.Devices, tc.devices)
			}
		})
	}
}

func TestMountOfNonBlockSource(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ source, fstype string }{
		{"tmpfs", "tmpfs"},
		{"nas:/export/backups", "nfs4"},
		{"pool/backups", "zfs"},
	} {
		f := run.NewFakeRunner().AddStdout(findmntKey("/mnt/nas"), findmntJSON("/mnt/nas", tc.source, tc.fstype))

		mount, err := New(f).MountOf(context.Background(), "/mnt/nas")
		if err != nil {
			t.Fatalf("MountOf with source %q: %v", tc.source, err)
		}

		if mount.Source != tc.source || len(mount.Devices) != 0 {
			t.Errorf("mount = %+v, want source %q and no devices", mount, tc.source)
		}

		// A source that is not a block device has nothing beneath it to list.
		if got := f.CommandLines(); len(got) != 1 {
			t.Errorf("commands = %v, want findmnt only", got)
		}
	}
}

func TestMountOfRejectsRelativePath(t *testing.T) {
	t.Parallel()

	f := run.NewFakeRunner()

	_, err := New(f).MountOf(context.Background(), "backups")
	if !errors.Is(err, errRelativePath) {
		t.Fatalf("err = %v, want errRelativePath", err)
	}

	if len(f.Calls) != 0 {
		t.Errorf("ran %v for a relative path", f.CommandLines())
	}
}

type findmntError struct{}

func (findmntError) Error() string { return "exit status 1" }

var errFindmnt = findmntError{}

func TestMountOfFindmntFailure(t *testing.T) {
	t.Parallel()

	// findmnt fails when the path does not exist.
	f := run.NewFakeRunner()
	f.Responses[findmntKey("/mnt/missing")] = run.FakeResponse{
		Result: run.Result{Stdout: nil, Stderr: nil, ExitCode: 1},
		Err:    errFindmnt,
	}

	if _, err := New(f).MountOf(context.Background(), "/mnt/missing"); !errors.Is(err, errFindmnt) {
		t.Fatalf("err = %v, want the findmnt failure", err)
	}
}

func TestMountOfNoFilesystem(t *testing.T) {
	t.Parallel()

	f := run.NewFakeRunner().AddStdout(findmntKey("/mnt/backup"), `{"filesystems": []}`)

	_, err := New(f).MountOf(context.Background(), "/mnt/backup")
	if !errors.Is(err, errNoFilesystem) {
		t.Fatalf("err = %v, want errNoFilesystem", err)
	}
}

func TestMountOfRejectsBadLsblkInput(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source string
		lsblk  string
	}{
		"source escaping /dev":     {source: "/dev/../etc/passwd", lsblk: ""},
		"no devices listed":        {source: "/dev/sdb1", lsblk: "\n"},
		"unsafe device name given": {source: "/dev/sdb1", lsblk: "sdb1\n../sdb\n"},
	}

	for name, tc := range cases {
		f := run.NewFakeRunner()
		f.AddStdout(findmntKey("/mnt/backup"), findmntJSON("/mnt/backup", tc.source, "ext4"))
		f.AddStdout(lsblkInverseKey(tc.source), tc.lsblk)

		if _, err := New(f).MountOf(context.Background(), "/mnt/backup"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestMountStoredOn(t *testing.T) {
	t.Parallel()

	mount := &Mount{Target: "/", Source: "/dev/mapper/vg0-root", FSType: "ext4", Devices: []string{"dm-0", "sda3", "sda"}}

	for _, dev := range []string{"sda", "sda3", "dm-0"} {
		if !mount.StoredOn(dev) {
			t.Errorf("StoredOn(%q) = false, want true", dev)
		}
	}

	for _, dev := range []string{"sdb", "sd", ""} {
		if mount.StoredOn(dev) {
			t.Errorf("StoredOn(%q) = true, want false", dev)
		}
	}
}

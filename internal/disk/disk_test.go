// SPDX-License-Identifier: EUPL-1.2

package disk

import (
	"bytes"
	"context"
	_ "embed"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/redo"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

//go:embed testdata/lsblk-sda.json
var sampleLsblk string

func TestFSTool(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ext4":     "extfs",
		"ext2":     "extfs",
		"vfat":     "fat",
		"fat32":    "fat",
		"exfat":    "exfat", // must win over the "fat" rule
		"ntfs":     "ntfs",
		"btrfs":    "btrfs",
		"xfs":      "xfs",
		"f2fs":     "f2fs",
		"hfsplus":  "hfsp",
		"nilfs2":   "nilfs2",
		"reiserfs": "reiser4",
		"minix":    "minix",
		"swap":     DDTool,
		"":         DDTool,
		"EXT4":     "extfs", // case-insensitive
	}
	for fs, want := range cases {
		if got := FSTool(fs); got != want {
			t.Errorf("FSTool(%q) = %q, want %q", fs, got, want)
		}
	}
}

// fakeWithDrive returns a FakeRunner programmed with a typical small drive.
func fakeWithDrive() *run.FakeRunner {
	f := run.NewFakeRunner()
	f.AddStdout("lsblk -J -o NAME,SIZE,FSTYPE,PARTTYPENAME,LABEL,MOUNTPOINT,TYPE -- /dev/sda", sampleLsblk)
	f.AddStdout("blockdev --getsize64 /dev/sda", "512110190592\n")
	f.AddStdout("blockdev --getsize64 /dev/sda1", "133169152\n")
	f.AddStdout("blockdev --getsize64 /dev/sda2", "299892736\n")

	return f
}

func TestDrive(t *testing.T) {
	t.Parallel()

	insp := New(fakeWithDrive())

	d, err := insp.Drive(context.Background(), "sda")
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}

	if d.Name != "sda" || d.Bytes != 512110190592 {
		t.Errorf("drive = %+v", d)
	}

	if len(d.Partitions) != 2 {
		t.Fatalf("got %d partitions, want 2", len(d.Partitions))
	}

	want := []Partition{
		{Name: "sda1", Bytes: 133169152, Size: "127M", Type: "EFI System", FS: "vfat", Label: "ESP"},
		{Name: "sda2", Bytes: 299892736, Size: "286M", Type: "Linux filesystem", FS: "ext4", Label: "boot"},
	}
	for i, w := range want {
		if d.Partitions[i] != w {
			t.Errorf("partition[%d] = %+v, want %+v", i, d.Partitions[i], w)
		}
	}
}

func TestDriveInvalidName(t *testing.T) {
	t.Parallel()

	insp := New(run.NewFakeRunner())
	if _, err := insp.Drive(context.Background(), "../sda"); err == nil {
		t.Fatal("expected error for invalid device name")
	}
}

func TestMBR(t *testing.T) {
	t.Parallel()

	f := run.NewFakeRunner()
	mbr := bytes.Repeat([]byte{0xAB}, redo.MBRSize)
	f.Responses["dd if=/dev/sda bs=32k count=1"] = run.FakeResponse{Result: run.Result{Stdout: mbr}}

	got, err := New(f).MBR(context.Background(), "sda")
	if err != nil {
		t.Fatalf("MBR: %v", err)
	}

	if !bytes.Equal(got, mbr) {
		t.Errorf("MBR content mismatch")
	}
}

func TestMBRWrongSize(t *testing.T) {
	t.Parallel()

	f := run.NewFakeRunner()

	f.Responses["dd if=/dev/sda bs=32k count=1"] = run.FakeResponse{Result: run.Result{Stdout: []byte("short")}}
	if _, err := New(f).MBR(context.Background(), "sda"); err == nil {
		t.Fatal("expected error for short MBR read")
	}
}

func TestPartitionTable(t *testing.T) {
	t.Parallel()

	dump := "label: gpt\ndevice: /dev/sda\n"
	f := run.NewFakeRunner().AddStdout("sfdisk --dump /dev/sda", dump)

	got, err := New(f).PartitionTable(context.Background(), "sda")
	if err != nil {
		t.Fatalf("PartitionTable: %v", err)
	}

	if string(got) != dump {
		t.Errorf("got %q, want %q", got, dump)
	}
}

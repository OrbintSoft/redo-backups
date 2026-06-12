// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
)

func cmdStrings(p Pipeline) []string {
	out := make([]string, len(p.Stages))
	for i, s := range p.Stages {
		out[i] = s.String()
	}

	return out
}

func TestPartitionPipelineExt4(t *testing.T) {
	t.Parallel()

	part := disk.Partition{Name: "sda2", FS: "ext4"}
	p := PartitionPipeline(part, "/dev/sda2", string(config.CompressorPigz), "4096M", "/tmp/sda2.log", "/dest", "20260105")

	want := []string{
		"partclone.extfs --clone --force --UI-fresh 1 --logfile /tmp/sda2.log --source /dev/sda2 --no_block_detail",
		"pigz --stdout",
		"split --numeric-suffixes=1 --suffix-length=3 --additional-suffix=.img --bytes=4096M - /dest/20260105_sda2_",
	}
	if got := cmdStrings(p); !reflect.DeepEqual(got, want) {
		t.Errorf("pipeline mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestPartitionPipelineDDFallback(t *testing.T) {
	t.Parallel()

	// An unknown filesystem must use partclone.dd WITHOUT the --clone flag.
	part := disk.Partition{Name: "sda3", FS: "swap"}
	p := PartitionPipeline(part, "/dev/sda3", string(config.CompressorGzip), "2G", "/tmp/sda3.log", "/dest", "id1")

	want := []string{
		"partclone.dd --force --UI-fresh 1 --logfile /tmp/sda3.log --source /dev/sda3 --no_block_detail",
		"gzip --stdout",
		"split --numeric-suffixes=1 --suffix-length=3 --additional-suffix=.img --bytes=2G - /dest/id1_sda3_",
	}
	if got := cmdStrings(p); !reflect.DeepEqual(got, want) {
		t.Errorf("pipeline mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func sampleDrive() *disk.Drive {
	return &disk.Drive{
		Name:  "sda",
		Bytes: 512110190592,
		Partitions: []disk.Partition{
			{Name: "sda1", Bytes: 133169152, Size: "127M", Type: "EFI System", FS: "vfat", Label: "ESP"},
			{Name: "sda2", Bytes: 299892736, Size: "286M", Type: "Linux filesystem", FS: "ext4", Label: "boot"},
		},
	}
}

func TestSelectPartitionsAuto(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{} // PartsAuto

	got, err := SelectPartitions(cfg, sampleDrive())
	if err != nil {
		t.Fatalf("SelectPartitions: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("got %d, want 2", len(got))
	}
}

func TestSelectPartitionsExplicit(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Parts: []string{"sda2"}}

	got, err := SelectPartitions(cfg, sampleDrive())
	if err != nil {
		t.Fatalf("SelectPartitions: %v", err)
	}

	if len(got) != 1 || got[0].Name != "sda2" {
		t.Errorf("got %+v", got)
	}
}

func TestSelectPartitionsMissing(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Parts: []string{"sda9"}}
	if _, err := SelectPartitions(cfg, sampleDrive()); err == nil {
		t.Fatal("expected error for missing partition")
	}
}

func TestBuildImage(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Version: "4.0.0", Notes: "test"}
	drive := sampleDrive()
	img := BuildImage(cfg, drive, drive.Partitions, "20260105", "Mon, 05 Jan 2026 18:15:11 +0000", []byte("MBR"), []byte("sfd"))

	if img.ID != "20260105" || img.DriveBytes != 512110190592 || img.Notes != "test" {
		t.Errorf("descriptor header wrong: %+v", img)
	}

	if got := img.Parts.Keys(); !reflect.DeepEqual(got, []string{"sda1", "sda2"}) {
		t.Errorf("parts order = %v", got)
	}

	p1, ok := img.Parts.Get("sda1")
	if !ok || p1.FS != "vfat" || p1.Desc != "ESP" {
		t.Errorf("sda1 = %+v ok=%v", p1, ok)
	}
}

func TestWriteDescriptor(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Version: "4.0.0"}
	drive := sampleDrive()
	img := BuildImage(cfg, drive, drive.Partitions, "20260105", "ts", []byte("MBR"), []byte("sfd"))

	dir := t.TempDir()

	path, err := WriteDescriptor(dir, img)
	if err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}

	if path != filepath.Join(dir, "20260105.redo") {
		t.Errorf("path = %q", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	want, _ := img.Marshal()
	if string(data) != string(want) {
		t.Errorf("written content mismatch")
	}
}

func TestFormatHelpers(t *testing.T) {
	t.Parallel()

	tm := time.Date(2026, time.January, 5, 18, 15, 11, 0, time.UTC)
	if got := FormatID(tm); got != "20260105" {
		t.Errorf("FormatID = %q", got)
	}

	if got := FormatTimestamp(tm); got != "Mon, 05 Jan 2026 18:15:11 +0000" {
		t.Errorf("FormatTimestamp = %q", got)
	}
}

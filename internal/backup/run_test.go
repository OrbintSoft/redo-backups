// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"bytes"
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/redo"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

//go:embed testdata/lsblk-sda.json
var lsblkSDA string

func fakeRunner() *run.FakeRunner {
	f := run.NewFakeRunner()
	f.AddStdout("lsblk -J -o NAME,SIZE,FSTYPE,PARTTYPENAME,LABEL,MOUNTPOINT,TYPE -- /dev/sda", lsblkSDA)
	f.AddStdout("blockdev --getsize64 /dev/sda", "512110190592\n")
	f.AddStdout("blockdev --getsize64 /dev/sda1", "133169152\n")
	f.AddStdout("blockdev --getsize64 /dev/sda2", "299892736\n")
	f.Responses["dd if=/dev/sda bs=32k count=1"] = run.FakeResponse{
		Result: run.Result{Stdout: bytes.Repeat([]byte{0}, redo.MBRSize)},
	}
	f.AddStdout("sfdisk --dump /dev/sda", "label: gpt\ndevice: /dev/sda\n")

	return f
}

func baseConfig(dest string) *config.Config {
	return &config.Config{
		Dest:        dest,
		Drive:       "sda",
		Version:     "4.0.0",
		Compressor:  config.CompressorPigz,
		SplitSize:   "4096M",
		Consistency: config.ConsistencyNone,
		Notes:       "test run",
	}
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, time.January, 5, 18, 15, 11, 0, time.UTC) }
}

func TestBackupRun(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := t.TempDir()
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	rep, err := b.Run(context.Background(), baseConfig(dest))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if rep.ID != "20260105" || rep.Drive != "sda" {
		t.Errorf("report = %+v", rep)
	}

	if len(rep.Partitions) != 2 {
		t.Errorf("partitions = %v", rep.Partitions)
	}

	// Descriptor written to dest/<id>.redo.
	descPath := filepath.Join(dest, "20260105.redo")
	if rep.DescriptorPath != descPath {
		t.Errorf("DescriptorPath = %q", rep.DescriptorPath)
	}

	data, err := os.ReadFile(descPath)
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}

	if !bytes.Contains(data, []byte(`"id":"20260105"`)) || !bytes.Contains(data, []byte(`"notes":"test run"`)) {
		t.Errorf("descriptor content unexpected: %s", data)
	}

	// Two imaging pipelines recorded, in drive order.
	if len(f.Pipelines) != 2 {
		t.Fatalf("got %d pipelines, want 2", len(f.Pipelines))
	}

	first := f.Pipelines[0]
	if len(first) != 3 || first[0].Name != "partclone.fat" {
		t.Errorf("first pipeline = %v", first)
	}

	wantSplit := "split --numeric-suffixes=1 --suffix-length=3 --additional-suffix=.img --bytes=4096M - " +
		filepath.Join(dest, "20260105_sda1_")
	if first[2].String() != wantSplit {
		t.Errorf("split stage = %q, want %q", first[2].String(), wantSplit)
	}
}

func TestBackupRunAutoDrive(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	f.AddStdout("findmnt -n -o SOURCE /", "/dev/sda2\n")
	f.AddStdout("lsblk -n -o PKNAME /dev/sda2", "sda\n")

	cfg := baseConfig(t.TempDir())
	cfg.Drive = "auto"
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	rep, err := b.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if rep.Drive != "sda" {
		t.Errorf("auto-detected drive = %q, want sda", rep.Drive)
	}
}

func TestBackupPlan(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := t.TempDir()
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	plan, err := b.Plan(context.Background(), baseConfig(dest))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if plan.Drive != "sda" || plan.ID != "20260105" {
		t.Errorf("plan = %+v", plan)
	}

	if len(plan.Partitions) != 2 || plan.Partitions[0].Name != "sda1" {
		t.Errorf("partitions = %+v", plan.Partitions)
	}

	if len(plan.Partitions[0].Commands) != 3 || plan.Partitions[0].Source != "/dev/sda1" {
		t.Errorf("commands/source = %+v", plan.Partitions[0])
	}

	// Plan must not write the descriptor or run any imaging pipeline.
	if _, err := os.Stat(filepath.Join(dest, "20260105.redo")); !os.IsNotExist(err) {
		t.Errorf("Plan wrote a descriptor; it must not")
	}

	if len(f.Pipelines) != 0 {
		t.Errorf("Plan executed %d pipelines; it must not run any", len(f.Pipelines))
	}
}

func TestBackupRunConsistencyUnsupported(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	cfg := baseConfig(t.TempDir())
	cfg.Consistency = "btrfs-snapshot" // removed/unknown strategy
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	if _, err := b.Run(context.Background(), cfg); err == nil {
		t.Fatal("expected error for unimplemented consistency strategy")
	}
}

func TestBackupRunPipelineFailure(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	f.PipelineErr = errBoom
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	if _, err := b.Run(context.Background(), baseConfig(t.TempDir())); err == nil {
		t.Fatal("expected error when a pipeline fails")
	}
}

type boomError struct{}

func (boomError) Error() string { return "boom" }

var errBoom = boomError{}

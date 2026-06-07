// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/run"
)

func TestLVMSnapshotPrepare(t *testing.T) {
	r := run.NewFakeRunner()
	r.AddStdout("lvs --noheadings -o vg_name,lv_name --separator / /dev/vg0-root", "  vg0/root\n")
	s := &LVMSnapshot{Runner: r, SnapshotSize: "10%ORIGIN"}

	p, err := s.Prepare(context.Background(), Target{Device: "vg0-root", Mountpoint: "/", FS: "ext4"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if p.Source != "/dev/vg0/root_redosnap" {
		t.Errorf("Source = %q, want /dev/vg0/root_redosnap", p.Source)
	}
	if err := p.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}

	want := []string{
		"lvs --noheadings -o vg_name,lv_name --separator / /dev/vg0-root",
		"lvcreate --snapshot --name root_redosnap --size 10%ORIGIN /dev/vg0/root",
		"lvremove --force /dev/vg0/root_redosnap",
	}
	got := r.CommandLines()
	if len(got) != len(want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLVMSnapshotNotAnLV(t *testing.T) {
	r := run.NewFakeRunner()
	// lvs not programmed -> returns empty stdout, no error in fake. Parsing the
	// empty output must fail.
	s := &LVMSnapshot{Runner: r, SnapshotSize: "10G"}
	if _, err := s.Prepare(context.Background(), Target{Device: "sda2"}); err == nil {
		t.Fatal("expected error when target is not a logical volume")
	}
}

func TestLVMSnapshotLvsError(t *testing.T) {
	r := run.NewFakeRunner()
	r.Responses["lvs --noheadings -o vg_name,lv_name --separator / /dev/sda2"] = run.FakeResponse{Err: errBoomLVM}
	s := &LVMSnapshot{Runner: r, SnapshotSize: "10G"}
	if _, err := s.Prepare(context.Background(), Target{Device: "sda2"}); err == nil {
		t.Fatal("expected error when lvs fails")
	}
}

type boomLVM struct{}

func (boomLVM) Error() string { return "boom" }

var errBoomLVM = boomLVM{}

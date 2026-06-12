// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/run"
)

// lsblk output for a PV partition (sda2) with two mounted LVs and one unmounted.
const lsblkPV = `{"blockdevices":[{"name":"sda2","mountpoint":null,"children":[
  {"name":"vg0-root","mountpoint":"/"},
  {"name":"vg0-home","mountpoint":"/home"},
  {"name":"vg0-swap","mountpoint":null}
]}]}`

func TestLVMFreezesSubtree(t *testing.T) {
	t.Parallel()

	r := run.NewFakeRunner()
	r.AddStdout("lsblk -J -o NAME,MOUNTPOINT /dev/sda2", lsblkPV)
	s := &LVM{Runner: r}

	p, err := s.Prepare(context.Background(), Target{Device: "sda2", FS: "LVM2_member"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if p.Source != "/dev/sda2" {
		t.Errorf("Source = %q, want /dev/sda2 (the PV partition, imaged raw)", p.Source)
	}

	if err := p.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}

	want := []string{
		"lsblk -J -o NAME,MOUNTPOINT /dev/sda2",
		"fsfreeze -f /",
		"fsfreeze -f /home",
		"fsfreeze -u /",
		"fsfreeze -u /home",
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

func TestLVMNoMounts(t *testing.T) {
	t.Parallel()

	r := run.NewFakeRunner()
	r.AddStdout("lsblk -J -o NAME,MOUNTPOINT /dev/sda2",
		`{"blockdevices":[{"name":"sda2","mountpoint":null,"children":[{"name":"vg0-swap","mountpoint":null}]}]}`)
	s := &LVM{Runner: r}

	p, err := s.Prepare(context.Background(), Target{Device: "sda2"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if err := p.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}
	// Only the lsblk call; nothing to freeze.
	if got := r.CommandLines(); len(got) != 1 {
		t.Errorf("commands = %v, want only the lsblk call", got)
	}
}

func TestLVMLsblkError(t *testing.T) {
	t.Parallel()

	r := run.NewFakeRunner()
	r.Responses["lsblk -J -o NAME,MOUNTPOINT /dev/sda2"] = run.FakeResponse{Err: errBoomLVM}

	s := &LVM{Runner: r}
	if _, err := s.Prepare(context.Background(), Target{Device: "sda2"}); err == nil {
		t.Fatal("expected error when lsblk fails")
	}
}

type boomLVMError struct{}

func (boomLVMError) Error() string { return "boom" }

var errBoomLVM = boomLVMError{}

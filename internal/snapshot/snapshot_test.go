// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

func TestForSelection(t *testing.T) {
	r := run.NewFakeRunner()
	if s, err := For(config.ConsistencyNone, r); err != nil || s.Name() != config.ConsistencyNone {
		t.Errorf("none: s=%v err=%v", s, err)
	}
	if s, err := For(config.ConsistencyFsfreeze, r); err != nil || s.Name() != config.ConsistencyFsfreeze {
		t.Errorf("fsfreeze: s=%v err=%v", s, err)
	}
	for _, name := range []string{config.ConsistencyLVMSnapshot, config.ConsistencyBtrfsSnapshot, config.ConsistencyRebootOffline, "bogus"} {
		if _, err := For(name, r); err == nil {
			t.Errorf("expected error for strategy %q", name)
		}
	}
}

func TestNonePrepare(t *testing.T) {
	p, err := None{}.Prepare(context.Background(), Target{Device: "sda2", Mountpoint: "/"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if p.Source != "/dev/sda2" {
		t.Errorf("Source = %q", p.Source)
	}
	if err := p.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}
}

func TestFsfreezeMounted(t *testing.T) {
	r := run.NewFakeRunner()
	s := &Fsfreeze{Runner: r}

	p, err := s.Prepare(context.Background(), Target{Device: "sda2", Mountpoint: "/data", FS: "ext4"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if p.Source != "/dev/sda2" {
		t.Errorf("Source = %q", p.Source)
	}
	if err := p.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}

	want := []string{"fsfreeze -f /data", "fsfreeze -u /data"}
	if got := r.CommandLines(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestFsfreezeUnmounted(t *testing.T) {
	r := run.NewFakeRunner()
	s := &Fsfreeze{Runner: r}

	p, err := s.Prepare(context.Background(), Target{Device: "sda2", Mountpoint: "", FS: "ext4"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := p.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}
	// Nothing mounted: no fsfreeze calls.
	if got := r.CommandLines(); len(got) != 0 {
		t.Errorf("expected no commands, got %v", got)
	}
}

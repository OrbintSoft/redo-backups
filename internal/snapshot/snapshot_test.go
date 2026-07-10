// SPDX-License-Identifier: EUPL-1.2

package snapshot

import (
	"context"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

func cfgFor(name config.Consistency) *config.Config {
	return &config.Config{Consistency: name}
}

func TestForSelection(t *testing.T) {
	t.Parallel()

	r := run.NewFakeRunner()
	for _, name := range []config.Consistency{config.ConsistencyNone, config.ConsistencyFsfreeze, config.ConsistencyLVM} {
		s, err := For(cfgFor(name), r)
		if err != nil || s.Name() != name {
			t.Errorf("%s: s=%v err=%v", name, s, err)
		}
	}

	for _, name := range []config.Consistency{"btrfs-snapshot", "reboot-offline", "bogus"} {
		if _, err := For(cfgFor(name), r); err == nil {
			t.Errorf("expected error for strategy %q", name)
		}
	}
}

func TestNonePrepare(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestFsfreezeUnfreezableFS(t *testing.T) {
	t.Parallel()

	for _, fs := range []string{"vfat", "VFAT", "exfat"} {
		r := run.NewFakeRunner()
		s := &Fsfreeze{Runner: r}

		p, err := s.Prepare(context.Background(), Target{Device: "sda1", Mountpoint: "/boot/efi", FS: fs})
		if err != nil {
			t.Fatalf("%s: Prepare: %v", fs, err)
		}

		if p.Source != "/dev/sda1" {
			t.Errorf("%s: Source = %q", fs, p.Source)
		}

		if err := p.Release(); err != nil {
			t.Errorf("%s: Release: %v", fs, err)
		}
		// Unfreezable filesystem: no fsfreeze calls, imaged directly.
		if got := r.CommandLines(); len(got) != 0 {
			t.Errorf("%s: expected no commands, got %v", fs, got)
		}
	}
}

func TestFsfreezeUnmounted(t *testing.T) {
	t.Parallel()

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

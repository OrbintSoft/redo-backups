// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestLoadDefaults(t *testing.T) {
	fsys := fstest.MapFS{
		"backup.conf": &fstest.MapFile{Data: []byte("dest = /mnt/backup\n")},
	}
	cfg, err := LoadFS(fsys, "backup")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if cfg.Dest != "/mnt/backup" {
		t.Errorf("Dest = %q", cfg.Dest)
	}
	if !cfg.DriveAuto() || !cfg.PartsAuto() {
		t.Errorf("expected auto drive/parts, got drive=%q parts=%v", cfg.Drive, cfg.Parts)
	}
	if cfg.Compressor != CompressorPigz || cfg.SplitSize != "4096M" || cfg.Consistency != ConsistencyNone {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Version != "4.0.0" {
		t.Errorf("Version = %q", cfg.Version)
	}
}

func TestLoadFull(t *testing.T) {
	conf := `
# Sample profile
dest = /mnt/backup
drive = sda
parts = sda1, sda2 sda3
id = nightly
notes = "After emerge"
compressor = gzip
split_size = 2G
consistency = fsfreeze
`
	fsys := fstest.MapFS{"box.conf": &fstest.MapFile{Data: []byte(conf)}}
	cfg, err := LoadFS(fsys, "box")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if cfg.Drive != "sda" {
		t.Errorf("Drive = %q", cfg.Drive)
	}
	if !reflect.DeepEqual(cfg.Parts, []string{"sda1", "sda2", "sda3"}) {
		t.Errorf("Parts = %v", cfg.Parts)
	}
	if cfg.Notes != "After emerge" {
		t.Errorf("Notes = %q", cfg.Notes)
	}
	if cfg.Compressor != "gzip" || cfg.SplitSize != "2G" || cfg.Consistency != ConsistencyFsfreeze {
		t.Errorf("unexpected: %+v", cfg)
	}
}

func TestDropInOverride(t *testing.T) {
	fsys := fstest.MapFS{
		"box.conf":                 &fstest.MapFile{Data: []byte("dest = /a\ndrive = sda\nconsistency = none\n")},
		"box.conf.d/10-drive.conf": &fstest.MapFile{Data: []byte("drive = sdb\n")},
		"box.conf.d/20-cons.conf":  &fstest.MapFile{Data: []byte("consistency = fsfreeze\n")},
		"box.conf.d/05-early.conf": &fstest.MapFile{Data: []byte("drive = sdz\n")},
		"box.conf.d/ignored.txt":   &fstest.MapFile{Data: []byte("drive = nope\n")},
		"other.conf":               &fstest.MapFile{Data: []byte("dest = /should-not-load\n")},
	}
	cfg, err := LoadFS(fsys, "box")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	// 10-drive.conf overrides 05-early.conf (lexical order), giving "sdb".
	if cfg.Drive != "sdb" {
		t.Errorf("Drive = %q, want sdb", cfg.Drive)
	}
	if cfg.Consistency != ConsistencyFsfreeze {
		t.Errorf("Consistency = %q, want fsfreeze", cfg.Consistency)
	}
	if cfg.Dest != "/a" {
		t.Errorf("Dest = %q, want /a", cfg.Dest)
	}
}

func TestMissingProfile(t *testing.T) {
	if _, err := LoadFS(fstest.MapFS{}, "nope"); err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"missing dest":    "drive = sda\n",
		"unknown key":     "dest = /a\nbogus = 1\n",
		"bad drive":       "dest = /a\ndrive = sd/a\n",
		"bad consistency": "dest = /a\nconsistency = magic\n",
		"bad compressor":  "dest = /a\ncompressor = lz4\n",
		"bad split_size":  "dest = /a\nsplit_size = big\n",
		"bad part":        "dest = /a\nparts = sda1 sd!2\n",
		"missing equals":  "dest = /a\njunkline\n",
	}
	for name, conf := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"p.conf": &fstest.MapFile{Data: []byte(conf)}}
			if _, err := LoadFS(fsys, "p"); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

func TestInvalidProfileName(t *testing.T) {
	if _, err := LoadFS(fstest.MapFS{}, "../etc"); err == nil {
		t.Fatal("expected error for invalid profile name")
	}
}

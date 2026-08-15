// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"reflect"
	"testing"
)

func TestParsePartRef(t *testing.T) {
	t.Parallel()

	cases := map[string]PartRef{
		"sda1":            {Tag: TagName, Value: "sda1"},
		"nvme0n1p2":       {Tag: TagName, Value: "nvme0n1p2"},
		"LABEL=LinuxRoot": {Tag: TagLabel, Value: "LinuxRoot"},
		"UUID=00000000-0000-4000-8000-0000000000b0": {Tag: TagUUID, Value: "00000000-0000-4000-8000-0000000000b0"},
		"PARTLABEL=root": {Tag: TagPartLabel, Value: "root"},
		"PARTUUID=00000000-0000-4000-8000-000000000002": {
			Tag: TagPartUUID, Value: "00000000-0000-4000-8000-000000000002",
		},
		// Tag names are case-insensitive and normalized to upper case.
		"label=LinuxHome": {Tag: TagLabel, Value: "LinuxHome"},
		// The value keeps its case: labels and UUIDs are matched verbatim.
		"LABEL=MiXeD": {Tag: TagLabel, Value: "MiXeD"},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePartRef(in)
			if err != nil {
				t.Fatalf("ParsePartRef(%q): %v", in, err)
			}

			if got != want {
				t.Errorf("ParsePartRef(%q) = %+v, want %+v", in, got, want)
			}

			if got.String() != in && got.Tag == TagName {
				t.Errorf("String() = %q, want %q", got.String(), in)
			}
		})
	}
}

func TestParsePartRefErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"bad kernel name": "sd!1",
		"path traversal":  "../sda1",
		"unknown tag":     "SERIAL=1234",
		"empty value":     "LABEL=",
		"empty input":     "",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParsePartRef(in); err == nil {
				t.Errorf("ParsePartRef(%q): expected error", in)
			}
		})
	}
}

func TestPartRefString(t *testing.T) {
	t.Parallel()

	cases := map[PartRef]string{
		{Tag: TagName, Value: "sda1"}:        "sda1",
		{Tag: TagLabel, Value: "LinuxRoot"}:  "LABEL=LinuxRoot",
		{Tag: TagPartUUID, Value: "0000-01"}: "PARTUUID=0000-01",
	}
	for ref, want := range cases {
		if got := ref.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", ref, got, want)
		}
	}
}

func TestPartRefs(t *testing.T) {
	t.Parallel()

	cfg := &Config{Parts: []string{"sda1", "LABEL=LinuxRoot", "PARTUUID=0000-02"}}

	got, err := cfg.PartRefs()
	if err != nil {
		t.Fatalf("PartRefs: %v", err)
	}

	want := []PartRef{
		{Tag: TagName, Value: "sda1"},
		{Tag: TagLabel, Value: "LinuxRoot"},
		{Tag: TagPartUUID, Value: "0000-02"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PartRefs = %+v, want %+v", got, want)
	}
}

func TestPartRefsAuto(t *testing.T) {
	t.Parallel()

	refs, err := (&Config{Parts: nil}).PartRefs()
	if err != nil {
		t.Fatalf("PartRefs: %v", err)
	}

	if refs != nil {
		t.Errorf("PartRefs = %+v, want nil for auto", refs)
	}
}

func TestDriveIsPath(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"auto":                        false,
		"sda":                         false,
		"/dev/disk/by-id/ata-MODEL_1": true,
	}
	for drive, want := range cases {
		if got := (&Config{Drive: drive}).DriveIsPath(); got != want {
			t.Errorf("DriveIsPath(%q) = %v, want %v", drive, got, want)
		}
	}
}

func TestValidateDrive(t *testing.T) {
	t.Parallel()

	valid := []string{
		"auto",
		"sda",
		"nvme0n1",
		"/dev/disk/by-id/ata-MODEL_SERIAL-1",
		"/dev/disk/by-id/wwn-0x0000000000000000",
		"/dev/disk/by-path/pci-0000:00:17.0-ata-1",
	}
	for _, drive := range valid {
		cfg := validConfigWithDrive(drive)
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate(drive=%q): %v", drive, err)
		}
	}

	invalid := []string{
		"sd/a",
		"/dev/sda",                     // only the persistent by-* links are accepted
		"/dev/disk/by-id/../../../etc", // no path traversal
		"/dev/disk/by-id/ata-X/sub",    // no nested paths
		"/etc/passwd",
	}
	for _, drive := range invalid {
		cfg := validConfigWithDrive(drive)
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate(drive=%q): expected error", drive)
		}
	}
}

// validConfigWithDrive returns an otherwise-valid config using the given drive.
func validConfigWithDrive(drive string) *Config {
	cfg := defaults()
	cfg.Dest = "/mnt/backup"
	cfg.Drive = drive

	return cfg
}

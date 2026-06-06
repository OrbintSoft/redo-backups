// SPDX-License-Identifier: EUPL-1.2

package redo

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// sampleImage builds a descriptor from synthetic, sanitized values (no real
// UUIDs/labels), used by both the golden and round-trip tests.
func sampleImage() *Image {
	parts := NewParts()
	parts.Set("sda1", Part{Bytes: 133169152, Size: "127M", Type: "EFI System", FS: "vfat", Desc: "ESP"})
	parts.Set("sda2", Part{Bytes: 299892736, Size: "286M", Type: "Linux filesystem", FS: "ext4", Desc: "boot"})

	return &Image{
		ID:         "20260105",
		Version:    FormatVersion,
		Timestamp:  "Mon, 05 Jan 2026 18:15:11 +0000",
		Notes:      "Example backup",
		DriveBytes: 512110190592,
		Parts:      parts,
		MBRBin:     []byte("MBR"),
		SFDBin:     []byte("label: gpt\ndevice: /dev/sda\n"),
	}
}

const goldenPath = "testdata/example.redo.json"

// TestMarshalGolden pins the exact bytes produced for a known descriptor.
// Regenerate with: REDO_UPDATE_GOLDEN=1 go test ./internal/redo/...
func TestMarshalGolden(t *testing.T) {
	got, err := sampleImage().Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if os.Getenv("REDO_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden file %s", goldenPath)
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with REDO_UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Marshal output does not match golden file\n got: %s\nwant: %s", got, want)
	}
}

// TestKeyOrder asserts the documented top-level field order is preserved.
func TestKeyOrder(t *testing.T) {
	got, err := sampleImage().Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"id":"20260105","version":"4.0.0","timestamp":"Mon, 05 Jan 2026 18:15:11 +0000","notes":"Example backup","drive_bytes":512110190592,"parts":{"sda1":{"bytes":133169152,"size":"127M","type":"EFI System","fs":"vfat","desc":"ESP"},"sda2":{"bytes":299892736,"size":"286M","type":"Linux filesystem","fs":"ext4","desc":"boot"}},"mbr_bin":"TUJS","sfd_bin":"bGFiZWw6IGdwdApkZXZpY2U6IC9kZXYvc2RhCg=="}`
	if string(got) != want {
		t.Errorf("unexpected JSON\n got: %s\nwant: %s", got, want)
	}
}

// TestRoundTrip checks Parse followed by Marshal reproduces the input bytes and
// preserves partition order.
func TestRoundTrip(t *testing.T) {
	orig := sampleImage()
	data, err := orig.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := parsed.Parts.Keys(); !reflect.DeepEqual(got, []string{"sda1", "sda2"}) {
		t.Errorf("partition order not preserved: got %v", got)
	}

	reMarshaled, err := parsed.Marshal()
	if err != nil {
		t.Fatalf("re-Marshal: %v", err)
	}
	if string(reMarshaled) != string(data) {
		t.Errorf("round-trip mismatch\n got: %s\nwant: %s", reMarshaled, data)
	}
}

// TestHTMLNotEscaped guards against encoding/json's default HTML escaping, which
// would mangle characters that legitimately appear in labels/descriptions.
func TestHTMLNotEscaped(t *testing.T) {
	img := sampleImage()
	img.Notes = "drive A&B <test>"
	got, err := img.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(got, []byte(`"notes":"drive A&B <test>"`)) {
		t.Errorf("expected unescaped notes, got: %s", got)
	}
}

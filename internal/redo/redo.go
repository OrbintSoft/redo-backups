// SPDX-License-Identifier: EUPL-1.2
//
// Package redo models the Redo Rescue ".redo" backup descriptor and produces it
// in a form the stock Redo Rescue live CD can restore. See docs/redo-format.md
// for the on-disk format this mirrors.
package redo

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// FormatVersion is the Redo Rescue on-disk format version this package targets.
const FormatVersion = "4.0.0"

// MBRSize is the number of bytes captured from the start of the drive into
// Image.MBRBin (MBR plus the GPT primary header/entries area).
const MBRSize = 32768

// Part describes a single backed-up partition, matching one entry of the
// ".redo" file's "parts" object.
type Part struct {
	// Bytes is the partition size in bytes.
	Bytes int64 `json:"bytes"`
	// Size is the human-readable size as reported by lsblk (e.g. "127M").
	Size string `json:"size"`
	// Type is the partition type description (e.g. "EFI System").
	Type string `json:"type"`
	// FS is the filesystem type (e.g. "vfat", "ext4").
	FS string `json:"fs"`
	// Desc is the label/OS description, possibly empty.
	Desc string `json:"desc"`
}

// Image is the full content of a ".redo" descriptor.
//
// Fields are declared in the exact order Redo Rescue writes them, which
// encoding/json preserves on output.
type Image struct {
	// ID is the backup identifier (e.g. "20260105").
	ID string `json:"id"`
	// Version is the on-disk format version (see FormatVersion).
	Version string `json:"version"`
	// Timestamp is the creation time in RFC 2822 form (e.g.
	// "Mon, 05 Jan 2026 18:15:11 +0000").
	Timestamp string `json:"timestamp"`
	// Notes is a free-text note for the backup.
	Notes string `json:"notes"`
	// DriveBytes is the total size of the whole drive in bytes.
	DriveBytes int64 `json:"drive_bytes"`
	// Parts maps partition device names (e.g. "sda1") to their metadata,
	// preserving insertion order.
	Parts Parts `json:"parts"`
	// MBRBin holds the first MBRSize bytes of the drive; it is base64-encoded
	// in JSON.
	MBRBin []byte `json:"mbr_bin"`
	// SFDBin holds the `sfdisk --dump` output; it is base64-encoded in JSON.
	SFDBin []byte `json:"sfd_bin"`
}

// Marshal renders the descriptor as compact JSON, matching how Redo Rescue
// stores it (no insignificant whitespace, fields in declaration order). HTML
// escaping is disabled so characters such as '<' and '&' are emitted verbatim.
func (img *Image) Marshal() ([]byte, error) {
	return marshalCompact(img)
}

// Parse decodes a ".redo" descriptor from its JSON form, preserving partition
// order.
func Parse(data []byte) (*Image, error) {
	var img Image
	if err := json.Unmarshal(data, &img); err != nil {
		return nil, fmt.Errorf("redo: parse: %w", err)
	}
	return &img, nil
}

// marshalCompact encodes v as compact JSON without HTML escaping and without a
// trailing newline.
func marshalCompact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

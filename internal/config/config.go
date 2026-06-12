// SPDX-License-Identifier: EUPL-1.2

// Package config loads backup profiles from /etc/redo-backups/. A profile is a
// "<profile>.conf" file in key=value form, optionally extended by drop-in files
// under "<profile>.conf.d/*.conf" (applied in lexical order, later values
// overriding earlier ones). All settings are documented in docs/redo-format.md
// and the examples/ directory.
package config

import (
	"github.com/OrbintSoft/redo-backups/internal/redo"
)

// DefaultDir is where profiles live on a running system.
const DefaultDir = "/etc/redo-backups"

// Consistency is a live-filesystem consistency strategy (see docs/redo-format.md).
type Consistency string

// Consistency strategies.
const (
	ConsistencyNone     Consistency = "none"
	ConsistencyFsfreeze Consistency = "fsfreeze"
	ConsistencyLVM      Consistency = "lvm"
)

// Compressor is the compression tool used for the image stream.
type Compressor string

// Supported compressors.
const (
	CompressorPigz Compressor = "pigz"
	CompressorGzip Compressor = "gzip"
)

// Sentinel value meaning "detect automatically" for drive/parts.
const auto = "auto"

// Config is a fully-resolved backup profile (defaults applied, validated).
type Config struct {
	// Dest is the destination directory for the backup (required).
	Dest string
	// Drive is the target drive name without "/dev/" (e.g. "sda"), or "auto"
	// to detect the drive hosting the root filesystem.
	Drive string
	// Parts lists partition names to back up; empty means all partitions of the
	// drive ("auto").
	Parts []string
	// ID is the backup identifier; empty means derive it from the date
	// (YYYYMMDD) at run time.
	ID string
	// Notes is a free-text note stored in the descriptor.
	Notes string
	// Version is the on-disk format version to write.
	Version string
	// Compressor is the compression tool ("pigz" or "gzip").
	Compressor Compressor
	// SplitSize is the chunk size passed to split (e.g. "4096M").
	SplitSize string
	// Consistency selects the live-consistency strategy.
	Consistency Consistency
}

// DriveAuto reports whether the drive should be auto-detected.
func (c *Config) DriveAuto() bool { return c.Drive == auto }

// PartsAuto reports whether all partitions should be backed up.
func (c *Config) PartsAuto() bool { return len(c.Parts) == 0 }

// defaults returns a Config pre-filled with default values.
func defaults() *Config {
	return &Config{
		Dest:        "",
		Drive:       auto,
		Parts:       nil,
		ID:          "",
		Notes:       "",
		Version:     redo.FormatVersion,
		Compressor:  CompressorPigz,
		SplitSize:   "4096M",
		Consistency: ConsistencyNone,
	}
}

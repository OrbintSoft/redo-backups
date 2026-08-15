// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	devNameRE   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	idRE        = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	splitSizeRE = regexp.MustCompile(`^[0-9]+[A-Za-z]*$`)
)

// Sentinel validation errors. Dynamic context (the offending value) is added by
// wrapping these with %w, so callers can still match them with errors.Is.
var (
	errDestRequired       = errors.New("config: 'dest' is required")
	errInvalidDrive       = errors.New("config: invalid 'drive' value")
	errInvalidPartName    = errors.New("config: invalid partition name")
	errInvalidID          = errors.New("config: invalid 'id' value")
	errVersionEmpty       = errors.New("config: 'version' must not be empty")
	errInvalidCompressor  = errors.New("config: invalid 'compressor'")
	errInvalidSplitSize   = errors.New("config: invalid 'split_size'")
	errInvalidConsistency = errors.New("config: invalid 'consistency'")
)

var validConsistency = map[Consistency]bool{
	ConsistencyNone:     true,
	ConsistencyFsfreeze: true,
	ConsistencyLVM:      true,
}

var validCompressor = map[Compressor]bool{
	CompressorPigz: true,
	CompressorGzip: true,
}

// Validate checks the resolved configuration for internal consistency. It is
// exported so callers that mutate a loaded Config (e.g. CLI overrides) can
// re-check it.
func (c *Config) Validate() error {
	if c.Dest == "" {
		return errDestRequired
	}

	if err := c.validateDrive(); err != nil {
		return err
	}

	if _, err := c.PartRefs(); err != nil {
		return err
	}

	if c.ID != "" && !idRE.MatchString(c.ID) {
		return fmt.Errorf("%w %q", errInvalidID, c.ID)
	}

	if c.Version == "" {
		return errVersionEmpty
	}

	if !validCompressor[c.Compressor] {
		return fmt.Errorf("%w %q (want pigz or gzip)", errInvalidCompressor, c.Compressor)
	}

	if !splitSizeRE.MatchString(c.SplitSize) {
		return fmt.Errorf("%w %q", errInvalidSplitSize, c.SplitSize)
	}

	if !validConsistency[c.Consistency] {
		return fmt.Errorf("%w %q", errInvalidConsistency, c.Consistency)
	}

	return nil
}

// validateDrive accepts the "auto" sentinel, a persistent "/dev/disk/by-*/"
// symlink, or a bare kernel device name such as "sda".
func (c *Config) validateDrive() error {
	switch {
	case c.DriveAuto():
		return nil
	case c.DriveIsPath():
		if !driveByPathRE.MatchString(c.Drive) {
			return fmt.Errorf("%w %q (want /dev/disk/by-<kind>/<name>)", errInvalidDrive, c.Drive)
		}

		return nil
	case devNameRE.MatchString(c.Drive):
		return nil
	default:
		return fmt.Errorf("%w %q", errInvalidDrive, c.Drive)
	}
}

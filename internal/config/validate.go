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

	if !c.DriveAuto() && !devNameRE.MatchString(c.Drive) {
		return fmt.Errorf("%w %q", errInvalidDrive, c.Drive)
	}

	for _, p := range c.Parts {
		if !devNameRE.MatchString(p) {
			return fmt.Errorf("%w %q in 'parts'", errInvalidPartName, p)
		}
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

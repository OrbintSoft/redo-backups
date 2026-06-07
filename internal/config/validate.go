// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"fmt"
	"regexp"
)

var (
	devNameRE   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	idRE        = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	splitSizeRE = regexp.MustCompile(`^[0-9]+[A-Za-z]*$`)
)

var validConsistency = map[Consistency]bool{
	ConsistencyNone:          true,
	ConsistencyFsfreeze:      true,
	ConsistencyLVMSnapshot:   true,
	ConsistencyBtrfsSnapshot: true,
	ConsistencyRebootOffline: true,
}

var validCompressor = map[Compressor]bool{
	CompressorPigz: true,
	CompressorGzip: true,
}

// validate checks the resolved configuration for internal consistency.
func (c *Config) validate() error {
	if c.Dest == "" {
		return fmt.Errorf("config: 'dest' is required")
	}
	if !c.DriveAuto() && !devNameRE.MatchString(c.Drive) {
		return fmt.Errorf("config: invalid 'drive' value %q", c.Drive)
	}
	for _, p := range c.Parts {
		if !devNameRE.MatchString(p) {
			return fmt.Errorf("config: invalid partition name %q in 'parts'", p)
		}
	}
	if c.ID != "" && !idRE.MatchString(c.ID) {
		return fmt.Errorf("config: invalid 'id' value %q", c.ID)
	}
	if c.Version == "" {
		return fmt.Errorf("config: 'version' must not be empty")
	}
	if !validCompressor[c.Compressor] {
		return fmt.Errorf("config: invalid 'compressor' %q (want pigz or gzip)", c.Compressor)
	}
	if !splitSizeRE.MatchString(c.SplitSize) {
		return fmt.Errorf("config: invalid 'split_size' %q", c.SplitSize)
	}
	if !validConsistency[c.Consistency] {
		return fmt.Errorf("config: invalid 'consistency' %q", c.Consistency)
	}
	return nil
}

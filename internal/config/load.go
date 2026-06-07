// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

// profileNameRE restricts profile names to a safe, file-friendly set.
var profileNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Load reads the named profile from dir (typically DefaultDir), merging its
// drop-in directory, and returns the resolved, validated configuration.
func Load(dir, profile string) (*Config, error) {
	return LoadFS(os.DirFS(dir), profile)
}

// ListProfiles returns the names of the profiles available in dir (each
// "<name>.conf" file), sorted. Drop-in directories ("<name>.conf.d") are not
// profiles and are ignored.
func ListProfiles(dir string) ([]string, error) {
	return listProfilesFS(os.DirFS(dir))
}

// listProfilesFS is the filesystem-agnostic core of ListProfiles.
func listProfilesFS(fsys fs.FS) ([]string, error) {
	matches, err := fs.Glob(fsys, "*.conf")
	if err != nil {
		return nil, fmt.Errorf("config: listing profiles: %w", err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, strings.TrimSuffix(m, ".conf"))
	}
	sort.Strings(names)
	return names, nil
}

// LoadFS is like Load but reads from an arbitrary filesystem, which makes the
// loader testable without touching the real /etc.
func LoadFS(fsys fs.FS, profile string) (*Config, error) {
	if !profileNameRE.MatchString(profile) {
		return nil, fmt.Errorf("config: invalid profile name %q", profile)
	}

	base := profile + ".conf"
	files := []string{base}

	dropinGlob := profile + ".conf.d/*.conf"
	dropins, err := fs.Glob(fsys, dropinGlob)
	if err != nil {
		return nil, fmt.Errorf("config: scanning drop-ins: %w", err)
	}
	sort.Strings(dropins)
	files = append(files, dropins...)

	merged := map[string]string{}
	loadedAny := false
	for i, name := range files {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			// The base file is required; drop-ins are optional, but Glob only
			// returns existing ones, so any read error there is real.
			if i == 0 && errorIsNotExist(err) {
				return nil, fmt.Errorf("config: profile %q not found (%s)", profile, name)
			}
			return nil, fmt.Errorf("config: reading %s: %w", name, err)
		}
		if err := parseInto(merged, data); err != nil {
			return nil, fmt.Errorf("config: %s: %w", name, err)
		}
		loadedAny = true
	}
	if !loadedAny {
		return nil, fmt.Errorf("config: profile %q has no configuration", profile)
	}

	cfg, err := fromMap(merged)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseInto parses key=value lines from data into dst (later keys override
// earlier ones). Blank lines and lines whose first non-space character is '#'
// are ignored. Values may be wrapped in matching single or double quotes.
func parseInto(dst map[string]string, data []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			return fmt.Errorf("line %d: missing '=' in %q", line, raw)
		}
		key := strings.TrimSpace(raw[:eq])
		if key == "" {
			return fmt.Errorf("line %d: empty key", line)
		}
		val := unquote(strings.TrimSpace(raw[eq+1:]))
		dst[key] = val
	}
	return sc.Err()
}

// unquote strips one layer of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// knownKeys lists every recognized configuration key. Unknown keys are rejected
// so typos surface immediately instead of being silently ignored.
var knownKeys = map[string]bool{
	"dest":              true,
	"drive":             true,
	"parts":             true,
	"id":                true,
	"notes":             true,
	"version":           true,
	"compressor":        true,
	"split_size":        true,
	"consistency":       true,
	"lvm_snapshot_size": true,
}

// fromMap builds a Config from merged key/value pairs, starting from defaults.
func fromMap(m map[string]string) (*Config, error) {
	for k := range m {
		if !knownKeys[k] {
			return nil, fmt.Errorf("config: unknown key %q", k)
		}
	}

	cfg := defaults()
	if v, ok := m["dest"]; ok {
		cfg.Dest = v
	}
	if v, ok := m["drive"]; ok {
		cfg.Drive = v
	}
	if v, ok := m["parts"]; ok {
		cfg.Parts = ParseParts(v)
	}
	if v, ok := m["id"]; ok {
		cfg.ID = v
	}
	if v, ok := m["notes"]; ok {
		cfg.Notes = v
	}
	if v, ok := m["version"]; ok {
		cfg.Version = v
	}
	if v, ok := m["compressor"]; ok {
		cfg.Compressor = Compressor(v)
	}
	if v, ok := m["split_size"]; ok {
		cfg.SplitSize = v
	}
	if v, ok := m["consistency"]; ok {
		cfg.Consistency = Consistency(v)
	}
	if v, ok := m["lvm_snapshot_size"]; ok {
		cfg.LVMSnapshotSize = v
	}
	return cfg, nil
}

// ParseParts splits a parts value on commas and whitespace. The sentinel "auto"
// (alone) yields an empty slice, meaning all partitions.
func ParseParts(v string) []string {
	if strings.TrimSpace(v) == auto {
		return nil
	}
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func errorIsNotExist(err error) bool {
	return err != nil && (os.IsNotExist(err) || strings.Contains(err.Error(), "file does not exist"))
}

// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PartTag names the stable identifier a partition reference is matched on. The
// syntax mirrors util-linux (fstab, findfs, blkid), so "LABEL=LinuxRoot" means
// in a profile exactly what it means there.
type PartTag string

// Recognized partition reference tags.
const (
	// TagName is the zero value: the reference is a bare kernel device name
	// such as "sda1", not a stable identifier.
	TagName PartTag = ""
	// TagLabel matches the filesystem label (lsblk LABEL).
	TagLabel PartTag = "LABEL"
	// TagUUID matches the filesystem UUID (lsblk UUID).
	TagUUID PartTag = "UUID"
	// TagPartLabel matches the partition-table name (lsblk PARTLABEL, GPT only).
	TagPartLabel PartTag = "PARTLABEL"
	// TagPartUUID matches the partition-table UUID (lsblk PARTUUID).
	TagPartUUID PartTag = "PARTUUID"
)

var validPartTag = map[PartTag]bool{
	TagLabel:     true,
	TagUUID:      true,
	TagPartLabel: true,
	TagPartUUID:  true,
}

// driveByPathRE matches a persistent whole-drive symlink such as
// "/dev/disk/by-id/ata-VENDOR_MODEL_SERIAL". Only the udev "by-*" directories
// are accepted, and the link name itself may not contain path separators, so a
// profile cannot point the tool at an arbitrary file.
var driveByPathRE = regexp.MustCompile(`^/dev/disk/by-[a-z]+/[a-zA-Z0-9_.:+-]+$`)

// devDiskPrefix is the udev directory holding the persistent device symlinks.
const devDiskPrefix = "/dev/disk/"

// Sentinel reference errors, wrapped with %w so callers keep errors.Is.
var (
	errUnknownPartTag = errors.New("config: unknown partition reference tag")
	errEmptyTagValue  = errors.New("config: empty partition reference value")
)

// PartRef is one entry of the 'parts' setting: either a kernel device name
// (Tag == TagName), or a stable identifier such as LABEL=/UUID=/PARTLABEL=/
// PARTUUID=. Kernel names are assigned in probe order and shift when drives are
// added or removed, so stable identifiers are preferred in saved profiles.
type PartRef struct {
	// Tag is the identifier the reference matches on, TagName for a kernel name.
	Tag PartTag
	// Value is the kernel device name (Tag == TagName) or the tag's value.
	Value string
}

// String renders the reference back in profile syntax.
func (r PartRef) String() string {
	if r.Tag == TagName {
		return r.Value
	}

	return string(r.Tag) + "=" + r.Value
}

// ParsePartRef parses one 'parts' entry. Tag names are accepted in any case and
// normalized to upper case; a value without '=' is taken as a kernel device
// name and validated as such.
//
// Values may not contain whitespace or commas, because those separate entries
// in the 'parts' list: a label with spaces must be referenced by UUID= or
// PARTUUID= instead.
func ParsePartRef(s string) (PartRef, error) {
	rawTag, value, found := strings.Cut(s, "=")
	if !found {
		if !devNameRE.MatchString(s) {
			return PartRef{}, fmt.Errorf("%w %q in 'parts'", errInvalidPartName, s)
		}

		return PartRef{Tag: TagName, Value: s}, nil
	}

	tag := PartTag(strings.ToUpper(strings.TrimSpace(rawTag)))
	if !validPartTag[tag] {
		return PartRef{}, fmt.Errorf("%w %q in 'parts' (want LABEL, UUID, PARTLABEL or PARTUUID)",
			errUnknownPartTag, rawTag)
	}

	if value == "" {
		return PartRef{}, fmt.Errorf("%w: %q in 'parts'", errEmptyTagValue, s)
	}

	return PartRef{Tag: tag, Value: value}, nil
}

// PartRefs parses the configured 'parts' entries. It returns nil when the
// partitions are auto-selected.
func (c *Config) PartRefs() ([]PartRef, error) {
	if c.PartsAuto() {
		return nil, nil
	}

	refs := make([]PartRef, 0, len(c.Parts))

	for _, p := range c.Parts {
		ref, err := ParsePartRef(p)
		if err != nil {
			return nil, err
		}

		refs = append(refs, ref)
	}

	return refs, nil
}

// DriveIsPath reports whether the drive is given as a persistent
// "/dev/disk/by-*/" symlink rather than a kernel device name.
func (c *Config) DriveIsPath() bool {
	return strings.HasPrefix(c.Drive, devDiskPrefix)
}

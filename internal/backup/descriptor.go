// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/redo"
)

// Sentinel backup errors. Dynamic context (partition / drive names) is added by
// wrapping these with %w, keeping them matchable with errors.Is.
var (
	errPartitionNotOnDrive = errors.New("backup: requested partition not on drive")
	errAmbiguousPartRef    = errors.New("backup: partition reference is ambiguous")
	errNoPartitions        = errors.New("backup: no partitions to back up on drive")
)

// descriptorPerm restricts the ".redo" descriptor to its owner. The descriptor
// (MBR, partition table, partition metadata) is part of a backup and may be
// sensitive; it is written and read back only by the root-run tool, so
// owner-only permissions are sufficient and safer than world-readable.
const descriptorPerm os.FileMode = 0o600

// FormatTimestamp renders t the way Redo Rescue stores it (RFC 2822, matching
// PHP's date('r')), e.g. "Mon, 05 Jan 2026 18:15:11 +0000".
func FormatTimestamp(t time.Time) string {
	return t.Format(time.RFC1123Z)
}

// FormatID renders the default backup identifier from t (YYYYMMDD).
func FormatID(t time.Time) string {
	return t.Format("20060102")
}

// SelectPartitions returns the partitions to back up, in drive order. With
// cfg.PartsAuto every partition is selected; otherwise each configured
// reference — a kernel device name or a stable identifier such as
// LABEL=/UUID=/PARTLABEL=/PARTUUID= (see config.PartRef) — must match exactly
// one partition of the drive.
func SelectPartitions(cfg *config.Config, drive *disk.Drive) ([]disk.Partition, error) {
	refs, err := cfg.PartRefs()
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return drive.Partitions, nil
	}

	want := make(map[string]bool, len(refs))

	for _, ref := range refs {
		name, err := resolvePartRef(ref, drive)
		if err != nil {
			return nil, err
		}

		want[name] = true
	}

	selected := make([]disk.Partition, 0, len(want))

	for _, p := range drive.Partitions {
		if want[p.Name] {
			selected = append(selected, p)
		}
	}

	return selected, nil
}

// resolvePartRef returns the name of the one partition of drive that ref
// matches. Matching nothing, or more than one partition (duplicate labels are
// possible), is an error: silently imaging the wrong partition would produce a
// backup that restores over the wrong data.
func resolvePartRef(ref config.PartRef, drive *disk.Drive) (string, error) {
	var matched []string

	for _, p := range drive.Partitions {
		if partitionMatches(p, ref) {
			matched = append(matched, p.Name)
		}
	}

	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return "", fmt.Errorf("%w: %q (drive %q); available: %s",
			errPartitionNotOnDrive, ref.String(), drive.Name, describePartitions(drive))
	default:
		return "", fmt.Errorf("%w: %q matches %s (drive %q)",
			errAmbiguousPartRef, ref.String(), strings.Join(matched, ", "), drive.Name)
	}
}

// partitionMatches reports whether p satisfies ref.
func partitionMatches(p disk.Partition, ref config.PartRef) bool {
	switch ref.Tag {
	case config.TagName:
		return p.Name == ref.Value
	case config.TagLabel:
		return p.Label == ref.Value
	case config.TagPartLabel:
		return p.PartLabel == ref.Value
	case config.TagUUID:
		// UUIDs are hexadecimal and different filesystems print them in
		// different cases (lower for ext4, upper for vfat), so compare them
		// case-insensitively; a label, by contrast, is matched verbatim.
		return p.UUID != "" && strings.EqualFold(p.UUID, ref.Value)
	case config.TagPartUUID:
		return p.PartUUID != "" && strings.EqualFold(p.PartUUID, ref.Value)
	default:
		return false
	}
}

// describePartitions lists the drive's partitions with the identifiers a
// reference can match, so a failed lookup shows what could be written instead.
func describePartitions(drive *disk.Drive) string {
	descs := make([]string, 0, len(drive.Partitions))

	for _, p := range drive.Partitions {
		ids := []string{p.Name}
		for _, id := range []struct {
			tag config.PartTag
			val string
		}{
			{config.TagLabel, p.Label},
			{config.TagUUID, p.UUID},
			{config.TagPartLabel, p.PartLabel},
			{config.TagPartUUID, p.PartUUID},
		} {
			if id.val != "" {
				ids = append(ids, config.PartRef{Tag: id.tag, Value: id.val}.String())
			}
		}

		descs = append(descs, strings.Join(ids, " "))
	}

	return strings.Join(descs, "; ")
}

// BuildImage assembles the ".redo" descriptor from the gathered facts. The
// caller supplies id, timestamp, MBR and partition-table bytes (so this stays
// pure and testable); parts is the already-selected partition set.
func BuildImage(
	cfg *config.Config,
	drive *disk.Drive,
	parts []disk.Partition,
	id, timestamp string,
	mbr, sfd []byte,
) *redo.Image {
	p := redo.NewParts()
	for _, part := range parts {
		p.Set(part.Name, redo.Part{
			Bytes: part.Bytes,
			Size:  part.Size,
			Type:  part.Type,
			FS:    part.FS,
			Desc:  part.Label,
		})
	}

	return &redo.Image{
		ID:         id,
		Version:    cfg.Version,
		Timestamp:  timestamp,
		Notes:      cfg.Notes,
		DriveBytes: drive.Bytes,
		Parts:      p,
		MBRBin:     mbr,
		SFDBin:     sfd,
	}
}

// WriteDescriptor writes the descriptor as "<dir>/<id>.redo" and returns the
// path written.
func WriteDescriptor(dir string, img *redo.Image) (string, error) {
	data, err := img.Marshal()
	if err != nil {
		return "", fmt.Errorf("backup: marshaling descriptor: %w", err)
	}

	path := filepath.Join(dir, img.ID+".redo")
	if err := os.WriteFile(path, data, descriptorPerm); err != nil {
		return "", fmt.Errorf("backup: writing %s: %w", path, err)
	}

	return path, nil
}

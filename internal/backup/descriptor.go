// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/redo"
)

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
// cfg.PartsAuto every partition is selected; otherwise only the named ones are,
// and a missing name is an error.
func SelectPartitions(cfg *config.Config, drive *disk.Drive) ([]disk.Partition, error) {
	if cfg.PartsAuto() {
		return drive.Partitions, nil
	}
	want := make(map[string]bool, len(cfg.Parts))
	for _, p := range cfg.Parts {
		want[p] = true
	}
	var selected []disk.Partition
	for _, p := range drive.Partitions {
		if want[p.Name] {
			selected = append(selected, p)
			delete(want, p.Name)
		}
	}
	if len(want) > 0 {
		for _, name := range cfg.Parts {
			if want[name] {
				return nil, fmt.Errorf("backup: partition %q is not on drive %q", name, drive.Name)
			}
		}
	}
	return selected, nil
}

// BuildImage assembles the ".redo" descriptor from the gathered facts. The
// caller supplies id, timestamp, MBR and partition-table bytes (so this stays
// pure and testable); parts is the already-selected partition set.
func BuildImage(cfg *config.Config, drive *disk.Drive, parts []disk.Partition, id, timestamp string, mbr, sfd []byte) *redo.Image {
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("backup: writing %s: %w", path, err)
	}
	return path, nil
}

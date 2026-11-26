// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/redo"
)

// completionExt is the extension of a set's completion record, "<id>.complete".
const completionExt = ".complete"

// completionVersion is the format version of the completion record.
const completionVersion = 1

// completionPerm restricts the completion record to its owner, like the
// descriptor it vouches for.
const completionPerm os.FileMode = 0o600

// errNoChunks reports a partition whose imaging finished without any chunk
// written for it.
var errNoChunks = errors.New("backup: no image chunks written")

// Completion is the record a run writes next to a backup set once every
// partition is imaged and on disk: the files that make up the set and their
// sizes. The descriptor is written before imaging, as Redo Rescue does, so it
// cannot tell a complete set from one whose run stopped half way; this record
// can. It is this tool's own addition, which Redo Rescue neither writes nor
// reads.
type Completion struct {
	// Version is the format version of the record.
	Version int `json:"version"`
	// ID is the backup identifier the set's files share.
	ID string `json:"id"`
	// Files lists the descriptor and then every image chunk, partition by
	// partition in the order the descriptor lists them.
	Files []SetFile `json:"files"`
}

// SetFile is one file of a backup set.
type SetFile struct {
	// Name is the file name, relative to the destination directory.
	Name string `json:"name"`
	// Bytes is the file size in bytes.
	Bytes int64 `json:"bytes"`
}

// startSet creates the destination, which checkDest has accepted, withdraws
// the completion record of any earlier set with the same id, and writes the
// descriptor. It returns the destination opened as a root for the set's
// files, which the caller closes, and the descriptor's path.
func startSet(dest string, img *redo.Image) (*os.Root, string, error) {
	if err := os.MkdirAll(dest, destDirPerm); err != nil {
		return nil, "", fmt.Errorf("backup: creating destination %s: %w", dest, err)
	}

	set, err := os.OpenRoot(dest)
	if err != nil {
		return nil, "", fmt.Errorf("backup: opening destination %s: %w", dest, err)
	}

	// Withdraw the earlier record first: from here on this run replaces that
	// set, and until it completes the set is not known to be complete.
	err = set.Remove(img.ID + completionExt)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = set.Close()

		return nil, "", fmt.Errorf("backup: withdrawing the completion record of set %s: %w", img.ID, err)
	}

	descPath, err := WriteDescriptor(dest, img)
	if err != nil {
		_ = set.Close()

		return nil, "", err
	}

	return set, descPath, nil
}

// removeChunks deletes the image chunks an earlier set with the same id left
// for partition dev. split overwrites chunks one by one, so without this a
// shorter image would leave the earlier set's last chunks behind, and a
// restore, which reads every chunk of the partition, would read them too.
func removeChunks(set *os.Root, id, dev string) error {
	names, err := chunkNames(set, id, dev)
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := set.Remove(name); err != nil {
			return fmt.Errorf("backup: removing %s of an earlier set: %w", name, err)
		}
	}

	return nil
}

// writeCompletion flushes every file of the set to disk and then writes the
// set's completion record, so that the record is never on disk before what it
// vouches for. It returns the record's file name.
func writeCompletion(set *os.Root, id string, parts []disk.Partition) (string, error) {
	names := []string{id + ".redo"}

	for _, part := range parts {
		chunks, err := chunkNames(set, id, part.Name)
		if err != nil {
			return "", err
		}

		if len(chunks) == 0 {
			return "", fmt.Errorf("%w for %s", errNoChunks, part.Name)
		}

		names = append(names, chunks...)
	}

	record := Completion{Version: completionVersion, ID: id, Files: make([]SetFile, 0, len(names))}

	for _, name := range names {
		size, err := syncFile(set, name)
		if err != nil {
			return "", err
		}

		record.Files = append(record.Files, SetFile{Name: name, Bytes: size})
	}

	data, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("backup: encoding the completion record of set %s: %w", id, err)
	}

	name := id + completionExt
	if err := writeSynced(set, name, data); err != nil {
		return "", err
	}

	return name, nil
}

// chunkNames lists, in chunk order, the image chunks of partition dev in set
// id: the files split names "<id>_<dev>_NNN.img" (see splitStage).
func chunkNames(set *os.Root, id, dev string) ([]string, error) {
	entries, err := fs.ReadDir(set.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("backup: listing %s: %w", set.Name(), err)
	}

	pattern := id + "_" + dev + "_[0-9][0-9][0-9].img"

	var names []string

	for _, entry := range entries {
		match, err := filepath.Match(pattern, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("backup: matching chunks of %s: %w", dev, err)
		}

		if match && entry.Type().IsRegular() {
			names = append(names, entry.Name())
		}
	}

	// ReadDir sorts by name, which for zero-padded numbers is chunk order.
	return names, nil
}

// syncFile flushes a file of the set to disk and returns its size.
func syncFile(set *os.Root, name string) (int64, error) {
	file, err := set.Open(name)
	if err != nil {
		return 0, fmt.Errorf("backup: opening %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()

	if err := file.Sync(); err != nil {
		return 0, fmt.Errorf("backup: flushing %s: %w", name, err)
	}

	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("backup: inspecting %s: %w", name, err)
	}

	return info.Size(), nil
}

// writeSynced writes data to name through a temporary file that is flushed
// before it is renamed into place, then flushes the directory, so that name
// holds either all of data or nothing new, whenever the system stops.
func writeSynced(set *os.Root, name string, data []byte) error {
	tmp := "." + name + ".tmp"

	file, err := set.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, completionPerm)
	if err != nil {
		return fmt.Errorf("backup: creating %s: %w", tmp, err)
	}

	_, werr := file.Write(data)
	if werr == nil {
		werr = file.Sync()
	}

	if cerr := file.Close(); werr == nil {
		werr = cerr
	}

	if werr == nil {
		werr = set.Rename(tmp, name)
	}

	if werr != nil {
		_ = set.Remove(tmp)

		return fmt.Errorf("backup: writing %s: %w", name, werr)
	}

	dir, err := set.Open(".")
	if err != nil {
		return fmt.Errorf("backup: opening %s: %w", set.Name(), err)
	}
	defer func() { _ = dir.Close() }()

	if err := dir.Sync(); err != nil {
		return fmt.Errorf("backup: flushing %s: %w", set.Name(), err)
	}

	return nil
}

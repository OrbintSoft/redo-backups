// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/redo"
)

// descriptorExt is the extension of a set's descriptor, "<id>.redo".
const descriptorExt = ".redo"

// errIncomplete marks a set that is not known to be complete; the reason is
// added by wrapping it.
var errIncomplete = errors.New("not known to be complete")

// Set is a complete backup set found in a destination.
type Set struct {
	// ID is the backup identifier.
	ID string
	// Taken is when the backup was taken: the descriptor's timestamp, which,
	// unlike a file's modification time, a copy of the set keeps.
	Taken time.Time
}

// IncompleteSet is a set Check found but did not count, and why.
type IncompleteSet struct {
	ID     string
	Reason string
}

// CheckReport is what Check found in a profile's destination.
type CheckReport struct {
	// Dest is the destination checked.
	Dest string
	// Newest is the newest complete set, or nil when there is none.
	Newest *Set
	// Age is how long before the check Newest was taken; zero without Newest,
	// negative for a timestamp still to come.
	Age time.Duration
	// Fresh reports whether Newest is younger than the maximum age.
	Fresh bool
	// Incomplete lists the sets found without a valid completion record.
	Incomplete []IncompleteSet
}

// Check reports whether the newest complete backup in cfg's destination is
// younger than maxAge. It refuses a destination that is not available as Run
// does (ErrDestUnavailable), and it only reads: it neither creates the
// destination nor reads the drive, whose name is all it needs.
func (b *Backup) Check(ctx context.Context, cfg *config.Config, maxAge time.Duration) (*CheckReport, error) {
	drive, err := b.resolveDrive(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := b.checkDest(ctx, cfg, drive); err != nil {
		return nil, err
	}

	newest, incomplete, err := findSets(cfg.Dest)
	if err != nil {
		return nil, err
	}

	report := &CheckReport{Dest: cfg.Dest, Newest: newest, Age: 0, Fresh: false, Incomplete: incomplete}
	if newest != nil {
		report.Age = b.now().Sub(newest.Taken)
		report.Fresh = report.Age < maxAge
	}

	return report, nil
}

// findSets returns the newest complete set in dir, by descriptor timestamp,
// and the sets that are not known to be complete. A dir that does not exist
// holds no set.
func findSets(dir string) (*Set, []IncompleteSet, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}

		return nil, nil, fmt.Errorf("backup: opening destination %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, nil, fmt.Errorf("backup: listing %s: %w", dir, err)
	}

	var (
		newest     *Set
		incomplete []IncompleteSet
	)

	for _, entry := range entries {
		id, isDescriptor := strings.CutSuffix(entry.Name(), descriptorExt)
		if !isDescriptor || !entry.Type().IsRegular() {
			continue
		}

		set, err := completeSet(root, id)
		if errors.Is(err, errIncomplete) {
			incomplete = append(incomplete, IncompleteSet{ID: id, Reason: err.Error()})

			continue
		}

		if err != nil {
			return nil, nil, err
		}

		if newer(set, newest) {
			newest = set
		}
	}

	return newest, incomplete, nil
}

// newer reports whether set was taken after newest, or at the same time
// under a later id; any set is newer than none.
func newer(set, newest *Set) bool {
	if newest == nil || set.Taken.After(newest.Taken) {
		return true
	}

	return set.Taken.Equal(newest.Taken) && set.ID > newest.ID
}

// completeSet returns set id when its completion record vouches for it: the
// record is there and names the set, every file it lists is there with the
// size it records, and each partition of the descriptor has chunks among them.
// Otherwise the error wraps errIncomplete, unless the set could not be read at
// all (permission denied, say), which tells nothing about it.
func completeSet(root *os.Root, id string) (*Set, error) {
	incomplete := func(reason string, args ...any) (*Set, error) {
		return nil, fmt.Errorf("%w: %s", errIncomplete, fmt.Sprintf(reason, args...))
	}

	data, err := root.ReadFile(id + completionExt)
	if errors.Is(err, fs.ErrNotExist) {
		return incomplete("no completion record")
	}

	if err != nil {
		return nil, fmt.Errorf("backup: reading the completion record of set %s: %w", id, err)
	}

	var record Completion
	if err := json.Unmarshal(data, &record); err != nil {
		return incomplete("unreadable completion record")
	}

	if record.Version != completionVersion || record.ID != id {
		return incomplete("completion record of another set or version")
	}

	listed := make([]string, 0, len(record.Files))

	for _, file := range record.Files {
		info, err := root.Stat(file.Name)
		if errors.Is(err, fs.ErrNotExist) {
			return incomplete("%s missing", file.Name)
		}

		if err != nil {
			return nil, fmt.Errorf("backup: inspecting %s: %w", file.Name, err)
		}

		if !info.Mode().IsRegular() || info.Size() != file.Bytes {
			return incomplete("%s is %d bytes, %d recorded", file.Name, info.Size(), file.Bytes)
		}

		listed = append(listed, file.Name)
	}

	if !slices.Contains(listed, id+descriptorExt) {
		return incomplete("completion record does not list the descriptor")
	}

	return describedSet(root, id, listed)
}

// describedSet reads the descriptor of set id and returns the set, checking
// that each partition it describes has its first chunk among the listed files.
func describedSet(root *os.Root, id string, listed []string) (*Set, error) {
	data, err := root.ReadFile(id + descriptorExt)
	if err != nil {
		return nil, fmt.Errorf("backup: reading the descriptor of set %s: %w", id, err)
	}

	img, err := redo.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: unreadable descriptor", errIncomplete)
	}

	for _, dev := range img.Parts.Keys() {
		if first := id + "_" + dev + "_001.img"; !slices.Contains(listed, first) {
			return nil, fmt.Errorf("%w: completion record lists no chunk of %s", errIncomplete, dev)
		}
	}

	taken, err := time.Parse(time.RFC1123Z, img.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("%w: descriptor timestamp %q unreadable", errIncomplete, img.Timestamp)
	}

	return &Set{ID: id, Taken: taken}, nil
}

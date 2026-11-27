// SPDX-License-Identifier: EUPL-1.2

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// newChunk is what the fake split writes into every chunk.
const newChunk = "new chunk"

// splitChunks stands in for split in the fake runner: for each imaging
// pipeline it writes n chunks at the prefix the split stage names.
func splitChunks(n int) func([]run.Command) error {
	return func(stages []run.Command) error {
		split := stages[len(stages)-1]
		prefix := split.Args[len(split.Args)-1]

		for i := 1; i <= n; i++ {
			if err := os.WriteFile(fmt.Sprintf("%s%03d.img", prefix, i), []byte(newChunk), 0o600); err != nil {
				return fmt.Errorf("fake split: %w", err)
			}
		}

		return nil
	}
}

// readCompletion decodes the completion record of set id in dir.
func readCompletion(t *testing.T, dir, id string) Completion {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, id+".complete"))
	if err != nil {
		t.Fatalf("reading the completion record: %v", err)
	}

	var record Completion
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decoding the completion record %s: %v", data, err)
	}

	return record
}

// fileNames lists the names of the files in dir.
func fileNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// Outcomes fileState tells apart for a file that held earlier content.
const (
	kept      = "kept"
	removed   = "removed"
	rewritten = "rewritten"
)

// fileState reports whether the file at path still holds the earlier content,
// was removed, or now holds something else.
func fileState(t *testing.T, path, earlier string) string {
	t.Helper()

	data, err := os.ReadFile(path)

	switch {
	case errors.Is(err, os.ErrNotExist):
		return removed
	case err != nil:
		t.Fatal(err)
	case string(data) == earlier:
		return kept
	}

	return rewritten
}

func TestBackupRunWritesCompletion(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	f.OnPipeline = splitChunks(2)
	dest := onBackupDisk(f, t.TempDir())
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	rep, err := b.Run(context.Background(), baseConfig(dest))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	desc, err := os.Stat(rep.DescriptorPath)
	if err != nil {
		t.Fatal(err)
	}

	chunk := int64(len(newChunk))
	want := Completion{Version: 1, ID: "20260105", Files: []SetFile{
		{Name: "20260105.redo", Bytes: desc.Size()},
		{Name: "20260105_sda1_001.img", Bytes: chunk},
		{Name: "20260105_sda1_002.img", Bytes: chunk},
		{Name: "20260105_sda2_001.img", Bytes: chunk},
		{Name: "20260105_sda2_002.img", Bytes: chunk},
	}}

	if got := readCompletion(t, dest, "20260105"); !reflect.DeepEqual(got, want) {
		t.Errorf("completion record = %+v, want %+v", got, want)
	}

	// The record is renamed into place; no temporary file stays behind.
	for _, name := range fileNames(t, dest) {
		if name[0] == '.' {
			t.Errorf("temporary file %s left in the destination", name)
		}
	}
}

func TestBackupRunReplacesEarlierSet(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	dest := onBackupDisk(f, t.TempDir())

	// An earlier, longer set with the same id, and files that are not its
	// chunks for the partitions being imaged.
	earlier := map[string]string{
		"20260105.complete":      rewritten, // withdrawn, then written anew
		"20260105_sda1_001.img":  rewritten, // overwritten by split
		"20260105_sda1_002.img":  removed,   // stale tail of the longer image
		"20260105_sda2_003.img":  removed,   // stale tail of the longer image
		"20260105_sda3_001.img":  kept,      // a partition this run does not image
		"20260105_sda10_001.img": kept,      // another partition whose name starts alike
		"20260104_sda1_001.img":  kept,      // another set
		"20260105_sda1_001.bak":  kept,      // not a chunk
	}
	for name := range earlier {
		if err := os.WriteFile(filepath.Join(dest, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	// A dry run removes nothing.
	if _, err := b.Plan(context.Background(), baseConfig(dest)); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if got := fileNames(t, dest); len(got) != len(earlier) {
		t.Fatalf("Plan changed the destination: %v", got)
	}

	if _, err := b.Run(context.Background(), baseConfig(dest)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for name, want := range earlier {
		if got := fileState(t, filepath.Join(dest, name), "old"); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}

	files := readCompletion(t, dest, "20260105").Files

	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}

	if want := []string{"20260105.redo", "20260105_sda1_001.img", "20260105_sda2_001.img"}; !slices.Equal(names, want) {
		t.Errorf("record lists %v, want %v", names, want)
	}
}

func TestBackupRunFailureLeavesNoCompletion(t *testing.T) {
	t.Parallel()

	cases := map[string]func(f *run.FakeRunner){
		"imaging fails":   func(f *run.FakeRunner) { f.PipelineErr = errBoom },
		"no chunk output": func(f *run.FakeRunner) { f.OnPipeline = splitChunks(0) },
	}

	for name, breakRun := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := fakeRunner()
			breakRun(f)

			dest := onBackupDisk(f, t.TempDir())

			// The record of an earlier set with the same id must not survive a
			// run that replaces the set and then fails.
			record := filepath.Join(dest, "20260105.complete")
			if err := os.WriteFile(record, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}

			b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

			if _, err := b.Run(context.Background(), baseConfig(dest)); err == nil {
				t.Fatal("Run succeeded; want an error")
			}

			if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("completion record present after a failed run (%v)", err)
			}
		})
	}
}

func TestBackupRunNoChunksError(t *testing.T) {
	t.Parallel()

	f := fakeRunner()
	f.OnPipeline = splitChunks(0)
	b := &Backup{Runner: f, Inspector: disk.New(f), Clock: fixedClock(), LogDir: t.TempDir()}

	_, err := b.Run(context.Background(), baseConfig(onBackupDisk(f, t.TempDir())))
	if !errors.Is(err, errNoChunks) {
		t.Fatalf("err = %v, want errNoChunks", err)
	}
}

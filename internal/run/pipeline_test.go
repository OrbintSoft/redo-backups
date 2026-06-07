// SPDX-License-Identifier: EUPL-1.2

package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRunPipeline runs a real 3-stage pipeline (printf | cat | dd of=file) and
// checks the bytes that reach the final stage.
func TestRunPipeline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.bin")
	err := ExecRunner{}.RunPipeline(context.Background(), []Command{
		{Name: "printf", Args: []string{"hello-pipeline"}},
		{Name: "cat"},
		{Name: "dd", Args: []string{"of=" + out, "status=none"}},
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "hello-pipeline" {
		t.Errorf("pipeline output = %q, want %q", got, "hello-pipeline")
	}
}

// TestRunPipelineStdin feeds Stdin into the first stage.
func TestRunPipelineStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.bin")
	err := ExecRunner{}.RunPipeline(context.Background(), []Command{
		{Name: "cat", Stdin: []byte("from-stdin")},
		{Name: "dd", Args: []string{"of=" + out, "status=none"}},
	})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "from-stdin" {
		t.Errorf("got %q", got)
	}
}

// TestRunPipelineError reports an error when a stage fails.
func TestRunPipelineError(t *testing.T) {
	err := ExecRunner{}.RunPipeline(context.Background(), []Command{
		{Name: "false"},
	})
	if err == nil {
		t.Fatal("expected error from failing stage")
	}
}

// TestRunPipelineMissingBinary reports an error when a stage cannot start.
func TestRunPipelineMissingBinary(t *testing.T) {
	err := ExecRunner{}.RunPipeline(context.Background(), []Command{
		{Name: "this-binary-does-not-exist-xyz"},
	})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

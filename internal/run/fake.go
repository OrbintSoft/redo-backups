// SPDX-License-Identifier: EUPL-1.2

package run

import (
	"context"
	"sync"
)

// FakeRunner is a test double for Runner. It records every command it is asked
// to run and returns programmed responses, so backup/disk logic can be tested
// deterministically without touching the system.
type FakeRunner struct {
	mu sync.Mutex

	// Responses maps a command's String() form to the Result (and optional
	// error) it should return. Commands without an entry return a zero Result
	// and no error, which is convenient for fire-and-forget calls.
	Responses map[string]FakeResponse

	// Calls records, in order, every command passed to Run.
	Calls []Command

	// Pipelines records, in order, every pipeline passed to RunPipeline.
	Pipelines [][]Command

	// PipelineErr, if set, is returned by every RunPipeline call.
	PipelineErr error
}

// FakeResponse is the canned outcome for a single command.
type FakeResponse struct {
	Result Result
	Err    error
}

// NewFakeRunner returns a ready-to-use FakeRunner.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{Responses: make(map[string]FakeResponse)}
}

// AddStdout programs a successful response with the given stdout for the command
// identified by key (the Command.String() form). It returns the receiver to
// allow chaining in tests.
func (f *FakeRunner) AddStdout(key, stdout string) *FakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Responses[key] = FakeResponse{Result: Result{Stdout: []byte(stdout)}}

	return f
}

// Run implements Runner by recording the call and returning the programmed
// response, if any.
func (f *FakeRunner) Run(_ context.Context, cmd Command) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls = append(f.Calls, cmd)
	if resp, ok := f.Responses[cmd.String()]; ok {
		return resp.Result, resp.Err
	}

	return Result{}, nil
}

// RunPipeline records the pipeline and returns PipelineErr (nil by default).
func (f *FakeRunner) RunPipeline(_ context.Context, cmds []Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	clone := append([]Command(nil), cmds...)
	f.Pipelines = append(f.Pipelines, clone)

	return f.PipelineErr
}

// CommandLines returns the String() form of every recorded call, in order.
func (f *FakeRunner) CommandLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = c.String()
	}

	return out
}

// compile-time assertion that FakeRunner satisfies Runner.
var _ Runner = (*FakeRunner)(nil)

// compile-time assertion that ExecRunner satisfies Runner.
var _ Runner = ExecRunner{}

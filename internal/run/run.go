// SPDX-License-Identifier: EUPL-1.2
//
// Package run is the single seam through which the rest of the program executes
// external commands (lsblk, sfdisk, dd, partclone, pigz, split, ...). Keeping
// all process execution behind the Runner interface lets the disk/backup logic
// be unit-tested with a fake, without root privileges or real hardware.
package run

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Command describes a single external command invocation.
type Command struct {
	// Name is the executable to run (looked up in PATH).
	Name string
	// Args are the arguments passed to the executable.
	Args []string
	// Stdin, if non-nil, is written to the command's standard input.
	Stdin []byte
}

// String returns a human-readable representation of the command, useful for
// logging and test assertions. It is not shell-quoted and must not be passed to
// a shell.
func (c Command) String() string {
	if len(c.Args) == 0 {
		return c.Name
	}
	return c.Name + " " + join(c.Args)
}

// Result holds the captured output of a finished command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner executes external commands.
type Runner interface {
	// Run executes cmd and returns its captured output. A non-zero exit code is
	// reported both via Result.ExitCode and as a non-nil error, so callers may
	// inspect either.
	Run(ctx context.Context, cmd Command) (Result, error)

	// RunPipeline executes cmds connected stdout->stdin, left to right. It
	// returns an error if any stage fails.
	RunPipeline(ctx context.Context, cmds []Command) error
}

// ExecRunner is the production Runner backed by os/exec.
type ExecRunner struct{}

// Run implements Runner using the real operating system.
func (ExecRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	if cmd.Stdin != nil {
		c.Stdin = bytes.NewReader(cmd.Stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	res := Result{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	// ProcessState is nil only if the process never started (e.g. executable
	// not found); in that case there is no meaningful exit code.
	if c.ProcessState != nil {
		res.ExitCode = c.ProcessState.ExitCode()
	} else {
		res.ExitCode = -1
	}
	if err != nil {
		return res, fmt.Errorf("run %q: %w", cmd.String(), err)
	}
	return res, nil
}

func join(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

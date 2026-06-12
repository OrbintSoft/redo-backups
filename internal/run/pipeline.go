// SPDX-License-Identifier: EUPL-1.2

package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// errPipeline reports one or more failing pipeline stages; the per-stage details
// are appended by wrapping it with %w.
var errPipeline = errors.New("run pipeline")

// RunPipeline executes cmds connected stdout->stdin, left to right, like a shell
// pipeline. The first command may take Stdin bytes; the last command's stdout is
// discarded (the imaging pipeline's final stage, split, writes files itself).
// Each command's stderr is captured and surfaced if that command fails.
func (ExecRunner) RunPipeline(ctx context.Context, cmds []Command) error {
	if len(cmds) == 0 {
		return nil
	}

	procs := make([]*execProc, len(cmds))
	for i, c := range cmds {
		procs[i] = newProc(ctx, c)
	}

	if cmds[0].Stdin != nil {
		procs[0].cmd.Stdin = bytes.NewReader(cmds[0].Stdin)
	}

	// Connect consecutive stages with OS pipes so data flows between child
	// processes without copying through this process.
	var parentEnds []io.Closer

	for i := range len(procs) - 1 {
		pr, pw, err := os.Pipe()
		if err != nil {
			closeAll(parentEnds)

			return fmt.Errorf("run pipeline: creating pipe: %w", err)
		}

		procs[i].cmd.Stdout = pw
		procs[i+1].cmd.Stdin = pr
		parentEnds = append(parentEnds, pw, pr)
	}

	started := 0

	for i := range procs {
		if err := procs[i].cmd.Start(); err != nil {
			closeAll(parentEnds)
			killStarted(procs[:started])

			return fmt.Errorf("run pipeline: starting %q: %w", procs[i].desc, err)
		}

		started++
	}

	// The child processes now own their inherited copies of the pipe ends;
	// close the parent's copies so EOF propagates as each stage finishes.
	closeAll(parentEnds)

	return waitAll(procs)
}

// waitAll waits for every stage and returns a combined error naming each that
// failed (with its captured stderr), or nil if all succeeded. Reporting every
// failing stage — not just the first — surfaces the real cause when an upstream
// stage only fails with a broken pipe because a downstream stage died first.
func waitAll(procs []*execProc) error {
	var failures []string

	for _, p := range procs {
		err := p.cmd.Wait()
		if err == nil {
			continue
		}

		msg := fmt.Sprintf("%q: %v", p.desc, err)
		if stderr := strings.TrimSpace(p.stderr.String()); stderr != "" {
			msg += " (stderr: " + stderr + ")"
		}

		failures = append(failures, msg)
	}

	if len(failures) > 0 {
		return fmt.Errorf("%w: %s", errPipeline, strings.Join(failures, "; "))
	}

	return nil
}

type execProc struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
	// desc is the command's display form, captured so failure messages need not
	// index back into the caller's cmds slice.
	desc string
}

func newProc(ctx context.Context, c Command) *execProc {
	var stderr bytes.Buffer

	// #nosec G204 -- see ExecRunner.Run: command names are fixed in code and the
	// only interpolated arguments are device names validated against a strict
	// allow-list; no shell is involved.
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Stderr = &stderr

	return &execProc{cmd: cmd, stderr: &stderr, desc: c.String()}
}

func closeAll(cs []io.Closer) {
	for _, c := range cs {
		_ = c.Close()
	}
}

func killStarted(procs []*execProc) {
	for _, p := range procs {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
}

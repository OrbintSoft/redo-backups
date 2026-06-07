// SPDX-License-Identifier: EUPL-1.2

package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

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
	for i := 0; i < len(procs)-1; i++ {
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
			return fmt.Errorf("run pipeline: starting %q: %w", cmds[i].String(), err)
		}
		started++
	}

	// The child processes now own their inherited copies of the pipe ends;
	// close the parent's copies so EOF propagates as each stage finishes.
	closeAll(parentEnds)

	// Wait for every stage and collect all failures. An upstream stage often
	// fails with a broken pipe only because a downstream stage died first, so
	// reporting every failing stage (not just the first) surfaces the real cause.
	var failures []string
	for i := range procs {
		err := procs[i].cmd.Wait()
		if err == nil {
			continue
		}
		msg := fmt.Sprintf("%q: %v", cmds[i].String(), err)
		if stderr := strings.TrimSpace(procs[i].stderr.String()); stderr != "" {
			msg += " (stderr: " + stderr + ")"
		}
		failures = append(failures, msg)
	}
	if len(failures) > 0 {
		return fmt.Errorf("run pipeline: %s", strings.Join(failures, "; "))
	}
	return nil
}

type execProc struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
}

func newProc(ctx context.Context, c Command) *execProc {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Stderr = &stderr
	return &execProc{cmd: cmd, stderr: &stderr}
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

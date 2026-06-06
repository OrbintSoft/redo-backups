// SPDX-License-Identifier: EUPL-1.2
//
// Command redo-backup creates Redo Rescue-compatible backups of a running
// system, driven by a named profile under /etc/redo-backups/.
//
// This is the CLI skeleton: argument parsing and dispatch are in place, but the
// backup logic itself is implemented in later steps.
package main

import (
	"fmt"
	"os"
)

// version is the redo-backups tool version (distinct from the Redo Rescue
// on-disk format version). Overridable at build time via -ldflags.
var version = "0.0.0-dev"

const usage = `redo-backup - create Redo Rescue-compatible backups

Usage:
  redo-backup run <profile>    Run the backup described by /etc/redo-backups/<profile>.conf
  redo-backup version          Print the version and exit
  redo-backup help             Show this help

Configuration lives under /etc/redo-backups/. Each profile is a <profile>.conf
file, optionally extended by drop-ins in <profile>.conf.d/*.conf.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a single CLI invocation and returns a process exit code. It
// takes its arguments and output streams as parameters so it can be tested.
func run(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch cmd := args[0]; cmd {
	case "run":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "error: 'run' requires exactly one argument: <profile>")
			fmt.Fprint(stderr, usage)
			return 2
		}
		return cmdRun(args[1], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// cmdRun will load the profile and execute the backup. The orchestration is
// implemented in a later step; for now it reports that clearly rather than
// silently doing nothing.
func cmdRun(profile string, _, stderr *os.File) int {
	fmt.Fprintf(stderr, "error: backup execution is not implemented yet (profile %q)\n", profile)
	return 1
}

// SPDX-License-Identifier: EUPL-1.2
//
// Command redo-backup creates Redo Rescue-compatible backups of a running
// system, driven by a named profile under /etc/redo-backups/.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/OrbintSoft/redo-backups/internal/backup"
	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// version is the redo-backups tool version (distinct from the Redo Rescue
// on-disk format version). Overridable at build time via -ldflags.
var version = "0.0.0-dev"

const usage = `redo-backup - create Redo Rescue-compatible backups

Usage:
  redo-backup run <profile>     Run the backup described by a profile
  redo-backup list              List available profiles
  redo-backup show <profile>    Print a profile's resolved configuration
  redo-backup version           Print the version and exit
  redo-backup help              Show this help

Common flags:
  --config-dir <dir>            Profile directory (default /etc/redo-backups)
  --dry-run                     (run) Validate and print the plan without imaging

Profiles are "<profile>.conf" files, optionally extended by drop-ins in
"<profile>.conf.d/*.conf".
`

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch handles a single CLI invocation and returns a process exit code. It
// takes its arguments and output streams as parameters so it can be tested.
func dispatch(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch cmd := args[0]; cmd {
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "list":
		return cmdList(args[1:], stdout, stderr)
	case "show":
		return cmdShow(args[1:], stdout, stderr)
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

// cmdRun loads the named profile and executes the backup it describes.
func cmdRun(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("config-dir", config.DefaultDir, "profile directory")
	dryRun := fs.Bool("dry-run", false, "validate and print the plan without touching disks")
	// Per-setting overrides; applied only when explicitly passed.
	oDest := fs.String("dest", "", "override: destination directory")
	oDrive := fs.String("drive", "", "override: target drive (or 'auto')")
	oParts := fs.String("parts", "", "override: partitions (comma/space list, or 'auto')")
	oID := fs.String("id", "", "override: backup id")
	oNotes := fs.String("notes", "", "override: notes")
	oCompressor := fs.String("compressor", "", "override: pigz|gzip")
	oSplit := fs.String("split-size", "", "override: chunk size (e.g. 4096M)")
	oConsistency := fs.String("consistency", "", "override: consistency strategy")
	oLVMSize := fs.String("lvm-snapshot-size", "", "override: LVM snapshot size")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: 'run' requires exactly one argument: <profile>")
		return 2
	}

	cfg, err := config.Load(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	// Apply only the overrides whose flags were explicitly provided.
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "dest":
			cfg.Dest = *oDest
		case "drive":
			cfg.Drive = *oDrive
		case "parts":
			cfg.Parts = config.ParseParts(*oParts)
		case "id":
			cfg.ID = *oID
		case "notes":
			cfg.Notes = *oNotes
		case "compressor":
			cfg.Compressor = config.Compressor(*oCompressor)
		case "split-size":
			cfg.SplitSize = *oSplit
		case "consistency":
			cfg.Consistency = config.Consistency(*oConsistency)
		case "lvm-snapshot-size":
			cfg.LVMSnapshotSize = *oLVMSize
		}
	})
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	runner := run.ExecRunner{}
	b := &backup.Backup{Runner: runner, Inspector: disk.New(runner)}

	if *dryRun {
		plan, err := b.Plan(context.Background(), cfg)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		printPlan(stdout, plan)
		return 0
	}

	report, err := b.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	fmt.Fprintf(stdout, "Backup %s complete on drive %s: wrote %s (%d partition(s): %v)\n",
		report.ID, report.Drive, report.DescriptorPath, len(report.Partitions), report.Partitions)
	return 0
}

// printPlan writes a human-readable preview of a backup run (the --dry-run view).
func printPlan(w *os.File, plan *backup.Plan) {
	fmt.Fprintf(w, "DRY RUN - no disks will be touched\n\n")
	fmt.Fprintf(w, "drive:        %s\n", plan.Drive)
	fmt.Fprintf(w, "id:           %s\n", plan.ID)
	fmt.Fprintf(w, "descriptor:   %s\n", plan.DescriptorPath)
	fmt.Fprintf(w, "consistency:  %s\n", plan.Consistency)
	fmt.Fprintf(w, "partitions:   %d\n", len(plan.Partitions))
	for _, p := range plan.Partitions {
		fmt.Fprintf(w, "\n[%s] %s\n", p.Name, p.Source)
		fmt.Fprintf(w, "  %s\n", strings.Join(p.Commands, " \\\n    | "))
	}
}

// cmdList prints the available profile names, one per line.
func cmdList(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("config-dir", config.DefaultDir, "profile directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	names, err := config.ListProfiles(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintf(stderr, "no profiles found in %s\n", *dir)
		return 0
	}
	for _, n := range names {
		fmt.Fprintln(stdout, n)
	}
	return 0
}

// cmdShow prints a profile's fully-resolved configuration.
func cmdShow(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("config-dir", config.DefaultDir, "profile directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: 'show' requires exactly one argument: <profile>")
		return 2
	}

	cfg, err := config.Load(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	printConfig(stdout, cfg)
	return 0
}

// printConfig writes a profile's resolved settings in a readable key: value form.
func printConfig(w *os.File, cfg *config.Config) {
	parts := "auto"
	if !cfg.PartsAuto() {
		parts = strings.Join(cfg.Parts, ", ")
	}
	id := cfg.ID
	if id == "" {
		id = "(derived from date)"
	}
	fmt.Fprintf(w, "dest:         %s\n", cfg.Dest)
	fmt.Fprintf(w, "drive:        %s\n", cfg.Drive)
	fmt.Fprintf(w, "parts:        %s\n", parts)
	fmt.Fprintf(w, "id:           %s\n", id)
	fmt.Fprintf(w, "notes:        %s\n", cfg.Notes)
	fmt.Fprintf(w, "version:      %s\n", cfg.Version)
	fmt.Fprintf(w, "compressor:   %s\n", cfg.Compressor)
	fmt.Fprintf(w, "split_size:   %s\n", cfg.SplitSize)
	fmt.Fprintf(w, "consistency:  %s\n", cfg.Consistency)
	if cfg.Consistency == config.ConsistencyLVMSnapshot {
		fmt.Fprintf(w, "lvm_snapshot_size: %s\n", cfg.LVMSnapshotSize)
	}
}

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
	"runtime/debug"
	"strings"

	"github.com/OrbintSoft/redo-backups/internal/backup"
	"github.com/OrbintSoft/redo-backups/internal/config"
	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// version is the redo-backups tool version (distinct from the Redo Rescue
// on-disk format version). Overridable at build time via -ldflags (the release
// build sets -X main.version=...); see resolveVersion for the runtime fallback.
var version = "0.0.0-dev"

// resolveVersion reports the tool version. It prefers the value injected at
// build time via -ldflags; when that was not set — for example when the binary
// was produced by `go install` — it falls back to the module version embedded
// in the build info, so installed builds report a meaningful version instead of
// the placeholder.
func resolveVersion() string {
	if version != "0.0.0-dev" {
		return version
	}

	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	return version
}

// Process exit codes returned by the CLI sub-commands.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

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

		return exitUsage
	}

	switch cmd := args[0]; cmd {
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "list":
		return cmdList(args[1:], stdout, stderr)
	case "show":
		return cmdShow(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, resolveVersion())

		return exitOK
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)

		return exitOK
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		fmt.Fprint(stderr, usage)

		return exitUsage
	}
}

// runOverrides holds the optional per-setting override flags for "run". A flag's
// value is applied to the loaded config only if it was explicitly passed.
type runOverrides struct {
	dest        *string
	drive       *string
	parts       *string
	id          *string
	notes       *string
	compressor  *string
	split       *string
	consistency *string
}

// registerOverrides declares the override flags on fs and returns their holder.
func registerOverrides(fs *flag.FlagSet) runOverrides {
	return runOverrides{
		dest:        fs.String("dest", "", "override: destination directory"),
		drive:       fs.String("drive", "", "override: target drive (or 'auto')"),
		parts:       fs.String("parts", "", "override: partitions (comma/space list, or 'auto')"),
		id:          fs.String("id", "", "override: backup id"),
		notes:       fs.String("notes", "", "override: notes"),
		compressor:  fs.String("compressor", "", "override: pigz|gzip"),
		split:       fs.String("split-size", "", "override: chunk size (e.g. 4096M)"),
		consistency: fs.String("consistency", "", "override: consistency strategy"),
	}
}

// applyTo copies into cfg only the overrides whose flag was explicitly set.
func (o runOverrides) applyTo(fs *flag.FlagSet, cfg *config.Config) {
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "dest":
			cfg.Dest = *o.dest
		case "drive":
			cfg.Drive = *o.drive
		case "parts":
			cfg.Parts = config.ParseParts(*o.parts)
		case "id":
			cfg.ID = *o.id
		case "notes":
			cfg.Notes = *o.notes
		case "compressor":
			cfg.Compressor = config.Compressor(*o.compressor)
		case "split-size":
			cfg.SplitSize = *o.split
		case "consistency":
			cfg.Consistency = config.Consistency(*o.consistency)
		}
	})
}

// cmdRun loads the named profile and executes the backup it describes.
func cmdRun(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("config-dir", config.DefaultDir, "profile directory")
	dryRun := fs.Bool("dry-run", false, "validate and print the plan without touching disks")
	overrides := registerOverrides(fs)

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: 'run' requires exactly one argument: <profile>")

		return exitUsage
	}

	cfg, err := config.Load(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)

		return exitFailure
	}

	overrides.applyTo(fs, cfg)

	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(stderr, "error:", err)

		return exitFailure
	}

	runner := run.ExecRunner{}
	b := &backup.Backup{Runner: runner, Inspector: disk.New(runner), Clock: nil, LogDir: ""}

	if *dryRun {
		plan, err := b.Plan(context.Background(), cfg)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)

			return exitFailure
		}

		printPlan(stdout, plan)

		return exitOK
	}

	report, err := b.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)

		return exitFailure
	}

	fmt.Fprintf(stdout, "Backup %s complete on drive %s: wrote %s (%d partition(s): %v)\n",
		report.ID, report.Drive, report.DescriptorPath, len(report.Partitions), report.Partitions)

	return exitOK
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
		return exitUsage
	}

	names, err := config.ListProfiles(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)

		return exitFailure
	}

	if len(names) == 0 {
		fmt.Fprintf(stderr, "no profiles found in %s\n", *dir)

		return exitOK
	}

	for _, n := range names {
		fmt.Fprintln(stdout, n)
	}

	return exitOK
}

// cmdShow prints a profile's fully-resolved configuration.
func cmdShow(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stderr)

	dir := fs.String("config-dir", config.DefaultDir, "profile directory")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: 'show' requires exactly one argument: <profile>")

		return exitUsage
	}

	cfg, err := config.Load(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)

		return exitFailure
	}

	printConfig(stdout, cfg)

	return exitOK
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
}

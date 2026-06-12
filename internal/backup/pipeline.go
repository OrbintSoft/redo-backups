// SPDX-License-Identifier: EUPL-1.2

// Package backup orchestrates a Redo Rescue-compatible backup: it builds the
// per-partition imaging pipelines and the ".redo" descriptor. Command execution
// lives behind run.Runner; this file only constructs the commands as typed data
// so the exact pipeline can be asserted in tests.
package backup

import (
	"path/filepath"

	"github.com/OrbintSoft/redo-backups/internal/disk"
	"github.com/OrbintSoft/redo-backups/internal/run"
)

// Pipeline is the ordered set of commands that image one partition. The stages
// are connected stdout->stdin in order; the final stage (split) writes the
// ".img" chunks itself.
type Pipeline struct {
	// Dev is the partition device name (e.g. "sda1").
	Dev string
	// Stages are the commands, piped left to right.
	Stages []run.Command
}

// partcloneStage builds the partclone command for a partition. tool is the
// suffix from disk.FSTool; for the "dd" fallback the binary is partclone.dd and
// the --clone flag is omitted, matching Redo Rescue. sourceDevice is the device
// path to read (the original partition, or a snapshot device).
func partcloneStage(tool, sourceDevice, logfile string) run.Command {
	args := []string{}
	if tool != disk.DDTool {
		args = append(args, "--clone")
	}

	args = append(args,
		"--force",
		"--UI-fresh", "1",
		"--logfile", logfile,
		"--source", sourceDevice,
		"--no_block_detail",
	)

	return run.Command{Name: "partclone." + tool, Args: args}
}

// compressorStage builds the compression command (pigz or gzip), reading stdin
// and writing the compressed stream to stdout.
func compressorStage(compressor string) run.Command {
	return run.Command{Name: compressor, Args: []string{"--stdout"}}
}

// splitStage builds the split command that slices the compressed stream into
// fixed-size ".img" chunks named "<id>_<dev>_NNN.img".
func splitStage(splitSize, outDir, id, dev string) run.Command {
	prefix := filepath.Join(outDir, id+"_"+dev+"_")

	return run.Command{Name: "split", Args: []string{
		"--numeric-suffixes=1",
		"--suffix-length=3",
		"--additional-suffix=.img",
		"--bytes=" + splitSize,
		"-",
		prefix,
	}}
}

// PartitionPipeline assembles the full imaging pipeline for one partition.
// sourceDevice is the device path partclone reads from (the original partition,
// or a snapshot device when a snapshot strategy is in use); the output chunks are
// always named after the original partition (part.Name).
func PartitionPipeline(part disk.Partition, sourceDevice, compressor, splitSize, logfile, outDir, id string) Pipeline {
	tool := disk.FSTool(part.FS)

	return Pipeline{
		Dev: part.Name,
		Stages: []run.Command{
			partcloneStage(tool, sourceDevice, logfile),
			compressorStage(compressor),
			splitStage(splitSize, outDir, id, part.Name),
		},
	}
}

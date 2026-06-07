# redo-backups

[![CI](https://github.com/OrbintSoft/redo-backups/actions/workflows/ci.yml/badge.svg)](https://github.com/OrbintSoft/redo-backups/actions/workflows/ci.yml)
[![Release](https://github.com/OrbintSoft/redo-backups/actions/workflows/release.yml/badge.svg)](https://github.com/OrbintSoft/redo-backups/actions/workflows/release.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/OrbintSoft/redo-backups/badges/coverage.json)](https://github.com/OrbintSoft/redo-backups/actions/workflows/coverage.yml)
[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue.svg)](LICENSE)

Create **live** disk/partition backups that are **100% compatible with
[Redo Rescue](https://redorescue.com)**, so they can be restored from the official Redo
Rescue live CD.

Redo Rescue normally runs offline from its live CD. The goal of this project is to produce
the **exact same on-disk format** (`.redo` metadata + split, gzip-compressed `partclone`
images) from a **running system**, driven by a saved configuration — so you can take a
backup on demand without rebooting into the live CD, and still restore it with Redo Rescue
if disaster strikes.

> ⚠️ **Status: early development.** The format and tooling are being built up step by step.
> Do not rely on this for critical backups yet.

## Why

- Take a Redo Rescue-compatible backup of a live machine (primary target: **Gentoo**, but
  kept distribution-independent).
- Drive it from a config file under `/etc/redo-backups/`, runnable manually.
- Keep restores working from the stock Redo Rescue live CD — no custom restore tooling
  required.

## Goals

1. A command that, given a saved profile under `/etc/redo-backups/`, runs a backup
   manually.
2. Output byte-format-compatible with Redo Rescue `version 4.0.0`.
3. Configurable data-consistency strategy for live filesystems (see below).
4. Distribution-independent; relies only on standard tools (`partclone`, `pigz`, `split`,
   `sfdisk`, `dd`, `lsblk`).

## Backup format

The produced backup matches Redo Rescue: a JSON `.redo` descriptor plus one or more
`*_<dev>_NNN.img` chunks per partition (4 GiB `partclone` images piped through `pigz` and
split). The format is documented in [docs/redo-format.md](docs/redo-format.md).

## Configuration

Backups are configured under `/etc/redo-backups/`:

- one profile per file: `/etc/redo-backups/<profile>.conf`;
- optional per-profile drop-in directory `/etc/redo-backups/<profile>.conf.d/*.conf`,
  applied in lexical order, with later files overriding earlier ones and the base profile.

A ready-to-edit profile with every setting documented is provided under
[examples/etc/redo-backups/](examples/etc/redo-backups/).

All settings and the consistency strategies (`none` and `fsfreeze` are implemented;
`lvm-snapshot` is implemented for LV devices; `btrfs-snapshot` and `reboot-offline` are
not yet) are documented in [docs/redo-format.md](docs/redo-format.md) and in the example
configuration.

## Quick start

Build the binary (Go 1.26+):

```sh
go build -o redo-backup ./cmd/redo-backup
```

Install a profile and run it (imaging needs root):

```sh
sudo install -D -m 0644 examples/etc/redo-backups/example.conf /etc/redo-backups/example.conf
sudo editor /etc/redo-backups/example.conf      # set 'dest', pick a consistency strategy
sudo ./redo-backup run example
```

On success the destination directory contains `<id>.redo` and the `<id>_<dev>_NNN.img`
chunks, restorable from the Redo Rescue live CD.

## Commands

```sh
redo-backup list                       # list available profiles
redo-backup show <profile>             # print a profile's resolved config
redo-backup run <profile>              # run the backup
redo-backup run <profile> --dry-run    # preview the plan, touch nothing
redo-backup version | help
```

Useful flags for `run` (also available where relevant):

- `--config-dir <dir>` — use a profile directory other than `/etc/redo-backups`.
- `--dry-run` — validate and print the imaging plan without touching disks.
- Per-setting overrides: `--dest`, `--drive`, `--parts`, `--id`, `--notes`,
  `--compressor`, `--split-size`, `--consistency`, `--lvm-snapshot-size`. These
  override the profile and are re-validated. Example:

```sh
redo-backup run nightly --dest /mnt/usb --consistency fsfreeze --dry-run
```

## Requirements

- Go (to build) — produces a single static binary.
- Runtime tools on the target system: `partclone.*`, `pigz`, `split`, `coreutils`,
  `util-linux` (`sfdisk`, `lsblk`, `wipefs`), and snapshot tooling for the chosen
  consistency strategy (`fsfreeze`, LVM, or Btrfs).

## Licence

[EUPL-1.2](LICENSE). © 2026 Stefano Balzarotti (Orbintsoft), with the support of Claude
agent AI.

## Acknowledgements

Backup/restore format and command semantics are derived from
[Redo Rescue](https://github.com/redorescue/redorescue) (© Zebradots Software, GPLv3),
studied for interoperability. This project is an independent, compatible implementation
and is not affiliated with or endorsed by Redo Rescue.

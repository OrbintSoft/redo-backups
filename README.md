# redo-backups

[![CI](https://github.com/OrbintSoft/redo-backups/actions/workflows/ci.yml/badge.svg)](https://github.com/OrbintSoft/redo-backups/actions/workflows/ci.yml)
[![Release](https://github.com/OrbintSoft/redo-backups/actions/workflows/release.yml/badge.svg)](https://github.com/OrbintSoft/redo-backups/actions/workflows/release.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/OrbintSoft/redo-backups/badges/coverage.json)](https://github.com/OrbintSoft/redo-backups/actions/workflows/coverage.yml)
[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue.svg)](LICENSE)
[![Sponsor](https://img.shields.io/badge/Sponsor-OrbintSoft-ea4aaa?logo=githubsponsors&logoColor=white)](https://github.com/sponsors/OrbintSoft)

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

### Consistency strategies

Imaging a live, mounted filesystem can capture an inconsistent state, so the strategy is
configurable (`consistency =` in the profile):

- **`none`** — image the device as-is. Fast, no requirements, but not crash-consistent.
- **`fsfreeze`** — freeze the partition's filesystem (`fsfreeze -f`) while imaging, then
  thaw it. Crash-consistent; briefly blocks writes. Use this for directly-formatted
  partitions, **including btrfs**.
- **`lvm`** — for an LVM physical-volume partition. See the note below.

#### Why `lvm` works the way it does

A backup is only useful if the **Redo Rescue live CD can restore it**, and Redo Rescue
restores at the **partition** level: it recreates the partition table and writes each
per-partition image back to `/dev/<part>`. It has no concept of logical volumes. Imaging
LVs individually (e.g. with LVM snapshots) would produce images Redo Rescue **cannot**
restore.

So for an LVM PV partition, `redo-backups` keeps the same unit Redo Rescue uses — the
**whole PV partition, imaged raw** — and makes it consistent by **freezing every mounted
filesystem on the PV's logical volumes** for the duration of imaging, then thawing them.
The result is crash-consistent *and* restorable from the live CD. The cost is a full-size
raw image (no free-space skipping) and a brief simultaneous write-freeze of those
filesystems.

There is intentionally **no** `btrfs-snapshot` (a btrfs snapshot is a subvolume, not a
block device — use `fsfreeze`) and **no** offline/reboot mode (that is just what the Redo
Rescue live CD already does).

These strategies are documented in full in [docs/redo-format.md](docs/redo-format.md) and
the [example configuration](examples/etc/redo-backups/example.conf).

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
  `--compressor`, `--split-size`, `--consistency`. These override the profile and are
  re-validated. Example:

```sh
redo-backup run nightly --dest /mnt/usb --consistency fsfreeze --dry-run
```

## Requirements

- Go (to build) — produces a single static binary.
- Runtime tools on the target system: `partclone.*`, `pigz`, `split`, `coreutils`,
  `util-linux` (`sfdisk`, `lsblk`, `blockdev`, `findmnt`, `fsfreeze`). The `fsfreeze` and
  `lvm` strategies use `fsfreeze`; nothing extra beyond `util-linux` is required.

## Sponsoring

If this project is useful to you, please consider supporting its development:

- [GitHub Sponsors](https://github.com/sponsors/OrbintSoft)
- [PayPal](https://paypal.com/orbintsoft)

This project would not exist without **[Redo Rescue](https://redorescue.com)**, whose
backup/restore format it implements. Redo Rescue is an independent third-party project
(not mine and not managed by me) — please consider supporting it too. Redo Rescue accepts
Bitcoin donations at:

```
1redoitC6r8JhUSrt5sVvF3ZEMj1kFyq2
```

## Licence

[EUPL-1.2](LICENSE). © 2026 Stefano Balzarotti (Orbintsoft), with the support of Claude
agent AI. See [COPYRIGHT.md](COPYRIGHT.md) for the copyright notice.

## Acknowledgements

Backup/restore format and command semantics are derived from
[Redo Rescue](https://github.com/redorescue/redorescue) (© Zebradots Software, GPLv3),
studied for interoperability. This project is an independent, compatible implementation
and is not affiliated with or endorsed by Redo Rescue.

# redo-backups

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

Each backup is then run by profile name (planned CLI):

```sh
redo-backup run <profile>
```

All settings and the consistency strategies (`none`, `fsfreeze`, `lvm-snapshot`,
`btrfs-snapshot`, and the planned `reboot-offline`) are documented in
[docs/redo-format.md](docs/redo-format.md) and in the example configuration under
`examples/`.

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

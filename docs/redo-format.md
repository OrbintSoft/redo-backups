# Redo Rescue backup format (`.redo` + `*.img`)

This document describes the on-disk backup format used by Redo Rescue, as understood for
interoperability. `redo-backups` produces output that matches this format so it can be
restored by the stock Redo Rescue live CD.

The reference is Redo Rescue format **version `4.0.0`**. The semantics were derived from
the upstream project (`redorescue/redorescue`, GPLv3) and confirmed against real backups.
All examples below use **synthetic, sanitized values** — no real UUIDs, labels, or
hostnames.

## File layout

A single backup is a set of files sharing one `<id>` prefix, written to one destination
directory:

```
<id>.redo                      # JSON descriptor (metadata + MBR + partition table)
<id>_<dev>_001.img             # partclone image of <dev>, chunk 1
<id>_<dev>_002.img             # chunk 2 (if the image exceeds the split size)
...
<id>_<otherdev>_001.img        # next partition
...
```

- `<id>` is the backup identifier, by default the date `YYYYMMDD` (e.g. `20260105`).
- `<dev>` is the **partition** device name without `/dev/` (e.g. `sda1`, `nvme0n1p2`).
- Chunks are numbered starting at `001`, zero-padded to 3 digits.

Example for a drive with four partitions:

```
20260105.redo
20260105_sda1_001.img
20260105_sda2_001.img
20260105_sda4_001.img  20260105_sda4_002.img  ... (large partition, multiple chunks)
20260105_sda5_001.img  20260105_sda5_002.img  ...
```

## The `.redo` descriptor (JSON)

UTF-8 JSON object. Top-level keys, in the order written by the reference implementation:

| Key           | Type    | Meaning |
|---------------|---------|---------|
| `id`          | string  | Backup identifier (e.g. `"20260105"`). |
| `version`     | string  | Format version, e.g. `"4.0.0"`. |
| `timestamp`   | string  | Creation time, RFC-2822 (PHP `date('r')`), e.g. `"Mon, 05 Jan 2026 18:15:11 +0000"`. |
| `notes`       | string  | Free-text note for the backup. |
| `drive_bytes` | integer | Total size of the **whole drive** in bytes. |
| `parts`       | object  | Map of partition device name → partition metadata (see below). |
| `mbr_bin`     | string  | base64 of the first **32768** bytes of the drive (MBR + GPT primary area). |
| `sfd_bin`     | string  | base64 of `sfdisk --dump /dev/<drive>` output. |

### `parts` entries

`parts` is an object keyed by partition device name (e.g. `"sda1"`). Each value:

| Key     | Type    | Meaning |
|---------|---------|---------|
| `bytes` | integer | Partition size in bytes. |
| `size`  | string  | Human-readable size as reported by `lsblk` (e.g. `"127M"`). |
| `type`  | string  | Partition type description (e.g. `"EFI System"`). |
| `fs`    | string  | Filesystem type (e.g. `"vfat"`, `"ext4"`, `"btrfs"`). |
| `desc`  | string  | Description: filesystem label and/or detected OS, space-joined (may be empty). |

### Sanitized example

```json
{
  "id": "20260105",
  "version": "4.0.0",
  "timestamp": "Mon, 05 Jan 2026 18:15:11 +0000",
  "notes": "Example backup",
  "drive_bytes": 512110190592,
  "parts": {
    "sda1": { "bytes": 133169152, "size": "127M", "type": "EFI System", "fs": "vfat", "desc": "ESP" },
    "sda2": { "bytes": 299892736, "size": "286M", "type": "Linux filesystem", "fs": "ext4", "desc": "boot" }
  },
  "mbr_bin": "<base64 of first 32768 bytes>",
  "sfd_bin": "<base64 of `sfdisk --dump` text>"
}
```

`sfd_bin`, once base64-decoded, is a standard `sfdisk` dump:

```
label: gpt
label-id: 00000000-0000-0000-0000-000000000000
device: /dev/sda
unit: sectors
first-lba: 34
last-lba: 1000215182
sector-size: 512

/dev/sda1 : start=...,  size=...,  type=...,  uuid=...
...
```

> The `label-id`, per-partition `uuid`, and labels are machine-specific. Treat them as
> sensitive; never commit real values to this repository.

## The `*.img` partition images

Each partition image is produced by piping `partclone` through `pigz` (gzip-compatible)
and splitting into fixed-size chunks:

```
partclone.<tool> --clone --force --UI-fresh 1 --logfile <log> \
    --source /dev/<dev> --no_block_detail \
  | pigz --stdout \
  | split --numeric-suffixes=1 --suffix-length=3 --additional-suffix=.img \
          --bytes=4096M - <dir>/<id>_<dev>_
```

- Each `.img` chunk is therefore raw **gzip** data; concatenating the chunks in order and
  decompressing yields the original `partclone` image stream.
- Split size is **4096M** = 4 × 1024 × 1024 × 1024 = 4 GiB per chunk.
- For filesystems `partclone` cannot clone, the tool falls back to **`partclone.dd`**
  (partclone's raw dd-like mode, not coreutils `dd`) and the `--clone` flag is omitted,
  producing a raw, full-size image.

### Filesystem → `partclone` tool

| Filesystem (matched, case-insensitive) | Tool            |
|-----------------------------------------|-----------------|
| `btrfs`                                 | `partclone.btrfs` |
| `exfat`                                 | `partclone.exfat` |
| `ext` (ext2/3/4)                        | `partclone.extfs` |
| `f2fs`                                  | `partclone.f2fs`  |
| `fat` (incl. vfat)                      | `partclone.fat`   |
| `hfs`                                   | `partclone.hfsp`  |
| `minix`                                 | `partclone.minix` |
| `nilfs`                                 | `partclone.nilfs2` |
| `ntfs`                                  | `partclone.ntfs`  |
| `reiser`                                | `partclone.reiser4` |
| `xfs`                                   | `partclone.xfs`   |
| anything else                           | `partclone.dd` (raw, no `--clone`) |

## Restore (baremetal) — for reference

Redo Rescue restores a whole-drive ("baremetal") backup in this order:

1. Unmount everything on the target drive.
2. `wipefs --all --force /dev/<drive>`
3. `dd if=<mbr> of=/dev/<drive> bs=32768 count=1` (restore the 32 KiB header)
4. `sync`
5. `sfdisk --force /dev/<drive> < <sfd>` (restore the partition table)
6. `partprobe /dev/<drive>`
7. For each partition:
   `cat <id>_<dev>_??*.img | pigz --decompress --stdout \
      | partclone.<tool> --restore --force --UI-fresh 1 --logfile <log> \
            --overwrite /dev/<dst> --no_block_detail`

`redo-backups` only needs to produce the artifacts in steps above; the restore itself is
performed by the Redo Rescue live CD. The restore sequence is documented here so the
produced format can be validated end-to-end.

## Live-backup consistency strategies

Backing up a **mounted, actively-written** filesystem with `partclone` can capture an
inconsistent state. `redo-backups` makes the strategy configurable:

| Strategy   | How it works | Trade-off |
|------------|--------------|-----------|
| `none`     | Read the live device as-is. | Fast, no requirements; **not crash-consistent** — only safe for idle/read-mostly filesystems. |
| `fsfreeze` | `fsfreeze -f` the partition's mount, image it, `fsfreeze -u` afterwards. Unmounted targets are imaged directly. | Crash-consistent; **blocks writes** to that filesystem during imaging. The right choice for directly-formatted partitions, **including btrfs**. |
| `lvm`      | For an LVM physical-volume partition: freeze every mounted filesystem on the PV's logical volumes, then image the **PV partition raw** (`partclone.dd`), then thaw. | Crash-consistent and **restorable by Redo Rescue** (the imaged unit is the partition, exactly what Redo Rescue restores). Image is full-size (raw, no free-space skipping) and all those filesystems are briefly write-frozen together. |

### Why no LVM snapshots, and why not a "btrfs snapshot"?

This tool images **block devices** (partitions) so that backups stay restorable by the
stock Redo Rescue live CD, whose baremetal restore only recreates the partition table and
writes each per-partition image back to `/dev/<part>` — it has **no concept of logical
volumes or subvolumes**.

- An **LVM snapshot** would let us image a *logical volume*, but an LV-level image is not
  restorable by Redo Rescue (after restoring the partition table there is no VG/LV to
  write it to). Redo Rescue instead images the whole **PV partition raw**. The `lvm`
  strategy therefore keeps that raw-PV unit and adds consistency by freezing the LV
  filesystems — see [internal/snapshot/lvm.go](../internal/snapshot/lvm.go).
- A **btrfs snapshot** is a subvolume (a filesystem object), not a block device, so it
  cannot be fed to `partclone`. For btrfs, use `fsfreeze`.
- An **offline/reboot** strategy would just replicate what the Redo Rescue live CD already
  does, so it is intentionally not provided.

Configuration keys for these strategies are documented alongside the example
configuration under [`examples/`](../examples/etc/redo-backups/example.conf).

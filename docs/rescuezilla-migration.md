<!-- SPDX-License-Identifier: EUPL-1.2 -->
# Migration analysis: Redo Rescue → Rescuezilla (live backup, USB restore)

Status: **planning note for the future** (no code change implied yet). Written 2026-06-21.

## 1. Current architecture and why it exists

The setup deliberately splits the two halves of "backup & restore" across two tools,
because they have opposite constraints:

- **Backup = online / unattended.** Done by *this* Go tool (`redo-backups`) against the
  **live, running** system disk (`sde`), producing the Redo Rescue `.redo` format (see
  [redo-format.md](redo-format.md)). The whole reason this project exists is to avoid
  having to reboot into a rescue USB just to take a backup.
- **Restore = offline / from USB.** Done by booting the **Redo Rescue live CD/USB** and
  doing a baremetal restore of a `.redo` backup. Restoring a live root in place is not a
  thing you want to do anyway, so "restore from USB" is fine and is kept as-is.

So the artifact contract is one-directional: the Go tool must keep **producing images that
the USB restore environment can read**. Today that environment is Redo Rescue and the
format is `.redo`. The migration question is: *what happens to that contract when the USB
restore environment changes?*

## 2. Why a migration is coming

- **Redo Rescue upstream** is effectively stalled, and the most active community fork
  (`mk2driver/redorescue`, Redo Rescue 5) was evaluated and **rejected**:
  - It is a one-person, 154-micro-commit fork, **unmaintained since Oct 2024**, with
    leftover debug code (`/tmp/diag.txt` write, commented-out `rm -rf /tmp/*`).
  - It introduces **format drift** away from the `.redo` 4.0.0 we target: zstd as the
    default compressor with a new `compression` field in the descriptor, and an MBR blob
    of **32256** bytes (63 sectors) instead of **32768**. Adopting it would mean tracking
    an unmaintained, diverging format.
  - Net: it does not solve anything we care about and adds maintenance risk.
- **Rescuezilla** is the realistic long-term destination for the USB restore role:
  maintained (GPL-3.0, Python + partclone, ~2.6k stars, **v2.6.2 / May 2026**), descends
  from the same Redo Backup lineage, supports **md RAID, LVM, and no-partition-table**
  disks, images only used space (partclone), and since **2.5.1 (Sep 2024) has a CLI**.

Conclusion: sooner or later the USB restore environment becomes **Rescuezilla**, "volente o
nolente". This document is about doing that switch on purpose instead of being forced.

## 3. The core problem: format incompatibility

Rescuezilla's native format is **Clonezilla's**, which is structurally different from
`.redo`. Rescuezilla advertises interop with "all known open-source imaging frontends
including Clonezilla" and with VM disk formats, but **does not advertise Redo Rescue
support**. Assume, until proven otherwise in a VM, that **Rescuezilla cannot restore our
`.redo` backups**.

| Aspect | `.redo` (what we produce now) | Clonezilla / Rescuezilla (target) |
|---|---|---|
| Container | One JSON descriptor `<id>.redo` + flat `<id>_<dev>_NNN.img` chunks, all in one shared dir | **One directory per image**, full of sidecar files |
| Partition images | `<id>_<dev>_NNN.img`, numeric `001…`, always **gzip** stream | `<dev>.<fs>-ptcl-img.<comp>.<aa,ab…>` — fs + compressor encoded in the name, **alphabetic** split suffixes |
| Partition table | `sfd_bin` (base64 `sfdisk --dump`) + `mbr_bin` (first 32768 B) **inside the JSON** | Separate files: `<disk>-pt.sf` (sfdisk), `<disk>-mbr` (dd of MBR area), `<disk>-gpt*` (sgdisk GPT backup), `<disk>-pt.parted` |
| Metadata | JSON keys: `id, version, timestamp, notes, drive_bytes, parts{…}` | Many text files: `parts`, `disk`, `blkdev.list`, `blkid.list`, `dev-fs.list`, `Info-*.txt`, `swappt-<part>.info`, EFI NVRAM, LVM/RAID dumps |
| Compression | gzip (pigz) only | gz / **zstd** / xz / lz4 / … (token recorded in the image filename) |
| Engine | partclone `--clone` \| pigz \| split | partclone \| compressor \| split (**same engine**, different packaging) |

The important good news: the **imaging engine is the same** (partclone reading used blocks,
piped through a compressor, split into volumes). What differs is the **packaging**: a
directory of sidecar metadata files plus a different naming convention. So this is a
bounded, well-understood change — not a rewrite of the imaging core.

## 4. What this tool must grow: a Clonezilla-compatible output format

Add a second **output format** to the Go tool, selected by config (keeps rule 5 of
[CLAUDE.md](../CLAUDE.md): testable, configurable, parametrizable). Proposed key:
`output_format = redo | clonezilla` (default stays `redo` until the switch).

What is **reusable** as-is:

- The fs → partclone-tool mapping and the `partclone | compressor | split` pipeline
  (see `PartitionPipeline` in [internal/backup/run.go](../internal/backup/run.go)).
- The consistency strategies (`none` / `fsfreeze` / `lvm`) — orthogonal, see §6.

What is **new** (a "Clonezilla packager"):

| Clonezilla artifact | Source of the data in this tool | Notes |
|---|---|---|
| image **directory** `<id>/` | new — replaces the flat shared-dir layout | one dir per backup |
| `<part>.<fs>-ptcl-img.<comp>.aa,ab…` | existing partclone+compressor stream, **re-named** + alphabetic split | confirm exact compressor token (`gz`/`zst`) per target version |
| `parts`, `disk` | partition/drive selection we already compute | plain text lists |
| `<disk>-pt.sf` | the `sfdisk --dump` we already capture as `sfd_bin` | write to a file instead of base64-in-JSON |
| `<disk>-mbr` | the MBR area we already capture as `mbr_bin` | Clonezilla dumps a larger MBR/gap area; confirm size |
| `<disk>-gpt*` (sgdisk) | **not currently captured** — would need `sgdisk --backup` | needed for GPT disks; **new dependency `gdisk`** (document per rule 8) |
| `blkdev.list`, `blkid.list`, `dev-fs.list` | from `lsblk`/`blkid` we already shell out to | straightforward |
| `swappt-<part>.info` | swap partitions (we currently exclude swap) | UUID/label instead of an image |
| `Info-*.txt`, EFI NVRAM, LVM/RAID dumps | partly new | start with the minimum Rescuezilla actually requires to restore |

Decision points to settle when implementing:

1. **Compressor**: stay on gzip (simplest, matches today) or move to **zstd** (faster,
   smaller; Rescuezilla supports it). If zstd, add it to [DEPENDENCIES.md](../DEPENDENCIES.md).
2. **GPT backup**: GPT disks likely need `sda-gpt*` via `sgdisk` (new `gdisk` dependency).
   Validate whether `sda-pt.sf` alone is enough for our disks.
3. **Minimum viable sidecar set**: do not reproduce every Info-*.txt blindly — find the
   smallest set Rescuezilla needs to perform a clean baremetal restore, and generate
   exactly that. Validate empirically in a VM.

**Do not** try to keep `.redo` and Clonezilla in lockstep forever. Once the USB restore
env is Rescuezilla, `clonezilla` becomes the primary output and `redo` can be frozen or
dropped.

## 5. Migrating the historical `.redo` archive

The existing `/mnt/backup` archive (Feb–Jun 2026 `.redo` backups) will **not** be natively
restorable by Rescuezilla. Options, in order of preference:

- **A. Keep a Redo Rescue (or the fork) USB around for legacy restore.** Cheapest and
  safest interim: the old archive stays restorable by the tool that wrote it; new backups
  go to the new format. Recommended default.
- **B. Offline converter `.redo → clonezilla`.** Feasible because the **partclone payload
  is identical** (gzip partclone stream): re-split/rename the chunks into Clonezilla
  naming and synthesize the sidecar files from the JSON (`sfd_bin → <disk>-pt.sf`,
  `mbr_bin → <disk>-mbr`, `parts → parts`/`dev-fs.list`, etc.). Risk: missing metadata
  Clonezilla expects (notably the **sgdisk GPT backup**, which `.redo` never stored).
  **Must be proven by an actual restore in a VM** before trusting it.
- **C. Re-bake.** At cutover, take one fresh full backup in the new format and **retire**
  the old archive (keep a Redo Rescue USB only for emergencies). Pragmatic if the old
  history is not precious.

**Golden rule:** never delete or overwrite an old `.redo` backup until a restore of the
replacement has been demonstrated end-to-end in a VM. Backups you cannot restore are not
backups.

## 6. The freeze problem is orthogonal to all of this

The "whole machine frozen for ~22 min while imaging the live root" issue (fsfreeze on a
mounted root, see the `live-backup-gaps` note) is **independent** of the output format.
It is about *how the live block device is read consistently* (`fsfreeze` vs an LVM/dm
snapshot), not about how the resulting image is packaged. Switching to a Clonezilla output
format does **not** fix it, and fixing the freeze does not require changing the format.
Track them as two separate workstreams:

- **Format track:** add the Clonezilla packager (this document).
- **Consistency track:** dm-snapshot / LVM-snapshot to image a frozen *snapshot* and thaw
  immediately, so the live system is only blocked for a moment instead of the whole run.

## 7. Recommended roadmap

1. **Now:** keep `redo-backups` (live `.redo`) + Redo Rescue USB restore. Do **not** adopt
   the mk2driver fork. No action forced.
2. **Before any cutover — validate assumptions in a VM:** confirm (a) Rescuezilla genuinely
   cannot restore a `.redo` backup, and (b) which exact Clonezilla sidecar files its
   restore path requires for our disk layout (GPT, ext4 partitions, swap, EFI).
3. **Implement** the `output_format = clonezilla` packager behind config; reuse the
   existing imaging pipeline; add any new deps (`gdisk`/`zstd`) to DEPENDENCIES.md.
4. **Prove a full Rescuezilla restore** of a fresh `clonezilla`-format backup in a VM.
5. **Decide the archive strategy** (A/B/C above) and, only after a proven restore, switch
   the default output to `clonezilla`.
6. Address the freeze separately on the consistency track.

## 8. Open questions to resolve in a VM

- Can Rescuezilla 2.6.x restore a stock Redo Rescue 4.0.0 `.redo` backup at all?
- Exact Clonezilla artifacts required for a clean baremetal restore of our layout
  (which `Info-*`, whether `sda-gpt*` is mandatory, MBR/gap dump size).
- gzip vs zstd as the compressor token Rescuezilla expects/accepts.
- Whether the offline `.redo → clonezilla` converter (option B) actually restores cleanly,
  given `.redo` lacks the sgdisk GPT backup.

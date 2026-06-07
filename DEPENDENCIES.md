<!-- SPDX-License-Identifier: EUPL-1.2 -->
# Dependencies

This document lists every external dependency of `redo-backups`, split into what
you need to **build** it, what you need to **run** it, and the (currently empty)
set of third-party Go modules. Keep it up to date: per the project rules, any new
dependency must be added here in the same change that introduces it.

## Go modules (third-party)

**None.** The program uses only the Go standard library. `go.mod` therefore has
no `require` block. If a third-party module is ever added, list it here with the
reason it is needed.

## Build-time dependencies

Needed to compile, test, lint, and release the project.

| Tool | Purpose | Notes |
|------|---------|-------|
| **Go** ≥ 1.26 | Compile and test | Version is pinned in [go.mod](go.mod). |
| **git** | Version stamping | `make` derives the version from `git describe`. Optional; falls back to `dev`. |
| **make** | Convenience build/install | Optional; you can call `go`/`install` directly. |
| **golangci-lint** v2 (≥ 2.12) | Linting (`make lint`, CI) | Installed in CI via `go install`. v2 required for Go 1.26. |
| **GoReleaser** | Release builds & packaging | Used only by the release workflow (tar.gz, deb, rpm, snap). |
| **snapcraft** | Building the snap package | Only needed for the snap artifact in the release pipeline. |

## Runtime dependencies

Needed on the target system to **take** a backup. Imaging requires **root**.

| Tool / package | Provides | Used for |
|----------------|----------|----------|
| **partclone** | `partclone.extfs`, `partclone.fat`, `partclone.ntfs`, `partclone.btrfs`, `partclone.xfs`, `partclone.f2fs`, `partclone.exfat`, `partclone.hfsp`, `partclone.minix`, `partclone.nilfs2`, `partclone.reiser4`, `partclone.dd` | Imaging each partition (see [docs/redo-format.md](docs/redo-format.md)). Only the binaries for the filesystems you back up are required. |
| **pigz** (or **gzip**) | `pigz` / `gzip` | Compressing the image stream. `pigz` is the default; `gzip` is selectable via config. |
| **coreutils** | `split`, `dd`, `cat`, `truncate` | Splitting chunks, reading the MBR, stream handling. |
| **util-linux** | `lsblk`, `sfdisk`, `blockdev`, `findmnt`, `fsfreeze`, `wipefs`, `partprobe` | Disk discovery, partition table dump, sizes, root-drive detection, freezing, and (restore-side) wiping/partitioning. |
| **lvm2** | `lvs`, `lvcreate`, `lvremove` | Only for the `lvm-snapshot` consistency strategy. |

### Optional / not yet required

| Tool | Status |
|------|--------|
| **os-prober** | Deferred. Upstream Redo Rescue uses it to add OS names to the `desc` field; `redo-backups` currently records only the filesystem label. |
| **btrfs-progs** | Not used. The `btrfs-snapshot` strategy is not implemented; use `fsfreeze` for btrfs. |

## Integration tests (manual)

Only needed to run the Vagrant-based end-to-end suite under
[test/integration/](test/integration/); not required for normal development.

| Tool | Purpose |
|------|---------|
| **Vagrant** + a provider (**libvirt** or **VirtualBox**) | Spin up the disposable test VM (host side). |
| **python3** | Decode the `.redo` descriptor in `restore.sh` (in-VM). |
| **e2fsprogs / dosfstools / xfsprogs / btrfs-progs** | `mkfs.*` for the test layouts (in-VM). |
| **shellcheck** | Optional; lint the harness shell scripts. |

The in-VM `partclone`, `pigz`, `util-linux`, and `lvm2` are the same runtime
dependencies listed above, installed by `test/integration/provision.sh`.

## Restore

Restoring a backup is performed by the **Redo Rescue live CD**, not by this tool,
so it has no additional runtime dependency on the source system. The restore
command sequence is documented in [docs/redo-format.md](docs/redo-format.md) for
reference and validation.

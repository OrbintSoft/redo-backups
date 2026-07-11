<!-- SPDX-License-Identifier: EUPL-1.2 -->
# Integration tests (Vagrant)

End-to-end backup → restore → verify tests for redo-backups, run inside a
disposable VM because they need root and real block devices.

These tests are **expensive** and are therefore **manual only** — they are not
part of the normal CI. Run them locally with Vagrant, or trigger the
`Integration` workflow manually on GitHub.

## What it does

For each disk layout (see `ALL_LAYOUTS` in [run-tests.sh](run-tests.sh)):

1. create a loopback disk and partition/format it (GPT or MBR; ext4/vfat/xfs/…);
2. write known files and record per-file SHA-256 checksums;
3. take a backup with `redo-backup run` (consistency `none`, partitions unmounted);
4. **delete the files** and drop a marker file, simulating data loss after the
   backup;
5. **restore** the disk with the exact Redo Rescue command sequence
   ([restore.sh](restore.sh): `wipefs` → `dd` MBR → `sfdisk` → `partprobe` →
   `partclone --restore`);
6. re-mount every partition and verify the deleted files are **back** with their
   original checksums and the marker file is **gone** (proving the restore
   overwrote the live filesystem).

Restoring with the same tooling Redo Rescue uses is what validates "100%
compatible, restorable from the live CD".

There is also an **`lvm-ext4`** layout that exercises the `lvm` consistency
strategy: it builds an LVM PV partition with two mounted ext4 logical volumes,
backs it up with `consistency = lvm` (which freezes the LV filesystems and images
the PV partition raw while they stay mounted), tears the VG down, restores the raw
PV, brings the VG back, and verifies the LVs. It is skipped automatically if
`lvm2` is not installed.

And an **`fsfreeze-mixed`** layout that exercises the `fsfreeze` consistency
strategy: a vfat partition and an ext4 partition stay mounted through the
backup, so `fsfreeze -f`/`-u` actually wraps the ext4 imaging while the vfat
one — which doesn't support the FIFREEZE ioctl — is detected and imaged
directly instead of erroring (see `unfreezableFS` in
`internal/snapshot/fsfreeze.go`). Unlike the other layouts, which unmount
before backing up, this is the only one that drives fsfreeze against a real
mounted filesystem.

## Requirements

- On the **host**: [Vagrant](https://www.vagrantup.com/) with a provider
  (libvirt or VirtualBox), and `make` + Go to build the binary.
- Inside the **VM** (installed by [provision.sh](provision.sh)): `partclone`,
  `pigz`, `util-linux`, `lvm2`, `python3`, and the `mkfs.*` tools
  (`e2fsprogs`, `dosfstools`, `xfsprogs`, `btrfs-progs`). The official Debian box
  (`debian/trixie64`) is used by default; override with `REDO_ITEST_BOX`
  (`provision.sh` handles both apk- and apt-based distributions).

  Note: the suite drives the imaging/restore pipeline with `gzip` rather than the
  `pigz` production default, because `pigz` was unusable on the old Alpine box;
  `pigz` is installed but not exercised here. The `.img` format is identical.

## Usage

The easiest way is via the Makefile from the repository root:

```sh
make integration                 # build + vagrant up + run the suite
make integration LAYOUTS="gpt-ext4 mbr-ext4"   # only some layouts
make integration-destroy         # tear down the VM

# Or step by step:
make integration-up              # build the binary and boot/provision the VM
make integration-run             # run the suite in the running VM
```

Set `VAGRANT="sudo vagrant"` if your libvirt setup needs root, e.g.
`make integration VAGRANT="sudo vagrant"`.

Equivalent raw commands:

```sh
make build                       # produces bin/redo-backup (uploaded into the VM)
cd test/integration
vagrant up                       # boots, provisions, and uploads the harness
# REDO_BACKUP_BIN must point at the uploaded binary; sudo does not inherit it
# from /etc/profile.d, so pass it explicitly (this is what the Makefile does):
vagrant ssh -c 'sudo REDO_BACKUP_BIN=/opt/itest/redo-backup /opt/itest/run-tests.sh'
vagrant destroy -f               # tear down
```

The suite exits non-zero if any layout fails.

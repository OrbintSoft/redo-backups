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

## Requirements

- On the **host**: [Vagrant](https://www.vagrantup.com/) with a provider
  (libvirt or VirtualBox), and `make` + Go to build the binary.
- Inside the **VM** (installed by [provision.sh](provision.sh)): `partclone`,
  `pigz`, `util-linux`, `lvm2`, `python3`, and the `mkfs.*` tools
  (`e2fsprogs`, `dosfstools`, `xfsprogs`, `btrfs-progs`). A lightweight Alpine
  box is used by default (override with `REDO_ITEST_BOX`).

## Usage

```sh
# From the repository root:
make build                       # produces bin/redo-backup (shared into the VM)

cd test/integration
vagrant up                       # boots, provisions, and uploads the harness
vagrant ssh -c 'sudo /opt/itest/run-tests.sh'
vagrant destroy -f               # tear down

# Run only some layouts:
vagrant ssh -c 'sudo LAYOUTS="gpt-ext4 mbr-ext4" /opt/itest/run-tests.sh'
```

The suite exits non-zero if any layout fails.

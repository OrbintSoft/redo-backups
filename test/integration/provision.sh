#!/usr/bin/env sh
# SPDX-License-Identifier: EUPL-1.2
#
# Provision the integration VM with the runtime tools redo-backups needs. Works
# on Alpine (apk) and Debian/Ubuntu (apt). No Go toolchain is installed: the
# binary is built on the host and shared at /redo-backups/bin/redo-backup.
set -eu

echo "redo-backups integration provisioning..."

if command -v apk >/dev/null 2>&1; then
	# Alpine. busybox lsblk lacks JSON output, so util-linux is required.
	apk update
	apk add --no-cache \
		bash \
		partclone \
		pigz \
		gzip \
		util-linux \
		lvm2 \
		python3 \
		e2fsprogs \
		dosfstools \
		xfsprogs \
		btrfs-progs \
		coreutils
elif command -v apt-get >/dev/null 2>&1; then
	# Debian/Ubuntu.
	export DEBIAN_FRONTEND=noninteractive
	apt-get update
	apt-get install -y --no-install-recommends \
		bash \
		partclone \
		pigz \
		gzip \
		util-linux \
		lvm2 \
		python3 \
		e2fsprogs \
		dosfstools \
		xfsprogs \
		btrfs-progs \
		coreutils
else
	echo "Unsupported distribution: need apk or apt-get" >&2
	exit 1
fi

echo "Provisioned. Tool locations and versions:"
for t in partclone.ext4 partclone.extfs partclone.fat partclone.xfs partclone.dd pigz split sfdisk lsblk blockdev; do
	printf '  %-16s -> %s\n' "$t" "$(command -v "$t" 2>/dev/null || echo MISSING)"
done
echo "  split --version: $(split --version 2>&1 | head -1)"
echo "  (a busybox 'split' here would break the imaging pipeline; GNU coreutils is required)"
echo "Run the suite with: sudo /opt/itest/run-tests.sh"

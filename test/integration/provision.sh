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

echo "Provisioned. Tool versions:"
partclone.ext4 -v 2>&1 | head -1 || true
pigz --version 2>&1 | head -1 || true
lsblk --version 2>&1 | head -1 || true
echo "Run the suite with: sudo /vagrant/run-tests.sh"

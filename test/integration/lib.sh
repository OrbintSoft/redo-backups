#!/usr/bin/env bash
# SPDX-License-Identifier: EUPL-1.2
#
# Shared helpers for the redo-backups integration harness.

log() { printf '[itest] %s\n' "$*"; }
err() { printf '[itest] ERROR: %s\n' "$*" >&2; }
die() { err "$*"; exit 1; }

need_root() {
	[ "$(id -u)" -eq 0 ] || die "must run as root"
}

# wait_for_block waits until block device $1 exists, re-reading the partition
# table of its parent loop device $2 each try. Loop-device partition nodes can
# lag the partition-table write, especially on systems without udev.
wait_for_block() {
	local dev="$1" loop="$2" _
	for _ in $(seq 1 30); do
		[ -b "$dev" ] && return 0
		if [ -n "$loop" ]; then
			partprobe "$loop" 2>/dev/null || true
			partx -u "$loop" 2>/dev/null || true
		fi
		command -v udevadm >/dev/null 2>&1 && udevadm settle --timeout=2 2>/dev/null || true
		sleep 0.5
	done
	[ -b "$dev" ] || die "block device $dev never appeared"
}

# activate_vg brings volume group $1 online and waits until every named logical
# volume ($2, $3, ...) has a device node. LVM autoactivation is driven by udev
# events, which loop devices don't emit, so after a raw PV restore we rescan and
# retry vgchange/vgmknodes by hand until the nodes appear. On timeout it dumps
# the LVM state (so CI logs show *why* activation failed) and dies. Plain pvscan
# is used, not `pvscan --cache`, which is a no-op without the lvmetad daemon.
activate_vg() {
	local vg="$1"; shift
	# Device-node directory; overridable so the retry logic is testable without
	# real LVM (defaults to /dev, i.e. /dev/<vg>/<lv>).
	local dev="${LVM_DEV_PREFIX:-/dev}"
	local lv _ all
	pvscan >/dev/null 2>&1 || true
	for _ in $(seq 1 30); do
		vgchange -ay "$vg" >/dev/null 2>&1 || true
		vgmknodes "$vg" >/dev/null 2>&1 || true
		all=1
		for lv in "$@"; do [ -b "$dev/$vg/$lv" ] || all=0; done
		[ "$all" -eq 1 ] && return 0
		command -v udevadm >/dev/null 2>&1 && udevadm settle --timeout=2 2>/dev/null || true
		sleep 0.5
	done
	err "could not activate all LVs in $vg after restore; LVM state follows:"
	{ pvscan; vgs; lvs; vgchange -ay "$vg"; } 2>&1 | sed 's/^/[itest]     /' || true
	die "logical volumes in $vg never appeared after restore"
}

# fs_tool maps a filesystem name to the partclone tool suffix, mirroring
# internal/disk.FSTool (and Redo Rescue's get_fs_tool).
fs_tool() {
	case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
		*btrfs*)  echo btrfs ;;
		*exfat*)  echo exfat ;;
		*ext*)    echo extfs ;;
		*f2fs*)   echo f2fs ;;
		*fat*)    echo fat ;;
		*hfs*)    echo hfsp ;;
		*minix*)  echo minix ;;
		*nilfs*)  echo nilfs2 ;;
		*ntfs*)   echo ntfs ;;
		*reiser*) echo reiser4 ;;
		*xfs*)    echo xfs ;;
		*)        echo dd ;;
	esac
}

# checksum_tree prints "sha256  ./path" for every regular file under $1, sorted,
# so two filesystem trees can be compared byte-for-byte by content.
checksum_tree() {
	( cd "$1" && find . -type f -print0 | sort -z | xargs -0 sha256sum 2>/dev/null )
}

# write_testdata fills a mounted filesystem with deterministic-after-write data.
write_testdata() {
	local mp="$1"
	mkdir -p "$mp/data"
	head -c 1048576 /dev/urandom > "$mp/data/rand1.bin"
	head -c 524288  /dev/urandom > "$mp/data/rand2.bin"
	printf 'redo-backups integration test payload\n' > "$mp/data/hello.txt"
	sync
}

# Marker file left behind by tamper_fs; it must be gone after a restore.
TAMPER_MARKER="SHOULD_BE_GONE_AFTER_RESTORE"

# tamper_fs simulates data loss after the backup: it deletes the test files and
# drops a marker file. A correct restore must bring the files back and remove the
# marker (proving it overwrote the live filesystem, not just filled a blank one).
tamper_fs() {
	local mp="$1"
	rm -rf "$mp/data"
	printf 'this file must not survive a restore\n' > "$mp/$TAMPER_MARKER"
	sync
}

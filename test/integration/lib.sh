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

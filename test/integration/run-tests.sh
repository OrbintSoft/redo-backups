#!/usr/bin/env bash
# SPDX-License-Identifier: EUPL-1.2
#
# redo-backups integration suite.
#
# For each disk layout it: builds a loopback disk, partitions and formats it,
# writes known data, takes a backup with redo-backup, wipes and restores the disk
# using the Redo Rescue command sequence (restore.sh), then verifies every
# partition's file content round-tripped intact.
#
# Run as root inside the integration VM:  sudo /opt/itest/run-tests.sh
# Optional: REDO_BACKUP_BIN=/path/to/redo-backup  LAYOUTS="gpt-ext4 mbr-ext4"
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
. "$here/lib.sh"

need_root

# Prefer GNU coreutils (in /usr/bin on Alpine) over busybox applets in /bin so
# that split/dd and friends support the long options the imaging pipeline relies
# on. Child processes spawned by redo-backup inherit this PATH.
export PATH="/usr/bin:$PATH"

BIN="${REDO_BACKUP_BIN:-/redo-backups/bin/redo-backup}"
[ -x "$BIN" ] || die "redo-backup binary not found/executable at $BIN (build it on the host with 'make build')"

# Layouts: "name | table | spec[ spec...]" where spec is "fs:sizeMiB".
ALL_LAYOUTS=(
	"gpt-ext4|gpt|ext4:200"
	"gpt-mixed|gpt|vfat:64 ext4:160"
	"mbr-ext4|dos|ext4:200"
	"gpt-xfs|gpt|xfs:300"
)

# Allow selecting a subset via the LAYOUTS env var.
declare -a LAYOUT_LIST
if [ -n "${LAYOUTS:-}" ]; then
	for want in $LAYOUTS; do
		for l in "${ALL_LAYOUTS[@]}"; do
			[ "${l%%|*}" = "$want" ] && LAYOUT_LIST+=("$l")
		done
	done
else
	LAYOUT_LIST=("${ALL_LAYOUTS[@]}")
fi
[ "${#LAYOUT_LIST[@]}" -gt 0 ] || die "no matching layouts"

WORK="$(mktemp -d)"
declare -a CLEANUP_LOOPS=()
cleanup() {
	set +e
	for mp in "$WORK"/mnt_*; do [ -d "$mp" ] && umount "$mp" 2>/dev/null; done
	for d in "${CLEANUP_LOOPS[@]}"; do losetup -d "$d" 2>/dev/null; done
	rm -rf "$WORK"
}
trap cleanup EXIT

PASS=0
FAIL=0
FAILED_NAMES=""

run_layout() {
	local spec="$1"
	local name table partspecs
	name="${spec%%|*}"; spec="${spec#*|}"
	table="${spec%%|*}"; partspecs="${spec#*|}"

	log "=== layout: $name (table=$table parts='$partspecs') ==="

	# Build a loopback disk.
	local img="$WORK/disk_$name.img"
	truncate -s 600M "$img"
	local loop base
	loop="$(losetup -fP --show "$img")"
	base="$(basename "$loop")"
	CLEANUP_LOOPS+=("$loop")

	# Build the sfdisk script for the requested partitions.
	local sfdscript="label: $table"$'\n'
	for ps in $partspecs; do
		local sz="${ps#*:}"
		sfdscript+=",${sz}MiB,L"$'\n'
	done
	printf '%s' "$sfdscript" | sfdisk "$loop" >/dev/null
	partprobe "$loop" 2>/dev/null || true

	# Format, fill, and snapshot checksums for each partition.
	local idx=0
	local -a fslist=()
	for ps in $partspecs; do
		idx=$((idx + 1))
		local fs="${ps%%:*}"
		fslist+=("$fs")
		local part="${loop}p${idx}"
		wait_for_block "$part" "$loop"
		log "  mkfs.$fs $part"
		case "$fs" in
			vfat) mkfs.vfat "$part" >/dev/null ;;
			ext4) mkfs.ext4 -q -F "$part" ;;
			xfs)  mkfs.xfs -q -f "$part" ;;
			btrfs) mkfs.btrfs -q -f "$part" ;;
			*) die "unsupported test fs: $fs" ;;
		esac
		local mp="$WORK/mnt_${name}_${idx}"
		mkdir -p "$mp"
		mount "$part" "$mp"
		write_testdata "$mp"
		checksum_tree "$mp" > "$WORK/sum_${name}_${idx}.before"
		umount "$mp"
	done

	# Configure and run the backup.
	local dest="$WORK/backup_$name"
	mkdir -p "$dest" /etc/redo-backups
	cat > /etc/redo-backups/itest.conf <<EOF
dest = $dest
drive = $base
parts = auto
id = $name
notes = integration $name
consistency = none
EOF
	log "  backup -> $dest"
	if ! "$BIN" run itest; then
		err "  backup command failed for $name"
		return 1
	fi
	log "  backup produced:"
	ls -la "$dest" | sed 's/^/[itest]     /'

	[ -f "$dest/$name.redo" ] || { err "  descriptor $dest/$name.redo missing"; return 1; }
	if ! ls "$dest/${name}_"*.img >/dev/null 2>&1; then
		err "  backup produced no .img chunks (check partclone/pigz/split in the VM)"
		return 1
	fi

	# Tamper: delete the files and drop a marker, so the restore has to bring the
	# files back and remove the marker.
	idx=0
	for fs in "${fslist[@]}"; do
		idx=$((idx + 1))
		local part="${loop}p${idx}"
		local mp="$WORK/tamper_${name}_${idx}"
		mkdir -p "$mp"
		mount "$part" "$mp"
		tamper_fs "$mp"
		umount "$mp"
	done

	# Wipe and restore onto the same drive (baremetal-style), then verify.
	log "  restore onto /dev/$base"
	"$here/restore.sh" "$dest/$name.redo" "$base"
	partprobe "$loop" || true
	sleep 1

	local ok=1
	idx=0
	for fs in "${fslist[@]}"; do
		idx=$((idx + 1))
		local part="${loop}p${idx}"
		wait_for_block "$part" "$loop"
		local mp="$WORK/verify_${name}_${idx}"
		mkdir -p "$mp"
		mount "$part" "$mp"
		local marker_present=0
		[ -e "$mp/$TAMPER_MARKER" ] && marker_present=1
		checksum_tree "$mp" > "$WORK/sum_${name}_${idx}.after"
		umount "$mp"
		if [ "$marker_present" -eq 1 ]; then
			err "  partition $idx ($fs): tamper marker survived restore"
			ok=0
		elif diff -u "$WORK/sum_${name}_${idx}.before" "$WORK/sum_${name}_${idx}.after" >/dev/null; then
			log "  partition $idx ($fs): files restored, marker gone — OK"
		else
			err "  partition $idx ($fs): CONTENT MISMATCH after restore"
			ok=0
		fi
	done

	[ "$ok" -eq 1 ]
}

for spec in "${LAYOUT_LIST[@]}"; do
	name="${spec%%|*}"
	if run_layout "$spec"; then
		log "RESULT $name: PASS"
		PASS=$((PASS + 1))
	else
		log "RESULT $name: FAIL"
		FAIL=$((FAIL + 1))
		FAILED_NAMES="$FAILED_NAMES $name"
	fi
done

echo
log "Summary: $PASS passed, $FAIL failed.${FAILED_NAMES:+ Failed:$FAILED_NAMES}"
[ "$FAIL" -eq 0 ]

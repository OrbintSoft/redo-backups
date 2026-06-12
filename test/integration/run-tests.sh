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

# is_selected reports whether a layout name should run (empty LAYOUTS = all).
is_selected() {
	[ -z "${LAYOUTS:-}" ] && return 0
	local want
	for want in $LAYOUTS; do
		[ "$want" = "$1" ] && return 0
	done
	return 1
}

WORK="$(mktemp -d)"
declare -a CLEANUP_LOOPS=()
declare -a CLEANUP_VGS=()
cleanup() {
	set +e
	for mp in "$WORK"/mnt_* "$WORK"/verify_*; do [ -d "$mp" ] && umount "$mp" 2>/dev/null; done
	for vg in "${CLEANUP_VGS[@]}"; do
		vgchange -an "$vg" 2>/dev/null
		vgremove -f "$vg" 2>/dev/null
	done
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
	# Use gzip rather than the pigz default: Alpine's pigz aborts against its
	# zlib build. The .img format is identical (gzip stream), so this still
	# exercises the full pipeline.
	cat > /etc/redo-backups/itest.conf <<EOF
dest = $dest
drive = $base
parts = auto
id = $name
notes = integration $name
consistency = none
compressor = gzip
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

# run_lvm_layout exercises the 'lvm' consistency strategy: an LVM PV partition
# whose logical volumes stay mounted is frozen and imaged raw, then restored.
run_lvm_layout() {
	local name="lvm-ext4" vg="vgredotest"
	log "=== layout: $name (LVM PV, 2 ext4 LVs, consistency=lvm) ==="

	local img="$WORK/disk_$name.img"
	truncate -s 800M "$img"
	local loop base
	loop="$(losetup -fP --show "$img")"
	base="$(basename "$loop")"
	CLEANUP_LOOPS+=("$loop")

	# One partition spanning the disk, used as an LVM physical volume.
	printf 'label: gpt\n,,L\n' | sfdisk "$loop" >/dev/null
	partprobe "$loop" 2>/dev/null || true
	local pv="${loop}p1"
	wait_for_block "$pv" "$loop"

	pvcreate -ff -y "$pv" >/dev/null
	vgcreate "$vg" "$pv" >/dev/null
	CLEANUP_VGS+=("$vg")
	lvcreate -y -L 200M -n root "$vg" >/dev/null
	lvcreate -y -L 200M -n home "$vg" >/dev/null
	vgchange -ay "$vg" >/dev/null
	vgmknodes "$vg" >/dev/null 2>&1 || true

	# Format and fill the LVs; KEEP them mounted (the lvm strategy freezes them).
	local -a lvs=(root home)
	local i=0 lv dev mp
	for lv in "${lvs[@]}"; do
		i=$((i + 1))
		dev="/dev/$vg/$lv"
		wait_for_block "$dev" ""
		mkfs.ext4 -q -F "$dev"
		mp="$WORK/mnt_${name}_${i}"
		mkdir -p "$mp"
		mount "$dev" "$mp"
		write_testdata "$mp"
		checksum_tree "$mp" > "$WORK/sum_${name}_${i}.before"
	done

	# Backup: the lvm strategy freezes the mounted LV filesystems and images the
	# PV partition raw (partclone.dd).
	local dest="$WORK/backup_$name"
	mkdir -p "$dest" /etc/redo-backups
	cat > /etc/redo-backups/itest.conf <<EOF
dest = $dest
drive = $base
parts = auto
id = $name
notes = integration $name
consistency = lvm
compressor = gzip
EOF
	if ! "$BIN" run itest; then
		err "  backup command failed for $name"
		return 1
	fi
	log "  backup produced:"
	ls -la "$dest" | sed 's/^/[itest]     /'
	if ! ls "$dest/${name}_"*.img >/dev/null 2>&1; then
		err "  backup produced no .img chunks"
		return 1
	fi

	# Tamper on the still-mounted LVs.
	i=0
	for lv in "${lvs[@]}"; do
		i=$((i + 1))
		tamper_fs "$WORK/mnt_${name}_${i}"
	done

	# Tear the VG down so the raw PV partition can be overwritten, restore, then
	# bring the VG back from the restored image.
	for lv in "${lvs[@]}"; do umount "/dev/$vg/$lv" 2>/dev/null || true; done
	vgchange -an "$vg" >/dev/null 2>&1 || true
	log "  restore (raw PV) onto /dev/$base"
	"$here/restore.sh" "$dest/$name.redo" "$base"
	partprobe "$loop" 2>/dev/null || true
	# The PV partition node must reappear after partprobe before LVM can scan it
	# (loop devices emit no udev events), then bring the VG and its LVs back.
	wait_for_block "$pv" "$loop"
	activate_vg "$vg" "${lvs[@]}"

	# Verify each LV: files back, marker gone.
	local ok=1 marker_present
	i=0
	for lv in "${lvs[@]}"; do
		i=$((i + 1))
		dev="/dev/$vg/$lv"
		wait_for_block "$dev" ""
		mp="$WORK/verify_${name}_${i}"
		mkdir -p "$mp"
		mount "$dev" "$mp"
		marker_present=0
		[ -e "$mp/$TAMPER_MARKER" ] && marker_present=1
		checksum_tree "$mp" > "$WORK/sum_${name}_${i}.after"
		umount "$mp"
		if [ "$marker_present" -eq 1 ]; then
			err "  LV $lv: tamper marker survived restore"
			ok=0
		elif diff -u "$WORK/sum_${name}_${i}.before" "$WORK/sum_${name}_${i}.after" >/dev/null; then
			log "  LV $lv: files restored, marker gone — OK"
		else
			err "  LV $lv: CONTENT MISMATCH after restore"
			ok=0
		fi
	done

	[ "$ok" -eq 1 ]
}

record_result() {
	local label="$1" rc="$2"
	if [ "$rc" -eq 0 ]; then
		log "RESULT $label: PASS"
		PASS=$((PASS + 1))
	else
		log "RESULT $label: FAIL"
		FAIL=$((FAIL + 1))
		FAILED_NAMES="$FAILED_NAMES $label"
	fi
}

ran_any=0
for spec in "${ALL_LAYOUTS[@]}"; do
	name="${spec%%|*}"
	is_selected "$name" || continue
	ran_any=1
	if run_layout "$spec"; then record_result "$name" 0; else record_result "$name" 1; fi
done

if is_selected "lvm-ext4"; then
	ran_any=1
	if ! command -v pvcreate >/dev/null 2>&1; then
		log "RESULT lvm-ext4: SKIP (lvm2 not installed)"
	elif run_lvm_layout; then
		record_result "lvm-ext4" 0
	else
		record_result "lvm-ext4" 1
	fi
fi

[ "$ran_any" -eq 1 ] || die "no matching layouts"

echo
log "Summary: $PASS passed, $FAIL failed.${FAILED_NAMES:+ Failed:$FAILED_NAMES}"
[ "$FAIL" -eq 0 ]

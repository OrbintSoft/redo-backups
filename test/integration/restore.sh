#!/usr/bin/env bash
# SPDX-License-Identifier: EUPL-1.2
#
# Restore a redo-backups (.redo + .img) backup using the same command sequence
# Redo Rescue performs from its live CD. This validates that backups produced by
# this project are restorable with the stock Redo Rescue tooling.
#
# Usage: restore.sh <path-to-.redo> <target-drive-basename>
#   e.g. restore.sh /tmp/backup/gpt-ext4.redo loop1
#
# The target drive is fully overwritten. The partition table from the backup is
# rewritten with the target's device name so a backup taken from one drive can be
# restored onto another.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
. "$here/lib.sh"

[ "$#" -eq 2 ] || die "usage: restore.sh <.redo file> <target drive basename>"
redo_file="$1"
target="$2"
[ -f "$redo_file" ] || die "no such file: $redo_file"
need_root

dest="$(dirname "$redo_file")"
id="$(basename "$redo_file" .redo)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mbr="$tmp/mbr.bin"
sfd="$tmp/sfd.txt"
parts="$tmp/parts.tsv"

# Decode the descriptor: MBR bytes, partition-table dump, and (name, fs) pairs.
python3 - "$redo_file" "$mbr" "$sfd" "$parts" <<'PY'
import base64, json, sys
redo, mbr_out, sfd_out, parts_out = sys.argv[1:5]
with open(redo) as fh:
    d = json.load(fh)
open(mbr_out, "wb").write(base64.b64decode(d["mbr_bin"]))
open(sfd_out, "wb").write(base64.b64decode(d["sfd_bin"]))
with open(parts_out, "w") as fh:
    for name, p in d["parts"].items():
        fh.write(f"{name}\t{p['fs']}\n")
PY

# The original drive name, taken from the sfdisk dump's "device:" line.
orig="$(awk -F'/dev/' '/^device:/ {print $2; exit}' "$sfd")"
[ -n "$orig" ] || die "could not determine original drive from $sfd"

log "Restoring backup '$id' (from drive '$orig') onto '/dev/$target'"

# 1. Wipe any existing signatures on the target.
wipefs --all --force "/dev/$target"

# 2. Restore the 32 KiB MBR/GPT header.
dd if="$mbr" of="/dev/$target" bs=32768 count=1 conv=notrunc
sync

# 3. Restore the partition table, rewriting the device name to the target.
sed "s#/dev/$orig#/dev/$target#g" "$sfd" | sfdisk --force "/dev/$target"
sync

# 4. Re-read the partition table.
partprobe "/dev/$target" || true
sleep 1

# 5. Restore each partition image with partclone.
while IFS=$'\t' read -r name fs; do
	[ -n "$name" ] || continue
	tool="$(fs_tool "$fs")"
	# Map the original partition name to the target drive.
	tgt_part="${name/$orig/$target}"
	shopt -s nullglob
	images=( "$dest/${id}_${name}_"*.img )
	shopt -u nullglob
	[ "${#images[@]}" -gt 0 ] || die "no image chunks for $name in $dest"

	log "  $name ($fs) -> /dev/$tgt_part via partclone.$tool (${#images[@]} chunk(s))"
	cat "${images[@]}" \
		| pigz --decompress --stdout \
		| partclone."$tool" --restore --force --UI-fresh 1 \
			--logfile "$tmp/${name}.log" --overwrite "/dev/$tgt_part" --no_block_detail
done < "$parts"

sync
log "Restore complete onto /dev/$target"

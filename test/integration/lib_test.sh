#!/usr/bin/env bash
# SPDX-License-Identifier: EUPL-1.2
#
# Unit tests for the integration-harness helpers in lib.sh that need neither root
# nor real LVM. They drive the logic with mock LVM commands and the
# LVM_DEV_PREFIX seam, anchoring the [ -b ] device-node checks on any real block
# device found on the host.
#
# Run:  test/integration/lib_test.sh
#
# The mock LVM functions below are invoked indirectly by the lib.sh helpers via
# bash's dynamic scoping (SC2329), and each scenario deliberately scopes its
# LVM_DEV_PREFIX to a command-substitution subshell (SC2030/SC2031); these are
# expected, so silence them file-wide (this directive must precede the first
# command to apply to the whole file).
# shellcheck disable=SC2329,SC2030,SC2031
set -u

here="$(cd "$(dirname "$0")" && pwd)"
LIB="$here/lib.sh"

# A real block device is needed so the faked LV nodes (symlinks to it) satisfy
# the [ -b ] checks inside activate_vg. Skip cleanly where none exists.
BDEV=""
while IFS= read -r d; do BDEV="$d"; break; done < <(find /dev -maxdepth 1 -type b 2>/dev/null)
if [ -z "$BDEV" ]; then
	echo "SKIP: no block device available to anchor [ -b ] checks"
	exit 0
fi

fail=0

# activate_vg succeeds once the LV nodes appear after a few vgchange retries.
test_activate_vg_success() {
	local out
	out="$(
		# shellcheck source=lib.sh
		source "$LIB"
		tmp="$(mktemp -d)"; export LVM_DEV_PREFIX="$tmp/dev"
		mkdir -p "$LVM_DEV_PREFIX/vgtest"
		count=0
		# Nodes only show up on the 3rd activation attempt.
		vgchange() {
			count=$((count + 1))
			if [ "$count" -ge 3 ]; then
				ln -sf "$BDEV" "$LVM_DEV_PREFIX/vgtest/root"
				ln -sf "$BDEV" "$LVM_DEV_PREFIX/vgtest/home"
			fi
		}
		vgmknodes() { :; }; pvscan() { :; }; vgs() { :; }; lvs() { :; }
		udevadm() { :; }; sleep() { :; }
		activate_vg vgtest root home && echo ACTIVATED
		rm -rf "$tmp"
	)"
	if [[ "$out" == *ACTIVATED* ]]; then
		echo "PASS: activate_vg success-after-retry"
	else
		echo "FAIL: activate_vg did not activate when nodes appeared"; fail=1
	fi
}

# activate_vg dies (exit 1), dumps LVM state, and does not continue when the LV
# nodes never appear.
test_activate_vg_timeout() {
	local out rc
	out="$( {
		# shellcheck source=lib.sh
		source "$LIB"
		tmp="$(mktemp -d)"; export LVM_DEV_PREFIX="$tmp/dev"
		mkdir -p "$LVM_DEV_PREFIX/vgtest"
		vgchange() { :; }; vgmknodes() { :; }
		pvscan() { echo MOCK-pvscan; }; vgs() { echo MOCK-vgs; }; lvs() { echo MOCK-lvs; }
		udevadm() { :; }; sleep() { :; }
		activate_vg vgtest root home
		echo REACHED-PAST-DIE
		rm -rf "$tmp"
	} 2>&1 )"
	rc=$?
	if [ "$rc" -eq 1 ] \
		&& [[ "$out" != *REACHED-PAST-DIE* ]] \
		&& [[ "$out" == *MOCK-pvscan* && "$out" == *MOCK-vgs* && "$out" == *MOCK-lvs* ]] \
		&& [[ "$out" == *"never appeared after restore"* ]]; then
		echo "PASS: activate_vg timeout dies with diagnostics"
	else
		echo "FAIL: activate_vg timeout behaviour (rc=$rc)"
		printf '%s\n' "$out" | sed 's/^/  > /'
		fail=1
	fi
}

test_activate_vg_success
test_activate_vg_timeout

echo
if [ "$fail" -eq 0 ]; then
	echo "lib_test.sh: all tests passed"
else
	echo "lib_test.sh: FAILURES"
fi
exit "$fail"

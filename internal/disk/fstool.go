// SPDX-License-Identifier: EUPL-1.2

package disk

import "strings"

// DDTool is the fallback "tool" used for filesystems partclone cannot clone; it
// signals a raw dd image rather than a partclone.<fs> invocation.
const DDTool = "dd"

// fsToolRule maps a case-insensitive substring of the filesystem name to the
// partclone tool suffix. Order is significant: the first matching rule wins,
// mirroring Redo Rescue's get_fs_tool() (e.g. "exfat" must be tested before
// "fat", and "ext" matches ext2/3/4).
var fsToolRules = []struct {
	match string
	tool  string
}{
	{"btrfs", "btrfs"},
	{"exfat", "exfat"},
	{"ext", "extfs"},
	{"f2fs", "f2fs"},
	{"fat", "fat"},
	{"hfs", "hfsp"},
	{"minix", "minix"},
	{"nilfs", "nilfs2"},
	{"ntfs", "ntfs"},
	{"reiser", "reiser4"},
	{"xfs", "xfs"},
}

// FSTool returns the partclone tool suffix for the given filesystem type, i.e.
// the "<tool>" in "partclone.<tool>". Unknown filesystems map to DDTool, meaning
// a raw image should be taken with dd instead of partclone.
func FSTool(fs string) string {
	f := strings.ToLower(fs)
	for _, r := range fsToolRules {
		if strings.Contains(f, r.match) {
			return r.tool
		}
	}

	return DDTool
}

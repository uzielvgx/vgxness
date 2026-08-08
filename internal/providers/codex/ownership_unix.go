//go:build darwin || linux

package codex

import (
	"os"
	"syscall"
)

func owner(info os.FileInfo, root bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == uint32(os.Geteuid()) || root && stat.Uid == 0)
}
func owned(info os.FileInfo) bool        { return owner(info, false) }
func trustedOwner(info os.FileInfo) bool { return owner(info, true) }

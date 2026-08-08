//go:build darwin || linux

package codex

import (
	"os"
	"syscall"
)

func owner(_ string, info os.FileInfo, root bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == uint32(os.Geteuid()) || root && stat.Uid == 0)
}
func safeAncestor(_ string, info os.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && owner("", info, true) && (info.Mode().Perm()&0o022 == 0 || info.Mode()&os.ModeSticky != 0)
}
func ownedDir(path string, info os.FileInfo) bool {
	return safeAncestor(path, info) && info.Mode().Perm()&0o022 == 0 && owner(path, info, false)
}

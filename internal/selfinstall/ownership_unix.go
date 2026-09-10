//go:build darwin || linux

package selfinstall

import (
	"io/fs"
	"os"
	"syscall"
)

func safeInstallAncestor(info fs.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) {
		return false
	}
	return info.Mode().Perm()&0o022 == 0 || info.Mode()&os.ModeSticky != 0
}

func ownedInstallDirectory(info fs.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

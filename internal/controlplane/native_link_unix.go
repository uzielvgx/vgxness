//go:build unix

package controlplane

import (
	"fmt"
	"os"
	"syscall"
)

func nativeSingleLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

func nativeFileIdentity(info os.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("unix:%d:%d", uint64(stat.Dev), uint64(stat.Ino)), true
}

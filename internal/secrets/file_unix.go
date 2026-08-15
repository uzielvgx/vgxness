//go:build darwin || linux

package secrets

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func readCredentialFile(path string) (string, error) {
	if !filepath.IsAbs(path) || !safeCredentialAncestors(path) {
		return "", ErrInvalid
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", ErrInvalid
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !safeCredentialInfo(before) {
		return "", ErrInvalid
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, pathInfo) || !safeCredentialInfo(pathInfo) {
		return "", ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCredentialFileBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxCredentialFileBytes {
		return "", ErrInvalid
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !sameCredentialMetadata(before, after) || !safeCredentialInfo(after) {
		return "", ErrInvalid
	}
	pathInfo, err = os.Lstat(path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(after, pathInfo) || !safeCredentialInfo(pathInfo) || !safeCredentialAncestors(path) {
		return "", ErrInvalid
	}
	return string(data), nil
}

func safeCredentialAncestors(path string) bool {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if current == filepath.Dir(current) {
			return true
		}
	}
}

func safeCredentialInfo(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o077 == 0 && stat.Uid == uint32(os.Getuid()) && info.Size() <= maxCredentialFileBytes
}

func sameCredentialMetadata(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Dev == y.Dev && x.Ino == y.Ino && x.Uid == y.Uid && a.Mode() == b.Mode() && a.Size() == b.Size()
}

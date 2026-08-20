//go:build darwin || linux

package secrets

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func readCredentialFile(path string) (string, error) {
	file, err := openCredentialFile(path, nil)
	if err != nil {
		return "", ErrInvalid
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !safeCredentialInfo(before) {
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
	return string(data), nil
}

// openCredentialFile walks ancestor directories using retained descriptors, so
// later pathname replacement cannot redirect the leaf open.
func openCredentialFile(path string, afterAncestors func()) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "" || parts[len(parts)-1] == "." || parts[len(parts)-1] == ".." {
		return nil, ErrInvalid
	}
	dir, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrInvalid
	}
	defer func() { unix.Close(dir) }()
	for _, part := range parts[:len(parts)-1] {
		if part == "." || part == ".." {
			return nil, ErrInvalid
		}
		next, err := openCredentialDirectory(dir, part)
		if err != nil {
			return nil, ErrInvalid
		}
		unix.Close(dir)
		dir = next
	}
	if afterAncestors != nil {
		afterAncestors()
	}
	var before unix.Stat_t
	leaf := parts[len(parts)-1]
	if unix.Fstatat(dir, leaf, &before, unix.AT_SYMLINK_NOFOLLOW) != nil || !safeCredentialStat(before) {
		return nil, ErrInvalid
	}
	fd, err := unix.Openat(dir, leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrInvalid
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !sameCredentialStat(before, info) || !safeCredentialInfo(info) {
		file.Close()
		return nil, ErrInvalid
	}
	return file, nil
}

func openCredentialDirectory(parent int, name string) (int, error) {
	var before unix.Stat_t
	if unix.Fstatat(parent, name, &before, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return -1, ErrInvalid
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if before.Mode&unix.S_IFMT == unix.S_IFLNK {
		return -1, ErrInvalid
	} else if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return -1, ErrInvalid
	}
	fd, err := unix.Openat(parent, name, flags, 0)
	if err != nil {
		return -1, ErrInvalid
	}
	var after unix.Stat_t
	if unix.Fstat(fd, &after) != nil || after.Mode&unix.S_IFMT != unix.S_IFDIR || before.Mode&unix.S_IFMT != unix.S_IFLNK && !sameUnixStat(before, after) {
		unix.Close(fd)
		return -1, ErrInvalid
	}
	return fd, nil
}

func safeCredentialStat(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0o077 == 0 && stat.Uid == uint32(os.Getuid()) && stat.Size <= maxCredentialFileBytes
}

func sameCredentialStat(stat unix.Stat_t, info os.FileInfo) bool {
	fileStat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Dev) == uint64(fileStat.Dev) && uint64(stat.Ino) == uint64(fileStat.Ino)
}

func sameUnixStat(a, b unix.Stat_t) bool {
	return uint64(a.Dev) == uint64(b.Dev) && uint64(a.Ino) == uint64(b.Ino)
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

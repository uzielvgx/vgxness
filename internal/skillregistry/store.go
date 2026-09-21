package skillregistry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxCacheBytes  = 4 << 20
	lockRetryDelay = 5 * time.Millisecond
	maxLockWait    = 2 * time.Second
	maxLockBytes   = 64
)

// readCache reads the cache as a bounded regular file through a rooted
// directory handle. A symlink, non-regular file, FIFO, oversized file, or
// identity change is rejected before any unbounded I/O can block.
func readCache(path string) (Registry, bool) {
	data, ok := readBoundedRootFile(filepath.Dir(path), filepath.Base(path), maxCacheBytes)
	if !ok {
		return Registry{}, false
	}
	var registry Registry
	if json.Unmarshal(data, &registry) != nil {
		return Registry{}, false
	}
	if registry.SchemaVersion != SchemaVersion || registry.Workspace == "" {
		return Registry{}, false
	}
	return registry, true
}

// Bounded-read classification errors. Callers map them to entry statuses.
var (
	errReadSymlink    = errors.New("skill registry path is a symlink")
	errReadOversized  = errors.New("skill registry file exceeds its byte bound")
	errReadNotRegular = errors.New("skill registry path is not a regular file")
	errReadUnreadable = errors.New("skill registry path is unreadable")
	errReadChanged    = errors.New("skill registry file changed during read")
)

func readRegularBounded(dir, name string, limit int64) ([]byte, os.FileInfo, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, errReadUnreadable
	}
	defer root.Close()
	return readRegularInRoot(root, name, limit)
}

func readBoundedRootFile(dir, name string, limit int64) ([]byte, bool) {
	data, _, err := readRegularBounded(dir, name, limit)
	return data, err == nil
}

func readRootFile(root *os.Root, name string, limit int64) ([]byte, bool) {
	data, _, err := readRegularInRoot(root, name, limit)
	return data, err == nil
}

// readRegularInRoot reads one regular file through a rooted handle under a byte
// bound. It rejects symlinks, non-regular files such as FIFOs, oversized files,
// and files whose identity, size, or mtime changes across the read, so a swap
// or growth race cannot hang the process or return arbitrary content.
func readRegularInRoot(root *os.Root, name string, limit int64) ([]byte, os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		return nil, nil, errReadUnreadable
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, info, errReadSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, info, errReadNotRegular
	}
	if info.Size() > limit {
		return nil, info, errReadOversized
	}
	// openReadFlags adds O_NOFOLLOW|O_NONBLOCK on Unix so a swapped FIFO cannot
	// block the process between the stat and the open.
	file, err := root.OpenFile(name, openReadFlags, 0)
	if err != nil {
		return nil, info, errReadUnreadable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, info, errReadChanged
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, info, errReadUnreadable
	}
	if int64(len(data)) > limit {
		return nil, info, errReadOversized
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, info, errReadChanged
	}
	return data, info, nil
}

// publish writes the cache atomically under a rooted directory handle. It
// refuses to write a document larger than the read bound, so it can never
// publish bytes that its own reader would reject.
func publish(ctx context.Context, path string, registry Registry) error {
	data, err := json.Marshal(registry)
	if err != nil {
		return ErrInvalid
	}
	data = append(data, '\n')
	if len(data) > maxCacheBytes {
		return ErrInvalid
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ErrInvalid
	}
	if err := rejectSymlink(dir); err != nil {
		return err
	}
	if err := rejectSymlink(path); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return ErrInvalid
	}
	defer root.Close()
	lock, err := acquireLock(ctx, root, "skill-registry.lock")
	if err != nil {
		return err
	}
	defer lock.release(root)

	temporary := ".skill-registry-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".tmp"
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ErrInvalid
	}
	removeTemporary := func() {
		_ = file.Close()
		_ = root.Remove(temporary)
	}
	if _, err := file.Write(data); err != nil {
		removeTemporary()
		return ErrInvalid
	}
	if err := file.Sync(); err != nil {
		removeTemporary()
		return ErrInvalid
	}
	if err := file.Close(); err != nil {
		_ = root.Remove(temporary)
		return ErrInvalid
	}
	if err := root.Rename(temporary, filepath.Base(path)); err != nil {
		_ = root.Remove(temporary)
		return ErrInvalid
	}
	// Best-effort durability of the rename; a directory sync failure is not a
	// write failure because not all filesystems support it.
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

type fileLock struct {
	name string
	info os.FileInfo
}

// acquireLock serializes publishers. It never deletes a lock on age alone: it
// removes a stale lock only when the recorded owner process is provably gone
// and the lock identity is unchanged. Otherwise it waits a bounded interval
// and reports ErrBusy.
func acquireLock(ctx context.Context, root *os.Root, name string) (*fileLock, error) {
	deadline := time.Now().Add(maxLockWait)
	for {
		if err := rejectRootSymlink(root, name); err != nil {
			return nil, err
		}
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
			_ = file.Sync()
			_ = file.Close()
			info, statErr := root.Lstat(name)
			if statErr != nil {
				_ = root.Remove(name)
				return nil, ErrInvalid
			}
			return &fileLock{name: name, info: info}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, ErrInvalid
		}
		if _, statErr := root.Lstat(name); errors.Is(statErr, fs.ErrNotExist) {
			// The lock vanished between the failed exclusive create and now.
			continue
		}
		if removed, recoverErr := recoverStaleLock(root, name); recoverErr != nil {
			return nil, recoverErr
		} else if removed {
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetryDelay):
		}
	}
}

// recoverStaleLock removes a lock only when it is old enough, the recorded
// owner process is gone, and the lock file identity still matches. Any
// uncertainty returns false so the caller fails closed instead of deleting a
// possibly active lock.
func recoverStaleLock(root *os.Root, name string) (bool, error) {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, ErrInvalid
	}
	if time.Since(info.ModTime()) < staleLockAge {
		return false, nil
	}
	data, ok := readRootFile(root, name, maxLockBytes)
	if !ok {
		return false, nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return false, nil
	}
	if processAlive(pid) {
		return false, nil
	}
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, current) {
		return false, nil
	}
	if err := root.Remove(name); err != nil {
		return false, ErrInvalid
	}
	return true, nil
}

func (lock *fileLock) release(root *os.Root) {
	if lock == nil {
		return
	}
	current, err := root.Lstat(lock.name)
	if err != nil || !os.SameFile(lock.info, current) {
		// Never remove a lock that another writer now owns.
		return
	}
	_ = root.Remove(lock.name)
}

func rejectRootSymlink(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return ErrInvalid
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	return nil
}

// rejectSymlink fails closed when the exact path exists as a symlink. It is
// used only for state paths this package owns; rooted operations confine any
// ancestor traversal to the opened directory.
func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return ErrInvalid
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	return nil
}

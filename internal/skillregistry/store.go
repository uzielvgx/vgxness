package skillregistry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// rootedFS is the subset of *os.Root used for lock and cache publication. It is
// an interface so per-call tests can inject scripted errors without a mutable
// package-global failure hook; production always passes a real *os.Root.
type rootedFS interface {
	Lstat(name string) (os.FileInfo, error)
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Remove(name string) error
	Rename(oldname, newname string) error
}

// ops carries the per-call filesystem seams for lock acquisition and cache
// publication. realOps wires the real calls and the production wait bound;
// tests copy it and override individual fields. It is passed by value per call
// and never stored or mutated globally, so it cannot race across goroutines.
type ops struct {
	contention func(error) bool
	retryDelay time.Duration
	maxWait    time.Duration

	lockWrite func(*os.File, []byte) (int, error)
	lockSync  func(*os.File) error
	lockClose func(*os.File) error

	tempWrite func(*os.File, []byte) (int, error)
	tempSync  func(*os.File) error
	tempClose func(*os.File) error

	remove func(rootedFS, string) error
	rename func(rootedFS, string, string) error
}

// realOps returns the production seams. retryableContention is the platform
// classifier (Windows-only transient classes; false elsewhere).
func realOps() ops {
	write := func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	sync := func(file *os.File) error { return file.Sync() }
	closeFile := func(file *os.File) error { return file.Close() }
	return ops{
		contention: retryableContention,
		retryDelay: lockRetryDelay,
		maxWait:    maxLockWait,
		lockWrite:  write,
		lockSync:   sync,
		lockClose:  closeFile,
		tempWrite:  write,
		tempSync:   sync,
		tempClose:  closeFile,
		remove:     func(root rootedFS, name string) error { return root.Remove(name) },
		rename:     func(root rootedFS, oldname, newname string) error { return root.Rename(oldname, newname) },
	}
}

// invalid marks an operational failure as ErrInvalid while preserving the
// underlying OS cause, so errors.Is keeps working for both the sentinel and the
// original error.
func invalid(err error) error {
	if err == nil {
		return ErrInvalid
	}
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}

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

func readRootFile(root rootedFS, name string, limit int64) ([]byte, bool) {
	data, _, err := readRegularInRoot(root, name, limit)
	return data, err == nil
}

// readRegularInRoot reads one regular file through a rooted handle under a byte
// bound. It rejects symlinks, non-regular files such as FIFOs, oversized files,
// and files whose identity, size, or mtime changes across the read, so a swap
// or growth race cannot hang the process or return arbitrary content.
func readRegularInRoot(root rootedFS, name string, limit int64) ([]byte, os.FileInfo, error) {
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
	return publishWith(ctx, path, registry, realOps())
}

func publishWith(ctx context.Context, path string, registry Registry, o ops) error {
	// Cancellation boundary: no cache, lock, or temporary file is created when
	// the context is already cancelled.
	if err := ctx.Err(); err != nil {
		return err
	}
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
	lock, err := acquireLockWith(ctx, root, "skill-registry.lock", o)
	if err != nil {
		return err
	}
	defer lock.releaseWith(root, o)

	// Staging boundary: no temporary file exists before this point, so a
	// cancellation here leaves any existing cache and lock untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	temporary := ".skill-registry-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".tmp"
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ErrInvalid
	}
	removeTemporary := func() {
		_ = o.tempClose(file)
		_ = o.remove(root, temporary)
	}
	if _, err := o.tempWrite(file, data); err != nil {
		removeTemporary()
		return ErrInvalid
	}
	if err := o.tempSync(file); err != nil {
		removeTemporary()
		return ErrInvalid
	}
	if err := o.tempClose(file); err != nil {
		_ = o.remove(root, temporary)
		return ErrInvalid
	}
	// Staged-publication boundary: the destination is replaced only here, so a
	// cancellation observed before this point leaves the previous cache intact.
	if err := ctx.Err(); err != nil {
		_ = o.remove(root, temporary)
		return err
	}
	if err := o.rename(root, temporary, filepath.Base(path)); err != nil {
		_ = o.remove(root, temporary)
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

// fileLock is one held publication lock. It retains the exact bytes written at
// acquisition and an open handle to that file, so release can prove the path
// still names the file it acquired instead of trusting a PID match or a
// re-stat that an inode or Windows FileId reuse could satisfy. Holding the
// handle also pins the file identity for the whole lock lifetime.
type fileLock struct {
	name    string
	content []byte
	file    *os.File
}

// acquireLock serializes publishers. It never deletes a lock on age alone: it
// removes a stale lock only when the recorded owner process is provably gone
// and the lock identity is unchanged. Otherwise it waits a bounded interval
// and reports ErrBusy.
//
// An acquisition writes a per-acquisition token alongside the PID. A successor
// that recycles the same PID, or a path that is recreated onto a reused inode
// or FileId, therefore cannot satisfy the byte comparison release performs.
// Legacy PID-only lock files remain readable for stale-owner recovery; they are
// never treated as owned by the current acquisition.
func acquireLock(ctx context.Context, root *os.Root, name string) (*fileLock, error) {
	return acquireLockWith(ctx, root, name, realOps())
}

func acquireLockWith(ctx context.Context, root rootedFS, name string, o ops) (*fileLock, error) {
	deadline := time.Now().Add(o.maxWait)
	for {
		// Boundary: never create or remove anything under an already-cancelled
		// context.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		precheckErr := rejectRootSymlink(root, name)
		if precheckErr == nil {
			file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err == nil {
				return finalizeLockWith(root, name, file, o)
			}
			if !errors.Is(err, fs.ErrExist) && !o.contention(err) {
				// Permission, symlink, path, and unknown errors fail closed
				// instead of being retried as if they were contention.
				return nil, invalid(err)
			}
			_, statErr := root.Lstat(name)
			switch {
			case statErr == nil:
				removed, recoverErr := recoverStaleLockWith(root, name, o)
				switch {
				case recoverErr != nil:
					if !o.contention(recoverErr) {
						return nil, recoverErr
					}
				case removed:
					continue
				}
			case errors.Is(statErr, fs.ErrNotExist):
				// The lock vanished or is pending deletion: bounded wait below.
			case o.contention(statErr):
				// Transient stat contention: bounded wait below.
			default:
				return nil, invalid(statErr)
			}
		} else if !o.contention(precheckErr) {
			if errors.Is(precheckErr, ErrInvalid) {
				return nil, precheckErr
			}
			return nil, invalid(precheckErr)
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(o.retryDelay):
		}
	}
}

// finalizeLockWith writes the acquisition proof to a freshly created lock file.
// On every failure path it discards only a lock it still provably owns and
// closes the descriptor, so a rejected acquisition cannot leak a descriptor or
// leave an unprovable lock behind.
func finalizeLockWith(root rootedFS, name string, file *os.File, o ops) (*fileLock, error) {
	content, err := newLockContent()
	if err != nil {
		discardLock(root, name, file, nil, o)
		return nil, ErrInvalid
	}
	if _, err := o.lockWrite(file, content); err != nil {
		discardLock(root, name, file, content, o)
		return nil, ErrInvalid
	}
	if err := o.lockSync(file); err != nil {
		discardLock(root, name, file, content, o)
		return nil, ErrInvalid
	}
	return &fileLock{name: name, content: content, file: file}, nil
}

// discardLock removes a lock created by this acquisition only while the held
// descriptor still names that same file and the on-disk bytes are a prefix of
// what this acquisition wrote (empty, partial, or complete). A replacement that
// unlinked and recreated the path has a different identity, and any other body
// is not a prefix, so neither is removed. The descriptor is always closed.
func discardLock(root rootedFS, name string, file *os.File, content []byte, o ops) {
	if file == nil {
		return
	}
	held, statErr := file.Stat()
	if statErr == nil {
		current, lstatErr := root.Lstat(name)
		if lstatErr == nil && os.SameFile(held, current) {
			if data, ok := readRootFile(root, name, maxLockBytes); ok && bytes.HasPrefix(content, data) {
				_ = o.remove(root, name)
			}
		}
	}
	_ = o.lockClose(file)
}

// newLockContent renders the bounded lock body: the owner PID on the first line
// and a cryptographically random per-acquisition token on the second. The
// format stays within maxLockBytes and is parseable by lockPID.
func newLockContent() ([]byte, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	return []byte(strconv.Itoa(os.Getpid()) + "\n" + hex.EncodeToString(token)), nil
}

// lockPID extracts the owner PID from a lock body. It accepts the token format
// and legacy PID-only files; any other content is not a valid lock.
func lockPID(data []byte) (int, bool) {
	line := string(data)
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// openLockHandle opens the lock read-only through the rooted handle and returns
// the held descriptor, its pinned identity, and the bounded bytes. The caller
// owns the descriptor. Holding it keeps the file identity stable so a
// delete/recreate cannot be mistaken for the file that was inspected.
func openLockHandle(root rootedFS, name string) (*os.File, os.FileInfo, []byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, info, nil, errReadSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, info, nil, errReadNotRegular
	}
	if info.Size() > maxLockBytes {
		return nil, info, nil, errReadOversized
	}
	file, err := root.OpenFile(name, openReadFlags, 0)
	if err != nil {
		return nil, info, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, info, nil, errReadChanged
	}
	data, err := io.ReadAll(io.LimitReader(file, maxLockBytes+1))
	if err != nil {
		_ = file.Close()
		return nil, info, nil, errReadUnreadable
	}
	if int64(len(data)) > maxLockBytes {
		_ = file.Close()
		return nil, info, nil, errReadOversized
	}
	return file, opened, data, nil
}

// recoverStaleLock removes a lock only when it is old enough, the recorded
// owner process is gone, and the lock file identity still matches. Any
// uncertainty, including a transient stat/remove result, returns false so the
// caller waits within its own bound instead of deleting a possibly active lock.
func recoverStaleLock(root rootedFS, name string) (bool, error) {
	return recoverStaleLockWith(root, name, realOps())
}

func recoverStaleLockWith(root rootedFS, name string, o ops) (bool, error) {
	info, err := root.Lstat(name)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		case o.contention(err):
			// Transient stat contention is not provably stale; let the caller
			// keep waiting within its existing bound.
			return false, nil
		default:
			return false, invalid(err)
		}
	}
	if time.Since(info.ModTime()) < staleLockAge {
		return false, nil
	}
	file, held, data, err := openLockHandle(root, name)
	if err != nil {
		// A vanished, changed, unreadable, non-regular, or oversized lock is
		// never provably stale, so recovery fails closed.
		return false, nil
	}
	defer file.Close()
	pid, ok := lockPID(data)
	if !ok || pid == os.Getpid() {
		// An unparseable lock, or one owned by a still-live PID, is left alone.
		return false, nil
	}
	if processAlive(pid) {
		return false, nil
	}
	// The held handle pins the identity, so an inode or FileId reused by a
	// delete/recreate cannot be mistaken for the lock that was inspected.
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(held, current) {
		return false, nil
	}
	if err := o.remove(root, name); err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		case o.contention(err):
			return false, nil
		default:
			return false, invalid(err)
		}
	}
	return true, nil
}

func (lock *fileLock) release(root *os.Root) {
	lock.releaseWith(root, realOps())
}

func (lock *fileLock) releaseWith(root rootedFS, o ops) {
	if lock == nil || lock.file == nil {
		return
	}
	defer func() { _ = o.lockClose(lock.file) }()
	held, err := lock.file.Stat()
	if err != nil {
		return
	}
	current, err := root.Lstat(lock.name)
	if err != nil || !os.SameFile(held, current) {
		// Never remove a lock that another writer now owns.
		return
	}
	data, ok := readRootFile(root, lock.name, maxLockBytes)
	if !ok || !bytes.Equal(data, lock.content) {
		// The path names a different lock: for example a same-PID successor or
		// a recreated file whose identity was reused. Leave it in place.
		return
	}
	_ = o.remove(root, lock.name)
}

// rejectRootSymlink fails closed on a symlink and preserves the underlying OS
// cause for any other stat failure, so the caller can distinguish transient
// contention from a real error. A missing path is not an error.
func rejectRootSymlink(root rootedFS, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("lock path stat: %w", err)
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

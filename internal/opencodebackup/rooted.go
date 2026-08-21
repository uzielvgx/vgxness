package opencodebackup

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// rootAnchor binds a configured directory name to the identity observed through
// an opened root. Callers own every root returned by open.
type rootAnchor struct {
	path string
	info os.FileInfo
	mu   sync.Mutex
}

func newRootAnchor(directory string) (*rootAnchor, error) {
	path, err := canonicalRoot(directory)
	if err != nil {
		return nil, invalid("resolve rooted directory", directory, err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, wrapFilesystem("open rooted directory", path, err)
	}
	info, statErr := root.Stat(".")
	closeErr := root.Close()
	if statErr != nil || closeErr != nil || !info.IsDir() {
		return nil, corrupt("pin rooted directory", path, errors.Join(statErr, closeErr))
	}
	return &rootAnchor{path: path, info: info}, nil
}

// newPrivateRootAnchor creates and pins a backup root without mutating a path
// after checking it. Each missing component is created through its held parent.
func newPrivateRootAnchor(directory string) (*rootAnchor, error) {
	path, err := canonicalRoot(directory)
	if err != nil {
		return nil, invalid("resolve backup root", directory, err)
	}
	missing := make([]string, 0)
	ancestor := path
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, unsupported("prepare backup root", ancestor, nil)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, wrapFilesystem("prepare backup root", ancestor, err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return nil, wrapFilesystem("prepare backup root", ancestor, err)
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
	root, err := os.OpenRoot(ancestor)
	if err != nil {
		return nil, wrapFilesystem("open backup ancestor", ancestor, err)
	}
	defer func() { _ = root.Close() }()
	for index := len(missing) - 1; index >= 0; index-- {
		name := missing[index]
		if info, err := root.Lstat(name); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, unsupported("prepare backup root", name, nil)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(name, 0o700); err != nil {
				return nil, wrapFilesystem("create backup root", name, err)
			}
		} else {
			return nil, wrapFilesystem("inspect backup root", name, err)
		}
		next, err := root.OpenRoot(name)
		if err != nil {
			return nil, wrapFilesystem("open backup root", name, err)
		}
		if err := root.Close(); err != nil {
			_ = next.Close()
			return nil, wrapFilesystem("close backup parent", name, err)
		}
		root = next
	}
	if err := root.Chmod(".", 0o700); err != nil {
		return nil, wrapFilesystem("secure backup root", path, err)
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() {
		return nil, corrupt("pin backup root", path, err)
	}
	canonical, err := canonicalRoot(path)
	if err != nil {
		return nil, invalid("resolve backup root", path, err)
	}
	opened, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, wrapFilesystem("open backup root", canonical, err)
	}
	openedInfo, statErr := opened.Stat(".")
	closeErr := opened.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(info, openedInfo) {
		return nil, errors.Join(corrupt("pin backup root", canonical, statErr), closeErr, ErrConflict)
	}
	return &rootAnchor{path: canonical, info: info}, nil
}

// canonicalRoot resolves every extant component, including symlink aliases,
// while retaining any absent leaf components for lazy backup initialization.
func canonicalRoot(directory string) (string, error) {
	path, err := absoluteRoot(directory)
	if err != nil {
		return "", err
	}
	missing := make([]string, 0)
	current := path
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func (a *rootAnchor) open() (*os.Root, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	root, err := os.OpenRoot(a.path)
	if err != nil {
		return nil, wrapFilesystem("open rooted directory", a.path, err)
	}
	info, statErr := root.Stat(".")
	if statErr != nil || !info.IsDir() || !os.SameFile(a.info, info) {
		return nil, errors.Join(corrupt("reopen rooted directory", a.path, statErr), root.Close(), ErrConflict)
	}
	return root, nil
}

// snapshotRef owns both handles for one snapshot operation. It is private so
// rooted authority never escapes the backup package.
type snapshotRef struct {
	backup   *os.Root
	snapshot *os.Root
	mu       sync.Mutex
	closed   bool
	closeErr error
}

func openSnapshot(backup *rootAnchor, snapshotID string) (*snapshotRef, error) {
	if err := ValidateSnapshotID(snapshotID); err != nil {
		return nil, err
	}
	backupRoot, err := backup.open()
	if err != nil {
		return nil, err
	}
	info, err := backupRoot.Lstat(snapshotID)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.Join(corrupt("inspect rooted snapshot", snapshotID, err), backupRoot.Close())
	}
	snapshotRoot, err := backupRoot.OpenRoot(snapshotID)
	if err != nil {
		return nil, errors.Join(corrupt("open rooted snapshot", snapshotID, err), backupRoot.Close())
	}
	opened, statErr := snapshotRoot.Stat(".")
	if statErr != nil || !opened.IsDir() || !os.SameFile(info, opened) {
		return nil, errors.Join(corrupt("open rooted snapshot", snapshotID, statErr), snapshotRoot.Close(), backupRoot.Close())
	}
	return &snapshotRef{backup: backupRoot, snapshot: snapshotRoot}, nil
}

func (r *snapshotRef) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	r.closeErr = errors.Join(r.snapshot.Close(), r.backup.Close())
	return r.closeErr
}

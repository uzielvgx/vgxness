package opencodebackup

import (
	"errors"
	"os"
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
	path, err := absoluteRoot(directory)
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

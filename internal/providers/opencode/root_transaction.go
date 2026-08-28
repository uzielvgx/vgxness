package opencode

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// rootTransaction keeps filesystem operations anchored to the selected config
// directory even if its original pathname is replaced after it is opened.
type rootTransaction struct {
	path               string
	root               *os.Root
	beforeRemoveRename func(string)
	afterRemoveRename  func(string, string)
}

type rootFile struct {
	*os.File
	name string
}

type rootStagedArtifact struct {
	temporary     string
	temporaryInfo os.FileInfo
	staging       string
	stagingInfo   os.FileInfo
	content       []byte
}

type rootArtifactBackup struct {
	name    string
	info    os.FileInfo
	content []byte
}

func (file *rootFile) Name() string { return file.name }

func openRootTransaction(path string, create bool) (*rootTransaction, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("invalid root path")
	}
	clean := cleanRootPath(path)
	volumeRoot := filepath.VolumeName(clean) + string(os.PathSeparator)
	relative, err := filepath.Rel(volumeRoot, clean)
	if err != nil || relative == "." || escapes(relative) {
		return nil, fmt.Errorf("invalid root path")
	}
	root, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		info, statErr := root.Lstat(component)
		if errors.Is(statErr, os.ErrNotExist) && create {
			if err := root.Mkdir(component, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				_ = root.Close()
				return nil, err
			}
			info, statErr = root.Lstat(component)
		}
		if statErr != nil {
			_ = root.Close()
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			_ = root.Close()
			return nil, fmt.Errorf("invalid root component %q", component)
		}
		child, err := root.OpenRoot(component)
		if err != nil {
			_ = root.Close()
			return nil, err
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			_ = child.Close()
			_ = root.Close()
			return nil, fmt.Errorf("root component changed while opening")
		}
		_ = root.Close()
		root = child
	}
	return &rootTransaction{path: clean, root: root}, nil
}

func (transaction *rootTransaction) Close() error { return transaction.root.Close() }

// HeldAtPath reports whether path still names the directory held by this
// transaction. Callers use it after path-based preflight and before mutation.
func (transaction *rootTransaction) HeldAtPath() (bool, error) {
	named, err := os.Lstat(transaction.path)
	if err != nil {
		return false, err
	}
	held, err := transaction.root.Stat(".")
	if err != nil {
		return false, err
	}
	return named.IsDir() && named.Mode()&os.ModeSymlink == 0 && os.SameFile(named, held), nil
}

func (transaction *rootTransaction) Relative(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("root path must be absolute")
	}
	relative, err := filepath.Rel(transaction.path, cleanRootPath(path))
	if err != nil || !validRelative(relative) {
		return "", fmt.Errorf("path escapes selected root")
	}
	return relative, nil
}

func (transaction *rootTransaction) ReadRegular(name string) ([]byte, error) {
	data, _, err := transaction.ReadRegularInfo(name)
	return data, err
}

func (transaction *rootTransaction) ReadRegularInfo(name string) ([]byte, os.FileInfo, error) {
	if !validRelative(name) {
		return nil, nil, fmt.Errorf("invalid relative name")
	}
	before, err := transaction.root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxArtifactBytes {
		return nil, nil, fmt.Errorf("not a regular file: %w", err)
	}
	file, err := transaction.root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Size() > maxArtifactBytes || !os.SameFile(before, after) {
		return nil, nil, fmt.Errorf("file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil || len(data) > maxArtifactBytes {
		return nil, nil, fmt.Errorf("read regular file: %w", err)
	}
	final, err := transaction.root.Lstat(name)
	if err != nil || !final.Mode().IsRegular() || !os.SameFile(before, final) {
		return nil, nil, fmt.Errorf("file changed while reading")
	}
	return data, final, nil
}

func (transaction *rootTransaction) Lstat(name string) (os.FileInfo, error) {
	if !validRelative(name) {
		return nil, fmt.Errorf("invalid relative name")
	}
	return transaction.root.Lstat(name)
}

func (transaction *rootTransaction) Open(name string) (*rootFile, error) {
	if !validRelative(name) {
		return nil, fmt.Errorf("invalid relative name")
	}
	file, err := transaction.root.Open(name)
	if err != nil {
		return nil, err
	}
	return &rootFile{File: file, name: name}, nil
}

func (transaction *rootTransaction) CreateExclusive(name string, permission os.FileMode) (*rootFile, error) {
	if !validRelative(name) {
		return nil, fmt.Errorf("invalid relative name")
	}
	file, err := transaction.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permission)
	if err != nil {
		return nil, err
	}
	return &rootFile{File: file, name: name}, nil
}

func (transaction *rootTransaction) CreateTemp(pattern string) (*rootFile, error) {
	return transaction.CreateTempIn(".", pattern)
}

func (transaction *rootTransaction) CreateTempIn(directory, pattern string) (*rootFile, error) {
	if !validDirectory(directory) || strings.ContainsAny(pattern, `/\\`) || pattern == "" {
		return nil, fmt.Errorf("invalid temporary path")
	}
	if err := transaction.requireDirectory(directory); err != nil {
		return nil, err
	}
	for range 100 {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(directory, strings.Replace(pattern, "*", fmt.Sprintf("%x", random), 1))
		file, err := transaction.CreateExclusive(name, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return file, err
	}
	return nil, fmt.Errorf("could not create unique temporary file")
}

func (transaction *rootTransaction) CreateTempDirectory(directory, pattern string) (string, os.FileInfo, error) {
	if !validDirectory(directory) || strings.ContainsAny(pattern, `/\\`) || pattern == "" {
		return "", nil, fmt.Errorf("invalid temporary path")
	}
	if err := transaction.requireDirectory(directory); err != nil {
		return "", nil, err
	}
	for range 100 {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := filepath.Join(directory, strings.Replace(pattern, "*", fmt.Sprintf("%x", random), 1))
		err := transaction.root.Mkdir(name, 0o700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		info, err := transaction.root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("temporary directory changed while creating")
		}
		return name, info, nil
	}
	return "", nil, fmt.Errorf("could not create unique temporary directory")
}

func (transaction *rootTransaction) EnsureDirectory(name string) error {
	if !validDirectory(name) {
		return fmt.Errorf("invalid relative directory")
	}
	if name == "." {
		return nil
	}
	current := "."
	for _, component := range strings.Split(name, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		info, err := transaction.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := transaction.root.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			if err := transaction.SyncDirectory(filepath.Dir(current)); err != nil {
				return err
			}
			info, err = transaction.root.Lstat(current)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("invalid directory component %q", current)
		}
	}
	return transaction.SyncDirectory(name)
}

func (transaction *rootTransaction) requireDirectory(name string) error {
	if name == "." {
		return nil
	}
	info, err := transaction.root.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("invalid relative directory")
	}
	return nil
}

func (transaction *rootTransaction) Publish(temporary, name string) error {
	if !validRelative(temporary) || !validRelative(name) {
		return fmt.Errorf("invalid relative name")
	}
	info, err := transaction.root.Lstat(temporary)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("temporary is not a regular file: %w", err)
	}
	return transaction.root.Link(temporary, name)
}

func (transaction *rootTransaction) StageArtifact(name string, content []byte, permission os.FileMode) (rootStagedArtifact, error) {
	if !validRelative(name) {
		return rootStagedArtifact{}, fmt.Errorf("invalid relative name")
	}
	parent := filepath.Dir(name)
	if err := transaction.EnsureDirectory(parent); err != nil {
		return rootStagedArtifact{}, err
	}
	staging, stagingInfo, err := transaction.CreateTempDirectory(parent, ".vgxness-stage-*")
	if err != nil {
		return rootStagedArtifact{}, err
	}
	temporary, err := transaction.CreateTempIn(staging, ".vgxness-*.tmp")
	if err != nil {
		_ = transaction.Remove(staging)
		return rootStagedArtifact{}, err
	}
	temporaryInfo, err := temporary.Stat()
	if err != nil || temporaryInfo == nil || !temporaryInfo.Mode().IsRegular() {
		_ = temporary.Close()
		_ = transaction.Remove(staging)
		return rootStagedArtifact{}, fmt.Errorf("inspect staged artifact")
	}
	staged := rootStagedArtifact{temporary: temporary.Name(), temporaryInfo: temporaryInfo, staging: staging, stagingInfo: stagingInfo, content: append([]byte(nil), content...)}
	if err := writeAndSyncRootFile(temporary, content); err != nil {
		_ = transaction.CleanupStaged(staged)
		return rootStagedArtifact{}, err
	}
	data, info, err := transaction.ReadRegularInfo(staged.temporary)
	if err != nil || !bytes.Equal(data, content) {
		_ = transaction.CleanupStaged(staged)
		return rootStagedArtifact{}, fmt.Errorf("staged artifact readback failed")
	}
	staged.temporaryInfo = info
	return staged, nil
}

func writeAndSyncRootFile(file *rootFile, content []byte) error {
	_, err := file.Write(content)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (transaction *rootTransaction) PublishStaged(staged rootStagedArtifact, name string) (os.FileInfo, error) {
	if !validRelative(name) {
		return nil, fmt.Errorf("invalid relative name")
	}
	data, info, err := transaction.ReadRegularInfo(staged.temporary)
	if err != nil || staged.temporaryInfo == nil || !os.SameFile(info, staged.temporaryInfo) || !bytes.Equal(data, staged.content) {
		return nil, fmt.Errorf("staged artifact changed before publish")
	}
	if err := transaction.root.Link(staged.temporary, name); err != nil {
		return nil, err
	}
	published, publishedInfo, err := transaction.ReadRegularInfo(name)
	if err != nil || !bytes.Equal(published, staged.content) || !os.SameFile(info, publishedInfo) {
		cleanupErr := transaction.RemoveExact(name, info, staged.content)
		return nil, errors.Join(fmt.Errorf("published artifact changed during publish"), cleanupErr)
	}
	if err := transaction.SyncDirectory(filepath.Dir(name)); err != nil {
		cleanupErr := transaction.RemoveExact(name, publishedInfo, staged.content)
		return nil, errors.Join(err, cleanupErr)
	}
	return publishedInfo, nil
}

func (transaction *rootTransaction) RemoveExact(name string, expected os.FileInfo, content []byte) error {
	data, info, err := transaction.ReadRegularInfo(name)
	if err != nil || expected == nil || !os.SameFile(info, expected) || !bytes.Equal(data, content) {
		return fmt.Errorf("artifact changed before removal")
	}
	directory := filepath.Dir(name)
	quarantineDirectory, _, err := transaction.CreateTempDirectory(directory, ".vgxness-remove-*")
	if err != nil {
		return err
	}
	quarantine := filepath.Join(quarantineDirectory, "artifact")
	if transaction.beforeRemoveRename != nil {
		transaction.beforeRemoveRename(name)
	}
	if err := transaction.root.Rename(name, quarantine); err != nil {
		cleanupErr := transaction.root.Remove(quarantineDirectory)
		return errors.Join(err, cleanupErr, transaction.SyncDirectory(directory))
	}
	if transaction.afterRemoveRename != nil {
		transaction.afterRemoveRename(name, quarantine)
	}
	quarantined, quarantinedInfo, readErr := transaction.ReadRegularInfo(quarantine)
	if readErr != nil || !os.SameFile(quarantinedInfo, expected) || !bytes.Equal(quarantined, content) {
		retentionErr := errors.Join(transaction.SyncDirectory(quarantineDirectory), transaction.SyncDirectory(directory))
		restoreErr := transaction.root.Link(quarantine, name)
		if restoreErr == nil {
			restoreErr = transaction.SyncDirectory(directory)
		}
		return errors.Join(fmt.Errorf("artifact changed during removal; retained at %q", filepath.Join(transaction.path, quarantine)), retentionErr, restoreErr)
	}
	if err := transaction.root.Remove(quarantine); err != nil {
		return err
	}
	if err := transaction.root.Remove(quarantineDirectory); err != nil {
		return err
	}
	if err := transaction.SyncDirectory(directory); err != nil {
		return err
	}
	if _, err := transaction.root.Lstat(name); err == nil {
		return fmt.Errorf("artifact recreated during removal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (transaction *rootTransaction) Backup(name string, content []byte) (rootArtifactBackup, error) {
	return transaction.BackupIn(name, filepath.Dir(name), content)
}

func (transaction *rootTransaction) BackupIn(name, directory string, content []byte) (rootArtifactBackup, error) {
	data, info, err := transaction.ReadRegularInfo(name)
	if err != nil || !bytes.Equal(data, content) {
		return rootArtifactBackup{}, fmt.Errorf("artifact changed before backup")
	}
	temporary, err := transaction.CreateTempIn(directory, ".vgxness-previous-*.tmp")
	if err != nil {
		return rootArtifactBackup{}, err
	}
	temporaryInfo, statErr := temporary.Stat()
	closeErr := temporary.Close()
	if statErr != nil || closeErr != nil || temporaryInfo == nil {
		return rootArtifactBackup{}, fmt.Errorf("inspect backup placeholder")
	}
	if err := transaction.RemoveExact(temporary.Name(), temporaryInfo, nil); err != nil {
		return rootArtifactBackup{}, err
	}
	if err := transaction.root.Link(name, temporary.Name()); err != nil {
		return rootArtifactBackup{}, err
	}
	backupData, backupInfo, err := transaction.ReadRegularInfo(temporary.Name())
	if err != nil || !bytes.Equal(backupData, content) || !os.SameFile(info, backupInfo) {
		cleanupErr := transaction.RemoveExact(temporary.Name(), info, content)
		return rootArtifactBackup{}, errors.Join(fmt.Errorf("backup changed while creating"), cleanupErr)
	}
	if err := transaction.SyncDirectory(directory); err != nil {
		cleanupErr := transaction.RemoveExact(temporary.Name(), backupInfo, content)
		return rootArtifactBackup{}, errors.Join(err, cleanupErr)
	}
	return rootArtifactBackup{name: temporary.Name(), info: backupInfo, content: append([]byte(nil), content...)}, nil
}

// BackupAs creates a durable, caller-named hard-link backup within the held
// root. It is used for user-visible uninstall backups whose names are part of
// the provider contract.
func (transaction *rootTransaction) BackupAs(name, backupName string, content []byte) (rootArtifactBackup, error) {
	if !validRelative(name) || !validRelative(backupName) {
		return rootArtifactBackup{}, fmt.Errorf("invalid relative name")
	}
	data, info, err := transaction.ReadRegularInfo(name)
	if err != nil || !bytes.Equal(data, content) {
		return rootArtifactBackup{}, fmt.Errorf("artifact changed before backup")
	}
	if _, err := transaction.root.Lstat(backupName); !errors.Is(err, os.ErrNotExist) {
		return rootArtifactBackup{}, fmt.Errorf("backup already exists")
	}
	if err := transaction.root.Link(name, backupName); err != nil {
		return rootArtifactBackup{}, err
	}
	backupData, backupInfo, err := transaction.ReadRegularInfo(backupName)
	if err != nil || !bytes.Equal(backupData, content) || !os.SameFile(info, backupInfo) {
		cleanupErr := transaction.RemoveExact(backupName, info, content)
		return rootArtifactBackup{}, errors.Join(fmt.Errorf("backup changed while creating"), cleanupErr)
	}
	if err := transaction.SyncDirectory(filepath.Dir(backupName)); err != nil {
		cleanupErr := transaction.RemoveExact(backupName, backupInfo, content)
		return rootArtifactBackup{}, errors.Join(err, cleanupErr)
	}
	return rootArtifactBackup{name: backupName, info: backupInfo, content: append([]byte(nil), content...)}, nil
}

// Anchor creates a durable hard-link predecessor under the artifact's held
// parent using the legacy reinstall anchor name pattern.
func (transaction *rootTransaction) Anchor(name string, content []byte) (rootArtifactBackup, error) {
	directory := filepath.Dir(name)
	data, info, err := transaction.ReadRegularInfo(name)
	if err != nil || !bytes.Equal(data, content) {
		return rootArtifactBackup{}, fmt.Errorf("artifact changed before anchor")
	}
	temporary, err := transaction.CreateTempIn(directory, ".vgxness-reinstall-old-*.tmp")
	if err != nil {
		return rootArtifactBackup{}, err
	}
	temporaryInfo, statErr := temporary.Stat()
	closeErr := temporary.Close()
	if statErr != nil || closeErr != nil || temporaryInfo == nil || transaction.RemoveExact(temporary.Name(), temporaryInfo, nil) != nil {
		return rootArtifactBackup{}, fmt.Errorf("inspect anchor placeholder")
	}
	if err := transaction.root.Link(name, temporary.Name()); err != nil {
		return rootArtifactBackup{}, err
	}
	anchor, anchorInfo, err := transaction.ReadRegularInfo(temporary.Name())
	if err != nil || !bytes.Equal(anchor, content) || !os.SameFile(info, anchorInfo) {
		cleanupErr := transaction.RemoveExact(temporary.Name(), info, content)
		return rootArtifactBackup{}, errors.Join(fmt.Errorf("anchor changed while creating"), cleanupErr)
	}
	if err := transaction.SyncDirectory(directory); err != nil {
		cleanupErr := transaction.RemoveExact(temporary.Name(), anchorInfo, content)
		return rootArtifactBackup{}, errors.Join(err, cleanupErr)
	}
	return rootArtifactBackup{name: temporary.Name(), info: anchorInfo, content: append([]byte(nil), content...)}, nil
}

func (transaction *rootTransaction) RestoreBackup(backup rootArtifactBackup, name string) error {
	if !validRelative(name) || !validRelative(backup.name) {
		return fmt.Errorf("invalid relative name")
	}
	if _, err := transaction.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("target exists before restore")
	}
	data, info, err := transaction.ReadRegularInfo(backup.name)
	if err != nil || backup.info == nil || !os.SameFile(info, backup.info) || !bytes.Equal(data, backup.content) {
		return fmt.Errorf("backup changed before restore")
	}
	if err := transaction.root.Link(backup.name, name); err != nil {
		return err
	}
	if err := transaction.RemoveExact(backup.name, info, backup.content); err != nil {
		return err
	}
	return transaction.SyncDirectory(filepath.Dir(name))
}

// RestoreObservedAnchor restores an anchor whose content may have changed after
// it was observed, but whose inode must still be the one originally anchored.
func (transaction *rootTransaction) RestoreObservedAnchor(anchor rootArtifactBackup, name string) error {
	if !validRelative(name) || !validRelative(anchor.name) {
		return fmt.Errorf("invalid relative name")
	}
	data, info, err := transaction.ReadRegularInfo(anchor.name)
	if err != nil || anchor.info == nil || !os.SameFile(info, anchor.info) {
		return fmt.Errorf("anchor changed before restore")
	}
	if _, err := transaction.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("target exists before restore")
	}
	if err := transaction.root.Link(anchor.name, name); err != nil {
		return err
	}
	restored, restoredInfo, err := transaction.ReadRegularInfo(name)
	if err != nil || !os.SameFile(restoredInfo, info) || !bytes.Equal(restored, data) {
		return fmt.Errorf("restored target changed during restore")
	}
	current, currentInfo, err := transaction.ReadRegularInfo(anchor.name)
	if err != nil || !os.SameFile(currentInfo, info) || !bytes.Equal(current, data) {
		return fmt.Errorf("anchor changed during restore")
	}
	if err := transaction.RemoveExact(anchor.name, info, data); err != nil {
		return err
	}
	return transaction.SyncDirectory(filepath.Dir(name))
}

func (transaction *rootTransaction) RollbackPublished(staged rootStagedArtifact, name string, published os.FileInfo, backup *rootArtifactBackup) error {
	data, current, err := transaction.ReadRegularInfo(name)
	if err != nil || published == nil || !os.SameFile(current, published) || !bytes.Equal(data, staged.content) {
		return fmt.Errorf("published artifact changed before rollback")
	}
	if err := transaction.RemoveExact(name, current, staged.content); err != nil {
		return err
	}
	if backup != nil {
		return transaction.RestoreBackup(*backup, name)
	}
	return nil
}

func (transaction *rootTransaction) CleanupStaged(staged rootStagedArtifact) error {
	if staged.temporary != "" {
		if _, err := transaction.root.Lstat(staged.temporary); err == nil {
			if err := transaction.RemoveExact(staged.temporary, staged.temporaryInfo, staged.content); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if staged.staging == "" {
		return nil
	}
	info, err := transaction.root.Lstat(staged.staging)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || staged.stagingInfo == nil || !info.IsDir() || !os.SameFile(info, staged.stagingInfo) {
		return fmt.Errorf("staging directory changed before cleanup")
	}
	directory, err := transaction.root.Open(staged.staging)
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil || len(entries) != 0 {
		return fmt.Errorf("staging directory is not empty")
	}
	if err := transaction.root.Remove(staged.staging); err != nil {
		return err
	}
	return transaction.SyncDirectory(filepath.Dir(staged.staging))
}

func (transaction *rootTransaction) Same(name string, info os.FileInfo) (bool, error) {
	if !validRelative(name) {
		return false, fmt.Errorf("invalid relative name")
	}
	current, err := transaction.root.Lstat(name)
	return err == nil && os.SameFile(info, current), err
}

func (transaction *rootTransaction) Remove(name string) error {
	if !validRelative(name) {
		return fmt.Errorf("invalid relative name")
	}
	return transaction.root.Remove(name)
}

func (transaction *rootTransaction) Rename(oldName, newName string) error {
	if !validRelative(oldName) || !validRelative(newName) {
		return fmt.Errorf("invalid relative name")
	}
	return transaction.root.Rename(oldName, newName)
}

func (transaction *rootTransaction) SyncDirectory(name string) error {
	if name != "." && !validRelative(name) {
		return fmt.Errorf("invalid relative name")
	}
	if err := transaction.requireDirectory(name); err != nil {
		return err
	}
	directory, err := transaction.root.Open(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %w", err)
	}
	return directory.Sync()
}

func validRelative(name string) bool {
	return name != "" && name != "." && !filepath.IsAbs(name) && filepath.Clean(name) == name && !escapes(name)
}

func validDirectory(name string) bool {
	return name == "." || validRelative(name)
}

func escapes(name string) bool {
	return name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator))
}

func cleanRootPath(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "darwin" && (clean == "/var" || strings.HasPrefix(clean, "/var/")) {
		return "/private" + clean
	}
	return clean
}

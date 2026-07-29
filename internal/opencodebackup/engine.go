package opencodebackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	manifestName = "manifest.json"
	payloadName  = "files"
)

var (
	snapshotIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{16}$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func ValidateSnapshotID(snapshotID string) error {
	if !snapshotIDPattern.MatchString(snapshotID) {
		return invalid("validate snapshot id", "", nil)
	}
	return nil
}

func ValidatePreviewSHA256(digest string) error {
	if !sha256Pattern.MatchString(digest) {
		return invalid("validate preview digest", "", nil)
	}
	return nil
}

type Engine struct {
	sourceRoot            string
	backupRoot            string
	managedPaths          []string
	launcher              *LauncherMetadata
	publishRestoreFile    func(string, string) error
	syncRestoreDirectory  func(string) error
	syncPublishedSnapshot func(string) error
}

type sourceFile struct {
	rel  string
	path string
}

func New(options Options) (*Engine, error) {
	sourceRoot, err := absoluteRoot(options.SourceRoot)
	if err != nil {
		return nil, invalid("resolve source root", "", err)
	}
	backupValue := options.BackupRoot
	if backupValue == "" {
		home := options.HomeDir
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				return nil, invalid("resolve home directory", "", err)
			}
		}
		home, err = absoluteRoot(home)
		if err != nil {
			return nil, invalid("resolve home directory", "", err)
		}
		backupValue = filepath.Join(home, ".local", "share", "vgxness", "backups", "opencode")
	}
	backupRoot, err := absoluteRoot(backupValue)
	if err != nil {
		return nil, invalid("resolve backup root", "", err)
	}
	if containsPath(sourceRoot, backupRoot) || containsPath(backupRoot, sourceRoot) {
		return nil, invalid("validate roots", "", nil)
	}

	managed := append([]string(nil), options.ManagedPaths...)
	if len(managed) > MaxEntries {
		return nil, invalid("validate managed paths", "", errors.New("entry count limit exceeded"))
	}
	seen := make(map[string]struct{}, len(managed))
	for _, relative := range managed {
		if err := validateRelativePath(relative); err != nil {
			return nil, invalid("validate managed path", relative, err)
		}
		if _, exists := seen[relative]; exists {
			return nil, invalid("validate managed path", relative, errors.New("duplicate path"))
		}
		seen[relative] = struct{}{}
	}
	sort.Strings(managed)
	launcher := cloneLauncher(options.Launcher)
	if err := validateLauncher(launcher); err != nil {
		return nil, invalid("validate launcher metadata", "", err)
	}
	return &Engine{
		sourceRoot: sourceRoot, backupRoot: backupRoot, managedPaths: managed, launcher: launcher,
		publishRestoreFile: os.Link, syncRestoreDirectory: syncDirectory, syncPublishedSnapshot: syncDirectory,
	}, nil
}

func (e *Engine) Create(ctx context.Context, mode Mode) (snapshot Snapshot, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := mode.Validate(); err != nil {
		return Snapshot{}, err
	}
	if err := requireSafeDirectory(e.sourceRoot); err != nil {
		return Snapshot{}, wrapFilesystem("inspect source root", e.sourceRoot, err)
	}
	if err := ensurePrivateRoot(e.backupRoot); err != nil {
		return Snapshot{}, wrapFilesystem("prepare backup root", e.backupRoot, err)
	}
	files, err := e.collect(ctx, mode)
	if err != nil {
		return Snapshot{}, err
	}

	id, err := newSnapshotID()
	if err != nil {
		return Snapshot{}, wrapFilesystem("generate snapshot id", "", err)
	}
	temporary, err := os.MkdirTemp(e.backupRoot, ".tmp-")
	if err != nil {
		return Snapshot{}, wrapFilesystem("create temporary snapshot", e.backupRoot, err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := os.Chmod(temporary, 0o700); err != nil {
		return Snapshot{}, wrapFilesystem("secure temporary snapshot", "", err)
	}
	payload := filepath.Join(temporary, payloadName)
	if err := os.Mkdir(payload, 0o700); err != nil {
		return Snapshot{}, wrapFilesystem("create snapshot payload", "", err)
	}
	if err := os.Chmod(payload, 0o700); err != nil {
		return Snapshot{}, wrapFilesystem("secure snapshot payload", "", err)
	}

	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		SnapshotID:    id,
		CreatedAt:     time.Now().UTC().Round(0),
		Mode:          mode,
		SourceRoot:    e.sourceRoot,
		Entries:       make([]Entry, 0, len(files)),
		Launcher:      cloneLauncher(e.launcher),
	}
	for _, source := range files {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		destination := filepath.Join(payload, filepath.FromSlash(source.rel))
		if err := makePrivateParents(payload, path.Dir(source.rel)); err != nil {
			return Snapshot{}, wrapFilesystem("create payload directory", source.rel, err)
		}
		entry, err := copySourceFile(ctx, source.path, destination, source.rel)
		if err != nil {
			return Snapshot{}, err
		}
		if manifest.TotalBytes > MaxTotalBytes-entry.Size {
			return Snapshot{}, invalid("create snapshot", source.rel, errors.New("total size limit exceeded"))
		}
		manifest.TotalBytes += entry.Size
		manifest.Entries = append(manifest.Entries, entry)
	}

	if err := writeManifest(temporary, manifest); err != nil {
		return Snapshot{}, err
	}
	if err := syncTreeDirectories(temporary); err != nil {
		return Snapshot{}, wrapFilesystem("sync temporary snapshot", "", err)
	}
	verified, err := verifyDirectory(ctx, temporary, id)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	final := filepath.Join(e.backupRoot, id)
	if err := os.Rename(temporary, final); err != nil {
		return Snapshot{}, wrapFilesystem("publish snapshot", id, err)
	}
	published = true
	verified.Directory = final
	if err := e.syncPublishedSnapshot(e.backupRoot); err != nil {
		return verified, wrapFilesystem("publish snapshot", id, err)
	}
	return verified, nil
}

func (e *Engine) Verify(ctx context.Context, snapshotID string) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshotIDPattern.MatchString(snapshotID) {
		return Snapshot{}, invalid("verify snapshot id", "", nil)
	}
	if err := requirePrivateDirectory(e.backupRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, &Error{Kind: ErrNotFound, Op: "verify snapshot"}
		}
		return Snapshot{}, corrupt("verify backup root", "", err)
	}
	directory := filepath.Join(e.backupRoot, snapshotID)
	if _, err := os.Lstat(directory); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, &Error{Kind: ErrNotFound, Op: "verify snapshot", Path: snapshotID}
		}
		return Snapshot{}, corrupt("inspect snapshot", snapshotID, err)
	}
	snapshot, err := verifyDirectory(ctx, directory, snapshotID)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Manifest.SourceRoot != e.sourceRoot {
		return Snapshot{}, corrupt("verify snapshot source root", snapshotID, nil)
	}
	return snapshot, nil
}

func (e *Engine) List(ctx context.Context) ([]Summary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensurePrivateRoot(e.backupRoot); err != nil {
		return nil, wrapFilesystem("prepare backup root", e.backupRoot, err)
	}
	entries, err := os.ReadDir(e.backupRoot)
	if err != nil {
		return nil, wrapFilesystem("list snapshots", e.backupRoot, err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		if !snapshotIDPattern.MatchString(entry.Name()) || !entry.IsDir() {
			return nil, corrupt("list snapshots", entry.Name(), nil)
		}
		ids = append(ids, entry.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	summaries := make([]Summary, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snapshot, err := e.Verify(ctx, id)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, snapshot.Summary)
	}
	return summaries, nil
}

func (e *Engine) collect(ctx context.Context, mode Mode) ([]sourceFile, error) {
	if mode == ModeManaged {
		files := make([]sourceFile, 0, len(e.managedPaths))
		for _, relative := range e.managedPaths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			file, exists, err := inspectManagedFile(e.sourceRoot, relative)
			if err != nil {
				return nil, err
			}
			if exists {
				files = append(files, file)
			}
		}
		return files, nil
	}
	files := make([]sourceFile, 0)
	err := filepath.WalkDir(e.sourceRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return wrapFilesystem("walk source", "", walkErr)
		}
		if filePath == e.sourceRoot {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return wrapFilesystem("inspect source entry", "", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return unsupported("walk source", relativeDisplay(e.sourceRoot, filePath), nil)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return unsupported("walk source", relativeDisplay(e.sourceRoot, filePath), nil)
		}
		relative := relativeDisplay(e.sourceRoot, filePath)
		if err := validateRelativePath(relative); err != nil {
			return invalid("walk source", relative, err)
		}
		if len(files) == MaxEntries {
			return invalid("walk source", "", errors.New("entry count limit exceeded"))
		}
		if info.Size() < 0 || info.Size() > MaxFileSize {
			return invalid("walk source", relative, errors.New("file size limit exceeded"))
		}
		files = append(files, sourceFile{rel: relative, path: filePath})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, nil
}

func inspectManagedFile(root, relative string) (sourceFile, bool, error) {
	current := root
	components := strings.Split(relative, "/")
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return sourceFile{}, false, nil
		}
		if err != nil {
			return sourceFile{}, false, wrapFilesystem("inspect managed path", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return sourceFile{}, false, unsupported("inspect managed path", relative, nil)
		}
		if index < len(components)-1 {
			if !info.IsDir() {
				return sourceFile{}, false, unsupported("inspect managed path", relative, nil)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return sourceFile{}, false, unsupported("inspect managed path", relative, nil)
		}
		if info.Size() < 0 || info.Size() > MaxFileSize {
			return sourceFile{}, false, invalid("inspect managed path", relative, errors.New("file size limit exceeded"))
		}
	}
	return sourceFile{rel: relative, path: current}, true, nil
}

func copySourceFile(ctx context.Context, source, destination, relative string) (Entry, error) {
	before, err := os.Lstat(source)
	if err != nil {
		return Entry{}, wrapFilesystem("inspect source file", relative, err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return Entry{}, unsupported("copy source file", relative, nil)
	}
	input, err := os.Open(source)
	if err != nil {
		return Entry{}, wrapFilesystem("open source file", relative, err)
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return Entry{}, unsupported("copy raced source file", relative, err)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Entry{}, wrapFilesystem("create payload file", relative, err)
	}
	remove := true
	defer func() {
		_ = output.Close()
		if remove {
			_ = os.Remove(destination)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(output, hash), io.LimitReader(contextReader{ctx: ctx, reader: input}, MaxFileSize+1))
	if err != nil {
		return Entry{}, wrapFilesystem("copy source file", relative, err)
	}
	if written > MaxFileSize {
		return Entry{}, invalid("copy source file", relative, errors.New("file size limit exceeded"))
	}
	afterOpen, statErr := input.Stat()
	afterPath, pathErr := os.Lstat(source)
	if statErr != nil || pathErr != nil || !os.SameFile(before, afterOpen) || !os.SameFile(before, afterPath) || before.Size() != afterOpen.Size() || !before.ModTime().Equal(afterOpen.ModTime()) || written != afterOpen.Size() {
		return Entry{}, unsupported("copy raced source file", relative, errors.Join(statErr, pathErr))
	}
	if err := output.Chmod(0o600); err != nil {
		return Entry{}, wrapFilesystem("secure payload file", relative, err)
	}
	if err := output.Sync(); err != nil {
		return Entry{}, wrapFilesystem("sync payload file", relative, err)
	}
	if err := output.Close(); err != nil {
		return Entry{}, wrapFilesystem("close payload file", relative, err)
	}
	remove = false
	return Entry{Path: relative, Size: written, Mode: uint32(before.Mode().Perm()), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func writeManifest(directory string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return corrupt("encode manifest", "", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > MaxManifestSize {
		return invalid("encode manifest", "", errors.New("manifest size limit exceeded"))
	}
	file, err := os.OpenFile(filepath.Join(directory, manifestName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return wrapFilesystem("create manifest", "", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return wrapFilesystem("write manifest", "", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return wrapFilesystem("secure manifest", "", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return wrapFilesystem("sync manifest", "", err)
	}
	if err := file.Close(); err != nil {
		return wrapFilesystem("close manifest", "", err)
	}
	return nil
}

func verifyDirectory(ctx context.Context, directory, expectedID string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := requirePrivateDirectory(directory); err != nil {
		return Snapshot{}, corrupt("verify snapshot directory", expectedID, err)
	}
	rootEntries, err := os.ReadDir(directory)
	if err != nil {
		return Snapshot{}, corrupt("read snapshot directory", expectedID, err)
	}
	if len(rootEntries) != 2 || rootEntries[0].Name() != payloadName || rootEntries[1].Name() != manifestName {
		return Snapshot{}, corrupt("verify snapshot layout", expectedID, nil)
	}
	if !rootEntries[0].IsDir() || rootEntries[1].Type()&os.ModeType != 0 {
		return Snapshot{}, corrupt("verify snapshot layout", expectedID, nil)
	}
	manifest, err := readManifest(filepath.Join(directory, manifestName), expectedID)
	if err != nil {
		return Snapshot{}, err
	}
	payload := filepath.Join(directory, payloadName)
	if err := requirePrivateDirectory(payload); err != nil {
		return Snapshot{}, corrupt("verify payload directory", expectedID, err)
	}
	expected := make(map[string]Entry, len(manifest.Entries))
	expectedDirectories := make(map[string]struct{})
	for _, entry := range manifest.Entries {
		expected[entry.Path] = entry
		for directory := path.Dir(entry.Path); directory != "."; directory = path.Dir(directory) {
			expectedDirectories[directory] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(expected))
	err = filepath.WalkDir(payload, func(filePath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return corrupt("walk payload", expectedID, walkErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filePath == payload {
			return nil
		}
		info, err := dirEntry.Info()
		if err != nil {
			return corrupt("inspect payload", expectedID, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return corrupt("verify payload type", expectedID, nil)
		}
		if info.IsDir() {
			if err := requirePrivateMode(info, 0o700); err != nil {
				return corrupt("verify payload permissions", expectedID, err)
			}
			if _, exists := expectedDirectories[relativeDisplay(payload, filePath)]; !exists {
				return corrupt("verify extra payload directory", expectedID, nil)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return corrupt("verify payload type", expectedID, nil)
		}
		relative := relativeDisplay(payload, filePath)
		entry, exists := expected[relative]
		if !exists {
			return corrupt("verify extra payload", relative, nil)
		}
		if err := requirePrivateMode(info, 0o600); err != nil {
			return corrupt("verify payload permissions", relative, err)
		}
		size, digest, _, err := hashRegularFile(ctx, filePath)
		if err != nil {
			return corrupt("hash payload", relative, err)
		}
		if size != entry.Size || digest != entry.SHA256 {
			return corrupt("verify payload digest", relative, nil)
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	if len(seen) != len(expected) {
		return Snapshot{}, corrupt("verify missing payload", expectedID, nil)
	}
	summary := summaryOf(manifest)
	return Snapshot{Manifest: manifest, Summary: summary, Directory: directory}, nil
}

func readManifest(filePath, expectedID string) (Manifest, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return Manifest{}, corrupt("inspect manifest", expectedID, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > MaxManifestSize {
		return Manifest{}, corrupt("verify manifest", expectedID, nil)
	}
	if err := requirePrivateMode(info, 0o600); err != nil {
		return Manifest{}, corrupt("verify manifest permissions", expectedID, err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return Manifest{}, corrupt("open manifest", expectedID, err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return Manifest{}, corrupt("open manifest", expectedID, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxManifestSize+1))
	afterOpen, statErr := file.Stat()
	afterPath, pathErr := os.Lstat(filePath)
	closeErr := file.Close()
	if err != nil || statErr != nil || pathErr != nil || closeErr != nil || int64(len(data)) > MaxManifestSize || !os.SameFile(info, afterOpen) || !os.SameFile(info, afterPath) || info.Size() != afterOpen.Size() || !info.ModTime().Equal(afterOpen.ModTime()) {
		return Manifest{}, corrupt("read manifest", expectedID, errors.Join(err, statErr, pathErr, closeErr))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, corrupt("decode manifest", expectedID, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Manifest{}, corrupt("decode manifest", expectedID, nil)
	}
	if err := requireManifestFields(data); err != nil {
		return Manifest{}, corrupt("validate manifest", expectedID, err)
	}
	if err := validateManifest(manifest, expectedID); err != nil {
		return Manifest{}, corrupt("validate manifest", expectedID, err)
	}
	return manifest, nil
}

func requireManifestFields(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	for _, key := range []string{"schemaVersion", "snapshotId", "createdAt", "mode", "sourceRoot", "entries", "totalBytes"} {
		if _, exists := root[key]; !exists {
			return errors.New("required manifest field is missing")
		}
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(root["entries"], &entries); err != nil || entries == nil {
		return errors.New("manifest entries must be an array")
	}
	for _, entry := range entries {
		for _, key := range []string{"path", "size", "mode", "sha256"} {
			if _, exists := entry[key]; !exists {
				return errors.New("required entry field is missing")
			}
		}
	}
	if raw, exists := root["launcher"]; exists {
		var launcher map[string]json.RawMessage
		if err := json.Unmarshal(raw, &launcher); err != nil || launcher == nil {
			return errors.New("launcher metadata must be an object")
		}
		for _, key := range []string{"version", "managedBy"} {
			if _, exists := launcher[key]; !exists {
				return errors.New("required launcher field is missing")
			}
		}
		for _, key := range []string{"launcherPath", "manifestPath"} {
			if value, exists := launcher[key]; exists {
				var pathValue string
				if err := json.Unmarshal(value, &pathValue); err != nil || pathValue == "" {
					return errors.New("launcher path field is invalid")
				}
			}
		}
	}
	return nil
}

func validateManifest(manifest Manifest, expectedID string) error {
	if manifest.SchemaVersion != SchemaVersion || manifest.SnapshotID != expectedID || !snapshotIDPattern.MatchString(manifest.SnapshotID) {
		return errors.New("manifest identity is invalid")
	}
	if manifest.CreatedAt.IsZero() {
		return errors.New("creation timestamp is invalid")
	}
	_, offset := manifest.CreatedAt.Zone()
	if offset != 0 {
		return errors.New("creation timestamp is not UTC")
	}
	if err := manifest.Mode.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(manifest.SourceRoot) || filepath.Clean(manifest.SourceRoot) != manifest.SourceRoot {
		return errors.New("source root is invalid")
	}
	if manifest.Entries == nil || len(manifest.Entries) > MaxEntries {
		return errors.New("entry count is invalid")
	}
	var total int64
	previous := ""
	for _, entry := range manifest.Entries {
		if err := validateRelativePath(entry.Path); err != nil || previous != "" && entry.Path <= previous {
			return errors.New("entry path order is invalid")
		}
		if entry.Size < 0 || entry.Size > MaxFileSize || entry.Mode > 0o777 || !sha256Pattern.MatchString(entry.SHA256) {
			return errors.New("entry metadata is invalid")
		}
		if total > MaxTotalBytes-entry.Size {
			return errors.New("total size overflow")
		}
		total += entry.Size
		previous = entry.Path
	}
	if manifest.TotalBytes != total {
		return errors.New("total size does not match entries")
	}
	return validateLauncher(manifest.Launcher)
}

func summaryOf(manifest Manifest) Summary {
	return Summary{
		SchemaVersion: manifest.SchemaVersion,
		SnapshotID:    manifest.SnapshotID,
		CreatedAt:     manifest.CreatedAt,
		Mode:          manifest.Mode,
		SourceRoot:    manifest.SourceRoot,
		EntryCount:    len(manifest.Entries),
		TotalBytes:    manifest.TotalBytes,
		Launcher:      cloneLauncher(manifest.Launcher),
	}
}

func hashRegularFile(ctx context.Context, filePath string) (int64, string, os.FileMode, error) {
	before, err := os.Lstat(filePath)
	if err != nil {
		return 0, "", 0, err
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return 0, "", 0, ErrUnsupported
	}
	file, err := os.Open(filePath)
	if err != nil {
		return 0, "", 0, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return 0, "", 0, errors.Join(ErrUnsupported, err)
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, MaxFileSize+1))
	if err != nil {
		return 0, "", 0, err
	}
	afterOpen, statErr := file.Stat()
	afterPath, pathErr := os.Lstat(filePath)
	if size > MaxFileSize || statErr != nil || pathErr != nil || !os.SameFile(before, afterOpen) || !os.SameFile(before, afterPath) || before.Size() != afterOpen.Size() || !before.ModTime().Equal(afterOpen.ModTime()) || size != afterOpen.Size() {
		return 0, "", 0, errors.Join(ErrUnsupported, statErr, pathErr)
	}
	return size, hex.EncodeToString(hash.Sum(nil)), before.Mode().Perm(), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func absoluteRoot(value string) (string, error) {
	if value == "" || strings.IndexByte(value, 0) >= 0 {
		return "", errors.New("path is empty or invalid")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateRelativePath(relative string) error {
	if relative == "" || len(relative) > MaxPathLength || !fs.ValidPath(relative) || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\:\x00") {
		return errors.New("path is not a canonical safe relative path")
	}
	return nil
}

func validateLauncher(launcher *LauncherMetadata) error {
	if launcher == nil {
		return nil
	}
	if launcher.Version == "" || len(launcher.Version) > 128 || launcher.ManagedBy == "" || len(launcher.ManagedBy) > 128 {
		return errors.New("launcher metadata is invalid")
	}
	if launcher.SHA256 != "" && !sha256Pattern.MatchString(launcher.SHA256) {
		return errors.New("launcher digest is invalid")
	}
	for _, value := range []string{launcher.LauncherSHA256, launcher.ActiveSHA256} {
		if value != "" && !sha256Pattern.MatchString(value) {
			return errors.New("launcher digest is invalid")
		}
	}
	for _, value := range []string{launcher.LauncherPath, launcher.ManifestPath} {
		if value != "" && (!filepath.IsAbs(value) || filepath.Clean(value) != value || len(value) > MaxPathLength) {
			return errors.New("launcher path is invalid")
		}
	}
	return nil
}

func cloneLauncher(launcher *LauncherMetadata) *LauncherMetadata {
	if launcher == nil {
		return nil
	}
	copy := *launcher
	return &copy
}

func newSnapshotID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z-") + hex.EncodeToString(random), nil
}

func relativeDisplay(root, filePath string) string {
	relative, err := filepath.Rel(root, filePath)
	if err != nil {
		return "entry"
	}
	return filepath.ToSlash(relative)
}

func makePrivateParents(root, relativeDirectory string) error {
	if relativeDirectory == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(relativeDirectory, "/") {
		current = filepath.Join(current, component)
		err := os.Mkdir(current, 0o700)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.Join(ErrUnsupported, err)
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func requireSafeDirectory(directory string) error {
	for _, component := range pathAncestors(directory) {
		info, err := os.Lstat(component)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsupported
		}
	}
	return nil
}

func ensurePrivateRoot(directory string) error {
	missing := make([]string, 0)
	current := directory
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return ErrUnsupported
			}
			if err := requireSafeDirectory(current); err != nil {
				return err
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return err
		}
		current = parent
	}
	for index := len(missing) - 1; index >= 0; index-- {
		if err := os.Mkdir(missing[index], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(missing[index])
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.Join(ErrUnsupported, err)
		}
		if err := os.Chmod(missing[index], 0o700); err != nil {
			return err
		}
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	return requirePrivateDirectory(directory)
}

func pathAncestors(filePath string) []string {
	paths := []string{filepath.Clean(filePath)}
	for {
		parent := filepath.Dir(paths[len(paths)-1])
		if parent == paths[len(paths)-1] {
			break
		}
		paths = append(paths, parent)
	}
	for left, right := 0, len(paths)-1; left < right; left, right = left+1, right-1 {
		paths[left], paths[right] = paths[right], paths[left]
	}
	return paths
}

func requirePrivateDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsupported
	}
	return requirePrivateMode(info, 0o700)
}

func requirePrivateMode(info os.FileInfo, expected os.FileMode) error {
	if runtime.GOOS != "windows" && info.Mode().Perm() != expected {
		return fmt.Errorf("permission mode is not private")
	}
	return nil
}

func syncTreeDirectories(root string) error {
	directories := make([]string, 0)
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, filePath)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(directory string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = file.Sync()
	return errors.Join(err, file.Close())
}

func invalid(op, filePath string, err error) error {
	return &Error{Kind: ErrInvalid, Op: op, Path: filePath, Err: err}
}

func corrupt(op, filePath string, err error) error {
	return &Error{Kind: ErrCorrupt, Op: op, Path: filePath, Err: err}
}

func unsupported(op, filePath string, err error) error {
	return &Error{Kind: ErrUnsupported, Op: op, Path: filePath, Err: err}
}

func wrapFilesystem(op, filePath string, err error) error {
	if errors.Is(err, ErrUnsupported) {
		return unsupported(op, filePath, err)
	}
	return &Error{Kind: ErrInvalid, Op: op, Path: filePath, Err: err}
}

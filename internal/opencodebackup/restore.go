package opencodebackup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
)

type observation struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	Size   int64  `json:"size,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type observeDestinationFunc func(context.Context, Entry) (observation, error)

type restoreRefs struct {
	source   *os.Root
	snapshot *snapshotRef
}

func (r *restoreRefs) Close() error { return errors.Join(r.source.Close(), r.snapshot.Close()) }

func (e *Engine) PreviewRestore(ctx context.Context, snapshotID string) (RestorePreview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RestorePreview{}, err
	}
	if err := ValidateSnapshotID(snapshotID); err != nil {
		return RestorePreview{}, err
	}
	refs, err := e.openRestoreRefs(ctx, snapshotID)
	if err != nil {
		return RestorePreview{}, err
	}
	defer refs.Close()
	snapshot, err := e.verifySnapshotRef(ctx, refs.snapshot, snapshotID)
	if err != nil {
		return RestorePreview{}, err
	}
	preview, _, err := e.previewWithObservations(ctx, snapshot, func(ctx context.Context, entry Entry) (observation, error) {
		return observeDestinationAt(ctx, refs.source, entry)
	})
	if err != nil {
		return RestorePreview{}, err
	}
	if err := e.validateRestoreRefs(ctx, refs, snapshotID); err != nil {
		return RestorePreview{}, err
	}
	return preview, nil
}

func (e *Engine) openRestoreRefs(ctx context.Context, snapshotID string) (*restoreRefs, error) {
	source, err := e.sourceAnchor.open()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = source.Close()
		return nil, err
	}
	backup, err := e.backupRootAnchor()
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	ref, err := openSnapshot(backup, snapshotID)
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	return &restoreRefs{source: source, snapshot: ref}, nil
}

func (e *Engine) verifySnapshotRef(ctx context.Context, ref *snapshotRef, snapshotID string) (Snapshot, error) {
	snapshot, err := verifyDirectory(ctx, ref.snapshot, snapshotID)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Manifest.SourceRoot != e.sourceRoot {
		return Snapshot{}, corrupt("verify snapshot source root", snapshotID, nil)
	}
	if err := e.validateMode(snapshot.Manifest.Mode); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (e *Engine) validateRestoreRefs(ctx context.Context, refs *restoreRefs, snapshotID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	held, err := refs.source.Stat(".")
	if err != nil || !held.IsDir() || !os.SameFile(e.sourceAnchor.info, held) {
		return &Error{Kind: ErrConflict, Op: "revalidate held restore source", Path: e.sourceRoot, Err: err}
	}
	source, err := e.sourceAnchor.open()
	if err != nil {
		return err
	}
	if err := source.Close(); err != nil {
		return err
	}
	backup, err := e.backupRootAnchor()
	if err != nil {
		return err
	}
	root, err := backup.open()
	if err != nil {
		return err
	}
	if err := root.Close(); err != nil {
		return err
	}
	return refs.snapshot.revalidate(snapshotID)
}

func (e *Engine) Restore(ctx context.Context, request RestoreRequest) (RestoreResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !snapshotIDPattern.MatchString(request.SnapshotID) || !sha256Pattern.MatchString(request.PreviewSHA256) {
		return RestoreResult{}, invalid("validate restore request", "", nil)
	}
	if len(request.ReplaceConflicts) != 0 {
		return RestoreResult{}, unsupported("replace conflicts", "", nil)
	}
	include := make(map[string]struct{}, len(request.IncludePaths))
	for _, relative := range request.IncludePaths {
		if err := validateRelativePath(relative); err != nil {
			return RestoreResult{}, invalid("validate included restore path", relative, err)
		}
		if _, exists := include[relative]; exists {
			return RestoreResult{}, invalid("validate included restore path", relative, errors.New("duplicate path"))
		}
		include[relative] = struct{}{}
	}
	refs, err := e.openRestoreRefs(ctx, request.SnapshotID)
	if err != nil {
		return RestoreResult{}, err
	}
	defer refs.Close()
	snapshot, err := e.verifySnapshotRef(ctx, refs.snapshot, request.SnapshotID)
	if err != nil {
		return RestoreResult{}, err
	}
	available := make(map[string]struct{}, len(snapshot.Manifest.Entries))
	for _, entry := range snapshot.Manifest.Entries {
		available[entry.Path] = struct{}{}
	}
	for relative := range include {
		if _, exists := available[relative]; !exists {
			return RestoreResult{}, invalid("validate included restore path", relative, nil)
		}
	}
	preview, observations, err := e.previewWithObservations(ctx, snapshot, func(ctx context.Context, entry Entry) (observation, error) {
		return observeDestinationAt(ctx, refs.source, entry)
	})
	if err != nil {
		return RestoreResult{}, err
	}
	if preview.SHA256 != request.PreviewSHA256 {
		return RestoreResult{}, &Error{Kind: ErrConflict, Op: "restore stale preview"}
	}
	byPath := make(map[string]observation, len(observations))
	for _, observed := range observations {
		byPath[observed.Path] = observed
	}
	result := RestoreResult{SnapshotID: request.SnapshotID, Unresolved: make([]string, 0)}
	for _, entry := range snapshot.Manifest.Entries {
		if len(include) > 0 {
			if _, selected := include[entry.Path]; !selected {
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		observed := byPath[entry.Path]
		switch observed.State {
		case "missing":
			published, err := e.restoreMissing(ctx, refs, entry)
			if published {
				result.Created++
			}
			if err != nil {
				return result, err
			}
		case "identical":
			result.Identical++
		default:
			result.Unresolved = append(result.Unresolved, entry.Path)
		}
	}
	if err := e.validateRestoreRefs(ctx, refs, request.SnapshotID); err != nil {
		return result, err
	}
	return result, nil
}

func (e *Engine) previewWithObservations(ctx context.Context, snapshot Snapshot, observe observeDestinationFunc) (RestorePreview, []observation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	preview := RestorePreview{SnapshotID: snapshot.Manifest.SnapshotID, Missing: []string{}, Identical: []string{}, Conflicts: []string{}}
	observations := make([]observation, 0, len(snapshot.Manifest.Entries))
	for _, entry := range snapshot.Manifest.Entries {
		if err := ctx.Err(); err != nil {
			return RestorePreview{}, nil, err
		}
		observed, err := observe(ctx, entry)
		if err != nil {
			return RestorePreview{}, nil, err
		}
		observations = append(observations, observed)
		switch observed.State {
		case "missing":
			preview.Missing = append(preview.Missing, entry.Path)
		case "identical":
			preview.Identical = append(preview.Identical, entry.Path)
		default:
			preview.Conflicts = append(preview.Conflicts, entry.Path)
		}
	}
	manifestBytes, err := json.Marshal(snapshot.Manifest)
	if err != nil {
		return RestorePreview{}, nil, corrupt("digest restore preview", snapshot.Manifest.SnapshotID, err)
	}
	binding := struct {
		SnapshotSHA256 string        `json:"snapshotSha256"`
		Observations   []observation `json:"observations"`
	}{
		SnapshotSHA256: digestBytes(manifestBytes),
		Observations:   observations,
	}
	bindingBytes, err := json.Marshal(binding)
	if err != nil {
		return RestorePreview{}, nil, corrupt("digest restore preview", snapshot.Manifest.SnapshotID, err)
	}
	preview.SHA256 = digestBytes(bindingBytes)
	return preview, observations, nil
}

func observeDestinationAt(ctx context.Context, root *os.Root, entry Entry) (observation, error) {
	observed := observation{Path: entry.Path}
	components := strings.Split(entry.Path, "/")
	for index := range components {
		current := strings.Join(components[:index+1], "/")
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			observed.State = "missing"
			return observed, nil
		}
		if err != nil {
			return observation{}, &Error{Kind: ErrConflict, Op: "inspect restore destination", Path: entry.Path, Err: err}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			observed.State = "conflict-symlink"
			observed.Mode = uint32(info.Mode().Perm())
			return observed, nil
		}
		if index < len(components)-1 {
			if !info.IsDir() {
				observed.State = "conflict-type"
				observed.Mode = uint32(info.Mode().Perm())
				return observed, nil
			}
			continue
		}
		if !info.Mode().IsRegular() {
			observed.State = "conflict-type"
			observed.Mode = uint32(info.Mode().Perm())
			return observed, nil
		}
		size, digest, mode, err := hashRegularFileAt(ctx, root, current)
		if err != nil {
			return observation{}, &Error{Kind: ErrConflict, Op: "inspect restore destination", Path: entry.Path, Err: err}
		}
		observed.Size = size
		observed.SHA256 = digest
		observed.Mode = uint32(mode)
		if size == entry.Size && digest == entry.SHA256 {
			observed.State = "identical"
		} else {
			observed.State = "conflict-regular"
		}
		return observed, nil
	}
	return observation{}, invalid("observe restore destination", entry.Path, nil)
}

func (e *Engine) restoreMissing(ctx context.Context, refs *restoreRefs, entry Entry) (created bool, err error) {
	if err := ensureDestinationParentsAt(refs.source, entry.Path); err != nil {
		return false, err
	}
	staging, err := newRestoreStagingName(entry.Path)
	if err != nil {
		return false, wrapFilesystem("reserve restored staging", entry.Path, err)
	}
	output, err := refs.source.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, wrapFilesystem("reserve restored staging", entry.Path, err)
	}
	stagingInfo, err := output.Stat()
	if err != nil {
		return false, errors.Join(wrapFilesystem("inspect reserved restored staging", entry.Path, err), output.Close())
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	cleanup := func(cause error) (bool, error) {
		if !closed {
			closeErr := output.Close()
			closed = true
			cause = errors.Join(cause, closeErr)
		}
		return false, errors.Join(cause, cleanupRestoreStaging(refs.source, staging, stagingInfo, syncRestoreDirectoriesAt))
	}
	if err := e.copySnapshotEntry(ctx, refs.snapshot.snapshot, entry, output); err != nil {
		return cleanup(err)
	}
	if err := output.Chmod(0o600); err != nil {
		return cleanup(wrapFilesystem("secure restored file", entry.Path, err))
	}
	if err := output.Sync(); err != nil {
		return cleanup(wrapFilesystem("sync restored file", entry.Path, err))
	}
	if err := output.Close(); err != nil {
		closed = true
		return cleanup(wrapFilesystem("close restored file", entry.Path, err))
	}
	closed = true
	if err := verifyRestoredFileAt(ctx, refs.source, staging, entry); err != nil {
		return cleanup(err)
	}
	if err := e.beforeRestorePublish(refs.source, entry.Path); err != nil {
		return cleanup(err)
	}
	if err := refs.source.Link(staging, entry.Path); err != nil {
		kind := wrapFilesystem("publish restored file", entry.Path, err)
		if errors.Is(err, os.ErrExist) {
			kind = &Error{Kind: ErrConflict, Op: "publish restored file", Path: entry.Path, Err: err}
		}
		return cleanup(kind)
	}
	finalInfo, err := refs.source.Lstat(entry.Path)
	if err != nil || !os.SameFile(stagingInfo, finalInfo) {
		return false, errors.Join(corrupt("publish restored file", entry.Path, err), cleanupRestoreStaging(refs.source, staging, stagingInfo, syncRestoreDirectoriesAt))
	}
	if err := verifyRestoredFileAt(ctx, refs.source, entry.Path, entry); err != nil {
		return true, err
	}
	if err := cleanupRestoreStaging(refs.source, staging, stagingInfo, syncRestoreDirectoriesAt); err != nil {
		return true, err
	}
	if err := e.syncRestoreDirectories(refs.source, entry.Path); err != nil {
		return true, wrapFilesystem("sync restored directory", entry.Path, err)
	}
	if err := verifyRestoredFileAt(ctx, refs.source, entry.Path, entry); err != nil {
		return true, err
	}
	return true, nil
}

func newRestoreStagingName(destination string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return path.Join(path.Dir(destination), ".vgxness-restore-"+hex.EncodeToString(bytes)), nil
}

func cleanupRestoreStaging(root *os.Root, staging string, expected os.FileInfo, sync func(*os.Root, string) error) error {
	current, err := root.Lstat(staging)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(current, expected) {
		return errors.Join(wrapFilesystem("inspect restored staging", staging, err), &Error{Kind: ErrConflict, Op: "cleanup restored staging", Path: staging})
	}
	quarantine, err := newRestoreStagingName(staging)
	if err != nil {
		return err
	}
	if err := root.Rename(staging, quarantine); err != nil {
		return err
	}
	quarantined, err := root.Lstat(quarantine)
	if err != nil || !os.SameFile(quarantined, expected) {
		restoreErr := root.Link(quarantine, staging)
		return errors.Join(&Error{Kind: ErrConflict, Op: "cleanup restored staging", Path: staging}, err, restoreErr)
	}
	if err := root.Remove(quarantine); err != nil {
		return err
	}
	return sync(root, staging)
}

func copySnapshotEntry(ctx context.Context, snapshot *os.Root, entry Entry, output io.Writer) error {
	source := path.Join(payloadName, entry.Path)
	before, err := snapshot.Lstat(source)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return corrupt("inspect snapshot payload", entry.Path, err)
	}
	input, err := snapshot.Open(source)
	if err != nil {
		return corrupt("open snapshot payload", entry.Path, err)
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return corrupt("open snapshot payload", entry.Path, err)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(output, hash), io.LimitReader(contextReader{ctx: ctx, reader: input}, MaxFileSize+1))
	if err != nil {
		return wrapFilesystem("copy snapshot payload", entry.Path, err)
	}
	afterOpen, statErr := input.Stat()
	afterPath, pathErr := snapshot.Lstat(source)
	if statErr != nil || pathErr != nil || !os.SameFile(before, afterOpen) || !os.SameFile(before, afterPath) || before.Size() != afterOpen.Size() || !before.ModTime().Equal(afterOpen.ModTime()) || written != afterOpen.Size() || written != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		return corrupt("copy snapshot payload", entry.Path, nil)
	}
	return nil
}

func verifyRestoredFileAt(ctx context.Context, root *os.Root, filePath string, entry Entry) error {
	size, digest, mode, err := hashRegularFileAt(ctx, root, filePath)
	if err != nil {
		return wrapFilesystem("verify restored file", entry.Path, err)
	}
	if size != entry.Size || digest != entry.SHA256 || runtime.GOOS != "windows" && mode != 0o600 {
		return corrupt("verify restored file", entry.Path, nil)
	}
	return nil
}

func ensureDestinationParentsAt(root *os.Root, relative string) error {
	current := "."
	components := strings.Split(relative, "/")
	for _, component := range components[:len(components)-1] {
		current = path.Join(current, component)
		mkdirErr := root.Mkdir(current, 0o700)
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return wrapFilesystem("create restored directory", relative, mkdirErr)
		}
		info, err := root.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return unsupported("create restored directory", relative, err)
		}
		if mkdirErr == nil {
			if err := root.Chmod(current, 0o700); err != nil {
				return wrapFilesystem("secure restored directory", relative, err)
			}
		}
	}
	return nil
}

func syncRestoreDirectoriesAt(root *os.Root, destination string) error {
	for directory := path.Dir(destination); ; directory = path.Dir(directory) {
		if err := syncDirectoryAt(root, directory); err != nil {
			return err
		}
		if directory == "." {
			return nil
		}
	}
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

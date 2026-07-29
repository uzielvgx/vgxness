package opencodebackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func (e *Engine) PreviewRestore(ctx context.Context, snapshotID string) (RestorePreview, error) {
	snapshot, err := e.Verify(ctx, snapshotID)
	if err != nil {
		return RestorePreview{}, err
	}
	return e.preview(ctx, snapshot)
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
	snapshot, err := e.Verify(ctx, request.SnapshotID)
	if err != nil {
		return RestoreResult{}, err
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
	available := make(map[string]struct{}, len(snapshot.Manifest.Entries))
	for _, entry := range snapshot.Manifest.Entries {
		available[entry.Path] = struct{}{}
	}
	for relative := range include {
		if _, exists := available[relative]; !exists {
			return RestoreResult{}, invalid("validate included restore path", relative, nil)
		}
	}
	preview, observations, err := e.previewWithObservations(ctx, snapshot)
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
			published, err := e.restoreMissing(ctx, snapshot, entry)
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
	return result, nil
}

func (e *Engine) preview(ctx context.Context, snapshot Snapshot) (RestorePreview, error) {
	preview, _, err := e.previewWithObservations(ctx, snapshot)
	return preview, err
}

func (e *Engine) previewWithObservations(ctx context.Context, snapshot Snapshot) (RestorePreview, []observation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := inspectExistingAncestors(e.sourceRoot); err != nil {
		return RestorePreview{}, nil, unsupported("inspect restore root", e.sourceRoot, err)
	}
	preview := RestorePreview{SnapshotID: snapshot.Manifest.SnapshotID, Missing: []string{}, Identical: []string{}, Conflicts: []string{}}
	observations := make([]observation, 0, len(snapshot.Manifest.Entries))
	for _, entry := range snapshot.Manifest.Entries {
		if err := ctx.Err(); err != nil {
			return RestorePreview{}, nil, err
		}
		observed, err := observeDestination(ctx, e.sourceRoot, entry)
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

func observeDestination(ctx context.Context, root string, entry Entry) (observation, error) {
	observed := observation{Path: entry.Path}
	current := root
	components := strings.Split(entry.Path, "/")
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
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
		size, digest, mode, err := hashRegularFile(ctx, current)
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

func (e *Engine) restoreMissing(ctx context.Context, snapshot Snapshot, entry Entry) (bool, error) {
	if err := ensureDestinationParents(e.sourceRoot, entry.Path); err != nil {
		return false, err
	}
	destination := filepath.Join(e.sourceRoot, filepath.FromSlash(entry.Path))
	directory := filepath.Dir(destination)
	output, err := os.CreateTemp(directory, ".vgxness-restore-*")
	if err != nil {
		return false, wrapFilesystem("create restored file", entry.Path, err)
	}
	temporary := output.Name()
	defer func() {
		_ = output.Close()
		_ = os.Remove(temporary)
	}()
	if err := copySnapshotEntry(ctx, snapshot, entry, output); err != nil {
		return false, err
	}
	if err := output.Chmod(0o600); err != nil {
		return false, wrapFilesystem("secure restored file", entry.Path, err)
	}
	if err := output.Sync(); err != nil {
		return false, wrapFilesystem("sync restored file", entry.Path, err)
	}
	if err := output.Close(); err != nil {
		return false, wrapFilesystem("close restored file", entry.Path, err)
	}
	if err := verifyRestoredFile(ctx, temporary, entry); err != nil {
		return false, err
	}
	if err := e.publishRestoreFile(temporary, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, &Error{Kind: ErrConflict, Op: "publish restored file", Path: entry.Path}
		}
		return false, wrapFilesystem("publish restored file", entry.Path, err)
	}
	if err := verifyRestoredFile(ctx, destination, entry); err != nil {
		return true, err
	}
	if err := e.syncRestoreDirectory(directory); err != nil {
		return true, wrapFilesystem("sync restored directory", entry.Path, err)
	}
	return true, nil
}

func copySnapshotEntry(ctx context.Context, snapshot Snapshot, entry Entry, output io.Writer) error {
	source := filepath.Join(snapshot.Directory, payloadName, filepath.FromSlash(entry.Path))
	before, err := os.Lstat(source)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return corrupt("inspect snapshot payload", entry.Path, err)
	}
	input, err := os.Open(source)
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
	afterPath, pathErr := os.Lstat(source)
	if statErr != nil || pathErr != nil || !os.SameFile(before, afterOpen) || !os.SameFile(before, afterPath) || before.Size() != afterOpen.Size() || !before.ModTime().Equal(afterOpen.ModTime()) || written != afterOpen.Size() || written != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		return corrupt("copy snapshot payload", entry.Path, nil)
	}
	return nil
}

func verifyRestoredFile(ctx context.Context, filePath string, entry Entry) error {
	size, digest, mode, err := hashRegularFile(ctx, filePath)
	if err != nil {
		return wrapFilesystem("verify restored file", entry.Path, err)
	}
	if size != entry.Size || digest != entry.SHA256 || runtime.GOOS != "windows" && mode != 0o600 {
		return corrupt("verify restored file", entry.Path, nil)
	}
	return nil
}

func ensureDestinationParents(root, relative string) error {
	if err := ensureRestoreRoot(root); err != nil {
		return err
	}
	current := root
	components := strings.Split(relative, "/")
	for _, component := range components[:len(components)-1] {
		current = filepath.Join(current, component)
		mkdirErr := os.Mkdir(current, 0o700)
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return wrapFilesystem("create restored directory", relative, mkdirErr)
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return unsupported("create restored directory", relative, err)
		}
		if mkdirErr == nil {
			if err := os.Chmod(current, 0o700); err != nil {
				return wrapFilesystem("secure restored directory", relative, err)
			}
		}
	}
	return nil
}

func ensureRestoreRoot(root string) error {
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		if err := ensurePrivateRoot(root); err != nil {
			return wrapFilesystem("create restore root", root, err)
		}
		return nil
	}
	if err := requireSafeDirectory(root); err != nil {
		return unsupported("inspect restore root", root, err)
	}
	return nil
}

func inspectExistingAncestors(filePath string) error {
	for _, ancestor := range pathAncestors(filePath) {
		info, err := os.Lstat(ancestor)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsupported
		}
	}
	return nil
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

package opencode

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
	"runtime"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
)

const (
	reinstallPendingVersion  = 1
	reinstallPendingName     = ".vgxness-reinstall-pending.json"
	maxReinstallPendingBytes = 64 * 1024
)

type reinstallPendingMarker struct {
	Version   int                     `json:"version"`
	Operation string                  `json:"operation"`
	Root      string                  `json:"root"`
	StartedAt string                  `json:"startedAt"`
	Artifacts []reinstallPendingEntry `json:"artifacts"`
}

type reinstallPendingEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type reinstallPendingEvidence struct {
	info   os.FileInfo
	digest [sha256.Size]byte
}

// ReinstallPending inspects provider-owned evidence without mutating it.
func (service *Integration) ReinstallPending(ctx context.Context, options integration.Options) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	root, err := integrationConfigDirectory(options)
	if err != nil {
		return false, err
	}
	held, err := openRootTransaction(root, false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer held.Close()
	return service.reinstallPendingAtRoot(ctx, options, held)
}

func (service *Integration) reinstallPendingAtRoot(ctx context.Context, options integration.Options, root *rootTransaction) (bool, error) {
	marker, _, err := readReinstallPendingAtRoot(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state, err := service.inspect(ctx, options)
	if err != nil {
		return false, err
	}
	held, heldErr := root.HeldAtPath()
	if heldErr != nil || !held {
		return false, fmt.Errorf("OpenCode config root changed during pending inspection")
	}
	layout, err := managedLayout(pendingMarkerRoot(root), state.artifacts)
	if err != nil || !pendingInventoryMatches(marker, layout) {
		return false, fmt.Errorf("invalid reinstall pending inventory")
	}
	return true, nil
}

func (service *Integration) writeReinstallPendingAtRoot(ctx context.Context, root *rootTransaction, layout integration.ManagedLayout) (reinstallPendingEvidence, error) {
	if err := ctx.Err(); err != nil {
		return reinstallPendingEvidence{}, err
	}
	operation := make([]byte, 16)
	if _, err := rand.Read(operation); err != nil {
		return reinstallPendingEvidence{}, err
	}
	markerRoot := pendingMarkerRoot(root)
	marker := reinstallPendingMarker{Version: reinstallPendingVersion, Operation: hex.EncodeToString(operation), Root: markerRoot, StartedAt: service.now().UTC().Format(time.RFC3339Nano), Artifacts: make([]reinstallPendingEntry, len(layout.Artifacts))}
	for index, artifact := range layout.Artifacts {
		marker.Artifacts[index] = reinstallPendingEntry{Path: artifact.RelativePath, SHA256: artifact.SHA256}
	}
	if err := validateReinstallPending(markerRoot, marker); err != nil {
		return reinstallPendingEvidence{}, err
	}
	body, err := json.Marshal(marker)
	if err != nil || len(body)+1 > maxReinstallPendingBytes {
		return reinstallPendingEvidence{}, fmt.Errorf("encode reinstall pending marker")
	}
	body = append(body, '\n')
	file, err := root.CreateExclusive(reinstallPendingName, 0o600)
	if err != nil {
		return reinstallPendingEvidence{}, fmt.Errorf("create reinstall pending marker at %q: %w", filepath.Join(root.path, reinstallPendingName), err)
	}
	if err := writeAndSyncRootFile(file, body); err != nil {
		return reinstallPendingEvidence{}, err
	}
	info, err := root.Lstat(reinstallPendingName)
	if err != nil || !privatePendingFile(info) {
		return reinstallPendingEvidence{}, fmt.Errorf("verify reinstall pending marker")
	}
	if err := root.SyncDirectory("."); err != nil {
		return reinstallPendingEvidence{}, err
	}
	return reinstallPendingEvidence{info: info, digest: sha256.Sum256(body)}, nil
}

func readReinstallPendingAtRoot(root *rootTransaction) (reinstallPendingMarker, os.FileInfo, error) {
	var marker reinstallPendingMarker
	body, info, err := root.ReadRegularInfo(reinstallPendingName)
	if err != nil {
		return marker, nil, err
	}
	if !privatePendingFile(info) || info.Size() <= 0 || info.Size() > maxReinstallPendingBytes || rejectDuplicateJSONKeys(body) != nil {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || validateReinstallPending(pendingMarkerRoot(root), marker) != nil {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker")
	}
	return marker, info, nil
}

func pendingMarkerRoot(root *rootTransaction) string {
	path := root.path
	if runtime.GOOS == "darwin" && strings.HasPrefix(path, "/private/var/") {
		return strings.TrimPrefix(path, "/private")
	}
	return path
}

func clearReinstallPendingAtRoot(root *rootTransaction, expected reinstallPendingEvidence) error {
	body, info, err := root.ReadRegularInfo(reinstallPendingName)
	if err != nil || expected.info == nil || !privatePendingFile(info) || !os.SameFile(info, expected.info) || sha256.Sum256(body) != expected.digest {
		return fmt.Errorf("%w: reinstall pending marker changed before cleanup at %q", integration.ErrRecovery, filepath.Join(root.path, reinstallPendingName))
	}
	if err := root.RemoveExact(reinstallPendingName, info, body); err != nil {
		return fmt.Errorf("%w: remove reinstall pending marker: %v", integration.ErrRecovery, err)
	}
	return nil
}

func (service *Integration) writeReinstallPending(ctx context.Context, root string, layout integration.ManagedLayout) (reinstallPendingEvidence, error) {
	if err := ctx.Err(); err != nil {
		return reinstallPendingEvidence{}, err
	}
	operation := make([]byte, 16)
	if _, err := rand.Read(operation); err != nil {
		return reinstallPendingEvidence{}, err
	}
	marker := reinstallPendingMarker{
		Version: reinstallPendingVersion, Operation: hex.EncodeToString(operation), Root: root,
		StartedAt: service.now().UTC().Format(time.RFC3339Nano),
		Artifacts: make([]reinstallPendingEntry, len(layout.Artifacts)),
	}
	for index, artifact := range layout.Artifacts {
		marker.Artifacts[index] = reinstallPendingEntry{Path: artifact.RelativePath, SHA256: artifact.SHA256}
	}
	if err := validateReinstallPending(root, marker); err != nil {
		return reinstallPendingEvidence{}, err
	}
	body, err := json.Marshal(marker)
	if err != nil || len(body)+1 > maxReinstallPendingBytes {
		return reinstallPendingEvidence{}, fmt.Errorf("encode reinstall pending marker")
	}
	body = append(body, '\n')
	markerPath := filepath.Join(root, reinstallPendingName)
	file, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return reinstallPendingEvidence{}, err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return reinstallPendingEvidence{}, err
	}
	if _, err := file.Write(body); err != nil {
		return reinstallPendingEvidence{}, err
	}
	if err := file.Sync(); err != nil {
		return reinstallPendingEvidence{}, err
	}
	opened, err := file.Stat()
	if err != nil || !privatePendingFile(opened) {
		return reinstallPendingEvidence{}, fmt.Errorf("verify reinstall pending marker")
	}
	if err := file.Close(); err != nil {
		return reinstallPendingEvidence{}, err
	}
	if err := syncDirectory(root); err != nil {
		return reinstallPendingEvidence{}, err
	}
	after, err := os.Lstat(markerPath)
	if err != nil || !privatePendingFile(after) || !os.SameFile(opened, after) {
		return reinstallPendingEvidence{}, fmt.Errorf("verify reinstall pending marker identity")
	}
	return reinstallPendingEvidence{info: opened, digest: sha256.Sum256(body)}, nil
}

func readReinstallPending(root string) (reinstallPendingMarker, os.FileInfo, error) {
	var marker reinstallPendingMarker
	markerPath := filepath.Join(root, reinstallPendingName)
	before, err := os.Lstat(markerPath)
	if err != nil {
		return marker, nil, err
	}
	if !privatePendingFile(before) || before.Size() <= 0 || before.Size() > maxReinstallPendingBytes {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker")
	}
	file, err := os.Open(markerPath)
	if err != nil {
		return marker, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !privatePendingFile(opened) {
		_ = file.Close()
		return marker, nil, fmt.Errorf("invalid reinstall pending marker identity")
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maxReinstallPendingBytes+1))
	closeErr := file.Close()
	after, pathErr := os.Lstat(markerPath)
	if readErr != nil || closeErr != nil || pathErr != nil || len(body) > maxReinstallPendingBytes || !os.SameFile(before, after) || !privatePendingFile(after) {
		return marker, nil, errors.Join(fmt.Errorf("invalid reinstall pending marker after read"), readErr, closeErr, pathErr)
	}
	if err := rejectDuplicateJSONKeys(body); err != nil {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return marker, nil, fmt.Errorf("invalid reinstall pending marker trailing data")
	}
	if err := validateReinstallPending(root, marker); err != nil {
		return marker, nil, err
	}
	return marker, before, nil
}

func validateReinstallPending(root string, marker reinstallPendingMarker) error {
	if marker.Version != reinstallPendingVersion || marker.Root != root || filepath.Clean(root) != root || len(marker.Operation) != 32 || len(marker.Artifacts) == 0 || len(marker.Artifacts) > 64 {
		return fmt.Errorf("invalid reinstall pending marker")
	}
	if _, err := hex.DecodeString(marker.Operation); err != nil {
		return fmt.Errorf("invalid reinstall pending operation")
	}
	if _, err := time.Parse(time.RFC3339Nano, marker.StartedAt); err != nil {
		return fmt.Errorf("invalid reinstall pending timestamp")
	}
	previous := ""
	for _, artifact := range marker.Artifacts {
		if artifact.Path <= previous || !fs.ValidPath(artifact.Path) || path.Clean(artifact.Path) != artifact.Path || strings.ContainsAny(artifact.Path, "\\:\x00") || !validPendingSHA256(artifact.SHA256) {
			return fmt.Errorf("invalid reinstall pending artifact")
		}
		previous = artifact.Path
	}
	return nil
}

func pendingInventoryMatches(marker reinstallPendingMarker, layout integration.ManagedLayout) bool {
	if marker.Root != layout.Root || len(marker.Artifacts) != len(layout.Artifacts) {
		return false
	}
	for index, artifact := range layout.Artifacts {
		if marker.Artifacts[index].Path != artifact.RelativePath || marker.Artifacts[index].SHA256 != artifact.SHA256 {
			return false
		}
	}
	return true
}

func clearReinstallPending(root string, expected reinstallPendingEvidence) error {
	markerPath := filepath.Join(root, reinstallPendingName)
	if err := matchesReinstallPendingEvidence(markerPath, expected); err != nil {
		return fmt.Errorf("%w: reinstall pending marker changed before cleanup at %q", integration.ErrRecovery, markerPath)
	}
	quarantineDirectory, err := os.MkdirTemp(root, ".vgxness-reinstall-marker-*")
	if err != nil {
		return fmt.Errorf("%w: prepare reinstall marker cleanup for %q: %v", integration.ErrRecovery, markerPath, err)
	}
	quarantine := filepath.Join(quarantineDirectory, "marker")
	if err := os.Rename(markerPath, quarantine); err != nil {
		_ = os.Remove(quarantineDirectory)
		return fmt.Errorf("%w: quarantine reinstall marker %q: %v", integration.ErrRecovery, markerPath, err)
	}
	if err := matchesReinstallPendingEvidence(quarantine, expected); err != nil {
		restoreErr := restoreQuarantinedFile(quarantine, markerPath)
		return fmt.Errorf("%w: reinstall marker replaced during cleanup; retained at %q or %q: %v", integration.ErrRecovery, markerPath, quarantine, restoreErr)
	}
	if err := os.Remove(quarantine); err != nil {
		return fmt.Errorf("%w: remove reinstall marker quarantine %q: %v", integration.ErrRecovery, quarantine, err)
	}
	if err := os.Remove(quarantineDirectory); err != nil {
		return fmt.Errorf("%w: remove reinstall marker quarantine directory %q: %v", integration.ErrRecovery, quarantineDirectory, err)
	}
	if _, err := os.Lstat(markerPath); err == nil {
		return fmt.Errorf("%w: reinstall pending marker was replaced during cleanup at %q", integration.ErrRecovery, markerPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: verify reinstall marker cleanup at %q: %v", integration.ErrRecovery, markerPath, err)
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("%w: sync reinstall marker cleanup: %v", integration.ErrRecovery, err)
	}
	return nil
}

func matchesReinstallPendingEvidence(markerPath string, expected reinstallPendingEvidence) error {
	before, err := os.Lstat(markerPath)
	if err != nil || expected.info == nil || !privatePendingFile(before) || !os.SameFile(before, expected.info) || before.Size() <= 0 || before.Size() > maxReinstallPendingBytes {
		return fmt.Errorf("invalid reinstall pending marker identity")
	}
	file, err := os.Open(markerPath)
	if err != nil {
		return err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !privatePendingFile(opened) || !os.SameFile(before, opened) || !os.SameFile(opened, expected.info) {
		_ = file.Close()
		return fmt.Errorf("invalid reinstall pending marker identity")
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maxReinstallPendingBytes+1))
	closeErr := file.Close()
	after, pathErr := os.Lstat(markerPath)
	if readErr != nil || closeErr != nil || pathErr != nil || len(body) > maxReinstallPendingBytes || !privatePendingFile(after) || !os.SameFile(after, expected.info) || sha256.Sum256(body) != expected.digest {
		return fmt.Errorf("invalid reinstall pending marker evidence")
	}
	return nil
}

func privatePendingFile(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && (runtime.GOOS == "windows" || info.Mode().Perm() == 0o600)
}

func validPendingSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid JSON key")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

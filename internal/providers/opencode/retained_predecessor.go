package opencode

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	retainedPredecessorVersion   = 1
	retainedPredecessorDirectory = "retained-predecessors"
	retainedAnchorDirectory      = "anchors"
	maxRetainedPredecessorBytes  = 4 * 1024
)

type retainedPredecessorMarker struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Root      string `json:"root"`
	Target    string `json:"target"`
	Anchor    string `json:"anchor"`
	SHA256    string `json:"sha256"`
}

func retainedPredecessorRoot(root string) string {
	return filepath.Join(root, "vgxness", retainedPredecessorDirectory)
}

func retainedAnchorRoot(root string) string {
	return filepath.Join(retainedPredecessorRoot(root), retainedAnchorDirectory)
}

func retainedPredecessorSupported() bool { return runtime.GOOS != "windows" }

func prepareRetainedPredecessorDirectories(root string) error {
	for _, directory := range []string{retainedPredecessorRoot(root), retainedAnchorRoot(root)} {
		if err := prepareDirectory(directory); err != nil {
			return err
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
			return fmt.Errorf("invalid retained predecessor directory")
		}
	}
	return nil
}

func persistRetainedPredecessor(root, target, anchor string, predecessor []byte) (string, error) {
	if root == "" {
		root = filepath.Dir(target)
	}
	root = filepath.Clean(root)
	if !retainedPredecessorSupported() || !validRetainedPaths(root, target, anchor) {
		return "", fmt.Errorf("invalid retained predecessor path")
	}
	anchorBytes, err := readRegularFile(anchor)
	if err != nil || !bytes.Equal(anchorBytes, predecessor) {
		return "", fmt.Errorf("invalid retained predecessor anchor")
	}
	if err := prepareRetainedPredecessorDirectories(root); err != nil {
		return "", err
	}
	operation := make([]byte, 16)
	if _, err := rand.Read(operation); err != nil {
		return "", err
	}
	marker := retainedPredecessorMarker{
		Version: retainedPredecessorVersion, Operation: hex.EncodeToString(operation), Root: root,
		Target: target, Anchor: anchor, SHA256: artifactSHA256(predecessor),
	}
	body, err := json.Marshal(marker)
	if err != nil || len(body)+1 > maxRetainedPredecessorBytes {
		return "", fmt.Errorf("encode retained predecessor")
	}
	body = append(body, '\n')
	path := filepath.Join(retainedPredecessorRoot(root), marker.Operation+".json")
	file, err := os.CreateTemp(retainedPredecessorRoot(root), ".vgxness-retained-*.tmp")
	if err != nil {
		return "", err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Link(temporary, path); err != nil {
		return "", err
	}
	if err := syncDirectory(retainedPredecessorRoot(root)); err != nil {
		return path, err
	}
	if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return path, err
	}
	if err := syncDirectory(retainedPredecessorRoot(root)); err != nil {
		return path, err
	}
	return path, nil
}

func retainedPredecessors(root string) ([]retainedPredecessorMarker, error) {
	directory := retainedPredecessorRoot(root)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		return nil, fmt.Errorf("invalid retained predecessor directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("invalid retained predecessor inventory")
	}
	markerEntries := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == retainedAnchorDirectory && entry.IsDir() {
			continue
		}
		markerEntries = append(markerEntries, entry)
	}
	result := make([]retainedPredecessorMarker, len(markerEntries))
	anchorInfo, anchorErr := os.Lstat(retainedAnchorRoot(root))
	if anchorErr != nil || !anchorInfo.IsDir() || anchorInfo.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && anchorInfo.Mode().Perm() != 0o700) {
		return result, fmt.Errorf("invalid retained predecessor anchor directory")
	}
	for index, entry := range markerEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || len(entry.Name()) != 37 {
			return result, fmt.Errorf("invalid retained predecessor entry")
		}
		marker, err := readRetainedPredecessor(filepath.Join(directory, entry.Name()))
		if err != nil || entry.Name() != marker.Operation+".json" || marker.Root != root || !validRetainedPaths(root, marker.Target, marker.Anchor) {
			return result, fmt.Errorf("invalid retained predecessor marker")
		}
		parentExists, parentDrifted, parentErr := inspectDirectory(filepath.Dir(marker.Anchor))
		anchor, err := readRegularFile(marker.Anchor)
		if parentErr != nil || !parentExists || parentDrifted || err != nil || artifactSHA256(anchor) != marker.SHA256 {
			return result, fmt.Errorf("invalid retained predecessor anchor")
		}
		result[index] = marker
	}
	return result, nil
}

func readRetainedPredecessor(path string) (retainedPredecessorMarker, error) {
	var marker retainedPredecessorMarker
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) || info.Size() <= 0 || info.Size() > maxRetainedPredecessorBytes {
		return marker, fmt.Errorf("invalid retained predecessor marker")
	}
	file, err := os.Open(path)
	if err != nil {
		return marker, err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		return marker, fmt.Errorf("invalid retained predecessor marker identity")
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maxRetainedPredecessorBytes+1))
	closeErr := file.Close()
	after, pathErr := os.Lstat(path)
	if readErr != nil || closeErr != nil || pathErr != nil || !os.SameFile(info, after) || len(body) > maxRetainedPredecessorBytes || rejectDuplicateJSONKeys(body) != nil {
		return marker, fmt.Errorf("invalid retained predecessor marker")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&marker) != nil || decoder.Decode(&struct{}{}) == nil || marker.Version != retainedPredecessorVersion || len(marker.Operation) != 32 || !validRetainedOperation(marker.Operation) || !validPendingSHA256(marker.SHA256) || !filepath.IsAbs(marker.Root) || filepath.Clean(marker.Root) != marker.Root {
		return retainedPredecessorMarker{}, fmt.Errorf("invalid retained predecessor marker")
	}
	return marker, nil
}

func validRetainedOperation(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func withinRetainedRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validRetainedPaths(root, target, anchor string) bool {
	if !filepath.IsAbs(target) || !filepath.IsAbs(anchor) || filepath.Clean(target) != target || filepath.Clean(anchor) != anchor || target == anchor || !withinRetainedRoot(root, target) || filepath.Dir(anchor) != retainedAnchorRoot(root) {
		return false
	}
	name := filepath.Base(anchor)
	return strings.HasPrefix(name, ".vgxness-previous-") && strings.HasSuffix(name, ".tmp")
}

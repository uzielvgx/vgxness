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

func retainedPredecessorRoot(root string) string { return filepath.Join(root, "vgxness", retainedPredecessorDirectory) }

func retainedAnchorRoot(root string) string { return filepath.Join(retainedPredecessorRoot(root), retainedAnchorDirectory) }

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
	if !validRetainedPaths(root, target, anchor) {
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
	marker := retainedPredecessorMarker{Version: retainedPredecessorVersion, Operation: hex.EncodeToString(operation), Root: root, Target: target, Anchor: anchor, SHA256: artifactSHA256(predecessor)}
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

type retainedInventory struct {
	markers       []retainedPredecessorMarker
	evidenceCount int
}

func retainedPredecessorInventory(root string) (retainedInventory, error) {
	directory := retainedPredecessorRoot(root)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return retainedInventory{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		return retainedInventory{evidenceCount: 1}, fmt.Errorf("invalid retained predecessor directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return retainedInventory{evidenceCount: 1}, fmt.Errorf("invalid retained predecessor inventory")
	}
	markerEntries := make([]os.DirEntry, 0, len(entries))
	aliasEntries := make([]os.DirEntry, 0)
	invalidEntries := 0
	for _, entry := range entries {
		if entry.Name() == retainedAnchorDirectory && entry.IsDir() {
			continue
		}
		if retainedMarkerName(entry.Name()) {
			markerEntries = append(markerEntries, entry)
		} else if retainedMarkerAliasName(entry.Name()) {
			aliasEntries = append(aliasEntries, entry)
		} else {
			invalidEntries++
		}
	}
	inventory := retainedInventory{markers: make([]retainedPredecessorMarker, 0, len(markerEntries)), evidenceCount: len(markerEntries) + len(aliasEntries) + invalidEntries}
	anchorInfo, anchorErr := os.Lstat(retainedAnchorRoot(root))
	if anchorErr != nil || !anchorInfo.IsDir() || anchorInfo.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && anchorInfo.Mode().Perm() != 0o700) {
		return inventory, fmt.Errorf("invalid retained predecessor anchor directory")
	}
	type markerEvidence struct {
		marker retainedPredecessorMarker
		info   os.FileInfo
		body   []byte
	}
	validated := make(map[string]markerEvidence, len(markerEntries))
	references := make(map[string]int, len(markerEntries))
	invalid := invalidEntries != 0
	for _, entry := range markerEntries {
		marker, markerInfo, markerBody, err := readRetainedPredecessorEvidence(filepath.Join(directory, entry.Name()))
		if entry.IsDir() || err != nil || entry.Name() != marker.Operation+".json" || marker.Root != root || !validRetainedPaths(root, marker.Target, marker.Anchor) {
			invalid = true
			continue
		}
		parentExists, parentDrifted, parentErr := inspectDirectory(filepath.Dir(marker.Anchor))
		anchor, err := readRegularFile(marker.Anchor)
		if parentErr != nil || !parentExists || parentDrifted || err != nil || artifactSHA256(anchor) != marker.SHA256 {
			invalid = true
			continue
		}
		inventory.markers = append(inventory.markers, marker)
		validated[marker.Operation] = markerEvidence{marker: marker, info: markerInfo, body: markerBody}
		references[marker.Anchor]++
	}
	for _, entry := range aliasEntries {
		marker, aliasInfo, aliasBody, err := readRetainedPredecessorEvidence(filepath.Join(directory, entry.Name()))
		final, ok := validated[marker.Operation]
		if entry.IsDir() || err != nil || !ok || !os.SameFile(aliasInfo, final.info) || !bytes.Equal(aliasBody, final.body) || marker != final.marker {
			invalid = true
			continue
		}
		inventory.evidenceCount--
	}
	anchorEntries, err := os.ReadDir(retainedAnchorRoot(root))
	if err != nil {
		return inventory, fmt.Errorf("invalid retained predecessor anchor inventory")
	}
	for _, entry := range anchorEntries {
		path := filepath.Join(retainedAnchorRoot(root), entry.Name())
		info, err := os.Lstat(path)
		if !strings.HasPrefix(entry.Name(), ".vgxness-previous-") || !strings.HasSuffix(entry.Name(), ".tmp") || len(entry.Name()) <= len(".vgxness-previous-.tmp") || err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || references[path] != 1 {
			inventory.evidenceCount++
			invalid = true
		}
	}
	for _, count := range references {
		if count != 1 {
			invalid = true
		}
	}
	if invalid {
		return inventory, fmt.Errorf("invalid retained predecessor inventory")
	}
	return inventory, nil
}

func readRetainedPredecessorEvidence(path string) (retainedPredecessorMarker, os.FileInfo, []byte, error) {
	var marker retainedPredecessorMarker
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) || info.Size() <= 0 || info.Size() > maxRetainedPredecessorBytes {
		return marker, nil, nil, fmt.Errorf("invalid retained predecessor marker")
	}
	file, err := os.Open(path)
	if err != nil {
		return marker, nil, nil, err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		return marker, nil, nil, fmt.Errorf("invalid retained predecessor marker identity")
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maxRetainedPredecessorBytes+1))
	closeErr := file.Close()
	after, pathErr := os.Lstat(path)
	if readErr != nil || closeErr != nil || pathErr != nil || !os.SameFile(info, after) || len(body) > maxRetainedPredecessorBytes || rejectDuplicateJSONKeys(body) != nil {
		return marker, nil, nil, fmt.Errorf("invalid retained predecessor marker")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&marker) != nil || decoder.Decode(&struct{}{}) == nil || marker.Version != retainedPredecessorVersion || len(marker.Operation) != 32 || !validRetainedOperation(marker.Operation) || !validPendingSHA256(marker.SHA256) || !filepath.IsAbs(marker.Root) || filepath.Clean(marker.Root) != marker.Root {
		return retainedPredecessorMarker{}, nil, nil, fmt.Errorf("invalid retained predecessor marker")
	}
	return marker, info, body, nil
}

func retainedMarkerName(name string) bool { return len(name) == 37 && strings.HasSuffix(name, ".json") && validRetainedOperation(strings.TrimSuffix(name, ".json")) }

func retainedMarkerAliasName(name string) bool { return strings.HasPrefix(name, ".vgxness-retained-") && strings.HasSuffix(name, ".tmp") && len(name) > len(".vgxness-retained-.tmp") }

func validRetainedOperation(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 32 && strings.ToLower(value) == value && err == nil
}

func withinRetainedRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validRetainedPaths(root, target, anchor string) bool {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(target) || !filepath.IsAbs(anchor) || filepath.Clean(target) != target || filepath.Clean(anchor) != anchor || target == anchor || !withinRetainedRoot(root, target) || filepath.Dir(anchor) != retainedAnchorRoot(root) {
		return false
	}
	name := filepath.Base(anchor)
	return strings.HasPrefix(name, ".vgxness-previous-") && strings.HasSuffix(name, ".tmp")
}

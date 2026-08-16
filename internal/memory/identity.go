package memory

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const projectIDMarkerMaxBytes = 160
const projectIDMarkerFormat = "vgxness-project-id/v1"

type projectIDMarker struct {
	Format    string `json:"format"`
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id"`
}
type markerTempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}
type markerDirFile interface {
	Sync() error
	Close() error
}

var projectMarkerFS = struct {
	createTemp func(string, string) (markerTempFile, error)
	link       func(string, string) error
	remove     func(string) error
	openDir    func(string) (markerDirFile, error)
}{
	createTemp: func(dir, pattern string) (markerTempFile, error) { return os.CreateTemp(dir, pattern) },
	link:       os.Link, remove: os.Remove,
	openDir: func(path string) (markerDirFile, error) { return os.Open(path) },
}
var projectIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ReadProjectID(workspace string) (id string, present bool, err error) {
	dir := filepath.Join(workspace, ".vgxness")
	info, statErr := os.Lstat(dir)
	if os.IsNotExist(statErr) {
		return "", false, nil
	}
	if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, fmt.Errorf("%w: invalid project marker directory", ErrInvalid)
	}
	path := filepath.Join(dir, "project-id")
	info, statErr = os.Lstat(path)
	if os.IsNotExist(statErr) {
		return "", false, nil
	}
	if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > projectIDMarkerMaxBytes {
		return "", false, fmt.Errorf("%w: invalid project marker", ErrInvalid)
	}
	file, openErr := os.Open(path)
	if openErr != nil {
		return "", false, fmt.Errorf("%w: unreadable project marker", ErrInvalid)
	}
	defer file.Close()
	data, readErr := io.ReadAll(io.LimitReader(file, projectIDMarkerMaxBytes+1))
	if readErr != nil || len(data) > projectIDMarkerMaxBytes || !utf8.Valid(data) {
		return "", false, fmt.Errorf("%w: invalid project marker", ErrInvalid)
	}
	marker, valid := decodeProjectIDMarker(data)
	if !valid {
		return "", false, fmt.Errorf("%w: invalid project marker", ErrInvalid)
	}
	return marker.ProjectID, true, nil
}
func decodeProjectIDMarker(data []byte) (projectIDMarker, bool) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return projectIDMarker{}, false
	}
	seen := map[string]bool{}
	values := projectIDMarker{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return projectIDMarker{}, false
		}
		seen[key] = true
		var value string
		if decoder.Decode(&value) != nil {
			return projectIDMarker{}, false
		}
		switch key {
		case "format":
			values.Format = value
		case "kind":
			values.Kind = value
		case "project_id":
			values.ProjectID = value
		default:
			return projectIDMarker{}, false
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(&struct{}{}) != io.EOF || len(seen) != 3 || values.Format != projectIDMarkerFormat || values.Kind != "project" || !projectIDPattern.MatchString(values.ProjectID) {
		return projectIDMarker{}, false
	}
	return values, true
}
func InitializeProjectID(workspace string) (id string, created bool, err error) {
	return ensureProjectID(workspace, "")
}
func EnsureProjectID(workspace, expected string) (id string, created bool, err error) {
	if !projectIDPattern.MatchString(expected) {
		return "", false, fmt.Errorf("%w: portable project identity", ErrInvalid)
	}
	return ensureProjectID(workspace, expected)
}
func ensureProjectID(workspace, expected string) (id string, created bool, err error) {
	if id, present, err := ReadProjectID(workspace); err != nil || present {
		if err != nil || !present {
			return id, false, err
		}
		if expected != "" && id != expected {
			return "", false, fmt.Errorf("%w: project marker changed", ErrConflict)
		}
		if err := syncProjectMarkerParent(workspace); err != nil {
			return "", false, err
		}
		if err := syncProjectMarkerDir(filepath.Join(workspace, ".vgxness")); err != nil {
			return "", false, err
		}
		return id, false, nil
	}
	dir := filepath.Join(workspace, ".vgxness")
	if err := os.Mkdir(dir, 0o700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", false, fmt.Errorf("%w: create project marker directory", ErrInvalid)
		}
	}
	if info, err := os.Lstat(dir); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, fmt.Errorf("%w: invalid project marker directory", ErrInvalid)
	}
	if err := syncProjectMarkerParent(workspace); err != nil {
		return "", false, err
	}
	id = expected
	if id == "" {
		id, err = newProjectUUID()
		if err != nil {
			return "", false, fmt.Errorf("%w: generate project marker", ErrInvalid)
		}
	}
	payload, err := json.Marshal(projectIDMarker{Format: projectIDMarkerFormat, Kind: "project", ProjectID: id})
	if err != nil {
		return "", false, fmt.Errorf("%w: encode project marker", ErrInvalid)
	}
	payload = append(payload, '\n')
	temp, err := projectMarkerFS.createTemp(dir, ".project-id-*")
	if err != nil {
		return "", false, fmt.Errorf("%w: create project marker temp", ErrInvalid)
	}
	tempPath := temp.Name()
	defer projectMarkerFS.remove(tempPath)
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(payload)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: write project marker", ErrInvalid)
	}
	path := filepath.Join(dir, "project-id")
	if err = projectMarkerFS.link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			id, present, readErr := ReadProjectID(workspace)
			if readErr != nil || !present {
				return "", false, fmt.Errorf("%w: existing project marker", ErrInvalid)
			}
			if expected != "" && id != expected {
				return "", false, fmt.Errorf("%w: project marker changed", ErrConflict)
			}
			if syncErr := syncProjectMarkerDir(dir); syncErr != nil {
				return "", false, syncErr
			}
			return id, false, nil
		}
		return "", false, fmt.Errorf("%w: publish project marker", ErrInvalid)
	}
	if err := syncProjectMarkerDir(dir); err != nil {
		return "", false, err
	}
	return id, true, nil
}
func newProjectUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

package launcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	SchemaVersion = "1"
	ManagedBy     = "vgxness"
	MaxBinarySize = int64(256 << 20)
	maxManifest   = int64(64 << 10)
)

var ErrInvalid = errors.New("invalid managed launcher")

type Manifest struct {
	SchemaVersion  string `json:"schemaVersion"`
	ManagedBy      string `json:"managedBy"`
	LauncherPath   string `json:"launcherPath"`
	LauncherSHA256 string `json:"launcherSha256"`
	DataDir        string `json:"dataDir"`
	ActivePath     string `json:"activePath"`
	ActiveSHA256   string `json:"activeSha256"`
	PreviousSHA256 string `json:"previousSha256,omitempty"`
	UpdatedAt      string `json:"updatedAt"`
}

func SidecarPath(executable string) string { return executable + ".launcher.json" }

// Forward replaces a managed launcher process with its exact active binary.
// It returns handled=false when executable is not a managed launcher.
func Forward(args, environment []string, stderr io.Writer) (handled bool, exitCode int) {
	executable, err := os.Executable()
	if err != nil {
		return false, 0
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return false, 0
	}
	manifest, err := Load(executable)
	if errors.Is(err, os.ErrNotExist) {
		return false, 0
	}
	if err != nil {
		writeFailure(stderr)
		return true, 1
	}
	launcherHash, err := FileSHA256(executable)
	if err != nil || launcherHash != manifest.LauncherSHA256 {
		writeFailure(stderr)
		return true, 1
	}
	activeHash, err := FileSHA256(manifest.ActivePath)
	if err != nil || activeHash != manifest.ActiveSHA256 || sameFile(executable, manifest.ActivePath) {
		writeFailure(stderr)
		return true, 1
	}
	forwardArgs := append([]string(nil), args...)
	if len(forwardArgs) == 0 {
		forwardArgs = []string{manifest.ActivePath}
	} else {
		forwardArgs[0] = manifest.ActivePath
	}
	forwardEnvironment := replaceEnvironment(environment,
		"VGXNESS_LAUNCHER", manifest.LauncherPath,
		"VGXNESS_ACTIVE_SHA256", manifest.ActiveSHA256,
	)
	if err := replaceProcess(manifest.ActivePath, forwardArgs, forwardEnvironment); err != nil {
		var childExit *childExitError
		if errors.As(err, &childExit) {
			return true, childExit.code
		}
		writeFailure(stderr)
		return true, 1
	}
	return true, 0
}

type childExitError struct{ code int }

func (failure *childExitError) Error() string {
	return fmt.Sprintf("managed child exited with code %d", failure.code)
}

func Load(executable string) (Manifest, error) {
	if !filepath.IsAbs(executable) {
		return Manifest{}, ErrInvalid
	}
	data, err := readRegular(SidecarPath(executable), maxManifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return Manifest{}, err
	}
	if err := Validate(manifest, executable); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func decodeManifest(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, ErrInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Manifest{}, ErrInvalid
	}
	if err := Validate(manifest, manifest.LauncherPath); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Validate(manifest Manifest, executable string) error {
	if manifest.SchemaVersion != SchemaVersion || manifest.ManagedBy != ManagedBy || !filepath.IsAbs(executable) || !equivalentPath(manifest.LauncherPath, executable) || !validDigest(manifest.LauncherSHA256) || !validDigest(manifest.ActiveSHA256) || manifest.PreviousSHA256 != "" && !validDigest(manifest.PreviousSHA256) || !filepath.IsAbs(manifest.DataDir) || !filepath.IsAbs(manifest.ActivePath) || strings.TrimSpace(manifest.UpdatedAt) == "" {
		return ErrInvalid
	}
	expected := VersionPath(manifest.DataDir, manifest.ActiveSHA256)
	if filepath.Clean(manifest.ActivePath) != expected {
		return ErrInvalid
	}
	return nil
}

func equivalentPath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}

func VersionPath(dataDir, digest string) string {
	return filepath.Join(filepath.Clean(dataDir), "versions", digest, binaryName())
}

func FileSHA256(path string) (string, error) {
	file, err := openRegular(path, MaxBinarySize)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, MaxBinarySize+1)); err != nil {
		return "", err
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position > MaxBinarySize {
		return "", ErrInvalid
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func readRegular(path string, maximum int64) ([]byte, error) {
	file, err := openRegular(path, maximum)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrInvalid
	}
	return data, nil
}

func openRegular(path string, maximum int64) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maximum {
		return nil, ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() < 1 || after.Size() > maximum {
		file.Close()
		return nil, ErrInvalid
	}
	return file, nil
}

func sameFile(first, second string) bool {
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

func replaceEnvironment(environment []string, values ...string) []string {
	replacements := map[string]string{}
	for index := 0; index+1 < len(values); index += 2 {
		replacements[values[index]] = values[index+1]
	}
	result := make([]string, 0, len(environment)+len(replacements))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if _, replace := replacements[key]; found && replace {
			continue
		}
		result = append(result, entry)
	}
	for key, value := range replacements {
		result = append(result, key+"="+value)
	}
	return result
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func writeFailure(writer io.Writer) {
	if writer != nil {
		_, _ = fmt.Fprintln(writer, "operational: managed VGXNESS launcher validation failed")
	}
}

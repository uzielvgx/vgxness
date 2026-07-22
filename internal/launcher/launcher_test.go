package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadValidatesExactManagedManifest(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", binaryName())
	dataDir := filepath.Join(root, "data")
	digest := strings.Repeat("a", 64)
	manifest := Manifest{
		SchemaVersion: SchemaVersion, ManagedBy: ManagedBy,
		LauncherPath: executable, LauncherSHA256: strings.Repeat("b", 64), DataDir: dataDir,
		ActivePath: VersionPath(dataDir, digest), ActiveSHA256: digest,
		PreviousSHA256: strings.Repeat("c", 64), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	writeManifestFixture(t, executable, manifest, false)
	loaded, err := Load(executable)
	if err != nil || loaded != manifest {
		t.Fatalf("Load() = %#v, %v", loaded, err)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"unknown":true}`)...)
	if err := os.WriteFile(SidecarPath(executable), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(executable); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestValidateRejectsVersionPathEscapeAndInvalidDigest(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, binaryName())
	digest := strings.Repeat("a", 64)
	manifest := Manifest{
		SchemaVersion: SchemaVersion, ManagedBy: ManagedBy,
		LauncherPath: executable, LauncherSHA256: strings.Repeat("b", 64), DataDir: filepath.Join(root, "data"),
		ActivePath: filepath.Join(root, "foreign", binaryName()), ActiveSHA256: digest, UpdatedAt: "now",
	}
	if err := Validate(manifest, executable); !errors.Is(err, ErrInvalid) {
		t.Fatalf("escaped active path error = %v", err)
	}
	manifest.ActivePath = VersionPath(manifest.DataDir, digest)
	manifest.ActiveSHA256 = strings.ToUpper(digest)
	if err := Validate(manifest, executable); !errors.Is(err, ErrInvalid) {
		t.Fatalf("uppercase digest error = %v", err)
	}
}

func TestValidateAcceptsEquivalentSymlinkAliasedLauncherPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	aliasRoot := filepath.Join(root, "alias")
	executable := filepath.Join(realRoot, "bin", binaryName())
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("launcher"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	manifest := Manifest{
		SchemaVersion: SchemaVersion, ManagedBy: ManagedBy,
		LauncherPath: executable, LauncherSHA256: strings.Repeat("b", 64), DataDir: filepath.Join(realRoot, "data"),
		ActivePath: VersionPath(filepath.Join(realRoot, "data"), digest), ActiveSHA256: digest, UpdatedAt: "now",
	}
	if err := Validate(manifest, filepath.Join(aliasRoot, "bin", binaryName())); err != nil {
		t.Fatalf("equivalent launcher alias rejected: %v", err)
	}
}

func TestFileSHA256RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "binary")
	content := []byte("vgxness-binary")
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(content)
	got, err := FileSHA256(path)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("FileSHA256() = %q, %v", got, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := FileSHA256(link); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestReplaceEnvironmentReplacesManagedValues(t *testing.T) {
	got := replaceEnvironment([]string{"A=1", "VGXNESS_LAUNCHER=old", "VGXNESS_ACTIVE_SHA256=old"},
		"VGXNESS_LAUNCHER", "/stable/vgxness", "VGXNESS_ACTIVE_SHA256", "abc")
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "=old") || !strings.Contains(joined, "VGXNESS_LAUNCHER=/stable/vgxness") || !strings.Contains(joined, "VGXNESS_ACTIVE_SHA256=abc") {
		t.Fatalf("replaceEnvironment() = %q", got)
	}
}

func writeManifestFixture(t *testing.T, executable string, manifest Manifest, symlink bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := SidecarPath(executable)
	if !symlink {
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	target := path + ".target"
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

package release

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestValidateMetadata(t *testing.T) {
	validVersions := []string{"v0.1.0", "v0.1.0-alpha.1", "v1.2.3-rc.1+build.5"}
	for _, version := range validVersions {
		if _, err := validateMetadata(version, testCommit, "2026-07-29T01:02:03Z"); err != nil {
			t.Errorf("valid metadata %q rejected: %v", version, err)
		}
	}
	invalid := []struct{ version, commit, date string }{
		{"1.2.3", testCommit, "2026-07-29T01:02:03Z"},
		{"v01.2.3", testCommit, "2026-07-29T01:02:03Z"},
		{"v1.2.3-01", testCommit, "2026-07-29T01:02:03Z"},
		{"v1.2.3/../../x", testCommit, "2026-07-29T01:02:03Z"},
		{"v1.2.3", strings.ToUpper(testCommit), "2026-07-29T01:02:03Z"},
		{"v1.2.3", testCommit[:39], "2026-07-29T01:02:03Z"},
		{"v1.2.3", testCommit, "2026-07-29"},
	}
	for _, candidate := range invalid {
		if _, err := validateMetadata(candidate.version, candidate.commit, candidate.date); err == nil {
			t.Errorf("invalid metadata accepted: %#v", candidate)
		}
	}
}

func TestValidateOutputRefusesTraversalAndNonemptyDirectory(t *testing.T) {
	root := t.TempDir()
	if err := validateOutput(filepath.Join(root, "dist")); err != nil {
		t.Fatalf("nonexistent output rejected: %v", err)
	}
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateOutput(empty); err == nil {
		t.Fatal("empty output accepted")
	}
	if err := os.WriteFile(filepath.Join(empty, "existing"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateOutput(empty); err == nil {
		t.Fatal("nonempty output accepted")
	}
	if err := validateOutput(root + string(os.PathSeparator) + "safe" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "dist"); err == nil {
		t.Fatal("traversal output accepted")
	}
}

func TestWriteTarGzLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	date := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	files := fixtureFiles("vgxness")
	if err := writeTarGz(path, "vgxness_0.1.0_linux_amd64", files, date); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if !header.ModTime.Equal(date) {
			t.Errorf("%s timestamp=%s", header.Name, header.ModTime)
		}
	}
	want := []string{"vgxness_0.1.0_linux_amd64/", "vgxness_0.1.0_linux_amd64/vgxness", "vgxness_0.1.0_linux_amd64/LICENSE", "vgxness_0.1.0_linux_amd64/README.md"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("archive names=%q, want %q", names, want)
	}
}

func TestWriteZipLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.zip")
	date := time.Date(2026, 7, 29, 1, 2, 4, 0, time.UTC)
	if err := writeZip(path, "vgxness_0.1.0_windows_amd64", fixtureFiles("vgxness.exe"), date); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	want := []string{"vgxness_0.1.0_windows_amd64/", "vgxness_0.1.0_windows_amd64/vgxness.exe", "vgxness_0.1.0_windows_amd64/LICENSE", "vgxness_0.1.0_windows_amd64/README.md"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("archive names=%q, want %q", names, want)
	}
}

func TestWriteChecksumsSortedAndLimitedToArchives(t *testing.T) {
	directory := t.TempDir()
	archives := []string{"vgxness_0.1.0_windows_amd64.zip", "vgxness_0.1.0_linux_amd64.tar.gz"}
	for _, name := range archives {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "not-an-archive"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeChecksums(directory, archives); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	want := make([]string, 0, 2)
	for _, name := range []string{archives[1], archives[0]} {
		digest := sha256.Sum256([]byte(name))
		want = append(want, fmt.Sprintf("%x  %s", digest, name))
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("checksum lines=%q, want %q", lines, want)
	}
}

func TestArchivesAreDeterministicForFixedInputs(t *testing.T) {
	date := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	for _, test := range []struct {
		name  string
		write func(string) error
	}{
		{
			name: "tar.gz",
			write: func(path string) error {
				return writeTarGz(path, "vgxness_0.1.0_linux_amd64", fixtureFiles("vgxness"), date)
			},
		},
		{
			name: "zip",
			write: func(path string) error {
				return writeZip(path, "vgxness_0.1.0_windows_amd64", fixtureFiles("vgxness.exe"), date)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := filepath.Join(t.TempDir(), "first."+test.name)
			second := filepath.Join(t.TempDir(), "second."+test.name)
			if err := test.write(first); err != nil {
				t.Fatal(err)
			}
			if err := test.write(second); err != nil {
				t.Fatal(err)
			}
			firstData, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			secondData, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(firstData, secondData) {
				t.Fatal("archive bytes differ for fixed inputs")
			}
		})
	}
}

func TestPackageDoesNotPublishWhenFinalBuildCancels(t *testing.T) {
	repository := t.TempDir()
	for name, contents := range map[string]string{"LICENSE": "license", "README.md": "readme"} {
		if err := os.WriteFile(filepath.Join(repository, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(t.TempDir(), "dist")
	ctx, cancel := context.WithCancel(context.Background())
	builds := 0
	err := packageWithBuild(ctx, Options{
		Version:    "v0.1.0-alpha.1",
		Commit:     testCommit,
		Date:       "2026-07-29T01:02:03Z",
		Output:     output,
		Repository: repository,
	}, func(_ context.Context, _, binaryPath string, _ target, _ Options) error {
		builds++
		if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
			return err
		}
		if builds == len(targets) {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Package error = %v, want context.Canceled", err)
	}
	if builds != len(targets) {
		t.Fatalf("builds = %d, want %d", builds, len(targets))
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output was published after cancellation: %v", err)
	}
}

func TestPublishAssetsCreatesAbsentOutput(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "artifact"), []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dist")
	if err := publishAssets(assets, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "release" {
		t.Fatalf("artifact = %q, want release", data)
	}
}

func TestPublishAssetsPublicationFailureLeavesNoPartialOutputAndRetries(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "artifact"), []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dist")
	if err := publishAssetsWith(assets, output, func(_, _ string) error { return errors.New("injected publication failure") }); err == nil {
		t.Fatal("publishAssets succeeded despite injected publication failure")
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial output remains after failed publication: %v", err)
	}
	if err := publishAssets(assets, output); err != nil {
		t.Fatalf("retry publishAssets: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(output, "artifact"))
	if err != nil || string(data) != "release" {
		t.Fatalf("retried artifact = %q, %v", data, err)
	}
}

func TestPublishAssetsDoesNotReplaceConcurrentDestination(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "artifact"), []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dist")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := publishAssets(assets, output); err == nil {
		t.Fatal("publishAssets replaced an existing empty directory")
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("concurrent destination disappeared: %v", err)
	}
}

func TestPublishAssetsDoesNotOverwriteExistingAsset(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "artifact"), []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dist")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(output, "artifact")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishAssets(assets, output); err == nil {
		t.Fatal("publishAssets overwrote an occupied output")
	}
	data, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foreign" {
		t.Fatalf("occupied artifact = %q, want foreign", data)
	}
}

func TestPublishAssetsDoesNotReplaceOutputFile(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "artifact"), []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "dist")
	if err := os.WriteFile(output, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishAssets(assets, output); err == nil {
		t.Fatal("publishAssets replaced an occupied output path")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foreign" {
		t.Fatalf("occupied output = %q, want foreign", data)
	}
}

func fixtureFiles(executable string) []archiveFile {
	return []archiveFile{
		{name: executable, data: []byte("binary"), mode: 0o755},
		{name: "LICENSE", data: []byte("license"), mode: 0o644},
		{name: "README.md", data: []byte("readme"), mode: 0o644},
	}
}

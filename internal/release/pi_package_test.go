package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiTarballContainsPackageMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vgxness-pi-0.1.0.tgz")
	if err := writeNpmTarball(path, []archiveFile{{name: "package.json", data: []byte(`{"name":"@vgxness/pi"}`), mode: 0o644}, {name: "LICENSE", data: []byte("license"), mode: 0o644}, {name: "resources/prompts/manager.md", data: []byte("prompt"), mode: 0o644}, {name: "resources/skills/sdd-lifecycle/SKILL.md", data: []byte("skill"), mode: 0o644}}); err != nil {
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
	defer gz.Close()
	reader := tar.NewReader(gz)
	header, err := reader.Next()
	if err != nil || header.Name != "package/package.json" {
		t.Fatalf("first entry = %v, %v", header, err)
	}
}

func TestPiSidecarReadbackRejectsTamperedManifestAndExtraFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vgxness-pi-backend-linux-arm64-0.1.0.tgz")
	binary := []byte("backend")
	digest := sha256.Sum256(binary)
	metadata := []byte(`{"name":"@vgxness/pi-backend-linux-arm64","version":"0.1.0","os":["linux"],"cpu":["arm64"]}`)
	manifest, _ := json.Marshal(map[string]string{"name": "@vgxness/pi-backend-linux-arm64", "version": "0.1.0", "binary": "bin/vgxness-pi-backend", "sha256": hex.EncodeToString(digest[:])})
	files := []archiveFile{{"package.json", metadata, 0o644}, {"manifest.json", manifest, 0o644}, {"bin/vgxness-pi-backend", binary, 0o755}}
	if err := writeNpmTarball(path, files); err != nil {
		t.Fatal(err)
	}
	if err := verifyPiTarball(path); err != nil {
		t.Fatal(err)
	}
	files[1].data = []byte(`{"name":"@vgxness/pi-backend-linux-arm64","version":"0.1.0","binary":"bin/vgxness-pi-backend","sha256":"` + strings.Repeat("0", 64) + `"}`)
	bad := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := writeNpmTarball(bad, files); err != nil {
		t.Fatal(err)
	}
	if err := verifyPiTarball(bad); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	files[1].data = manifest
	files = append(files, archiveFile{"extra", []byte("x"), 0o644})
	extra := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := writeNpmTarball(extra, files); err != nil {
		t.Fatal(err)
	}
	if err := verifyPiTarball(extra); err == nil {
		t.Fatal("extra sidecar file accepted")
	}
}

func TestPiReleaseRejectsExistingOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "exists")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	err := PackagePi(context.Background(), t.TempDir(), output)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
}

func TestPiReleaseVerificationRejectsExtraAssetsAndMalformedChecksums(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"PROVENANCE.json", "SHA256SUMS", "vgxness-pi-0.1.0.tgz"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range piTargets {
		name := "vgxness-pi-backend-" + target.platform + "-" + target.arch + "-0.1.0.tgz"
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "foreign.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseDirectory(directory); err == nil {
		t.Fatal("extra release asset accepted")
	}
	if _, err := parsePiChecksums("" + strings.Repeat("a", 64) + "  one.tgz\n" + strings.Repeat("a", 64) + "  one.tgz\n"); err == nil {
		t.Fatal("duplicate checksum accepted")
	}
}

func TestPiProvenanceRejectsDuplicateKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PROVENANCE.json")
	content := `{"name":"@vgxness/pi","name":"other","version":"0.1.0","sourceSHA256":"source","provenance":"local source snapshot; unpublished"}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPiProvenance(path, "source"); err == nil {
		t.Fatal("duplicate provenance key accepted")
	}
}

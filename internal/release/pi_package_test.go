package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiSourceIdentityIgnoresCoverageOutputButDetectsSourceDrift(t *testing.T) {
	repository := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	source := filepath.Join(repository, "source.go")
	if err := os.WriteFile(source, []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repository, "add", "source.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	before, err := piSourceIdentity(repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "coverage.out"), []byte("mode: atomic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterCoverage, err := piSourceIdentity(repository)
	if err != nil {
		t.Fatal(err)
	}
	if before != afterCoverage {
		t.Fatal("coverage output changed Pi source identity")
	}
	if err := os.WriteFile(filepath.Join(repository, "extra.go"), []byte("package extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterSource, err := piSourceIdentity(repository)
	if err != nil {
		t.Fatal(err)
	}
	if before == afterSource {
		t.Fatal("tracked source drift did not change Pi source identity")
	}
}

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

func TestPiPortableReadbackRejectsLegacySidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vgxness-pi-backend-linux-arm64-0.1.0.tgz")
	if err := writeNpmTarball(path, []archiveFile{{"package.json", []byte(`{"name":"@vgxness/pi-backend-linux-arm64"}`), 0o644}}); err != nil {
		t.Fatal(err)
	}
	if err := verifyPiTarball(path); err == nil {
		t.Fatal("legacy sidecar accepted")
	}
}
func TestPiPortableMetadataRejectsRuntimeDependenciesAndOldEngine(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := readJSON(filepath.Join(root, "packages", "pi", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePortablePiMetadata(pkg); err != nil {
		t.Fatal(err)
	}
	pkg["optionalDependencies"] = map[string]any{"@vgxness/pi-backend-linux-x64": piVersion}
	if err := validatePortablePiMetadata(pkg); err == nil {
		t.Fatal("sidecar dependency accepted")
	}
	delete(pkg, "optionalDependencies")
	pkg["engines"] = map[string]any{"node": ">=22"}
	if err := validatePortablePiMetadata(pkg); err == nil {
		t.Fatal("unsupported engine accepted")
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

func TestPiPortableArtifactNeedsNoBuildToolsAndRejectsMigrationDrift(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	stage := t.TempDir()
	if err := packagePiMain(repository, stage); err != nil {
		t.Fatal(err)
	}
	names, err := piRegularNames(stage)
	if err != nil || len(names) != 1 || names[0] != "vgxness-pi-0.1.0.tgz" {
		t.Fatalf("names=%v err=%v", names, err)
	}
	path := filepath.Join(stage, names[0])
	if err := verifyPiTarball(path); err != nil {
		t.Fatal(err)
	}
	entries, err := readPiTarball(path)
	if err != nil {
		t.Fatal(err)
	}
	files := []archiveFile{}
	for name, entry := range entries {
		if strings.HasSuffix(name, "001_memory.sql") {
			entry.data = append(entry.data, []byte("\n-- drift")...)
		}
		files = append(files, archiveFile{strings.TrimPrefix(name, "package/"), entry.data, entry.mode})
	}
	bad := filepath.Join(t.TempDir(), names[0])
	if err := writeNpmTarball(bad, files); err != nil {
		t.Fatal(err)
	}
	if err := verifyPiTarball(bad); err == nil || !strings.Contains(err.Error(), "migration hash") {
		t.Fatalf("drift result=%v", err)
	}
}

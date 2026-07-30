package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestGoCIWorkflowContract(t *testing.T) {
	workflow := readRepositoryFile(t, "../../.github/workflows/go-ci.yml")
	t.Run("workflow is auditable", func(t *testing.T) {
		for _, want := range []string{
			"workflow_call:", "permissions:\n  contents: read", "pull_request:", "push:", "branches: [main]",
			"go-version: 1.26.3", "persist-credentials: false", "cancel-in-progress: true",
			"ref: ${{ inputs.ref || github.sha }}",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0",
			"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1",
		} {
			if !strings.Contains(workflow, want) {
				t.Errorf("workflow missing %q", want)
			}
		}
		if strings.Count(workflow, "branches: [main]") != 2 {
			t.Error("workflow must target main for pull requests and pushes")
		}
		pinned := regexp.MustCompile(`^[^@]+@[0-9a-f]{40} # v[0-9]+\.[0-9]+\.[0-9]+$`)
		for _, line := range strings.Split(workflow, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "uses: ") && !pinned.MatchString(strings.TrimPrefix(line, "uses: ")) {
				t.Errorf("mutable or unauditable action: %q", line)
			}
		}
	})
	t.Run("standard lanes are independent and aggregated", func(t *testing.T) {
		lanes := []string{"coverage", "race", "static", "linux-e2e", "fuzz-openspec", "fuzz-launcher-manifest", "windows-compile", "windows-install", "darwin-smoke"}
		for _, lane := range lanes {
			if !strings.Contains(workflow, "  "+lane+":\n") {
				t.Errorf("workflow missing standard lane %q", lane)
			}
		}
		if strings.Count(workflow, "    needs:") != 1 || !strings.Contains(workflow, "needs: [coverage, race, static, linux-e2e, fuzz-openspec, fuzz-launcher-manifest, windows-compile, windows-install, darwin-smoke]") {
			t.Error("only the standard aggregate gate may depend on validation lanes")
		}
		if !strings.Contains(workflow, "  quality:\n    name: quality\n    if: ${{ always() }}") {
			t.Error("workflow must preserve the always-running quality check required by branch protection")
		}
		for _, result := range []string{"needs.coverage.result", "needs.race.result", "needs.static.result", "needs.linux-e2e.result", "needs.fuzz-openspec.result", "needs.fuzz-launcher-manifest.result", "needs.windows-compile.result", "needs.windows-install.result", "needs.darwin-smoke.result"} {
			if !strings.Contains(workflow, result) {
				t.Errorf("aggregate gate does not require %q", result)
			}
		}
	})
	t.Run("all standard evidence is declared", func(t *testing.T) {
		for _, command := range []string{
			"go test -count=1 -covermode=atomic -coverprofile=coverage.out ./...", "go test -count=1 -race ./...",
			"go tool cover -func=coverage.out", "required=74.5", "Coverage floor failed:",
			"go vet ./...", "gofmt -l .", "go mod tidy -diff", "git diff --check",
			"go mod verify", "go build -trimpath ./...",
			"go test -tags=e2e -count=1 -run '^TestCleanCheckoutSetupAndNativeSDD$' ./internal/e2e",
			"go test -count=1 -run '^$' -fuzz '^FuzzParseOpenSpecProjection$' -fuzztime=10s ./internal/sdd",
			"go test -count=1 -run '^$' -fuzz '^FuzzDecodeManifest$' -fuzztime=10s ./internal/launcher",
			"GOOS=windows GOARCH=amd64 go test -count=1 -run '^$' -exec=/usr/bin/true ./...",
			"GOOS=windows GOARCH=amd64 go test -tags=e2e -count=1 -run '^$' -exec=/usr/bin/true ./internal/e2e",
			"go test -count=1 ./...",
			"go build -trimpath -o vgxness ./cmd/vgxness", "./vgxness version",
			"go test -count=1 ./internal/launcher ./internal/config ./internal/memory ./internal/providers/...",
		} {
			if !strings.Contains(workflow, command) {
				t.Errorf("workflow missing gate %q", command)
			}
		}
		if strings.Contains(workflow, "run: go test ./...") {
			t.Error("coverage is the ordinary test evidence; an uncounted duplicate plain full-test pass is forbidden")
		}
		if strings.Contains(workflow, "go mod tidy\n") || strings.Contains(workflow, "go test -c -o") || strings.Contains(workflow, "while IFS=") {
			t.Error("workflow contains a mutating tidy or serial Windows test compilation")
		}
		if strings.Count(workflow, "run: go mod download") != 7 {
			t.Error("the seven cold-runner test and smoke jobs must prefetch modules")
		}
	})
	t.Run("coverage upload survives failure", func(t *testing.T) {
		upload := strings.Index(workflow, "- name: Upload coverage")
		race := strings.Index(workflow, "  race:")
		if upload < 0 || race <= upload || !strings.Contains(workflow[upload:race], "if: always()") || !strings.Contains(workflow[upload:race], "path: coverage.out") {
			t.Error("coverage upload must always run and publish coverage.out")
		}
	})
}

func TestReleaseWorkflowContract(t *testing.T) {
	workflow := readRepositoryFile(t, "../../.github/workflows/release.yml")
	for _, want := range []string{
		"uses: ./.github/workflows/go-ci.yml", "ref: ${{ github.sha }}", "  build:\n    runs-on: ubuntu-24.04",
		"needs: [standard-validation, build, windows-smoke]", "contents: write", "sha256sum -c SHA256SUMS",
		"Verify Linux artifact and self-install", "Verify Windows artifact and self-install", "--verify-tag", "--prerelease",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1",
		"actions/download-artifact@37930b1c2abaa49bbe596cd826c3c89aef350131 # v7.0.0",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	for _, asset := range []string{
		"linux_amd64.tar.gz", "linux_arm64.tar.gz", "darwin_amd64.tar.gz", "darwin_arm64.tar.gz",
		"windows_amd64.zip", "windows_arm64.zip", "dist/SHA256SUMS",
	} {
		if strings.Count(workflow, asset) == 0 {
			t.Errorf("release workflow missing asset %q", asset)
		}
	}
	if strings.Contains(workflow, "Validate tagged source") || strings.Contains(workflow, "run: go test ./...") {
		t.Error("release must reuse exact-SHA standard validation instead of maintaining a partial inline gate")
	}
	if strings.Contains(workflow, "  build:\n    needs: standard-validation") {
		t.Error("release asset construction should overlap standard validation; publication remains the joining gate")
	}
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate foundation test source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), path))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestFoundationProductContract(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	current := string(readme)
	for _, claim := range []string{"Go 1.26", "`status`", "`doctor`", "SQLite", "read-only"} {
		if !strings.Contains(current, claim) {
			t.Errorf("README omits delivered foundation claim %q", claim)
		}
	}
	for _, stale := range []string{"does not contain Go source", "complete product runtime", "Chronicle writer is delivered"} {
		if strings.Contains(current, stale) {
			t.Errorf("README contains unsupported claim %q", stale)
		}
	}
	migrations, err := filepath.Glob("../memory/migrations/*.sql")
	if err != nil || len(migrations) != 5 {
		t.Fatalf("foundation must retain exactly five migrations: %v %v", migrations, err)
	}
	if err := filepath.WalkDir("../../.github", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.Contains(strings.ToLower(path), "ruleset") || strings.Contains(strings.ToLower(path), "branch-protection") {
			t.Errorf("branch protection remains deferred: %s", path)
		}
		return walkErr
	}); err != nil {
		t.Fatal(err)
	}
	assertOpenCodeDocumentationContract(t)
}

func assertOpenCodeDocumentationContract(t *testing.T) {
	t.Helper()
	documents := map[string][]string{
		"../../README.md":                     {"20 managed artifacts", "semantic merge", "`opencode.jsonc` bytes remain unchanged", "default-agent.json"},
		"../../CHANGELOG.md":                  {"20 managed artifacts", "semantic merge", "`opencode.jsonc` bytes remain unchanged", "default-agent.json"},
		"../../docs/product-blueprint.md":     {"exactly 20 artifacts", "semantic merge", "`opencode.jsonc` bytes remain unchanged", "default-agent.json"},
		"../../docs/product-blueprint.es.md":  {"exactamente 20 artefactos", "fusi\u00f3n sem\u00e1ntica", "conserva intactos los bytes de cualquier `opencode.jsonc` existente", "default-agent.json"},
		"../../docs/go-implementation.md":     {"20 exact managed artifacts", "semantic merge", "`opencode.jsonc` bytes remain unchanged", "default-agent.json"},
		"../../docs/opencode-setup-wizard.md": {"20 managed artifacts", "semantic merge", "`opencode.jsonc` bytes remain unchanged", "default-agent.json"},
	}
	for path, claims := range documents {
		content := readRepositoryFile(t, path)
		currentContent := content
		if path == "../../CHANGELOG.md" {
			currentContent = strings.SplitN(content, "\n## v", 2)[0]
		}
		for _, claim := range claims {
			if !strings.Contains(currentContent, claim) {
				t.Errorf("%s omits OpenCode contract claim %q", path, claim)
			}
		}
		for _, stale := range []string{"19 managed artifacts", "exactly 19 artifacts", "exactamente 19 artefactos", "dedicated `opencode.jsonc`", "exact `opencode.jsonc` overlay"} {
			if strings.Contains(currentContent, stale) {
				t.Errorf("%s contains stale OpenCode contract claim %q", path, stale)
			}
		}
	}
}

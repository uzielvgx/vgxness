//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The CI workflow is the product's evidence contract; these checks keep it
// auditable (pinned actions, one quality gate over every lane) without
// restating each command.
func TestGoCIWorkflowContract(t *testing.T) {
	workflow := readRepositoryFile(t, "../../.github/workflows/go-ci.yml")
	for _, want := range []string{
		"workflow_call:", "permissions:\n  contents: read", "pull_request:", "push:", "branches: [main]",
		"cancel-in-progress: true", "ref: ${{ inputs.ref || github.sha }}",
		"go test -count=1 -covermode=atomic -coverprofile=coverage.out ./...", "Coverage floor failed:",
		"go test -count=1 -race ./...", "go vet ./...", "gofmt -l .", "go mod tidy -diff", "go mod verify",
		"go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
		"claude plugin validate .claude-plugin/marketplace.json --strict", "claude plugin validate plugins/vgxness --strict",
		"vgxness claude-code hook session-start", "vgxness mcp --full",
		"go test -tags=e2e -count=1 ./internal/e2e",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	assertPinnedActions(t, workflow)
	if strings.Contains(workflow, "go mod tidy\n") {
		t.Error("workflow must not run a mutating tidy")
	}
	goVersions := regexp.MustCompile(`go-version: ([0-9.]+)`).FindAllStringSubmatch(workflow, -1)
	for _, match := range goVersions {
		if match[1] != "1.26.6" {
			t.Errorf("inconsistent Go version %q", match[1])
		}
	}
	jobs := regexp.MustCompile(`(?m)^  ([a-z0-9-]+):\n`).FindAllStringSubmatch(workflow[strings.Index(workflow, "\njobs:\n"):], -1)
	var lanes []string
	for _, job := range jobs {
		if job[1] != "quality" {
			lanes = append(lanes, job[1])
		}
	}
	quality := workflow[strings.Index(workflow, "  quality:\n"):]
	if !strings.Contains(quality, "needs: ["+strings.Join(lanes, ", ")+"]") {
		t.Errorf("quality gate must need every lane in order: %v", lanes)
	}
	for _, lane := range lanes {
		if !strings.Contains(quality, "require_success "+lane+" ") {
			t.Errorf("quality gate does not require lane %q", lane)
		}
	}
}

func TestReleaseWorkflowContract(t *testing.T) {
	workflow := readRepositoryFile(t, "../../.github/workflows/release.yml")
	for _, want := range []string{
		"uses: ./.github/workflows/go-ci.yml", "ref: ${{ github.sha }}", "needs: standard-validation",
		"contents: write", "id-token: write", "attestations: write", "fetch-depth: 0",
		"claude plugin validate plugins/vgxness --strict", "args: release --clean", "TAP_GITHUB_TOKEN", "dist/SHA256SUMS",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	assertPinnedActions(t, workflow)
	config := readRepositoryFile(t, "../../.goreleaser.yaml")
	for _, want := range []string{
		"main: ./cmd/vgxness", "CGO_ENABLED=0", "name_template: SHA256SUMS", "wrap_in_directory: true",
		"internal/buildinfo.Version={{ .Tag }}", "internal/buildinfo.Commit={{ .FullCommit }}", "internal/buildinfo.Date={{ .CommitDate }}",
		"name: homebrew-tap", "name: scoop-bucket",
	} {
		if !strings.Contains(config, want) {
			t.Errorf(".goreleaser.yaml missing %q", want)
		}
	}
}

func TestMakefileContract(t *testing.T) {
	makefile := readRepositoryFile(t, "../../Makefile")
	const target = "\nvuln:\n\tgo run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./..."
	if !strings.Contains(makefile, ".PHONY: fast verify vuln") || !strings.Contains(makefile, target) {
		t.Error("Makefile must expose the exact pinned vulnerability scan as a separate vuln target")
	}
	verify, vuln := strings.Index(makefile, "\nverify:\n"), strings.Index(makefile, "\nvuln:\n")
	if verify < 0 || vuln < 0 {
		t.Fatal("Makefile must retain verify and vuln targets")
	}
	verifyRecipe := makefile[verify:]
	if next := strings.Index(makefile[verify+1:], "\n\n"); next >= 0 {
		verifyRecipe = makefile[verify : verify+1+next]
	}
	if strings.Contains(verifyRecipe, "govulncheck") {
		t.Error("network-dependent vulnerability scanning must remain outside make verify")
	}
}

// assertPinnedActions requires every third-party action to be pinned to a
// full commit SHA with its version noted, and every checkout to drop the
// token from the working tree.
func assertPinnedActions(t *testing.T, workflow string) {
	t.Helper()
	uses := regexp.MustCompile(`(?m)uses: ([^\s]+)(.*)$`).FindAllStringSubmatch(workflow, -1)
	pinned := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	for _, use := range uses {
		if strings.HasPrefix(use[1], "./") {
			continue
		}
		if !pinned.MatchString(use[1]) || !strings.Contains(use[2], "# v") {
			t.Errorf("action %q is not pinned to a commit SHA with a version comment", use[1])
		}
	}
	checkouts := strings.Count(workflow, "actions/checkout@")
	if checkouts == 0 || strings.Count(workflow, "persist-credentials: false") != checkouts {
		t.Errorf("every checkout (%d) must set persist-credentials: false", checkouts)
	}
}

func TestDependencyFloors(t *testing.T) {
	goMod := readRepositoryFile(t, "../../go.mod")
	goSum := readRepositoryFile(t, "../../go.sum")
	for _, check := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "go.mod requirement", content: goMod, want: "golang.org/x/text v0.39.0 // indirect"},
		{name: "go.sum module checksum", content: goSum, want: "golang.org/x/text v0.39.0 "},
		{name: "go.sum go.mod checksum", content: goSum, want: "golang.org/x/text v0.39.0/go.mod "},
	} {
		if !strings.Contains(check.content, check.want) {
			t.Errorf("dependency floor missing %s %q", check.name, check.want)
		}
	}
}

func TestDocumentedSQLiteSchemaMatchesMigrationHead(t *testing.T) {
	migrations := readRepositoryFile(t, "../memory/migrations.go")
	versions := regexp.MustCompile(`\{version: ([0-9]+),`).FindAllStringSubmatch(migrations, -1)
	if len(versions) == 0 {
		t.Fatal("memory migration ledger has no versions")
	}
	head := versions[len(versions)-1][1]
	for _, path := range []string{"../../README.md", "../../docs/memory.md"} {
		document := readRepositoryFile(t, path)
		if !strings.Contains(document, "schema v"+head) && !strings.Contains(document, "schema-v"+head) {
			t.Errorf("%s does not declare current SQLite schema v%s", path, head)
		}
		for version := 1; version < len(versions); version++ {
			stale := "v" + strconv.Itoa(version)
			phrase := regexp.MustCompile(`(?:schema |schema-|older supported schema to )` + regexp.QuoteMeta(stale) + `(?:\D|$)`)
			if phrase.MatchString(document) {
				t.Errorf("%s retains stale current SQLite schema phrase %q", path, stale)
			}
		}
	}
}

func TestDocumentedDockerAdmissionBoundary(t *testing.T) {
	compose := readRepositoryFile(t, "../../deploy/docker/compose.yaml")
	for _, want := range []string{
		"VGXNESS_SYNC_AUTH_GLOBAL_PER_MINUTE: \"120\"",
		"VGXNESS_SYNC_AUTH_DEVICE_PER_MINUTE: \"60\"",
		"VGXNESS_SYNC_AUTH_DEVICE_STATES: \"256\"",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("Docker deployment omits admission default %q", want)
		}
	}
	for _, path := range []string{"../../deploy/docker/README.md", "../../docs/sync.md"} {
		if !strings.Contains(readRepositoryFile(t, path), "rate limit") {
			t.Errorf("%s omits the distributed admission-limit boundary", path)
		}
	}
}

func TestFoundationProductContract(t *testing.T) {
	readme := readRepositoryFile(t, "../../README.md")
	for _, claim := range []string{"Go 1.26", "SQLite", "`vgxness doctor`", "claude plugin"} {
		if !strings.Contains(readme, claim) {
			t.Errorf("README omits delivered foundation claim %q", claim)
		}
	}
	for _, stale := range []string{"OpenCode", "Codex", "Pi Manager", "self-install", "vgxness setup"} {
		if strings.Contains(readme, stale) {
			t.Errorf("README still describes the retired %q surface", stale)
		}
	}
	migrations, err := filepath.Glob("../memory/migrations/*.sql")
	if err != nil || len(migrations) != 23 {
		t.Fatalf("foundation must retain exactly twenty-three migrations: %v %v", migrations, err)
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

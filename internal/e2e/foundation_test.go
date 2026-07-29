package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGoCIWorkflowContract(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/go-ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	t.Run("workflow is auditable", func(t *testing.T) {
		for _, want := range []string{
			"permissions:\n  contents: read", "pull_request:", "push:", "branches: [main]",
			"go-version: 1.26.3", "persist-credentials: false", "cancel-in-progress: true",
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
	t.Run("all gates are declared", func(t *testing.T) {
		for _, command := range []string{
			"go test ./...", "go test -race ./...", "go test -covermode=atomic -coverprofile=coverage.out ./...",
			"go vet ./...", "gofmt -l .", "go mod tidy", "git diff --exit-code -- go.mod go.sum",
			"go mod verify", "go build -trimpath ./...",
		} {
			if !strings.Contains(workflow, command) {
				t.Errorf("workflow missing gate %q", command)
			}
		}
	})
	t.Run("coverage upload survives failure", func(t *testing.T) {
		upload := strings.Index(workflow, "- name: Upload coverage")
		if upload < 0 || !strings.Contains(workflow[upload:], "if: always()") || !strings.Contains(workflow[upload:], "path: coverage.out") {
			t.Error("coverage upload must always run and publish coverage.out")
		}
	})
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
}

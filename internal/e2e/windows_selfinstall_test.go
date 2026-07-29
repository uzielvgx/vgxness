//go:build e2e && windows

package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsSelfInstallLifecycle(t *testing.T) {
	repository := repositoryRoot(t)
	root := t.TempDir()
	homeDirectory := filepath.Join(root, "home")
	temporaryDirectory := filepath.Join(root, "tmp")
	fakeBinDirectory := filepath.Join(root, "fake-bin")
	binDirectory := filepath.Join(root, "installed", "bin")
	dataDirectory := filepath.Join(root, "installed", "data")
	for _, directory := range []string{homeDirectory, temporaryDirectory, fakeBinDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	firstCandidate := filepath.Join(root, "candidate-first.exe")
	secondCandidate := filepath.Join(root, "candidate-second.exe")
	build(t, goExecutable, repository, firstCandidate, "./cmd/vgxness")
	buildDistinctCandidate(t, goExecutable, repository, secondCandidate)

	environment := isolatedEnvironment(homeDirectory, temporaryDirectory, fakeBinDirectory)
	firstPreview := selfInstall(t, environment, repository, firstCandidate, "preview", binDirectory, dataDirectory)
	firstPreview.require(t, map[string]string{
		"state":              "absent",
		"active_sha256":      "",
		"update_available":   "false",
		"rollback_available": "false",
		"changed":            "true",
	})
	firstSHA := firstPreview.values["source_sha256"]
	if firstSHA == "" {
		t.Fatalf("first candidate source hash is empty:\n%s", firstPreview.raw)
	}

	firstInstall := selfInstall(t, environment, repository, firstCandidate, "install", binDirectory, dataDirectory)
	firstInstall.require(t, map[string]string{
		"state":              "installed",
		"source_sha256":      firstSHA,
		"active_sha256":      firstSHA,
		"previous_sha256":    "",
		"update_available":   "false",
		"rollback_available": "false",
		"changed":            "true",
	})
	launcher := filepath.Join(binDirectory, "vgxness.exe")
	firstStatus := selfInstall(t, environment, repository, launcher, "status", binDirectory, dataDirectory)
	firstStatus.require(t, map[string]string{
		"state":              "installed",
		"source_sha256":      firstSHA,
		"active_sha256":      firstSHA,
		"update_available":   "false",
		"rollback_available": "false",
		"changed":            "false",
	})

	secondPreview := selfInstall(t, environment, repository, secondCandidate, "preview", binDirectory, dataDirectory)
	secondSHA := secondPreview.values["source_sha256"]
	if secondSHA == "" || secondSHA == firstSHA {
		t.Fatalf("candidate source hashes must differ: first=%q second=%q\n%s", firstSHA, secondSHA, secondPreview.raw)
	}
	secondPreview.require(t, map[string]string{
		"state":              "installed",
		"active_sha256":      firstSHA,
		"previous_sha256":    "",
		"update_available":   "true",
		"rollback_available": "false",
		"changed":            "true",
	})

	secondInstall := selfInstall(t, environment, repository, secondCandidate, "install", binDirectory, dataDirectory)
	secondInstall.require(t, map[string]string{
		"state":              "installed",
		"source_sha256":      secondSHA,
		"active_sha256":      secondSHA,
		"previous_sha256":    firstSHA,
		"update_available":   "false",
		"rollback_available": "true",
		"changed":            "true",
	})
	updatedStatus := selfInstall(t, environment, repository, launcher, "status", binDirectory, dataDirectory)
	updatedStatus.require(t, map[string]string{
		"state":              "installed",
		"source_sha256":      secondSHA,
		"active_sha256":      secondSHA,
		"previous_sha256":    firstSHA,
		"update_available":   "false",
		"rollback_available": "true",
		"changed":            "false",
	})

	rollback := selfInstall(t, environment, repository, launcher, "rollback", binDirectory, dataDirectory)
	rollback.require(t, map[string]string{
		"state":              "installed",
		"active_sha256":      firstSHA,
		"previous_sha256":    "",
		"rollback_available": "false",
		"changed":            "true",
	})
	finalStatus := selfInstall(t, environment, repository, launcher, "status", binDirectory, dataDirectory)
	finalStatus.require(t, map[string]string{
		"state":              "installed",
		"source_sha256":      firstSHA,
		"active_sha256":      firstSHA,
		"previous_sha256":    "",
		"update_available":   "false",
		"rollback_available": "false",
		"changed":            "false",
	})
}

type selfInstallResult struct {
	raw    string
	values map[string]string
}

func selfInstall(t *testing.T, environment []string, directory, executable, action, binDirectory, dataDirectory string) selfInstallResult {
	t.Helper()
	output := run(t, environment, directory, executable, "self", action, "--bin-dir", binDirectory, "--data-dir", dataDirectory)
	result := selfInstallResult{raw: output, values: make(map[string]string)}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !ok || key == "" {
			t.Fatalf("invalid self-install output line %q:\n%s", line, output)
		}
		result.values[key] = value
	}
	return result
}

func (result selfInstallResult) require(t *testing.T, expected map[string]string) {
	t.Helper()
	for key, value := range expected {
		actual, present := result.values[key]
		if !present {
			t.Fatalf("self-install output is missing %s:\n%s", key, result.raw)
		}
		if actual != value {
			t.Fatalf("self-install %s=%q, want %q:\n%s", key, actual, value, result.raw)
		}
	}
}

func buildDistinctCandidate(t *testing.T, goExecutable, repository, output string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goExecutable, "build", "-trimpath", "-ldflags=-buildid=vgxness-windows-smoke-second", "-o", output, "./cmd/vgxness")
	command.Dir = repository
	command.Env = replaceEnvironment(os.Environ(), map[string]string{"GOPROXY": "off", "GOTOOLCHAIN": "local"}, "GOPROXY", "GOTOOLCHAIN")
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build distinct candidate: %v\n%s", err, combined)
	}
}

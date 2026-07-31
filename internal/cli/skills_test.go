package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/skills"
)

func TestRunSkillsLifecycleUsesIsolatedAbsoluteDirectory(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	for _, command := range []string{"preview", "install", "status", "uninstall"} {
		stdout.Reset()
		code := RunSkills(context.Background(), []string{command, "--skills-dir", directory}, &stdout, &stderr, skills.New())
		if code != 0 || !strings.Contains(stdout.String(), "state=") || !strings.Contains(stdout.String(), "update_needed=") || !strings.Contains(stdout.String(), "sha256[") || stderr.Len() != 0 {
			t.Fatalf("command=%s code=%d stdout=%q stderr=%q", command, code, stdout.String(), stderr.String())
		}
		if command == "uninstall" && !strings.Contains(stdout.String(), "backup_path=") {
			t.Fatalf("uninstall output=%q", stdout.String())
		}
	}
}

func TestSkillsRootUsesCanonicalTempDirectory(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		t.Fatalf("directory=%q info=%v err=%v", directory, info, err)
	}
}

func TestRunSkillsRejectsInvalidRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunSkills(context.Background(), []string{"preview", "--skills-dir", "relative"}, &stdout, &stderr, skills.New()); code != 2 || stderr.Len() == 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestFailureMapsSkillsRecovery(t *testing.T) {
	if code, message := failure(skills.ErrRecovery); code != 1 || !strings.Contains(message, "skills rollback failed") {
		t.Fatalf("code=%d message=%q", code, message)
	}
}

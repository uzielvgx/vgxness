package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/skillregistry"
	"github.com/vgxness/vgxness/internal/skills"
)

func writeRegistrySkill(t *testing.T, workspace, name string) {
	t.Helper()
	directory := filepath.Join(workspace, ".agents", "skills", name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + " workflow\ncompatibility: Agent Skills hosts\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunSkillsRegistryLifecycleIsBoundedAndOffline(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "skill-registry.json")
	writeRegistrySkill(t, workspace, "alpha")
	writeRegistrySkill(t, workspace, "beta")
	var stdout, stderr bytes.Buffer

	if code := RunSkills(context.Background(), []string{"registry", "refresh", "--workspace", workspace, "--cache-path", cache}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 0 || !strings.Contains(stdout.String(), "entries=2") || strings.Contains(stdout.String(), "body") || stderr.Len() != 0 {
		t.Fatalf("refresh code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "ensure", "--workspace", workspace, "--cache-path", cache}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 0 || !strings.Contains(stdout.String(), "state=ready") || !strings.Contains(stdout.String(), "complete=true") || stderr.Len() != 0 {
		t.Fatalf("ensure code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "search", "--workspace", workspace, "--cache-path", cache, "--query", "alpha", "--limit", "1"}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 0 || !strings.Contains(stdout.String(), "name=alpha") || !strings.Contains(stdout.String(), "sha256=") {
		t.Fatalf("search code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "resolve", "--workspace", workspace, "--cache-path", cache, "--name", "beta"}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 0 || !strings.Contains(stdout.String(), "name=beta") {
		t.Fatalf("resolve code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "resolve", "--workspace", workspace, "--cache-path", cache, "--name", "missing"}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 1 || !strings.Contains(stderr.String(), "not_found") {
		t.Fatalf("missing resolve code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "refresh", "--workspace", workspace, "--cache-path", cache}, &stdout, &stderr, skills.New()); code != 1 || !strings.Contains(stderr.String(), "unavailable") {
		t.Fatalf("nil runtime code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunSkillsRegistryStatusNeverClaimsFalseFresh(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "skill-registry.json")
	var stdout, stderr bytes.Buffer
	if code := RunSkills(context.Background(), []string{"registry", "status", "--workspace", workspace, "--cache-path", cache}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 0 || !strings.Contains(stdout.String(), "cache_state=absent") || !strings.Contains(stdout.String(), "fresh=false") {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "unlock", "--workspace", workspace, "--cache-path", cache}, &stdout, &stderr, skills.New(), skillregistry.New()); code != 0 || !strings.Contains(stdout.String(), "recovered=false") {
		t.Fatalf("unlock code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunSkillsRegistryStatusReportsPresentButStale(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "skill-registry.json")
	writeRegistrySkill(t, workspace, "alpha")
	var stdout, stderr bytes.Buffer
	service := skillregistry.New()
	if code := RunSkills(context.Background(), []string{"registry", "refresh", "--workspace", workspace, "--cache-path", cache, "--max-age", "1ns"}, &stdout, &stderr, skills.New(), service); code != 0 {
		t.Fatalf("refresh code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunSkills(context.Background(), []string{"registry", "status", "--workspace", workspace, "--cache-path", cache, "--max-age", "1ns"}, &stdout, &stderr, skills.New(), service); code != 0 || !strings.Contains(stdout.String(), "cache_state=present") || !strings.Contains(stdout.String(), "fresh=false") {
		t.Fatalf("status code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

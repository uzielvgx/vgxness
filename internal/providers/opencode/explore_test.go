package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

func managerFrontmatter(t *testing.T, prompt string) string {
	t.Helper()
	frontmatter, _, ok := strings.Cut(prompt, "\n---\n")
	if !ok {
		t.Fatal("manager prompt has no frontmatter terminator")
	}
	return frontmatter
}

func TestManagedExploreAgentIsCodeGraphFirstAndStrictlyReadOnly(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	assignment := bundle.resolved.Roles[sdd.RoleResearch]
	prompt, ok := bundle.agents["explore.md"]
	if !ok {
		t.Fatal("managed explore override is missing")
	}

	expectedFrontmatter := fmt.Sprintf(`---
description: VGXNESS-managed read-only repository exploration
mode: subagent
model: %s
variant: %s
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow`, assignment.Model, assignment.Variant)
	if got := managerFrontmatter(t, string(prompt)); got != expectedFrontmatter {
		t.Fatalf("unexpected explore frontmatter:\n%s", got)
	}
	for _, contract := range []string{
		"artifact: opencode-agent/explore; version: 1",
		"Use codegraph_explore first",
		"Do not use shell",
	} {
		if strings.Count(string(prompt), contract) != 1 {
			t.Errorf("explore contract %q count=%d", contract, strings.Count(string(prompt), contract))
		}
	}
}

func TestIntegrationPreservesForeignExploreOverrideAndReturnsConflict(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	agentsDirectory := filepath.Join(configDirectory, "agents")
	testutil.NoError(t, os.MkdirAll(agentsDirectory, 0o700))
	target := filepath.Join(agentsDirectory, "explore.md")
	foreign := []byte("user-owned explore prompt\n")
	testutil.NoError(t, os.WriteFile(target, foreign, 0o600))

	_, installErr := NewIntegration().Install(context.Background(), integration.Options{ConfigDir: configDirectory})
	after, readErr := os.ReadFile(target)
	testutil.Require(t, readErr == nil && errors.Is(installErr, integration.ErrConflict) && bytes.Equal(after, foreign), "foreign explore override changed: err=%v read=%v", installErr, readErr)
}

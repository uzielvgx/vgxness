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
  codegraph_codegraph_explore: allow`, assignment.Model, assignment.Variant)
	if got := managerFrontmatter(t, string(prompt)); got != expectedFrontmatter {
		t.Fatalf("unexpected explore frontmatter:\n%s", got)
	}
	for _, contract := range []string{
		"artifact: opencode-agent/explore; version: 2",
		"Use codegraph_codegraph_explore first",
		"Do not use shell",
	} {
		if strings.Count(string(prompt), contract) != 1 {
			t.Errorf("explore contract %q count=%d", contract, strings.Count(string(prompt), contract))
		}
	}
}

func previousExploreAgent(t *testing.T, current []byte) []byte {
	t.Helper()
	predecessor := string(current)
	for _, replacement := range []struct{ current, previous string }{
		{"codegraph_codegraph_explore: allow", "codegraph_explore: allow"},
		{"artifact: opencode-agent/explore; version: 2", "artifact: opencode-agent/explore; version: 1"},
		{"Use codegraph_codegraph_explore first", "Use codegraph_explore first"},
	} {
		if strings.Count(predecessor, replacement.current) != 1 {
			t.Fatalf("current explore prompt missing unique %q", replacement.current)
		}
		predecessor = strings.Replace(predecessor, replacement.current, replacement.previous, 1)
	}
	return []byte(predecessor)
}

func completeV1ExploreBundle(t *testing.T) modelPlanBundle {
	t.Helper()
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	agents := make(map[string][]byte, len(current.agents))
	for name, content := range current.agents {
		agents[name] = append([]byte(nil), content...)
	}
	agents[exploreAgentName] = previousExploreAgent(t, current.agents[exploreAgentName])
	predecessor, err := encodeModelPlanBundle(current.config, current.resolved, agents)
	testutil.NoError(t, err)
	return predecessor
}

func writeCompleteV1ExploreBundle(t *testing.T, configDirectory string, bundle modelPlanBundle) {
	t.Helper()
	for name, content := range bundle.agents {
		testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "agents", name), content, 0o600))
	}
	testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "vgxness", modelPlanManifestName), bundle.manifest, 0o600))
}

func TestIntegrationUpgradesExactCompleteV1ExploreBundle(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	_, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)

	candidate, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor := completeV1ExploreBundle(t)
	writeCompleteV1ExploreBundle(t, configDirectory, predecessor)
	explorePath := filepath.Join(configDirectory, "agents", exploreAgentName)
	manifestPath := filepath.Join(configDirectory, "vgxness", modelPlanManifestName)

	preview, previewErr := service.Preview(context.Background(), options)
	afterPreviewExplore, exploreReadErr := os.ReadFile(explorePath)
	afterPreviewManifest, manifestReadErr := os.ReadFile(manifestPath)
	testutil.Require(t, previewErr == nil && preview.State == integration.StatePartial && preview.Changed && bytes.Equal(afterPreviewExplore, predecessor.agents[exploreAgentName]) && bytes.Equal(afterPreviewManifest, predecessor.manifest), "exact v1 preview=%+v err=%v exploreRead=%v manifestRead=%v", preview, previewErr, exploreReadErr, manifestReadErr)

	upgraded, installErr := service.Install(context.Background(), options)
	afterUpgradeExplore, exploreReadErr := os.ReadFile(explorePath)
	afterUpgradeManifest, manifestReadErr := os.ReadFile(manifestPath)
	testutil.Require(t, installErr == nil && upgraded.State == integration.StateInstalled && upgraded.Changed && bytes.Equal(afterUpgradeExplore, candidate.agents[exploreAgentName]) && bytes.Equal(afterUpgradeManifest, candidate.manifest), "exact v1 upgrade=%+v err=%v exploreRead=%v manifestRead=%v", upgraded, installErr, exploreReadErr, manifestReadErr)
}

func TestIntegrationRejectsModifiedV1ExploreAgent(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	_, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)

	predecessor := completeV1ExploreBundle(t)
	writeCompleteV1ExploreBundle(t, configDirectory, predecessor)
	target := filepath.Join(configDirectory, "agents", exploreAgentName)
	modified := append(predecessor.agents[exploreAgentName], []byte("\nmodified")...)
	testutil.NoError(t, os.WriteFile(target, modified, 0o600))

	preview, previewErr := service.Preview(context.Background(), options)
	_, installErr := service.Install(context.Background(), options)
	after, readErr := os.ReadFile(target)
	testutil.Require(t, previewErr == nil && preview.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && bytes.Equal(after, modified), "modified v1 preview=%+v previewErr=%v installErr=%v read=%v", preview, previewErr, installErr, readErr)
}

func TestIntegrationRejectsModifiedV1ExploreManifest(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	_, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)

	predecessor := completeV1ExploreBundle(t)
	writeCompleteV1ExploreBundle(t, configDirectory, predecessor)
	target := filepath.Join(configDirectory, "vgxness", modelPlanManifestName)
	modified := append(predecessor.manifest, []byte("\nmodified")...)
	testutil.NoError(t, os.WriteFile(target, modified, 0o600))

	_, previewErr := service.Preview(context.Background(), options)
	_, installErr := service.Install(context.Background(), options)
	after, readErr := os.ReadFile(target)
	testutil.Require(t, errors.Is(previewErr, integration.ErrConflict) && errors.Is(installErr, integration.ErrConflict) && bytes.Equal(after, modified), "modified v1 manifest previewErr=%v installErr=%v read=%v", previewErr, installErr, readErr)
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

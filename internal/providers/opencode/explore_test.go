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

func TestSDDResearchBootstrapContractKeepsDownstreamPredecessorsBound(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)

	research := string(bundle.agents[sddResearchName])
	for _, contract := range []string{
		`"artifact":"explore","acceptedInputs":[]`,
		"acceptedInputs:[] is permitted only for the first-phase research/explore bootstrap",
		"Reject non-empty or fabricated bootstrap inputs",
		"artifact: opencode-agent/vgxness-sdd-research; version: 4",
	} {
		if !strings.Contains(research, contract) {
			t.Errorf("research bootstrap contract missing %q", contract)
		}
	}

	for role, name := range map[sdd.Role]string{
		sdd.RoleProposal: sddProposalName,
		sdd.RoleSpec:     sddSpecName,
		sdd.RoleDesign:   sddDesignName,
		sdd.RoleTasks:    sddTasksName,
	} {
		prompt := string(bundle.agents[name])
		for _, contract := range []string{
			fmt.Sprintf(`"artifact":"%s","acceptedInputs":[{"artifactId":"exact ID","revisionId":"exact ID","digest":"sha256"}]`, role),
			"accepted input revision IDs and SHA-256 digests",
		} {
			if !strings.Contains(prompt, contract) {
				t.Errorf("%s downstream contract missing %q", role, contract)
			}
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
	testutil.NoError(t, os.MkdirAll(filepath.Join(configDirectory, "agents"), 0o700))
	testutil.NoError(t, os.MkdirAll(filepath.Join(configDirectory, "vgxness"), 0o700))
	for name, content := range bundle.agents {
		testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "agents", name), content, 0o600))
	}
	testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "vgxness", modelPlanManifestName), bundle.manifest, 0o600))
}

func TestPreviousSDDBundleMatchesTrustedDigest(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessorV3, err := previousSDDModelPlanBundle(current)
	testutil.NoError(t, err)
	if digest := artifactSHA256(predecessorV3.manifest); digest != "0beb7973e3b6de036ee58ab960c64f5cd0a2fcb8bc0a3de560bd9f45608fe365" {
		t.Fatalf("current SDD manifest=%s", digest)
	}
	for _, profile := range []struct {
		name    string
		role    sdd.Role
		version int
	}{
		{sddResearchName, sdd.RoleResearch, 3}, {sddProposalName, sdd.RoleProposal, 3}, {sddSpecName, sdd.RoleSpec, 3}, {sddDesignName, sdd.RoleDesign, 3}, {sddTasksName, sdd.RoleTasks, 3}, {sddApplyName, sdd.RoleApply, 4},
	} {
		if !strings.Contains(string(predecessorV3.agents[profile.name]), fmt.Sprintf("artifact: opencode-agent/vgxness-sdd-%s; version: %d", profile.role, profile.version)) {
			t.Fatalf("v41 %s does not have version %d", profile.name, profile.version)
		}
	}
	predecessor, err := previousSDDModelPlanBundleV2(current)
	testutil.NoError(t, err)
	if digest := artifactSHA256(predecessor.manifest); digest != "de2752c15d13b85b0589fb8e996b29ecb9f49af3295d1902a7e5c2d5f0ee775b" {
		t.Fatalf("legacy SDD manifest=%s", digest)
	}
	for name, digest := range map[string]string{sddResearchName: "7bcd1f18790e34c48c3c684cbc5c409d0b9163422e89b49a3f389cc026b53906", sddProposalName: "f53bd6fb3c6d92902330e34ab18870512ac0e9b83652dfe9c433e0b0f993d0cf", sddSpecName: "f194eff7b6f9aae7cd4cb54e14e5c60ce37aba7c2f93b73c8d672272ee76de63", sddDesignName: "3a5183faba7d09cd3c592c640f29ee44648023aab395459a5ec9222cc356af15", sddTasksName: "ce768ae7f1fc8df9b780ea3ec4de03951f052933943c51087b1c5c25ea4686d8", sddApplyName: "b14a8e3fa51272749576b5470c6f0f1b0ac67c389b2fe3ad2cf42d917a3cd0b2"} {
		if artifactSHA256(predecessor.agents[name]) != digest {
			t.Fatalf("prior SDD %s digest=%s", name, artifactSHA256(predecessor.agents[name]))
		}
	}
	priorExplore, err := previousExploreModelPlanBundle(current)
	testutil.NoError(t, err)
	combined, err := previousSDDModelPlanBundleV2(priorExplore)
	testutil.NoError(t, err)
	if digest := artifactSHA256(combined.manifest); digest != "96b2c23b4ea6456cb229f841cfabc7b2cd580e1f8bd37aaa78b3802f4f9eb39b" {
		t.Fatalf("combined SDD manifest=%s", digest)
	}
}

func TestIntegrationSDDPredecessorBundles(t *testing.T) {
	config := filepath.Join(t.TempDir(), "opencode")
	service, options := NewIntegration(), integration.Options{ConfigDir: config}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	prior, err := previousSDDModelPlanBundle(current)
	testutil.NoError(t, err)
	legacy, err := previousSDDModelPlanBundleV2(current)
	testutil.NoError(t, err)
	combinedBase, err := previousExploreModelPlanBundle(current)
	testutil.NoError(t, err)
	combined, err := previousSDDModelPlanBundle(combinedBase)
	testutil.NoError(t, err)
	for _, tc := range []struct {
		name   string
		bundle modelPlanBundle
		mutate func()
	}{
		{"mixed SDD", prior, func() {
			for _, name := range []string{sddResearchName, sddApplyName} {
				testutil.NoError(t, os.WriteFile(filepath.Join(config, "agents", name), current.agents[name], 0o600))
			}
		}},
		{"legacy SDD", legacy, func() {}},
		{"combined", combined, func() {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeCompleteV1ExploreBundle(t, config, tc.bundle)
			tc.mutate()
			preview, previewErr := service.Preview(context.Background(), options)
			installed, installErr := service.Install(context.Background(), options)
			status, statusErr := NewIntegration().Status(context.Background(), options)
			idempotent, idempotentErr := service.Install(context.Background(), options)
			testutil.Require(t, previewErr == nil && preview.State == integration.StatePartial && installErr == nil && installed.State == integration.StateInstalled && statusErr == nil && status.State == integration.StateInstalled && status.RetainedPredecessorCount > 0 && status.RetainedPredecessorPath != "" && idempotentErr == nil && !idempotent.Changed, "preview=%+v install=%v status=%+v idempotent=%+v", preview, installErr, status, idempotent)
		})
	}
}

func TestIntegrationUpgradesExactManagerPredecessorCombinations(t *testing.T) {
	config := filepath.Join(t.TempDir(), "opencode")
	service, options := NewIntegration(), integration.Options{ConfigDir: config}
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	managerV41, err := previousManagerModelPlanBundleV41(current)
	testutil.NoError(t, err)
	managerV40, err := previousManagerModelPlanBundleV40(managerV41)
	testutil.NoError(t, err)
	managerV39, err := previousManagerModelPlanBundleV39(managerV40)
	testutil.NoError(t, err)
	for _, tc := range []struct {
		name   string
		bundle modelPlanBundle
	}{
		{"v41", managerV41}, {"v40", managerV40}, {"v39", managerV39},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeCompleteV1ExploreBundle(t, config, tc.bundle)
			preview, previewErr := service.Preview(context.Background(), options)
			installed, installErr := service.Install(context.Background(), options)
			status, statusErr := service.Status(context.Background(), options)
			idempotent, idempotentErr := service.Install(context.Background(), options)
			readback, readErr := os.ReadFile(installed.Path)
			testutil.Require(t,
				previewErr == nil && preview.State == integration.StatePartial &&
					installErr == nil && installed.State == integration.StateInstalled &&
					statusErr == nil && status.State == integration.StateInstalled &&
					idempotentErr == nil && !idempotent.Changed && readErr == nil && bytes.Equal(readback, current.agents[managerAgentName]),
				"preview=%+v install=%v status=%+v idempotent=%+v read=%v", preview, installErr, status, idempotent, readErr,
			)
		})
	}
}

func TestModelPlanBundleForManifestRejectsUnknownManagerPredecessor(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	unknown := append(append([]byte(nil), current.manifest...), ' ')
	if _, err := modelPlanBundleForManifest(unknown, current.config); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("unknown manifest error=%v, want ErrDrift", err)
	}
}

func TestModelPlanBundleForManifestRecognizesAllPredecessorCombinations(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	candidates, err := predecessorBundles(current)
	testutil.NoError(t, err)
	if len(candidates) != 24 {
		t.Fatalf("predecessor combinations=%d", len(candidates))
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		digest := artifactSHA256(candidate.manifest)
		if _, ok := seen[digest]; ok {
			t.Fatalf("duplicate predecessor manifest=%s", digest)
		}
		seen[digest] = struct{}{}
		resolved, resolveErr := modelPlanBundleForManifest(candidate.manifest, candidate.config)
		if resolveErr != nil || !bytes.Equal(resolved.manifest, candidate.manifest) {
			t.Fatalf("manifest=%s resolved=%s err=%v", artifactSHA256(candidate.manifest), artifactSHA256(resolved.manifest), resolveErr)
		}
	}
}

func TestIntegrationRejectsInvalidSDDPredecessorBundles(t *testing.T) {
	for name, mutate := range map[string]func(modelPlanBundle) (string, []byte){
		"modified SDD": func(b modelPlanBundle) (string, []byte) {
			p := filepath.Join("agents", sddSpecName)
			return p, append(b.agents[sddSpecName], '!')
		},
		"modified combined": func(b modelPlanBundle) (string, []byte) {
			p := filepath.Join("agents", exploreAgentName)
			return p, append(b.agents[exploreAgentName], '!')
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "opencode")
			current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
			testutil.NoError(t, err)
			bundle, err := previousSDDModelPlanBundle(current)
			testutil.NoError(t, err)
			if name == "modified combined" {
				base, e := previousExploreModelPlanBundle(current)
				testutil.NoError(t, e)
				bundle, e = previousSDDModelPlanBundle(base)
				testutil.NoError(t, e)
			}
			writeCompleteV1ExploreBundle(t, config, bundle)
			relative, modified := mutate(bundle)
			target := filepath.Join(config, relative)
			testutil.NoError(t, os.WriteFile(target, modified, 0o600))
			service := NewIntegration()
			options := integration.Options{ConfigDir: config}
			preview, previewErr := service.Preview(context.Background(), options)
			_, installErr := service.Install(context.Background(), options)
			after, readErr := os.ReadFile(target)
			testutil.Require(t, previewErr == nil && preview.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && readErr == nil && bytes.Equal(after, modified), "preview=%+v install=%v", preview, installErr)
		})
	}
}

func TestIntegrationRejectsIncompleteSDDPredecessorManifest(t *testing.T) {
	config := filepath.Join(t.TempDir(), "opencode")
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	agents := map[string][]byte{}
	for name, content := range current.agents {
		agents[name] = content
	}
	agents[sddResearchName] = previousSDDAgentPredecessor(sdd.RoleResearch, current.agents[sddResearchName])
	incomplete, err := encodeModelPlanBundle(current.config, current.resolved, agents)
	testutil.NoError(t, err)
	writeCompleteV1ExploreBundle(t, config, current)
	target := filepath.Join(config, "vgxness", modelPlanManifestName)
	testutil.NoError(t, os.WriteFile(target, incomplete.manifest, 0o600))
	service := NewIntegration()
	options := integration.Options{ConfigDir: config}
	_, previewErr := service.Preview(context.Background(), options)
	_, installErr := service.Install(context.Background(), options)
	after, readErr := os.ReadFile(target)
	testutil.Require(t, errors.Is(previewErr, integration.ErrConflict) && errors.Is(installErr, integration.ErrConflict) && readErr == nil && bytes.Equal(after, incomplete.manifest), "preview=%v install=%v", previewErr, installErr)
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

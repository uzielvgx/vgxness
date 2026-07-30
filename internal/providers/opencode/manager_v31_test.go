package opencode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestManagerV31DefinesEvidenceBoundedContract(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	current, err := buildV31ModelPlanBundle(config)
	testutil.NoError(t, err)
	previous, err := buildV30ModelPlanBundle(config)
	testutil.NoError(t, err)

	prompt := string(current.agents[managerAgentName])
	for _, contract := range []string{
		"artifact: opencode-agent/vgxness-manager; version: 31",
		"# Evidence-bounded delegation",
		"goal, scope, nonGoals, acceptanceCriteria, evidenceScope, validation, and stopCondition",
		"fact, inference, or unknown",
		"Do not broaden scope without a consequential decision",
		"Never claim validation passed without observed output",
		"Do not add speculative functionality or abstractions",
		"Stop when the acceptance criteria are satisfied",
	} {
		if strings.Count(prompt, contract) != 1 {
			t.Errorf("manager contract %q count=%d", contract, strings.Count(prompt, contract))
		}
	}
	if strings.Count(prompt, "model: "+current.resolved.Roles[sdd.RoleManager].Model+"\n") != 1 ||
		strings.Count(prompt, "variant: "+string(current.resolved.Roles[sdd.RoleManager].Variant)+"\n") != 1 {
		t.Errorf("manager model selection is not bound exactly once")
	}
	if managerFrontmatter(t, prompt) != managerFrontmatter(t, string(previous.agents[managerAgentName])) {
		t.Error("v31 changed manager permissions or the subagent allowlist")
	}
	if got := artifactSHA256([]byte(managerPrompt)); got != "27ff0b19e70b796e386c39b57db3e83d2be029b583b10a0622e11cf121e8e13d" {
		t.Errorf("embedded historical manager v27 bytes changed: sha256=%s", got)
	}
	if got := artifactSHA256(current.agents[managerAgentName]); got != "6bca613211e89ee8e5140f173519ee00be17a037774845c3e1c1db81aaf16b37" {
		t.Errorf("frozen manager v31 bytes changed: sha256=%s", got)
	}
	if got := artifactSHA256(current.manifest); got != "b9cb67c5806a22c66859fd7b516141209a3deae0b83611bb95a82e83f72ce375" {
		t.Errorf("frozen bundle v31 manifest changed: sha256=%s", got)
	}
}

func TestManagerV31RejectsMissingOrDuplicateInsertionAnchor(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	bundle, err := buildModelPlanBundle(config)
	testutil.NoError(t, err)
	assignment := bundle.resolved.Roles[sdd.RoleManager]
	original := managerPrompt
	t.Cleanup(func() { managerPrompt = original })

	anchor := "# Native authority and delegation\n"
	for name, mutate := range map[string]func(string) string{
		"missing": func(value string) string { return strings.Replace(value, anchor, "# Native delegation\n", 1) },
		"duplicate": func(value string) string {
			return value + "\n" + anchor
		},
	} {
		t.Run(name, func(t *testing.T) {
			managerPrompt = mutate(original)
			_, err := bindManagerV31(assignment)
			if !errors.Is(err, integration.ErrInvalid) {
				t.Fatalf("anchor guard error=%v", err)
			}
		})
	}
}

func TestHistoricalModelPlansRecognizeV31V30V29AndV28(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	for name, build := range map[string]struct {
		version      string
		applyVersion string
		build        func(sdd.ModelPlanConfig) (modelPlanBundle, error)
	}{
		"immediate v31": {version: "version: 31", applyVersion: "version: 2", build: buildV31ModelPlanBundle},
		"immediate v30": {version: "version: 30", applyVersion: "version: 2", build: buildV30ModelPlanBundle},
		"legacy v29":    {version: "version: 29", applyVersion: "version: 2", build: buildV29ModelPlanBundle},
		"legacy v28":    {version: "version: 28", applyVersion: "version: 1", build: buildV28ModelPlanBundle},
	} {
		t.Run(name, func(t *testing.T) {
			bundle, err := build.build(config)
			testutil.NoError(t, err)
			if !bytes.Contains(bundle.agents[managerAgentName], []byte(build.version)) {
				t.Fatalf("historical manager does not contain %q", build.version)
			}
			apply := bundle.agents[sddApplyName]
			if !bytes.Contains(apply, []byte("artifact: opencode-agent/vgxness-sdd-apply; "+build.applyVersion)) {
				t.Fatalf("historical apply does not contain %q", build.applyVersion)
			}
			if build.applyVersion == "version: 2" && !bytes.Contains(apply, []byte("The manager alone validates hashes, writes workspace files, runs tests, saves or accepts revisions, records projections, and advances lifecycle state.")) {
				t.Fatal("historical v2 apply wording changed")
			}
			_, parsed, err := parseInstalledModelPlanManifest(bundle.manifest)
			testutil.NoError(t, err)
			if !bytes.Equal(parsed.manifest, bundle.manifest) {
				t.Fatal("historical model-plan manifest did not round trip exactly")
			}
		})
	}
}

func TestV31HistoricalManifestDigestsAcrossPlans(t *testing.T) {
	want := map[sdd.Plan]string{
		sdd.PlanLow:    "1ea2ff614b9732301eeb2a294aafcb48e555a9a8f2ebb744fca289c94eb7503c",
		sdd.PlanMedium: "4b698045a53b2268c119434e450ba2a6b44136b9e1a3232c93f493f3ea6ad3ed",
		sdd.PlanHigh:   "1e3b42cc57bdaef62b6fbd036e27e90533e1178be0f0ba4c4d1fa88880a3b630",
	}
	for plan, expected := range want {
		config := sdd.DefaultModelPlanConfig()
		config.ActivePlan = plan
		config.Provenance = sdd.ModelPlanCLI
		bundle, err := buildV31ModelPlanBundle(config)
		testutil.NoError(t, err)
		if got := artifactSHA256(bundle.manifest); got != expected {
			t.Errorf("%s v31 manifest sha256=%s, want %s", plan, got, expected)
		}
		_, parsed, err := parseInstalledModelPlanManifest(bundle.manifest)
		testutil.NoError(t, err)
		if !bytes.Equal(parsed.manifest, bundle.manifest) {
			t.Errorf("%s v31 manifest did not round trip", plan)
		}
	}
}

func TestV31HighPlanMatchesShippedManifest(t *testing.T) {
	config := sdd.DefaultModelPlanConfig()
	config.ActivePlan = sdd.PlanHigh
	config.Provenance = sdd.ModelPlanCLI
	bundle, err := buildV31ModelPlanBundle(config)
	testutil.NoError(t, err)
	const shippedSHA256 = "1e3b42cc57bdaef62b6fbd036e27e90533e1178be0f0ba4c4d1fa88880a3b630"
	if got := artifactSHA256(bundle.manifest); got != shippedSHA256 {
		t.Fatalf("high-plan v31 manifest sha256=%s, want shipped %s", got, shippedSHA256)
	}
	_, parsed, err := parseInstalledModelPlanManifest(bundle.manifest)
	testutil.NoError(t, err)
	if !bytes.Equal(parsed.manifest, bundle.manifest) {
		t.Fatal("shipped high-plan v31 identity did not round trip")
	}
}

func TestIntegrationUpgradesOnlyExactManagedV30(t *testing.T) {
	for _, modified := range []bool{false, true} {
		name := "exact"
		if modified {
			name = "modified"
		}
		t.Run(name, func(t *testing.T) {
			configDirectory := filepath.Join(t.TempDir(), "opencode")
			options := integration.Options{ConfigDir: configDirectory}
			service := NewIntegration()
			installed, err := service.Install(context.Background(), options)
			testutil.NoError(t, err)

			current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
			testutil.NoError(t, err)
			previous, err := buildV30ModelPlanBundle(sdd.DefaultModelPlanConfig())
			testutil.NoError(t, err)
			if !bytes.Contains(previous.agents[managerAgentName], []byte("version: 30")) {
				t.Fatal("upgrade fixture is not an exact managed v30 artifact")
			}
			for artifactName, content := range previous.agents {
				testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "agents", artifactName), content, 0o600))
			}
			testutil.NoError(t, os.WriteFile(installed.ManifestPath, previous.manifest, 0o600))
			priorManager := previous.agents[managerAgentName]
			if modified {
				priorManager = append(append([]byte(nil), priorManager...), []byte("\nuser modification\n")...)
				testutil.NoError(t, os.WriteFile(installed.Path, priorManager, 0o600))
			}

			status, err := service.Status(context.Background(), options)
			testutil.NoError(t, err)
			if modified {
				_, installErr := service.Install(context.Background(), options)
				after, readErr := os.ReadFile(installed.Path)
				testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && readErr == nil && bytes.Equal(after, priorManager), "modified v30 changed: status=%+v err=%v", status, installErr)
				return
			}

			testutil.Require(t, status.State == integration.StatePartial, "exact v30 status=%+v", status)
			upgraded, err := service.Install(context.Background(), options)
			testutil.Require(t, err == nil && upgraded.State == integration.StateInstalled && upgraded.Changed, "v30 upgrade=%+v err=%v", upgraded, err)
			manager, managerErr := os.ReadFile(installed.Path)
			manifest, manifestErr := os.ReadFile(installed.ManifestPath)
			testutil.Require(t, managerErr == nil && manifestErr == nil && bytes.Equal(manager, current.agents[managerAgentName]) && bytes.Equal(manifest, current.manifest), "v31 readback mismatch: manager=%v manifest=%v", managerErr, manifestErr)
			testutil.Require(t, upgraded.ArtifactSHA256 == artifactSHA256(manager) && upgraded.ManifestSHA256 == artifactSHA256(manifest), "v31 hashes do not bind readback")
			second, err := service.Install(context.Background(), options)
			testutil.Require(t, err == nil && second.State == integration.StateInstalled && !second.Changed, "v31 install is not idempotent: result=%+v err=%v", second, err)
		})
	}
}

func TestIntegrationRejectsModifiedManagedV31(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	agentsDirectory := filepath.Join(configDirectory, "agents")
	pluginsDirectory := filepath.Join(configDirectory, "plugins")
	manifestDirectory := filepath.Join(configDirectory, "vgxness")
	for _, directory := range []string{agentsDirectory, pluginsDirectory, manifestDirectory} {
		testutil.NoError(t, os.MkdirAll(directory, 0o700))
	}
	previous, err := buildV31ModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	for name, content := range previous.agents {
		testutil.NoError(t, os.WriteFile(filepath.Join(agentsDirectory, name), content, 0o600))
	}
	modified := append(append([]byte(nil), previous.agents[managerAgentName]...), []byte("\nuser modification\n")...)
	managerPath := filepath.Join(agentsDirectory, managerAgentName)
	testutil.NoError(t, os.WriteFile(managerPath, modified, 0o600))
	testutil.NoError(t, os.WriteFile(filepath.Join(manifestDirectory, modelPlanManifestName), previous.manifest, 0o600))
	service := NewIntegration()
	plugin, err := memoryPluginContent(service.executable)
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(filepath.Join(pluginsDirectory, memoryPluginName), previousMemoryPluginV4(plugin), 0o600))

	options := integration.Options{ConfigDir: configDirectory}
	status, statusErr := service.Status(context.Background(), options)
	_, installErr := service.Install(context.Background(), options)
	after, readErr := os.ReadFile(managerPath)
	testutil.Require(t, statusErr == nil && status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && readErr == nil && bytes.Equal(after, modified), "modified v31 changed: status=%+v install=%v read=%v", status, installErr, readErr)
}

func managerFrontmatter(t *testing.T, prompt string) string {
	t.Helper()
	frontmatter, _, ok := strings.Cut(prompt, "\n---\n")
	if !ok {
		t.Fatal("manager prompt has no frontmatter terminator")
	}
	return frontmatter
}

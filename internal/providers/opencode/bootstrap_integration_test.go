package opencode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/agentmodels"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

func TestBootstrapReceiptlessReinstallMigratesAndPreservesModelConfiguration(t *testing.T) {
	customAssignments, err := ExplicitModels(mustBootstrapSelection(t))
	if err != nil {
		t.Fatal(err)
	}
	v2 := schemaV2TestConfig(t)
	cases := []struct {
		name        string
		bundle      func() (modelPlanBundle, error)
		assertModel func(*testing.T, integration.Result)
	}{
		{
			name: "v1 plan", bundle: func() (modelPlanBundle, error) {
				config := modelplan.DefaultModelPlanConfig()
				config.ActivePlan = modelplan.PlanHigh
				return buildBootstrapModelPlanBundle(config)
			},
			assertModel: func(t *testing.T, result integration.Result) {
				t.Helper()
				if result.ModelSchemaVersion != 1 || result.ModelPlan != modelplan.PlanHigh {
					t.Fatalf("V1 model result = %+v", result)
				}
			},
		},
		{
			name: "v2 slots", bundle: func() (modelPlanBundle, error) {
				return buildBootstrapModelPlanBundleV2(v2)
			},
			assertModel: func(t *testing.T, result integration.Result) {
				t.Helper()
				if result.ModelSchemaVersion != 2 || result.ModelPlan != v2.ActivePlan || result.ModelEfficient != v2.Slots[modelplan.CapabilityEfficient].Reference || result.ModelBalanced != v2.Slots[modelplan.CapabilityBalanced].Reference || result.ModelFrontier != v2.Slots[modelplan.CapabilityFrontier].Reference {
					t.Fatalf("V2 model result = %+v", result)
				}
			},
		},
		{
			name: "v3 custom assignments", bundle: func() (modelPlanBundle, error) {
				return buildBootstrapModelPlanBundleV3(modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: "openai", Assignments: customAssignments, Provenance: modelplan.ModelPlanCLI})
			},
			assertModel: func(t *testing.T, result integration.Result) {
				t.Helper()
				if result.ModelSchemaVersion != 3 || result.ModelAssignments == nil {
					t.Fatalf("V3 model result = %+v", result)
				}
				for _, assignment := range result.ModelAssignments {
					if assignment.Model != "openai/gpt-5.6-terra" {
						t.Fatalf("custom assignment was not preserved: %+v", assignment)
					}
				}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			configDir := filepath.Join(t.TempDir(), "opencode")
			options := integration.Options{ConfigDir: configDir}
			service := NewIntegration()
			bundle, err := test.bundle()
			if err != nil {
				t.Fatal(err)
			}
			seedReceiptlessBootstrapOpenCode(t, service, options, bundle)

			migrated, err := service.Reinstall(context.Background(), options)
			if err != nil || migrated.State != integration.StateInstalled || !migrated.Changed {
				t.Fatalf("Reinstall(receiptless bootstrap) = %+v, %v", migrated, err)
			}
			test.assertModel(t, migrated)
			receipt, receiptBytes, err := readInstallationReceipt(configDir)
			if err != nil || receiptBytes == nil || receipt.Contract != orchestration.ManagerContractDigest() || len(receipt.Files) != 9 {
				t.Fatalf("new receipt = %+v, bytes=%t, err=%v", receipt, receiptBytes != nil, err)
			}
			status, err := service.Status(context.Background(), options)
			if err != nil || status.State != integration.StateInstalled || status.Changed {
				t.Fatalf("Status(after bootstrap migration) = %+v, %v", status, err)
			}
			test.assertModel(t, status)
			again, err := service.Install(context.Background(), options)
			if err != nil || again.State != integration.StateInstalled || again.Changed {
				t.Fatalf("Install(after bootstrap migration) = %+v, %v", again, err)
			}
		})
	}
}

func TestBootstrapReceiptlessReinstallRejectsRealModifiedAndMixedAgents(t *testing.T) {
	for _, test := range []struct {
		name    string
		replace func(*testing.T, string)
	}{
		{name: "modified", replace: func(t *testing.T, path string) {
			t.Helper()
			writeBootstrapFile(t, path, []byte("user modification\n"))
		}},
		{name: "mixed", replace: func(t *testing.T, path string) {
			t.Helper()
			config := modelplan.DefaultModelPlanConfig()
			config.ActivePlan = modelplan.PlanLow
			other, err := buildBootstrapModelPlanBundle(config)
			if err != nil {
				t.Fatal(err)
			}
			writeBootstrapFile(t, path, other.agents[generalAgentName])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDir := filepath.Join(t.TempDir(), "opencode")
			options := integration.Options{ConfigDir: configDir}
			service := NewIntegration()
			config := modelplan.DefaultModelPlanConfig()
			config.ActivePlan = modelplan.PlanHigh
			bundle, err := buildBootstrapModelPlanBundle(config)
			if err != nil {
				t.Fatal(err)
			}
			seedReceiptlessBootstrapOpenCode(t, service, options, bundle)
			path := filepath.Join(configDir, "agents", generalAgentName)
			test.replace(t, path)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Reinstall(context.Background(), options); !errors.Is(err, integration.ErrDrift) {
				t.Fatalf("Reinstall(%s agent) error = %v, want drift", test.name, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("rejected %s agent changed: %q, %v", test.name, after, err)
			}
			if _, receipt, err := readInstallationReceipt(configDir); err != nil || receipt != nil {
				t.Fatalf("rejected %s agent created receipt: present=%t, err=%v", test.name, receipt != nil, err)
			}
		})
	}
}

func mustBootstrapSelection(t *testing.T) agentmodels.Config {
	t.Helper()
	selection, err := agentmodels.Single("openai/gpt-5.6-terra", "high")
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func seedReceiptlessBootstrapOpenCode(t *testing.T, service *Integration, options integration.Options, bundle modelPlanBundle) {
	t.Helper()
	for name, body := range bundle.agents {
		writeBootstrapFile(t, filepath.Join(options.ConfigDir, "agents", name), body)
	}
	writeBootstrapFile(t, filepath.Join(options.ConfigDir, "vgxness", modelPlanManifestName), bundle.manifest)
	plugin, err := memoryLifecyclePluginContent(service.executable)
	if err != nil {
		t.Fatal(err)
	}
	writeBootstrapFile(t, filepath.Join(options.ConfigDir, "plugins", memoryLifecyclePluginName), plugin)
	state, err := service.inspect(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range state.artifacts {
		if artifact.defaultState || artifact.defaultAgent != nil {
			writeBootstrapFile(t, artifact.path, artifact.content)
		}
	}
	if _, receipt, err := readInstallationReceipt(options.ConfigDir); err != nil || receipt != nil {
		t.Fatalf("bootstrap seed receipt: present=%t, err=%v", receipt != nil, err)
	}
}

func writeBootstrapFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

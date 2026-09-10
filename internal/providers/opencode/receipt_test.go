package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/testutil"
)

func syntheticReceiptInstallation(t *testing.T, root string) {
	t.Helper()
	receiptPath := receiptArtifactPath(root)
	data, err := os.ReadFile(receiptPath)
	testutil.NoError(t, err)
	r, err := integration.DecodeReceipt(data, "opencode")
	testutil.NoError(t, err)
	manager := "agents/" + managerAgentName
	testutil.NoError(t, os.WriteFile(filepath.Join(root, manager), []byte("synthetic previous manager\n"), 0600))
	manifestPath := filepath.Join(root, "vgxness", modelPlanManifestName)
	data, err = os.ReadFile(manifestPath)
	testutil.NoError(t, err)
	var m modelPlanManifest
	testutil.NoError(t, json.Unmarshal(data, &m))
	m.Artifacts[manager] = artifactSHA256([]byte("synthetic previous manager\n"))
	data, err = json.MarshalIndent(m, "", "  ")
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(manifestPath, append(data, '\n'), 0600))
	files := map[string][]byte{}
	for name := range r.Files {
		files[name], err = os.ReadFile(filepath.Join(root, name))
		testutil.NoError(t, err)
	}
	data, err = integration.EncodeReceipt("opencode", strings.Repeat("a", 64), "", files)
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(receiptPath, data, 0600))
}
func TestReceiptUpgradeAndBootstrap(t *testing.T) {
	for _, scenario := range []string{"previous", "current-without-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "opencode")
			service := NewIntegration()
			options := integration.Options{ConfigDir: root}
			_, err := service.Install(context.Background(), options)
			testutil.NoError(t, err)
			before, err := os.ReadFile(filepath.Join(root, "agents", managerAgentName))
			testutil.NoError(t, err)
			if scenario == "previous" {
				syntheticReceiptInstallation(t, root)
			} else {
				testutil.NoError(t, os.Remove(receiptArtifactPath(root)))
			}
			result, err := service.Reinstall(context.Background(), options)
			if err != nil || result.State != integration.StateInstalled {
				t.Fatalf("upgrade state=%s err=%v", result.State, err)
			}
			after, err := os.ReadFile(filepath.Join(root, "agents", managerAgentName))
			testutil.NoError(t, err)
			if !bytes.Equal(before, after) {
				t.Fatal("current manager was not restored")
			}
			r, _, err := readInstallationReceipt(root)
			testutil.NoError(t, err)
			if len(r.Files) != 9 {
				t.Fatal("incomplete receipt")
			}
		})
	}
}
func TestReceiptDriftNeverMutates(t *testing.T) {
	for _, scenario := range []string{"modified-manager", "modified-receipt", "missing-receipt", "mixed-generation"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "opencode")
			service := NewIntegration()
			options := integration.Options{ConfigDir: root}
			_, err := service.Install(context.Background(), options)
			testutil.NoError(t, err)
			manager := filepath.Join(root, "agents", managerAgentName)
			current, err := os.ReadFile(manager)
			testutil.NoError(t, err)
			syntheticReceiptInstallation(t, root)
			switch scenario {
			case "modified-manager":
				testutil.NoError(t, os.WriteFile(manager, []byte("user edit\n"), 0600))
			case "modified-receipt":
				testutil.NoError(t, os.WriteFile(receiptArtifactPath(root), []byte("{}"), 0600))
			case "missing-receipt":
				testutil.NoError(t, os.Remove(receiptArtifactPath(root)))
			case "mixed-generation":
				testutil.NoError(t, os.WriteFile(manager, current, 0600))
			}
			before, err := os.ReadFile(manager)
			testutil.NoError(t, err)
			_, err = service.Reinstall(context.Background(), options)
			if err == nil {
				t.Fatal("accepted drift")
			}
			after, err := os.ReadFile(manager)
			testutil.NoError(t, err)
			if !bytes.Equal(before, after) {
				t.Fatal("modified manager changed")
			}
		})
	}
}

func TestReceiptUpgradeRollbackPreservesPreviousBytes(t *testing.T) {
	for _, point := range []string{reinstallCheckpointMoved, reinstallCheckpointPublished, reinstallCheckpointVerified} {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "opencode")
			service := NewIntegration()
			options := integration.Options{ConfigDir: root}
			_, err := service.Install(context.Background(), options)
			testutil.NoError(t, err)
			syntheticReceiptInstallation(t, root)
			r, receipt, err := readInstallationReceipt(root)
			testutil.NoError(t, err)
			before := map[string][]byte{"vgxness/installation-receipt.json": receipt}
			for name := range r.Files {
				before[name], err = os.ReadFile(filepath.Join(root, name))
				testutil.NoError(t, err)
			}
			service.reinstallCheckpoint = func(check, path string) error {
				if check == point {
					return fmt.Errorf("injected interruption")
				}
				return nil
			}
			_, err = service.Reinstall(context.Background(), options)
			if err == nil {
				t.Fatal("expected interruption")
			}
			for name, want := range before {
				got, e := os.ReadFile(filepath.Join(root, name))
				if e != nil || !bytes.Equal(got, want) {
					t.Fatalf("rollback lost %s: %v", name, e)
				}
			}
		})
	}
}

func TestReceiptTerminalReadbackRejectsDriftAfterVerification(t *testing.T) {
	root := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: root}
	_, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)
	state, err := service.inspect(context.Background(), options)
	testutil.NoError(t, err)
	held, err := openRootTransaction(root, false)
	testutil.NoError(t, err)
	defer held.Close()
	transaction := reinstallTransaction{root: held, state: state}
	changed := []byte("concurrent edit after verification\n")
	target := filepath.Join(root, "agents", managerAgentName)
	testutil.NoError(t, os.WriteFile(target, changed, 0600))
	err = transaction.finish(nil, false)
	if !errors.Is(err, integration.ErrDrift) || !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("terminal drift error=%v", err)
	}
	got, e := os.ReadFile(target)
	testutil.NoError(t, e)
	if !bytes.Equal(got, changed) {
		t.Fatal("overwrote concurrent edit")
	}
}

func TestReceiptBootstrapNormalizesOnlyRetiredAssignmentKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "opencode")
	config := projectModelPlanToV3(modelplan.DefaultModelPlanConfig())
	if len(config.Assignments) != 13 {
		t.Fatal("fixture must exercise thirteen configuration keys")
	}
	bundle, err := buildModelPlanBundleV3(config)
	testutil.NoError(t, err)
	if len(bundle.agents) != 7 {
		t.Fatal("current bridge output has seven agent files")
	}
	writeModelPlanBundleFixture(t, root, bundle)
	result, err := NewIntegration().Install(context.Background(), integration.Options{ConfigDir: root})
	testutil.NoError(t, err)
	if result.State != integration.StateInstalled {
		t.Fatal(result.State)
	}
	data, err := os.ReadFile(filepath.Join(root, "vgxness", modelPlanManifestName))
	testutil.NoError(t, err)
	m, err := decodeModelPlanManifest(data)
	testutil.NoError(t, err)
	if m.ConfigV3 == nil || len(m.ConfigV3.Assignments) != 7 {
		t.Fatal("did not normalize active selections")
	}
	for key, value := range m.ConfigV3.Assignments {
		if !reflect.DeepEqual(value, config.Assignments[key]) {
			t.Errorf("model selection changed: %s", key)
		}
	}
}

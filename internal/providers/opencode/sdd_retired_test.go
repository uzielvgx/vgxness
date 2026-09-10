package opencode

import (
	"bytes"
	"context"
	"errors"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredSDDManager61ManifestRecognition(t *testing.T) {
	v2 := mustBuildModelPlanV2(t, schemaV2TestConfig(t))
	v3, err := buildModelPlanBundleV3(modelplan.ModelPlanConfigV3{SchemaVersion: 3, Provider: "acme", Provenance: modelplan.ModelPlanCLI, Assignments: completeModelAssignmentsV3()})
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []modelPlanBundle{v2, v3} {
		old, err := managerV60Bundle(current)
		if err != nil {
			t.Fatal(err)
		}
		prior, err := sharedBundleForContract(old, orchestration.PreviousManagerContract(), true)
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := parseInstalledModelPlanManifest(prior.manifest)
		if err != nil || !bytes.Equal(got.manifest, prior.manifest) {
			t.Fatalf("exact Manager61 rejected: %v", err)
		}
		root := t.TempDir()
		writeModelPlanBundleFixture(t, root, prior)
		result, err := managedIntegrationForTest(t).Install(context.Background(), integration.Options{ConfigDir: root})
		if err != nil || result.State != integration.StateInstalled {
			t.Fatalf("Manager61 upgrade: state=%s err=%v", result.State, err)
		}
		for name := range prior.agents {
			if strings.HasPrefix(name, "vgxness-sdd-") {
				if _, err := os.Lstat(filepath.Join(root, "agents", name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("retired profile retained: %s (%v)", name, err)
				}
			}
		}
		if _, _, err := parseInstalledModelPlanManifest(mutateManifestDigest(t, prior, managerAgentName)); !errors.Is(err, integration.ErrDrift) {
			t.Fatalf("modified Manager61 accepted: %v", err)
		}
	}
}

package codex

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
)

// A synthetic predecessor proves lifecycle behavior without storing an old prompt.
func syntheticReceiptPackage(t *testing.T, plan modelplan.Plan) Package {
	t.Helper()
	pkg, err := RenderPlan("v0.0.0", plan)
	require(t, err == nil)
	pkg.Artifacts = pkg.Artifacts[:len(pkg.Artifacts)-1]
	pkg.Artifacts[0].Bytes = []byte("synthetic prior manager\n")
	require(t, appendReceipt(&pkg) == nil)
	pkg.SHA256 = aggregateSHA256(pkg.Artifacts)
	return pkg
}
func TestReceiptUpgradePreservesPlanAndUserFiles(t *testing.T) {
	for _, plan := range []modelplan.Plan{modelplan.PlanLow, modelplan.PlanMedium, modelplan.PlanHigh, modelplan.PlanUltra} {
		t.Run(string(plan), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			old := syntheticReceiptPackage(t, plan)
			writePackage(t, root, old)
			sentinel := []byte("user-owned configuration\n")
			require(t, os.WriteFile(filepath.Join(root, "config.toml"), sentinel, 0600) == nil)
			result, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
			current, _ := RenderPlan("v0.0.0", plan)
			if err != nil || result.State != integration.StateInstalled || result.ArtifactSHA256 != current.SHA256 || result.ModelPlan != plan {
				t.Fatalf("upgrade: state=%s plan=%s err=%v", result.State, result.ModelPlan, err)
			}
			after, err := os.ReadFile(filepath.Join(root, "config.toml"))
			require(t, err == nil && bytes.Equal(after, sentinel))
		})
	}
}
func TestReceiptBootstrapAndDrift(t *testing.T) {
	for _, scenario := range []string{"current-without-receipt", "unknown-without-receipt", "modified", "malformed-receipt", "missing-old-file"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			pkg := syntheticReceiptPackage(t, modelplan.PlanMedium)
			if scenario == "current-without-receipt" {
				pkg, _ = Render("v0.0.0")
			}
			writePackage(t, root, pkg)
			switch scenario {
			case "current-without-receipt", "unknown-without-receipt":
				require(t, os.Remove(filepath.Join(root, receiptPath)) == nil)
			case "modified":
				require(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("user edit\n"), 0600) == nil)
			case "malformed-receipt":
				require(t, os.WriteFile(filepath.Join(root, receiptPath), []byte("{}"), 0600) == nil)
			case "missing-old-file":
				require(t, os.Remove(filepath.Join(root, "AGENTS.md")) == nil)
			}
			before := snapshotActivationTree(t, root)
			result, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
			if scenario == "current-without-receipt" {
				require(t, err == nil && result.State == integration.StateInstalled)
				return
			}
			require(t, errors.Is(err, integration.ErrDrift))
			after := snapshotActivationTree(t, root)
			if len(before) != len(after) {
				t.Fatal("drift mutated inventory")
			}
			for p, b := range before {
				if !reflect.DeepEqual(after[p], b) {
					t.Fatalf("drift mutated %s", p)
				}
			}
		})
	}
}
func TestReceiptRecoveryUsesExactOnDiskSidecars(t *testing.T) {
	for _, suffix := range []string{".vgxness-stage", ".vgxness-remove"} {
		t.Run(suffix, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			pkg := syntheticReceiptPackage(t, modelplan.PlanHigh)
			writePackage(t, root, pkg)
			require(t, os.WriteFile(filepath.Join(root, pendingName), pendingEvidence(pkg.SHA256), 0600) == nil)
			for _, a := range pkg.Artifacts {
				require(t, os.Rename(filepath.Join(root, a.Path), filepath.Join(root, a.Path+suffix)) == nil)
			}
			result, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
			if err != nil || result.State != integration.StateInstalled || result.ArtifactSHA256 != pkg.SHA256 {
				t.Fatalf("recovery=%s err=%v", result.State, err)
			}
			assertNoEvidence(t, root)
		})
	}
}

func TestReceiptReplacementFailureRetainsExactLocalBackup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	old := syntheticReceiptPackage(t, modelplan.PlanMedium)
	writePackage(t, root, old)
	service := NewIntegration()
	injected := errors.New("replacement publication failed")
	service.checkpoint = func(point, name string) error {
		if point == "published" {
			return injected
		}
		return nil
	}
	result, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root})
	if !errors.Is(err, injected) || result.BackupPath == "" {
		t.Fatalf("backup=%q err=%v", result.BackupPath, err)
	}
	for _, a := range old.Artifacts {
		body, readErr := os.ReadFile(filepath.Join(result.BackupPath, a.Path))
		if readErr != nil || !bytes.Equal(body, a.Bytes) {
			t.Fatalf("lost predecessor %s: %v", a.Path, readErr)
		}
	}
}

func TestReceiptInstallRejectsDriftDuringCLIActivation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	fake := &fakeCodexCLI{root: root, fail: map[string]error{}, after: map[string]error{}}
	changed := []byte("concurrent edit during activation\n")
	fake.afterMutation = func(call string) {
		if strings.Contains(call, "plugin add") {
			require(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), changed, 0600) == nil)
		}
	}
	_, err := fakeActivationIntegration(fake).Install(context.Background(), integration.Options{ConfigDir: root})
	if !errors.Is(err, integration.ErrDrift) || !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("activation drift error=%v", err)
	}
	got, e := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	require(t, e == nil && bytes.Equal(got, changed))
	if _, e = os.Stat(filepath.Join(root, activationPendingName)); e != nil {
		t.Fatalf("missing recovery evidence: %v", e)
	}
}

package codex

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
)

func TestBootstrapReceiptlessReinstallMigratesEveryPlan(t *testing.T) {
	for _, plan := range []modelplan.Plan{modelplan.PlanLow, modelplan.PlanMedium, modelplan.PlanHigh, modelplan.PlanUltra} {
		t.Run(string(plan), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			bootstrap, err := renderBootstrapPlan("v0.0.0", plan)
			if err != nil {
				t.Fatal(err)
			}
			writePackage(t, root, bootstrap)
			sentinel := []byte("user-owned configuration\n")
			if err := os.WriteFile(filepath.Join(root, "config.toml"), sentinel, 0o600); err != nil {
				t.Fatal(err)
			}

			migrated, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
			current, renderErr := RenderPlan("v0.0.0", plan)
			if err != nil || renderErr != nil || migrated.State != integration.StateInstalled || !migrated.Changed || migrated.ModelPlan != plan || migrated.ArtifactSHA256 != current.SHA256 {
				t.Fatalf("Reinstall(receiptless %s) = %+v, %v, render=%v", plan, migrated, err, renderErr)
			}
			receipt, present, err := readBootstrapReceiptPackage(root)
			if err != nil || !present || receipt.SHA256 != current.SHA256 || !receipt.fromReceipt {
				t.Fatalf("new receipt package = %+v, present=%t, err=%v", receipt, present, err)
			}
			status, err := NewIntegration().Status(context.Background(), integration.Options{ConfigDir: root})
			if err != nil || status.State != integration.StateInstalled || status.Changed || status.ModelPlan != plan {
				t.Fatalf("Status(after bootstrap migration) = %+v, %v", status, err)
			}
			again, err := NewIntegration().Install(context.Background(), integration.Options{ConfigDir: root})
			if err != nil || again.State != integration.StateInstalled || again.Changed || again.ModelPlan != plan {
				t.Fatalf("Install(after bootstrap migration) = %+v, %v", again, err)
			}
			got, err := os.ReadFile(filepath.Join(root, "config.toml"))
			if err != nil || !bytes.Equal(got, sentinel) {
				t.Fatalf("user file after bootstrap migration = %q, %v", got, err)
			}
		})
	}
}

func TestBootstrapReceiptlessReinstallRejectsRealModifiedAndMixedFiles(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents func(*testing.T) []byte
	}{
		{name: "modified", contents: func(*testing.T) []byte { return []byte("user modification\n") }},
		{name: "mixed", contents: func(t *testing.T) []byte {
			t.Helper()
			other, err := renderBootstrapPlan("v0.0.0", modelplan.PlanLow)
			if err != nil {
				t.Fatal(err)
			}
			return artifact(t, other, "agents/general.toml").Bytes
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			bootstrap, err := renderBootstrapPlan("v0.0.0", modelplan.PlanHigh)
			if err != nil {
				t.Fatal(err)
			}
			writePackage(t, root, bootstrap)
			path := filepath.Join(root, "agents", "general.toml")
			before := test.contents(t)
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root}); !errors.Is(err, integration.ErrDrift) {
				t.Fatalf("Reinstall(%s artifact) error = %v, want drift", test.name, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("rejected %s artifact changed: %q, %v", test.name, after, err)
			}
			if _, present, err := readBootstrapReceiptPackage(root); err != nil || present {
				t.Fatalf("rejected %s artifact created receipt: present=%t, err=%v", test.name, present, err)
			}
		})
	}
}

func readBootstrapReceiptPackage(path string) (Package, bool, error) {
	root, err := OpenRoot(context.Background(), integration.Options{ConfigDir: path}, false)
	if err != nil {
		return Package{}, false, err
	}
	defer root.Close()
	return readReceiptPackage(root)
}

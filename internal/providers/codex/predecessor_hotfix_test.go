package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestCodexV14PackageIsExactPredecessorAndMixedBytesDrift(t *testing.T) {
	predecessor, err := renderActiveV14("v1.2.3", sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	manager := string(artifact(t, predecessor, "AGENTS.md").Bytes)
	if !strings.Contains(manager, "artifact: codex-agent/manager; version: 14; parity: opencode-v54") || strings.Contains(manager, currentCodexCandidateCapsuleContract) {
		t.Fatal("v14 predecessor changed")
	}
	mixed, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	mixed.Artifacts[0] = artifact(t, predecessor, "AGENTS.md")
	mixed.SHA256 = aggregate(mixed.Artifacts)
	if mixed.Validate() == nil {
		t.Fatal("Validate accepted mixed v14/v15 package")
	}
	root := filepath.Join(t.TempDir(), "codex")
	writePackage(t, root, predecessor)
	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: root})
	if err != nil || status.State != integration.StatePartial {
		t.Fatalf("v14 status=%+v err=%v", status, err)
	}
	path := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = service.Status(context.Background(), integration.Options{ConfigDir: root})
	if err != nil || status.State != integration.StateDrifted {
		t.Fatalf("one-byte v14 drift status=%+v err=%v", status, err)
	}
}

func TestCodexV14CAREPackageRecoversPendingMarkerThenUpgrades(t *testing.T) {
	profiles, err := profilesForPlan(sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	historical, err := renderPackage("v1.2.3", profiles, sdd.PlanMedium, false)
	if err != nil {
		t.Fatal(err)
	}
	historical.Artifacts[0].Bytes = []byte(activeV14ManagerInstructions())
	historical.SHA256 = aggregate(historical.Artifacts)

	predecessor, err := renderActiveV14("v1.2.3", sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	if predecessor.SHA256 != historical.SHA256 {
		t.Fatal("v14 predecessor does not retain the historical CARE package")
	}

	root := filepath.Join(t.TempDir(), "codex")
	writePackage(t, root, historical)
	if err := os.WriteFile(filepath.Join(root, pendingName), []byte("codex-pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewIntegration()
	options := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanMedium}
	if _, err := service.Reinstall(context.Background(), options); err != nil {
		t.Fatalf("recover pending v14 CARE package: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, pendingName)); !os.IsNotExist(err) {
		t.Fatalf("pending marker remained after recovery: %v", err)
	}
	status, err := service.Status(context.Background(), options)
	if err != nil || status.State != integration.StatePartial {
		t.Fatalf("recovered v14 status=%+v err=%v", status, err)
	}
	if _, err := service.Reinstall(context.Background(), options); err != nil {
		t.Fatalf("upgrade recovered v14 package: %v", err)
	}
	current, err := RenderPlan("v1.2.3", sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range current.Artifacts {
		got, err := os.ReadFile(filepath.Join(root, artifact.Path))
		if err != nil || !bytes.Equal(got, artifact.Bytes) {
			t.Fatalf("upgraded artifact %q differs: %v", artifact.Path, err)
		}
	}
	status, err = service.Status(context.Background(), options)
	if err != nil || status.State != integration.StateInstalled {
		t.Fatalf("upgraded v15 status=%+v err=%v", status, err)
	}
}

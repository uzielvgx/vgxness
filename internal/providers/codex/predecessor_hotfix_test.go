package codex

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
)

func TestCodexV16PackageIsExactPredecessorAndRejectsDrift(t *testing.T) {
	predecessor, err := renderActiveV16("v1.2.3", sdd.PlanMedium)
	if err != nil || predecessor.Validate() != nil {
		t.Fatalf("v16 predecessor = %v", err)
	}
	if !strings.Contains(string(artifact(t, predecessor, "AGENTS.md").Bytes), "artifact: codex-agent/manager; version: 16; parity: opencode-v56") {
		t.Fatal("v16 predecessor marker changed")
	}
	predecessor.Artifacts[0].Bytes = append(predecessor.Artifacts[0].Bytes, '\n')
	predecessor.SHA256 = aggregate(predecessor.Artifacts)
	if predecessor.Validate() == nil {
		t.Fatal("Validate accepted one-byte v16 drift")
	}
}

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
	current, err := RenderPlan("v0.0.0", sdd.PlanMedium)
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

func TestCodexPreTerminalClosureV18PackageUpgradesAndRejectsDrift(t *testing.T) {
	plan := sdd.PlanMedium
	predecessor, err := renderActiveV18PreTerminalClosure("v0.0.0", plan)
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "codex")
	writePackage(t, root, predecessor)
	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: root}
	service := fakeActivationIntegration(fake)
	options := integration.Options{ConfigDir: root, ModelPlan: plan}
	result, err := service.Reinstall(context.Background(), options)
	if err != nil || result.State != integration.StateInstalled || result.ModelPlan != plan || !containsCall(fake.calls, "[plugin add vgxness@vgxness --json]") {
		t.Fatalf("pre-terminal v18 reinstall=%+v err=%v calls=%v", result, err, fake.calls)
	}
	current, err := RenderPlan("v0.0.0", plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Artifacts) != len(predecessor.Artifacts)+3 {
		t.Fatalf("lifecycle artifacts = %d, want 3", len(current.Artifacts)-len(predecessor.Artifacts))
	}
	for _, item := range current.Artifacts {
		got, readErr := os.ReadFile(filepath.Join(root, item.Path))
		if readErr != nil || !bytes.Equal(got, item.Bytes) {
			t.Fatalf("published %q = %q, %v", item.Path, got, readErr)
		}
	}

	writePackage(t, root, predecessor)
	path := filepath.Join(root, "AGENTS.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before = append(before, "arbitrary drift\n"...)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = service.Reinstall(context.Background(), options)
	after, readErr := os.ReadFile(path)
	if !errors.Is(err, integration.ErrDrift) || readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("drift reinstall err=%v after=%q readErr=%v", err, after, readErr)
	}
}

func TestCodexPreTerminalClosureV18NoFlagReinstallInfersPlanAndUpgrades(t *testing.T) {
	plan := sdd.PlanHigh
	predecessor, err := renderActiveV18PreTerminalClosure("v0.0.0", plan)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "codex")
	writePackage(t, root, predecessor)
	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: root}
	result, err := fakeActivationIntegration(fake).Reinstall(context.Background(), integration.Options{ConfigDir: root})
	if err != nil || result.State != integration.StateInstalled || result.ModelPlan != plan {
		t.Fatalf("no-flag reinstall=%+v err=%v", result, err)
	}
	current, err := RenderPlan("v0.0.0", plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range current.Artifacts {
		got, readErr := os.ReadFile(filepath.Join(root, item.Path))
		if readErr != nil || !bytes.Equal(got, item.Bytes) {
			t.Fatalf("upgraded artifact %q = %q, %v", item.Path, got, readErr)
		}
	}
}

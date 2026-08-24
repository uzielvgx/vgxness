package codex

import (
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

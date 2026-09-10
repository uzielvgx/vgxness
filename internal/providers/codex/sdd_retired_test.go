package codex

import (
	"context"
	"errors"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManager20UpgradeRemovesRetiredProfiles(t *testing.T) {
	root := t.TempDir()
	old, err := renderActiveV20("v0.0.0", modelplan.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, old)
	result, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
	if err != nil || result.State != integration.StateInstalled {
		t.Fatalf("upgrade: %+v %v", result, err)
	}
	for _, a := range old.Artifacts {
		if strings.HasPrefix(a.Path, "agents/sdd-") {
			if _, err := os.Lstat(filepath.Join(root, a.Path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retired profile retained: %s (%v)", a.Path, err)
			}
		}
	}
}

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
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/testutil"
)

func managerFrontmatter(t *testing.T, prompt string) string {
	t.Helper()
	frontmatter, _, ok := strings.Cut(prompt, "\n---\n")
	if !ok {
		t.Fatal("manager prompt has no frontmatter terminator")
	}
	return frontmatter
}

func previousExploreAgent(t *testing.T, current []byte) []byte {
	t.Helper()
	predecessor := string(current)
	for _, replacement := range []struct{ current, previous string }{
		{"codegraph_codegraph_explore: allow", "codegraph_explore: allow"},
		{"artifact: opencode-agent/explore; version: 2", "artifact: opencode-agent/explore; version: 1"},
		{"Use codegraph_codegraph_explore first", "Use codegraph_explore first"},
	} {
		if strings.Count(predecessor, replacement.current) != 1 {
			t.Fatalf("current explore prompt missing unique %q", replacement.current)
		}
		predecessor = strings.Replace(predecessor, replacement.current, replacement.previous, 1)
	}
	return []byte(predecessor)
}

func writeCompleteV1ExploreBundle(t *testing.T, configDirectory string, bundle modelPlanBundle) {
	t.Helper()
	testutil.NoError(t, os.MkdirAll(filepath.Join(configDirectory, "agents"), 0o700))
	testutil.NoError(t, os.MkdirAll(filepath.Join(configDirectory, "vgxness"), 0o700))
	for name, content := range bundle.agents {
		testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "agents", name), content, 0o600))
	}
	testutil.NoError(t, os.WriteFile(filepath.Join(configDirectory, "vgxness", modelPlanManifestName), bundle.manifest, 0o600))
}

func TestModelPlanBundleForManifestRejectsUnknownManagerPredecessor(t *testing.T) {
	current, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	unknown := append(append([]byte(nil), current.manifest...), ' ')
	if _, err := modelPlanBundleForManifest(unknown, current.config); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("unknown manifest error=%v, want ErrDrift", err)
	}
}

func TestIntegrationPreservesForeignExploreOverrideAndReturnsConflict(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	agentsDirectory := filepath.Join(configDirectory, "agents")
	testutil.NoError(t, os.MkdirAll(agentsDirectory, 0o700))
	target := filepath.Join(agentsDirectory, "explore.md")
	foreign := []byte("user-owned explore prompt\n")
	testutil.NoError(t, os.WriteFile(target, foreign, 0o600))

	_, installErr := NewIntegration().Install(context.Background(), integration.Options{ConfigDir: configDirectory})
	after, readErr := os.ReadFile(target)
	testutil.Require(t, readErr == nil && errors.Is(installErr, integration.ErrConflict) && bytes.Equal(after, foreign), "foreign explore override changed: err=%v read=%v", installErr, readErr)
}

package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestRenderedMarketplaceRegistersWithCodex(t *testing.T) {
	bin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("Codex CLI unavailable")
	}
	root, err := os.MkdirTemp(".", ".codex-marketplace-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	s := NewIntegration()
	s.runner = runCodex
	s.codexBin = bin
	options := integration.Options{ConfigDir: mustAbs(t, root)}
	installed, err := s.Install(context.Background(), options)
	if err != nil || installed.State != integration.StateInstalled {
		t.Fatalf("Install=%+v err=%v", installed, err)
	}
	status, err := s.Status(context.Background(), options)
	if err != nil || status.State != integration.StateInstalled {
		t.Fatalf("Status=%+v err=%v", status, err)
	}
	held, err := s.openRoot(context.Background(), options, false)
	if err != nil {
		t.Fatal(err)
	}
	activation, activationErr := s.activation(context.Background(), held)
	_ = held.Close()
	if activationErr != nil || activation != activationActive {
		t.Fatalf("activation=%v err=%v", activation, activationErr)
	}
	removed, err := s.Uninstall(context.Background(), options)
	if err != nil || removed.State != integration.StateAbsent {
		t.Fatalf("Uninstall=%+v err=%v", removed, err)
	}
	final, err := s.Status(context.Background(), options)
	if err != nil || final.State != integration.StateAbsent {
		t.Fatalf("final Status=%+v err=%v", final, err)
	}
}

func TestRunCodexPassesOnlyManagedHomeToRealChild(t *testing.T) {
	if os.Getenv("VGXNESS_CODEX_ENV_HELPER") == "1" {
		for _, value := range os.Environ() {
			if strings.HasPrefix(value, "CODEX_HOME=") {
				_, _ = os.Stdout.WriteString(value + "\n")
			}
		}
		return
	}
	t.Setenv("CODEX_HOME", "/live/home")
	output, err := runCodex(context.Background(), os.Args[0], []string{"-test.run=TestRunCodexPassesOnlyManagedHomeToRealChild", "--"}, append(filteredEnv(os.Environ()), "CODEX_HOME=/managed/home", "VGXNESS_CODEX_ENV_HELPER=1"))
	if err != nil || !strings.HasPrefix(string(output), "CODEX_HOME=/managed/home\n") || strings.Contains(string(output), "/live/home") {
		t.Fatalf("child env=%q err=%v", output, err)
	}
}

func mustAbs(t *testing.T, value string) string {
	t.Helper()
	result, err := filepath.Abs(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

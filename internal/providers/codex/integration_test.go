package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestIntegrationInstallAndIdempotence(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	service := NewIntegration()
	before, err := service.Status(context.Background(), options)
	require(t, err == nil && before.State == integration.StateAbsent && before.ArtifactCount == 15)
	installed, err := service.Install(context.Background(), options)
	require(t, err == nil && installed.State == integration.StateInstalled && installed.Changed && installed.RestartRequired)
	again, err := service.Install(context.Background(), options)
	require(t, err == nil && !again.Changed && !again.RestartRequired && again.State == integration.StateInstalled)
}
func TestIntegrationReinstallsPartialAndPreservesUnrelatedFiles(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	service := NewIntegration()
	mustInstall(t, service, options)
	config := []byte("model = \"safe\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, "config.toml"), config, 0o600) == nil)
	require(t, os.Remove(filepath.Join(options.ConfigDir, "agents", "explore.toml")) == nil)
	partial, err := service.Status(context.Background(), options)
	require(t, err == nil && partial.State == integration.StatePartial)
	result, err := service.Reinstall(context.Background(), options)
	require(t, err == nil && result.State == integration.StateInstalled && result.Changed)
	body, err := os.ReadFile(filepath.Join(options.ConfigDir, "config.toml"))
	require(t, err == nil && string(body) == string(config))
}
func TestIntegrationBlocksDriftAndPendingEvidence(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	service := NewIntegration()
	mustInstall(t, service, options)
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, ".vgxness-pending"), []byte("codex-pending\n"), 0o600) == nil)
	pending, err := service.ReinstallPending(context.Background(), options)
	require(t, err == nil && pending)
	_, err = service.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	require(t, os.Remove(filepath.Join(options.ConfigDir, ".vgxness-pending")) == nil)
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, "agents", "explore.toml"), []byte("changed"), 0o600) == nil)
	status, err := service.Status(context.Background(), options)
	require(t, err == nil && status.State == integration.StateDrifted)
	_, err = service.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrDrift))
}

func TestStatusReportsRecoveryWhenClearPendingFails(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	s := NewIntegration()
	syncs := 0
	s.open = func(ctx context.Context, o integration.Options, create bool) (*Root, error) {
		r, err := OpenRoot(ctx, o, create)
		if err == nil {
			r.syncHook = func(name string) error {
				if name == "." {
					syncs++
					if syncs == 5 {
						return errors.New("clear pending")
					}
				}
				return nil
			}
		}
		return r, err
	}
	_, err := s.Install(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	assertPending(t, options.ConfigDir)

	status, err := s.Status(context.Background(), options)
	require(t, status.State == integration.StateInstalled && errors.Is(err, integration.ErrRecovery))
	assertPending(t, options.ConfigDir)
}
func TestManagedLayoutExcludesPluginArtifacts(t *testing.T) {
	layout, err := NewIntegration().ManagedLayout(context.Background(), integration.Options{ConfigDir: t.TempDir() + "/codex"})
	require(t, err == nil && len(layout.Artifacts) == 15)
	for _, item := range layout.Artifacts {
		require(t, item.RelativePath != "config.toml" && item.RelativePath != ".mcp.json" && filepath.Ext(item.RelativePath) != ".plugin")
	}
}
func TestUninstallLeavesAbsentAndUnrelated(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	s := NewIntegration()
	mustInstall(t, s, options)
	config := []byte("model = \"safe\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, "config.toml"), config, 0o600) == nil)
	got, err := s.Uninstall(context.Background(), options)
	require(t, err == nil && got.State == integration.StateAbsent && got.Changed && got.RestartRequired)
	b, err := os.ReadFile(filepath.Join(options.ConfigDir, "config.toml"))
	require(t, err == nil && string(b) == string(config))
	assertNoEvidence(t, options.ConfigDir)
}
func TestPendingSidecarsBlockOnlyKnownArtifacts(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	s := NewIntegration()
	mustInstall(t, s, options)
	for _, suffix := range []string{".vgxness-stage", ".vgxness-remove"} {
		name := filepath.Join(options.ConfigDir, "AGENTS.md"+suffix)
		require(t, os.WriteFile(name, []byte("evidence"), 0o600) == nil)
		pending, err := s.ReinstallPending(context.Background(), options)
		require(t, err == nil && pending)
		for _, call := range []func(context.Context, integration.Options) (integration.Result, error){s.Install, s.Reinstall, s.Uninstall} {
			_, err := call(context.Background(), options)
			require(t, errors.Is(err, integration.ErrRecovery))
		}
		require(t, os.Remove(name) == nil)
	}
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, ".vgxness-custom"), []byte("keep"), 0o600) == nil)
	pending, err := s.ReinstallPending(context.Background(), options)
	require(t, err == nil && !pending)
}
func TestInstallCancellationAndPostLinkFailureRetainRecoveryEvidence(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require(t, os.Mkdir(options.ConfigDir, 0o700) == nil)
		config := []byte("model = \"safe\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
		require(t, os.WriteFile(filepath.Join(options.ConfigDir, "config.toml"), config, 0o600) == nil)
		s := NewIntegration()
		s.checkpoint = func(point, _ string) error {
			if point == "published" {
				cancel()
			}
			return nil
		}
		_, err := s.Install(ctx, options)
		require(t, errors.Is(err, context.Canceled) && errors.Is(err, integration.ErrRecovery))
		_, err = os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md"))
		require(t, errors.Is(err, os.ErrNotExist))
		b, err := os.ReadFile(filepath.Join(options.ConfigDir, "config.toml"))
		require(t, err == nil && string(b) == string(config))
		assertPending(t, options.ConfigDir)
		recovered, err := s.Reinstall(context.Background(), options)
		require(t, err == nil && recovered.State == integration.StateInstalled)
		assertNoEvidence(t, options.ConfigDir)
	})
	t.Run("post-link", func(t *testing.T) {
		options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
		calls := 0
		s := NewIntegration()
		s.open = func(ctx context.Context, o integration.Options, create bool) (*Root, error) {
			r, err := OpenRoot(ctx, o, create)
			if err == nil {
				r.syncHook = func(name string) error {
					if name == "agents" {
						calls++
						if calls == 2 {
							return errors.New("post-link")
						}
					}
					return nil
				}
			}
			return r, err
		}
		_, err := s.Install(context.Background(), options)
		require(t, errors.Is(err, integration.ErrRecovery))
		assertPending(t, options.ConfigDir)
	})
}
func TestUninstallCleanupFailureReinstalls(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	s := NewIntegration()
	mustInstall(t, s, options)
	s.checkpoint = func(point, _ string) error {
		if point == "cleanup" {
			return errors.New("cleanup")
		}
		return nil
	}
	_, err := s.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	_, targetErr := os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md"))
	_, sidecarErr := os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md.vgxness-remove"))
	require(t, os.IsNotExist(targetErr) && sidecarErr == nil)
	recovered, err := s.Reinstall(context.Background(), options)
	require(t, err == nil && recovered.State == integration.StateInstalled && recovered.Changed && recovered.RestartRequired)
	assertNoEvidence(t, options.ConfigDir)
}
func TestUninstallRejectsReplacedIdentityAndRetainsBackup(t *testing.T) {
	options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
	s := NewIntegration()
	mustInstall(t, s, options)
	pkg, err := Render("v0.0.0")
	require(t, err == nil)
	body := artifact(t, pkg, "AGENTS.md").Bytes
	s.checkpoint = func(point, name string) error {
		if point == "before-backup" && name == "AGENTS.md" {
			replacement := filepath.Join(options.ConfigDir, "replacement")
			if err := os.WriteFile(replacement, body, 0o600); err != nil {
				return err
			}
			return os.Rename(replacement, filepath.Join(options.ConfigDir, name))
		}
		return nil
	}
	_, err = s.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	b, err := os.ReadFile(filepath.Join(options.ConfigDir, "AGENTS.md"))
	require(t, err == nil && string(b) == string(body))
	assertPending(t, options.ConfigDir)
}
func TestUninstallFailedRestoreAndSymlinksRemainRecoveryOrDrift(t *testing.T) {
	t.Run("restore", func(t *testing.T) {
		options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
		s := NewIntegration()
		mustInstall(t, s, options)
		s.checkpoint = func(point, name string) error {
			if point == "removed" && name == "AGENTS.md" {
				_ = os.WriteFile(filepath.Join(options.ConfigDir, name), []byte("racer"), 0o600)
				return errors.New("stop")
			}
			return nil
		}
		_, err := s.Uninstall(context.Background(), options)
		require(t, errors.Is(err, integration.ErrRecovery))
		_, err = os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md.vgxness-remove"))
		require(t, err == nil)
		assertPending(t, options.ConfigDir)
	})
	t.Run("symlink", func(t *testing.T) {
		options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
		s := NewIntegration()
		mustInstall(t, s, options)
		require(t, os.Remove(filepath.Join(options.ConfigDir, "agents", "explore.toml")) == nil)
		if err := os.Symlink("missing", filepath.Join(options.ConfigDir, "agents", "explore.toml")); err != nil {
			if errors.Is(err, os.ErrPermission) {
				t.Skip("symlink privilege unavailable")
			}
			t.Fatal(err)
		}
		got, err := s.Status(context.Background(), options)
		require(t, err == nil && got.State == integration.StateDrifted)
	})
	t.Run("agents-dir", func(t *testing.T) {
		options := integration.Options{ConfigDir: t.TempDir() + "/codex"}
		s := NewIntegration()
		mustInstall(t, s, options)
		pkg, _ := Render("v0.0.0")
		for _, item := range pkg.Artifacts {
			if filepath.Dir(item.Path) == "agents" {
				require(t, os.Remove(filepath.Join(options.ConfigDir, item.Path)) == nil)
			}
		}
		require(t, os.Remove(filepath.Join(options.ConfigDir, "agents")) == nil)
		if err := os.Symlink("missing", filepath.Join(options.ConfigDir, "agents")); err != nil {
			if errors.Is(err, os.ErrPermission) {
				t.Skip("symlink privilege unavailable")
			}
			t.Fatal(err)
		}
		got, err := s.Status(context.Background(), options)
		require(t, err == nil && got.State == integration.StateDrifted)
	})
}
func TestReinstallAbsentIsInvalid(t *testing.T) {
	_, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: t.TempDir() + "/codex"})
	require(t, errors.Is(err, integration.ErrInvalid))
}
func assertPending(t *testing.T, root string) {
	_, err := os.Stat(filepath.Join(root, ".vgxness-pending"))
	require(t, err == nil)
}
func assertNoEvidence(t *testing.T, root string) {
	assertAbsent := func(name string) {
		_, err := os.Stat(filepath.Join(root, name))
		require(t, errors.Is(err, os.ErrNotExist))
	}
	assertAbsent(".vgxness-pending")
	pkg, _ := Render("v0.0.0")
	for _, item := range pkg.Artifacts {
		assertAbsent(item.Path + ".vgxness-stage")
		assertAbsent(item.Path + ".vgxness-remove")
	}
}
func mustInstall(t *testing.T, s *Integration, options integration.Options) {
	_, err := s.Install(context.Background(), options)
	require(t, err == nil)
}
func require(t *testing.T, ok bool) {
	if !ok {
		t.Fatal("requirement failed")
	}
}

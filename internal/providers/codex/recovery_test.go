package codex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/opencodebackup"
)

func TestRecoveryManagedPathsAreExactAndOrdered(t *testing.T) {
	root := t.TempDir()
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: root}, BackupRoot: filepath.Join(t.TempDir(), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	paths := recovery.ManagedPaths()
	expected := []string{".agents/plugins/marketplace.json", "AGENTS.md", "agents/care-challenger.toml", "agents/care-reviewer.toml", "agents/care-specialist.toml", "agents/explore.toml", "agents/general.toml", "agents/sdd-apply.toml", "agents/sdd-design.toml", "agents/sdd-proposal.toml", "agents/sdd-research.toml", "agents/sdd-spec.toml", "agents/sdd-tasks.toml", "agents/verifier.toml", "plugins/vgxness/.codex-plugin/plugin.json", "plugins/vgxness/hooks.json"}
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("paths=%v", paths)
	}
	for _, path := range []string{"config.toml", "auth.json", "history.jsonl", "logs/log", "secrets", "unknown"} {
		for _, managed := range paths {
			if managed == path {
				t.Fatalf("unexpected managed path %q", path)
			}
		}
	}
}

func TestRecoveryManagedPathsDefensiveCopy(t *testing.T) {
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: t.TempDir()}, BackupRoot: filepath.Join(t.TempDir(), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	paths := recovery.ManagedPaths()
	paths[0] = "changed"
	if recovery.ManagedPaths()[0] == "changed" {
		t.Fatal("managed paths are mutable")
	}
}

func TestRecoveryRejectsFullMode(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backups")
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: t.TempDir()}, BackupRoot: backup})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovery.Create(context.Background(), opencodebackup.ModeFull); err == nil {
		t.Fatal("Codex recovery accepted full snapshot")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("full rejection created backup root: %v", err)
	}
}

func TestRecoveryUsesProviderSeparatedBackupAndRejectsManagedSymlink(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: root}, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if found, err := recovery.HasManagedFiles(context.Background()); err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("excluded"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(root, "unknown")); err != nil {
		t.Fatal(err)
	}
	if found, err := recovery.HasManagedFiles(context.Background()); err != nil || found {
		t.Fatalf("excluded found=%v err=%v", found, err)
	}
	managed := "AGENTS.md"
	if err := os.WriteFile(filepath.Join(root, managed), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := recovery.HasManagedFiles(context.Background()); err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	snapshot, err := recovery.Create(context.Background(), opencodebackup.ModeManaged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "vgxness", "backups", "codex", snapshot.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, managed)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(root, managed)); err != nil {
		t.Fatal(err)
	}
	if _, err := recovery.HasManagedFiles(context.Background()); err == nil {
		t.Fatal("Create accepted symlink")
	}
}

func TestRecoveryRejectsReplacedSourceRoot(t *testing.T) {
	root := t.TempDir()
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: root}, BackupRoot: filepath.Join(t.TempDir(), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-prior"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if found, err := recovery.HasManagedFiles(context.Background()); err == nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestRecoveryRejectsMissingSourceRoot(t *testing.T) {
	root := t.TempDir()
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: root}, BackupRoot: filepath.Join(t.TempDir(), "backups")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-prior"); err != nil {
		t.Fatal(err)
	}
	if found, err := recovery.HasManagedFiles(context.Background()); err == nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

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
	expected := []string{"AGENTS.md", "agents/explore.toml", "agents/general.toml", "agents/readability.toml", "agents/refuter.toml", "agents/reliability.toml", "agents/resilience.toml", "agents/risk.toml", "agents/sdd-apply.toml", "agents/sdd-design.toml", "agents/sdd-proposal.toml", "agents/sdd-research.toml", "agents/sdd-spec.toml", "agents/sdd-tasks.toml", "agents/verifier.toml"}
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

func TestRecoveryUsesProviderSeparatedBackupAndRejectsManagedSymlink(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	recovery, err := NewRecovery(context.Background(), RecoveryOptions{Integration: integration.Options{ConfigDir: root}, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	paths := recovery.ManagedPaths()
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
	if err := os.WriteFile(filepath.Join(root, paths[0]), []byte("managed"), 0o600); err != nil {
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
	if err := os.Remove(filepath.Join(root, paths[0])); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(root, paths[0])); err != nil {
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

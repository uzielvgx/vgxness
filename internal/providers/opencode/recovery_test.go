package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestManagedLayoutUsesInstalledArtifactAuthority(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}

	before, err := service.ManagedLayout(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(configDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ManagedLayout mutated config root: %v", err)
	}
	wantPaths := []string{
		"agents/explore.md",
		"agents/general.md",
		"agents/vgxness-manager.md",
		"agents/vgxness-review-readability.md",
		"agents/vgxness-review-refuter.md",
		"agents/vgxness-review-reliability.md",
		"agents/vgxness-review-resilience.md",
		"agents/vgxness-review-risk.md",
		"agents/vgxness-sdd-apply.md",
		"agents/vgxness-sdd-design.md",
		"agents/vgxness-sdd-proposal.md",
		"agents/vgxness-sdd-research.md",
		"agents/vgxness-sdd-spec.md",
		"agents/vgxness-sdd-tasks.md",
		"agents/vgxness-verifier.md",
		"plugins/vgxness.ts",
		"skills/vgxness-autonomous-stacked-pr/SKILL.md",
		"vgxness/model-plan.json",
	}
	if before.Root != configDirectory || len(before.Artifacts) != 18 || len(before.AggregateSHA256) != 64 {
		t.Fatalf("unexpected layout: %+v", before)
	}
	paths := managedPaths(before)
	if !reflect.DeepEqual(paths, wantPaths) || !sort.StringsAreSorted(paths) {
		t.Fatalf("managed paths = %v, want %v", paths, wantPaths)
	}

	installed, err := service.Install(context.Background(), options)
	if err != nil || installed.ArtifactCount != 18 {
		t.Fatalf("Install() = %+v, %v", installed, err)
	}
	for _, artifact := range before.Artifacts {
		data, err := os.ReadFile(filepath.Join(configDirectory, filepath.FromSlash(artifact.RelativePath)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if got := hex.EncodeToString(digest[:]); got != artifact.SHA256 {
			t.Errorf("%s hash = %s, want %s", artifact.RelativePath, got, artifact.SHA256)
		}
	}
	after, err := service.ManagedLayout(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("layout changed after install: before=%+v after=%+v", before, after)
	}
	before.Artifacts[0].RelativePath = "mutated"
	cloned, err := service.ManagedLayout(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Artifacts[0].RelativePath == "mutated" {
		t.Fatal("ManagedLayout returned aliased artifacts")
	}
}

func TestReinstallChangesOnlyManagedArtifacts(t *testing.T) {
	skipShortIntegration(t)
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	unrelated := []string{"opencode.json", "credentials.json", "plugins/user.ts", "agents/user.md"}
	before := make(map[string]os.FileInfo, len(unrelated))
	for _, relative := range unrelated {
		path := filepath.Join(configDirectory, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user-owned:"+relative), 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		before[relative] = info
	}

	result, err := service.Reinstall(context.Background(), options)
	if err != nil || result.State != integration.StateInstalled || !result.Changed {
		t.Fatalf("Reinstall() = %+v, %v", result, err)
	}
	for _, relative := range unrelated {
		path := filepath.Join(configDirectory, filepath.FromSlash(relative))
		data, readErr := os.ReadFile(path)
		info, statErr := os.Stat(path)
		if readErr != nil || statErr != nil || string(data) != "user-owned:"+relative || !os.SameFile(before[relative], info) {
			t.Errorf("unrelated file changed: %s data=%q read=%v stat=%v", relative, data, readErr, statErr)
		}
	}
	if _, err := os.Lstat(filepath.Join(configDirectory, ".vgxness-backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Reinstall created legacy backup directory: %v", err)
	}
}

func TestReinstallRollbackRestoresOldManagedSet(t *testing.T) {
	skipShortIntegration(t)
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	layout, err := service.ManagedLayout(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	oldInfo := make(map[string]os.FileInfo, len(layout.Artifacts))
	for _, artifact := range layout.Artifacts {
		info, err := os.Stat(filepath.Join(layout.Root, filepath.FromSlash(artifact.RelativePath)))
		if err != nil {
			t.Fatal(err)
		}
		oldInfo[artifact.RelativePath] = info
	}
	injected := errors.New("injected reinstall failure")
	service.reinstallCheckpoint = func(stage, _ string) error {
		if stage == "published" {
			return injected
		}
		return nil
	}
	if _, err := service.Reinstall(context.Background(), options); !errors.Is(err, injected) {
		t.Fatalf("Reinstall() error = %v, want injected failure", err)
	}
	status, err := service.Status(context.Background(), options)
	if err != nil || status.State != integration.StateInstalled {
		t.Fatalf("rollback status = %+v, %v", status, err)
	}
	for _, artifact := range layout.Artifacts {
		info, err := os.Stat(filepath.Join(layout.Root, filepath.FromSlash(artifact.RelativePath)))
		if err != nil || !os.SameFile(oldInfo[artifact.RelativePath], info) {
			t.Errorf("old managed artifact was not restored: %s err=%v", artifact.RelativePath, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(configDirectory, ".vgxness-backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback created legacy backup directory: %v", err)
	}
}

func TestReinstallRollbackNeverOverwritesConcurrentReplacement(t *testing.T) {
	skipShortIntegration(t)
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("concurrent user replacement")
	injected := errors.New("stop after concurrent replacement")
	replacedPath := ""
	service.reinstallCheckpoint = func(stage, target string) error {
		if stage != "published" || replacedPath != "" {
			return nil
		}
		temporary := filepath.Join(filepath.Dir(target), "concurrent.tmp")
		if err := os.WriteFile(temporary, concurrent, 0o600); err != nil {
			return err
		}
		if err := os.Rename(temporary, target); err != nil {
			return err
		}
		replacedPath = target
		return injected
	}
	_, err := service.Reinstall(context.Background(), options)
	if !errors.Is(err, injected) || !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("Reinstall() error = %v, want injected and ErrRecovery", err)
	}
	data, readErr := os.ReadFile(replacedPath)
	if readErr != nil || !bytes.Equal(data, concurrent) {
		t.Fatalf("concurrent replacement changed: %q, %v", data, readErr)
	}
}

func TestDurableRemovalNeverUnlinksConcurrentReplacement(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	expected := filepath.Join(directory, "expected")
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(expected, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(expected, target); err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("concurrent replacement")
	if err := os.WriteFile(replacement, concurrent, 0o600); err != nil {
		t.Fatal(err)
	}

	err := removeSameFileDurablyAtCheckpoint(target, expected, func() error {
		return os.Rename(replacement, target)
	})
	if err != nil {
		t.Fatalf("removeSameFileDurablyAtCheckpoint() error = %v", err)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil || !bytes.Equal(data, concurrent) {
		t.Fatalf("concurrent replacement was removed: %q, %v", data, readErr)
	}
}

func TestUpgradeNeverOverwritesConcurrentReplacement(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed")
	replacement := filepath.Join(directory, "replacement")
	prior := []byte("managed predecessor")
	current := []byte("managed replacement")
	concurrent := []byte("concurrent user replacement")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, concurrent, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := upgradeArtifactAtCheckpoint(context.Background(), artifact{path: target, content: current, prior: prior}, func() error {
		if err := os.Remove(target); err != nil {
			return err
		}
		return os.Rename(replacement, target)
	})
	if !errors.Is(err, integration.ErrConflict) {
		t.Fatalf("upgradeArtifactAtCheckpoint() error = %v, want ErrConflict", err)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil || !bytes.Equal(data, concurrent) {
		t.Fatalf("concurrent replacement changed: %q, %v", data, readErr)
	}
	anchors, globErr := filepath.Glob(filepath.Join(directory, ".vgxness-previous-*.tmp"))
	if globErr != nil || len(anchors) != 1 {
		t.Fatalf("predecessor recovery anchor = %v, %v", anchors, globErr)
	}
	backup, backupErr := os.ReadFile(anchors[0])
	if backupErr != nil || !bytes.Equal(backup, prior) {
		t.Fatalf("predecessor recovery anchor changed: %q, %v", backup, backupErr)
	}
}

func TestReinstallCancellationRestoresManagedSet(t *testing.T) {
	skipShortIntegration(t)
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service.reinstallCheckpoint = func(stage, _ string) error {
		if stage == "published" {
			cancel()
		}
		return nil
	}
	if _, err := service.Reinstall(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reinstall() error = %v, want cancellation", err)
	}
	status, err := service.Status(context.Background(), options)
	if err != nil || status.State != integration.StateInstalled {
		t.Fatalf("cancellation rollback status = %+v, %v", status, err)
	}
}

func TestReinstallMovesRecognizedTargetToAnchorBeforePublishing(t *testing.T) {
	skipShortIntegration(t)
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("stop after move")
	moved := false
	service.reinstallCheckpoint = func(stage, target string) error {
		if stage != "moved" || moved {
			return nil
		}
		moved = true
		if target != installed.Path {
			return nil
		}
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed target still existed after atomic move: %v", err)
		}
		staged := 0
		_ = filepath.WalkDir(configDirectory, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && strings.HasPrefix(entry.Name(), ".vgxness-") && strings.HasSuffix(entry.Name(), ".tmp") {
				staged++
			}
			return nil
		})
		if staged < 15 {
			t.Fatalf("desired artifacts were not all staged before first move: temporary files=%d", staged)
		}
		anchors, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".vgxness-reinstall-old-*.tmp"))
		if err != nil || len(anchors) == 0 {
			t.Fatalf("moved predecessor anchor missing: %v %v", anchors, err)
		}
		return injected
	}
	if _, err := service.Reinstall(context.Background(), options); !errors.Is(err, injected) {
		t.Fatalf("Reinstall() error=%v", err)
	}
	if !moved {
		t.Fatal("reinstall did not expose moved checkpoint")
	}
	restored, err := os.Stat(installed.Path)
	if err != nil || !os.SameFile(oldInfo, restored) {
		t.Fatalf("moved predecessor was not restored: %v", err)
	}
}

func TestReinstallMovedRollbackNeverOverwritesConcurrentTarget(t *testing.T) {
	skipShortIntegration(t)
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("concurrent target after move")
	injected := errors.New("stop after concurrent move replacement")
	service.reinstallCheckpoint = func(stage, target string) error {
		if stage != "moved" || target != installed.Path {
			return nil
		}
		if err := os.WriteFile(target, concurrent, 0o600); err != nil {
			return err
		}
		return injected
	}
	_, err = service.Reinstall(context.Background(), options)
	if !errors.Is(err, injected) || !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("Reinstall() error=%v, want injected and ErrRecovery", err)
	}
	data, readErr := os.ReadFile(installed.Path)
	if readErr != nil || !bytes.Equal(data, concurrent) {
		t.Fatalf("concurrent target was overwritten: %q %v", data, readErr)
	}
	anchors, globErr := filepath.Glob(filepath.Join(filepath.Dir(installed.Path), ".vgxness-reinstall-old-*.tmp"))
	if globErr != nil || len(anchors) != 1 {
		t.Fatalf("reinstall predecessor recovery anchor = %v, %v", anchors, globErr)
	}
	backup, backupErr := os.ReadFile(anchors[0])
	if backupErr != nil || !bytes.Equal(backup, prior) {
		t.Fatalf("reinstall predecessor recovery anchor changed: %q, %v", backup, backupErr)
	}
}

func TestReinstallRevalidatesPredecessorAnchorBeforeCleanup(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("changed predecessor anchor")
	mutated := false
	service.reinstallCheckpoint = func(stage, target string) error {
		if stage != "verified" || mutated || target != installed.Path {
			return nil
		}
		anchors, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".vgxness-reinstall-old-*.tmp"))
		if err != nil || len(anchors) == 0 {
			return fmt.Errorf("find predecessor anchor: %v %v", anchors, err)
		}
		for _, anchor := range anchors {
			info, statErr := os.Stat(anchor)
			if statErr == nil && os.SameFile(oldInfo, info) {
				mutated = true
				return os.WriteFile(anchor, changed, 0o600)
			}
		}
		return fmt.Errorf("manager predecessor anchor not found")
	}
	_, err = service.Reinstall(context.Background(), options)
	if err == nil || !mutated {
		t.Fatalf("changed predecessor was accepted: mutated=%t err=%v", mutated, err)
	}
	data, readErr := os.ReadFile(installed.Path)
	if readErr != nil || !bytes.Equal(data, changed) {
		t.Fatalf("changed anchor was not restored safely: %q %v", data, readErr)
	}
	if _, err := os.Lstat(filepath.Join(configDirectory, ".vgxness-backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reinstall created legacy backup directory: %v", err)
	}
}

func managedPaths(layout integration.ManagedLayout) []string {
	paths := make([]string, len(layout.Artifacts))
	for index, artifact := range layout.Artifacts {
		paths[index] = artifact.RelativePath
	}
	return paths
}

func TestManagedLayoutAggregateIsDeterministic(t *testing.T) {
	service := NewIntegration()
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "opencode")}
	first, err := service.ManagedLayout(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ManagedLayout(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.AggregateSHA256 != second.AggregateSHA256 || strings.Trim(first.AggregateSHA256, "0123456789abcdef") != "" {
		t.Fatalf("aggregate is not deterministic lowercase SHA-256: %q / %q", first.AggregateSHA256, second.AggregateSHA256)
	}
}

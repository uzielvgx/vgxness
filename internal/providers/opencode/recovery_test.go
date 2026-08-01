package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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
		"vgxness/default-agent.json",
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
	if err != nil || installed.ArtifactCount != 19 {
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
	unrelated := []string{"opencode.jsonc", "credentials.json", "plugins/user.ts", "agents/user.md"}
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
	anchors, globErr := filepath.Glob(filepath.Join(retainedAnchorRoot(directory), ".vgxness-previous-*.tmp"))
	if globErr != nil || len(anchors) != 1 {
		t.Fatalf("predecessor recovery anchor = %v, %v", anchors, globErr)
	}
	backup, backupErr := os.ReadFile(anchors[0])
	if backupErr != nil || !bytes.Equal(backup, prior) {
		t.Fatalf("predecessor recovery anchor changed: %q, %v", backup, backupErr)
	}
}

func TestUpgradeRejectsInPlaceConcurrentPredecessorRewrite(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed")
	prior := []byte("managed predecessor")
	current := []byte("managed replacement")
	concurrent := []byte("concurrent same-inode rewrite")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := upgradeArtifactAtCheckpoint(context.Background(), artifact{path: target, content: current, prior: prior}, func() error {
		file, openErr := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, 0)
		if openErr != nil {
			return openErr
		}
		if _, writeErr := file.Write(concurrent); writeErr != nil {
			_ = file.Close()
			return writeErr
		}
		return file.Close()
	})
	data, readErr := os.ReadFile(target)
	if !errors.Is(err, integration.ErrConflict) || readErr != nil || !bytes.Equal(data, concurrent) {
		t.Fatalf("upgrade error=%v data=%q read=%v", err, data, readErr)
	}
}

func TestUpgradeRejectsHeldDescriptorRewriteAfterQuarantine(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed")
	prior := []byte("managed predecessor")
	current := []byte("managed replacement")
	concurrent := []byte("concurrent quarantined rewrite")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	_, err = upgradeArtifactAtStagedCheckpoint(context.Background(), artifact{path: target, content: current, prior: prior}, nil, func() error {
		if truncateErr := writer.Truncate(0); truncateErr != nil {
			return truncateErr
		}
		if _, writeErr := writer.WriteAt(concurrent, 0); writeErr != nil {
			return writeErr
		}
		return writer.Sync()
	})
	data, readErr := os.ReadFile(target)
	if !errors.Is(err, integration.ErrConflict) || readErr != nil || !bytes.Equal(data, concurrent) || bytes.Equal(data, current) {
		t.Fatalf("upgrade error=%v data=%q read=%v", err, data, readErr)
	}
}

func TestUpgradeRetainsHeldDescriptorWritesAfterPublication(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed")
	prior := []byte("managed predecessor")
	current := []byte("managed replacement")
	concurrent := []byte("post-publication predecessor write")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	installed, err := upgradeArtifactWithCheckpoints(context.Background(), artifact{path: target, content: current, prior: prior, retainedRoot: directory}, nil, nil, func() error {
		if truncateErr := writer.Truncate(0); truncateErr != nil {
			return truncateErr
		}
		if _, writeErr := writer.WriteAt(concurrent, 0); writeErr != nil {
			return writeErr
		}
		return writer.Sync()
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanupInstalledArtifact(installed)
	managed, managedErr := os.ReadFile(target)
	anchor, anchorErr := os.ReadFile(installed.backup)
	if managedErr != nil || anchorErr != nil || !bytes.Equal(managed, current) || !bytes.Equal(anchor, concurrent) {
		t.Fatalf("managed=%q managedErr=%v anchor=%q anchorErr=%v", managed, managedErr, anchor, anchorErr)
	}
}

func TestUpgradeRollbackRestoresRetainedConcurrentPredecessor(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed")
	prior := []byte("managed predecessor")
	current := []byte("managed replacement")
	concurrent := []byte("post-publication predecessor write")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	installed, err := upgradeArtifactWithCheckpoints(context.Background(), artifact{path: target, content: current, prior: prior, retainedRoot: directory}, nil, nil, func() error {
		if truncateErr := writer.Truncate(0); truncateErr != nil {
			return truncateErr
		}
		if _, writeErr := writer.WriteAt(concurrent, 0); writeErr != nil {
			return writeErr
		}
		return writer.Sync()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackInstalledArtifact(installed); err != nil {
		t.Fatal(err)
	}
	restored, restoredErr := os.ReadFile(target)
	anchor, anchorErr := os.ReadFile(installed.backup)
	if restoredErr != nil || anchorErr != nil || !bytes.Equal(restored, concurrent) || !bytes.Equal(anchor, concurrent) || !sameFile(target, installed.backup) {
		t.Fatalf("restored=%q restoredErr=%v anchor=%q anchorErr=%v", restored, restoredErr, anchor, anchorErr)
	}
}

func TestUpgradeRetainsPublishedMarkerAndAnchorBeforeQuarantineFailure(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed")
	prior := []byte("managed predecessor")
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := upgradeArtifactAtCheckpoint(context.Background(), artifact{path: target, content: []byte("managed replacement"), prior: prior, retainedRoot: directory}, func() error {
		return errors.New("stop before quarantine")
	})
	retained, retainedErr := retainedPredecessors(directory)
	data, readErr := os.ReadFile(target)
	if err == nil || retainedErr != nil || len(retained) != 1 || readErr != nil || !bytes.Equal(data, prior) {
		t.Fatalf("upgrade=%v retained=%+v retainedErr=%v target=%q readErr=%v", err, retained, retainedErr, data, readErr)
	}
}

func TestRetainedPredecessorEvidenceFailsClosedWithoutDeletion(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "agents", "managed")
	if err := prepareRetainedPredecessorDirectories(root); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(retainedAnchorRoot(root), ".vgxness-previous-retained.tmp")
	prior := []byte("managed predecessor")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, anchor); err != nil {
		t.Fatal(err)
	}
	marker, err := persistRetainedPredecessor(root, target, anchor, prior)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(anchor); err != nil {
		t.Fatal(err)
	}
	_, retainedErr := retainedPredecessors(root)
	if _, markerErr := os.Lstat(marker); retainedErr == nil || markerErr != nil {
		t.Fatalf("retainedErr=%v markerErr=%v", retainedErr, markerErr)
	}
}

func TestRetainedPredecessorSymlinkMarkerFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	directory := retainedPredecessorRoot(root)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(directory, strings.Repeat("a", 32)+".json")
	if err := os.Symlink(foreign, marker); err != nil {
		t.Fatal(err)
	}
	_, retainedErr := retainedPredecessors(root)
	data, readErr := os.ReadFile(foreign)
	if retainedErr == nil || readErr != nil || !bytes.Equal(data, []byte("foreign")) {
		t.Fatalf("retainedErr=%v data=%q readErr=%v", retainedErr, data, readErr)
	}
}

func TestIntegrationReportsInvalidRetainedPredecessorEvidenceWithoutDeletion(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	directory := retainedPredecessorRoot(configDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(directory, strings.Repeat("a", 32)+".json")
	malformed := []byte("{bad}\n")
	if err := os.WriteFile(marker, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	status, statusErr := NewIntegration().Status(context.Background(), options)
	_, installErr := service.Install(context.Background(), options)
	after, readErr := os.ReadFile(marker)
	if statusErr != nil || status.State != integration.StateDrifted || status.RetainedPredecessorCount != 1 || status.RetainedPredecessorPath != directory || !errors.Is(installErr, integration.ErrConflict) || readErr != nil || !bytes.Equal(after, malformed) {
		t.Fatalf("status=%+v statusErr=%v installErr=%v after=%q readErr=%v", status, statusErr, installErr, after, readErr)
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
	predecessor, err := os.ReadFile(installed.Path)
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
			info, statErr := os.Lstat(anchor)
			contents, readErr := os.ReadFile(anchor)
			if statErr == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && readErr == nil && bytes.Equal(contents, predecessor) {
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

func TestReinstallMarkerIsRemovedAfterSuccessAndHandledRollback(t *testing.T) {
	for _, test := range []struct {
		name string
		fail bool
	}{
		{name: "success"},
		{name: "handled rollback", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDirectory := filepath.Join(t.TempDir(), "opencode")
			service := NewIntegration()
			options := integration.Options{ConfigDir: configDirectory}
			if _, err := service.Install(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			markerObserved := false
			injected := errors.New("handled reinstall failure")
			service.reinstallCheckpoint = func(stage, _ string) error {
				if stage != reinstallCheckpointPublished || markerObserved {
					return nil
				}
				markerObserved = true
				info, err := os.Lstat(filepath.Join(configDirectory, reinstallPendingName))
				if err != nil || !privatePendingFile(info) {
					t.Fatalf("pending marker was not durable before publication: %v", err)
				}
				if test.fail {
					return injected
				}
				return nil
			}
			_, err := service.Reinstall(context.Background(), options)
			if test.fail != errors.Is(err, injected) {
				t.Fatalf("Reinstall() error = %v", err)
			}
			if !markerObserved {
				t.Fatal("reinstall never exposed the pending marker")
			}
			if _, err := os.Lstat(filepath.Join(configDirectory, reinstallPendingName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("completed transaction retained marker: %v", err)
			}
		})
	}
}

func TestReinstallCancellationBeforePendingCheckIsNotRecoveryFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewIntegration()
	_, err := service.Reinstall(ctx, integration.Options{ConfigDir: filepath.Join(t.TempDir(), "opencode")})
	if !errors.Is(err, context.Canceled) || errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("Reinstall() error = %v, want only context.Canceled", err)
	}
}

func TestReinstallNeverOverwritesConcurrentDefaultAgentReplacement(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	defaultAgentPath := filepath.Join(configDirectory, defaultAgentConfigName)
	replacement := []byte("{\"default_agent\":\"user-agent\",\"concurrent\":true}\n")
	replaced := false
	service.reinstallCheckpoint = func(stage, target string) error {
		if stage != reinstallCheckpointMoved || replaced || target == defaultAgentPath {
			return nil
		}
		replaced = true
		return os.WriteFile(defaultAgentPath, replacement, 0o600)
	}
	_, err := service.Reinstall(context.Background(), options)
	if !replaced || !errors.Is(err, integration.ErrConflict) {
		t.Fatalf("Reinstall() error = %v, replaced=%t", err, replaced)
	}
	current, readErr := os.ReadFile(defaultAgentPath)
	if readErr != nil || !bytes.Equal(current, replacement) {
		t.Fatalf("concurrent default-agent replacement changed: %q, %v", current, readErr)
	}
}

func TestReinstallPreservesConcurrentPredecessorAnchor(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	competing := []byte("concurrent predecessor anchor")
	anchorPath := ""
	service.afterReinstallAnchorPath = func(path string) {
		if anchorPath != "" {
			return
		}
		anchorPath = path
		if err := os.WriteFile(path, competing, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := service.Reinstall(context.Background(), options)
	if anchorPath == "" || !errors.Is(err, integration.ErrConflict) {
		t.Fatalf("Reinstall() error = %v, anchorPath=%q", err, anchorPath)
	}
	current, readErr := os.ReadFile(anchorPath)
	if readErr != nil || !bytes.Equal(current, competing) {
		t.Fatalf("concurrent predecessor anchor changed: %q, %v", current, readErr)
	}
}

func TestReinstallInvalidConfigDirIsNotRecoveryFailure(t *testing.T) {
	service := NewIntegration()
	_, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: "relative"})
	if !errors.Is(err, integration.ErrInvalid) || errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("Reinstall() error = %v, want ErrInvalid without ErrRecovery", err)
	}
}

func TestReinstallRejectsDefaultAgentChangedDuringInspection(t *testing.T) {
	for _, test := range []struct {
		name        string
		replacement []byte
		remove      bool
	}{
		{name: "managed replacement", replacement: []byte("{\"default_agent\":\"vgxness-manager\",\"foreign\":true}\n")},
		{name: "user-agent replacement", replacement: []byte("{\"default_agent\":\"user-agent\",\"foreign\":true}\n")},
		{name: "deletion", remove: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDirectory := filepath.Join(t.TempDir(), "opencode")
			service := NewIntegration()
			options := integration.Options{ConfigDir: configDirectory}
			if _, err := service.Install(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			defaultAgentPath := filepath.Join(configDirectory, defaultAgentConfigName)
			replaced := false
			service.afterDefaultAgentSnapshot = func() {
				if replaced {
					return
				}
				replaced = true
				if test.remove {
					if err := os.Remove(defaultAgentPath); err != nil {
						t.Fatal(err)
					}
					return
				}
				if err := os.WriteFile(defaultAgentPath, test.replacement, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := service.Reinstall(context.Background(), options)
			if !replaced || !errors.Is(err, integration.ErrDrift) {
				t.Fatalf("Reinstall() error = %v, replaced=%t", err, replaced)
			}
			current, readErr := os.ReadFile(defaultAgentPath)
			if test.remove {
				if !errors.Is(readErr, os.ErrNotExist) {
					t.Fatalf("concurrent default-agent deletion was not preserved: %q, %v", current, readErr)
				}
			} else if readErr != nil || !bytes.Equal(current, test.replacement) {
				t.Fatalf("concurrent default-agent replacement changed: %q, %v", current, readErr)
			}
		})
	}
}

func TestReinstallCrashLeavesPendingEvidenceAndBlocksMutation(t *testing.T) {
	if stage := os.Getenv("VGXNESS_TEST_REINSTALL_CRASH_STAGE"); stage != "" {
		service := NewIntegration()
		service.reinstallCheckpoint = func(current, _ string) error {
			if current == stage {
				os.Exit(73)
			}
			return nil
		}
		_, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: os.Getenv("VGXNESS_TEST_REINSTALL_CRASH_ROOT")})
		fmt.Fprintln(os.Stderr, "child reinstall returned without crash:", err)
		os.Exit(74)
	}

	for _, stage := range []string{reinstallCheckpointMoved, reinstallCheckpointPublished, reinstallCheckpointVerified} {
		t.Run(stage, func(t *testing.T) {
			configDirectory := filepath.Join(t.TempDir(), "opencode")
			service := NewIntegration()
			options := integration.Options{ConfigDir: configDirectory}
			if _, err := service.Install(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestReinstallCrashLeavesPendingEvidenceAndBlocksMutation$")
			command.Env = append(os.Environ(),
				"VGXNESS_TEST_REINSTALL_CRASH_STAGE="+stage,
				"VGXNESS_TEST_REINSTALL_CRASH_ROOT="+configDirectory,
			)
			output, err := command.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 73 {
				t.Fatalf("crash child error = %v, output=%s", err, output)
			}
			markerPath := filepath.Join(configDirectory, reinstallPendingName)
			info, err := os.Lstat(markerPath)
			if err != nil || !privatePendingFile(info) {
				t.Fatalf("crash did not retain private marker: %v", err)
			}
			pending, err := service.ReinstallPending(context.Background(), options)
			if err != nil || !pending {
				t.Fatalf("ReinstallPending() = %t, %v", pending, err)
			}
			before, err := os.ReadFile(markerPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Reinstall(context.Background(), options); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("second Reinstall() error = %v, want ErrRecovery", err)
			}
			after, err := os.ReadFile(markerPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("blocked reinstall changed evidence: %v", err)
			}
		})
	}
}

func TestReinstallPendingRejectsMalformedEvidenceWithoutMutation(t *testing.T) {
	for name, body := range map[string][]byte{
		"truncated":     []byte(`{"version":1`),
		"duplicate key": []byte(`{"version":1,"version":1}`),
		"unknown field": []byte(`{"unexpected":true}`),
	} {
		t.Run(name, func(t *testing.T) {
			configDirectory := filepath.Join(t.TempDir(), "opencode")
			if err := os.MkdirAll(configDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			markerPath := filepath.Join(configDirectory, reinstallPendingName)
			if err := os.WriteFile(markerPath, body, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(markerPath)
			if err != nil {
				t.Fatal(err)
			}
			service := NewIntegration()
			options := integration.Options{ConfigDir: configDirectory}
			if pending, err := service.ReinstallPending(context.Background(), options); err == nil || pending {
				t.Fatalf("ReinstallPending() = %t, %v", pending, err)
			}
			if _, err := service.Reinstall(context.Background(), options); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Reinstall() error = %v, want ErrRecovery", err)
			}
			after, statErr := os.Lstat(markerPath)
			data, readErr := os.ReadFile(markerPath)
			if statErr != nil || readErr != nil || !os.SameFile(before, after) || !bytes.Equal(data, body) {
				t.Fatalf("malformed evidence changed: stat=%v read=%v", statErr, readErr)
			}
		})
	}
}

func TestReinstallPendingRejectsUnsafeMarkerFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions and symlinks are not portable to Windows")
	}
	for _, test := range []struct {
		name   string
		unsafe func(string) error
	}{
		{name: "public permissions", unsafe: func(markerPath string) error { return os.Chmod(markerPath, 0o644) }},
		{name: "symlink", unsafe: func(markerPath string) error {
			target := markerPath + ".target"
			if err := os.Rename(markerPath, target); err != nil {
				return err
			}
			return os.Symlink(target, markerPath)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			configDirectory := t.TempDir()
			service := NewIntegration()
			options := integration.Options{ConfigDir: configDirectory}
			layout, err := service.ManagedLayout(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.writeReinstallPending(context.Background(), configDirectory, layout); err != nil {
				t.Fatal(err)
			}
			markerPath := filepath.Join(configDirectory, reinstallPendingName)
			if err := test.unsafe(markerPath); err != nil {
				t.Fatal(err)
			}
			if pending, err := service.ReinstallPending(context.Background(), options); err == nil || pending {
				t.Fatalf("ReinstallPending() = %t, %v", pending, err)
			}
			if _, err := service.Reinstall(context.Background(), options); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Reinstall() error = %v, want ErrRecovery", err)
			}
		})
	}
}

func TestClearReinstallPendingPreservesConcurrentReplacement(t *testing.T) {
	configDirectory := t.TempDir()
	markerPath := filepath.Join(configDirectory, reinstallPendingName)
	if err := os.WriteFile(markerPath, []byte("expected"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("replaced")
	temporary := filepath.Join(configDirectory, "replacement")
	if err := os.WriteFile(temporary, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, markerPath); err != nil {
		t.Fatal(err)
	}
	if err := clearReinstallPending(configDirectory, reinstallPendingEvidence{info: expected, digest: sha256.Sum256([]byte("expected"))}); !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("clearReinstallPending() error = %v, want ErrRecovery", err)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil || !bytes.Equal(data, replacement) {
		t.Fatalf("concurrent marker replacement changed: %q, %v", data, err)
	}
}

func TestClearReinstallAnchorPreservesConcurrentReplacement(t *testing.T) {
	directory := t.TempDir()
	anchorPath := filepath.Join(directory, "anchor")
	original := []byte("original predecessor")
	if err := os.WriteFile(anchorPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(anchorPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("concurrent anchor replacement")
	temporary := filepath.Join(directory, "replacement")
	if err := os.WriteFile(temporary, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, anchorPath); err != nil {
		t.Fatal(err)
	}
	err = clearReinstallAnchor(reinstallAnchor{target: filepath.Join(directory, "target"), path: anchorPath, bytes: original, info: info})
	if !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("clearReinstallAnchor() error = %v, want ErrRecovery", err)
	}
	current, readErr := os.ReadFile(anchorPath)
	if readErr != nil || !bytes.Equal(current, replacement) {
		t.Fatalf("concurrent anchor replacement changed: %q, %v", current, readErr)
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

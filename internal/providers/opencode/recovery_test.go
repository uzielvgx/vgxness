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
		"agents/vgxness-care-challenger.md",
		"agents/vgxness-care-reviewer.md",
		"agents/vgxness-care-specialist.md",
		"agents/vgxness-manager.md",
		"agents/vgxness-verifier.md",
		"plugins/vgxness-memory-lifecycle.ts",
		"vgxness/default-agent.json",
		"vgxness/installation-receipt.json",
		"vgxness/model-plan.json",
	}
	if before.Root != configDirectory || len(before.Artifacts) != 11 || len(before.AggregateSHA256) != 64 {
		t.Fatalf("unexpected layout: %+v", before)
	}
	paths := managedPaths(before)
	if !reflect.DeepEqual(paths, wantPaths) || !sort.StringsAreSorted(paths) {
		t.Fatalf("managed paths = %v, want %v", paths, wantPaths)
	}

	installed, err := service.Install(context.Background(), options)
	if err != nil || installed.ArtifactCount != 12 {
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

func TestInstallRootArtifactPreservesHeldPredecessorWriteAfterQuarantine(t *testing.T) {
	root, err := openRootTransaction(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	predecessor := []byte("managed predecessor")
	mutated := []byte("held predecessor rewrite")
	file, err := root.CreateExclusive("artifact", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAndSyncRootFile(file, predecessor); err != nil {
		t.Fatal(err)
	}
	writer, err := openDeleteSharingWriter(filepath.Join(root.path, "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	root.afterRemoveRename = func(name, _ string) {
		if name != "artifact" {
			return
		}
		if err := writer.Truncate(0); err != nil {
			t.Error(err)
			return
		}
		if _, err := writer.WriteAt(mutated, 0); err != nil {
			t.Error(err)
			return
		}
		if err := writer.Sync(); err != nil {
			t.Error(err)
		}
	}

	_, err = installRootArtifact(context.Background(), root, "artifact", artifact{content: []byte("managed replacement"), prior: predecessor, upgrade: true})
	current, readErr := root.ReadRegular("artifact")
	inventory, inventoryErr := retainedPredecessorInventory(root.path)
	if err == nil || readErr != nil || !bytes.Equal(current, mutated) || inventoryErr == nil || inventory.evidenceCount == 0 {
		t.Fatalf("install=%v current=%q read=%v inventory=%+v inventoryErr=%v", err, current, readErr, inventory, inventoryErr)
	}
}

func TestInstallRootArtifactRetainsHeldPredecessorWriteAfterPublication(t *testing.T) {
	root, err := openRootTransaction(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	predecessor := []byte("managed predecessor")
	managed := []byte("managed replacement")
	mutated := []byte("post-publication predecessor rewrite")
	file, err := root.CreateExclusive("artifact", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAndSyncRootFile(file, predecessor); err != nil {
		t.Fatal(err)
	}
	writer, err := openDeleteSharingWriter(filepath.Join(root.path, "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	root.afterPublishLink = func(name string) error {
		if name != "artifact" {
			return nil
		}
		if err := writer.Truncate(0); err != nil {
			return err
		}
		if _, err := writer.WriteAt(mutated, 0); err != nil {
			return err
		}
		return writer.Sync()
	}

	installed, err := installRootArtifact(context.Background(), root, "artifact", artifact{content: managed, prior: predecessor, upgrade: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.CleanupStaged(installed.staged); err != nil {
		t.Fatal(err)
	}
	current, currentErr := root.ReadRegular("artifact")
	anchor, anchorErr := root.ReadRegular(installed.backup.name)
	inventory, inventoryErr := retainedPredecessorInventory(root.path)
	if currentErr != nil || anchorErr != nil || !bytes.Equal(current, managed) || !bytes.Equal(anchor, mutated) || inventoryErr == nil || inventory.evidenceCount == 0 {
		t.Fatalf("current=%q currentErr=%v anchor=%q anchorErr=%v inventory=%+v inventoryErr=%v", current, currentErr, anchor, anchorErr, inventory, inventoryErr)
	}
}

func TestRollbackRootInstalledArtifactRetainsHeldPredecessorWriteForRecovery(t *testing.T) {
	root, err := openRootTransaction(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	predecessor := []byte("managed predecessor")
	managed := []byte("managed replacement")
	mutated := []byte("post-publication predecessor rewrite")
	file, err := root.CreateExclusive("artifact", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAndSyncRootFile(file, predecessor); err != nil {
		t.Fatal(err)
	}
	writer, err := openDeleteSharingWriter(filepath.Join(root.path, "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	root.afterPublishLink = func(name string) error {
		if name != "artifact" {
			return nil
		}
		if err := writer.Truncate(0); err != nil {
			return err
		}
		if _, err := writer.WriteAt(mutated, 0); err != nil {
			return err
		}
		return writer.Sync()
	}

	installed, err := installRootArtifact(context.Background(), root, "artifact", artifact{content: managed, prior: predecessor, upgrade: true})
	if err != nil {
		t.Fatal(err)
	}
	err = rollbackRootInstalledArtifact(root, installed)
	_, targetErr := root.Lstat("artifact")
	anchor, anchorErr := root.ReadRegular(installed.backup.name)
	inventory, inventoryErr := retainedPredecessorInventory(root.path)
	if err == nil || !errors.Is(targetErr, os.ErrNotExist) || anchorErr != nil || !bytes.Equal(anchor, mutated) || inventoryErr == nil || inventory.evidenceCount == 0 {
		t.Fatalf("rollback=%v target=%v anchor=%q anchorErr=%v inventory=%+v inventoryErr=%v", err, targetErr, anchor, anchorErr, inventory, inventoryErr)
	}
}

func TestRootTransactionCleanupStagedPreservesHeldTemporaryWrite(t *testing.T) {
	root, err := openRootTransaction(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	managed := []byte("managed staged temporary")
	mutated := []byte("held temporary rewrite")
	staged, err := root.StageArtifact("artifact", managed, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	stagingInfo, err := root.Lstat(staged.staging)
	if err != nil {
		t.Fatal(err)
	}
	temporaryInfo, err := root.Lstat(staged.temporary)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && (stagingInfo.Mode().Perm() != 0o700 || temporaryInfo.Mode().Perm() != 0o600) {
		t.Fatalf("staging modes=%o/%o", stagingInfo.Mode().Perm(), temporaryInfo.Mode().Perm())
	}
	writer, err := openDeleteSharingWriter(filepath.Join(root.path, staged.temporary))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	root.beforeRemoveRename = func(name string) {
		if name != staged.temporary {
			return
		}
		if err := writer.Truncate(0); err != nil {
			t.Error(err)
			return
		}
		if _, err := writer.WriteAt(mutated, 0); err != nil {
			t.Error(err)
			return
		}
		if err := writer.Sync(); err != nil {
			t.Error(err)
		}
	}

	err = root.CleanupStaged(staged)
	current, readErr := root.ReadRegular(staged.temporary)
	if !errors.Is(err, integration.ErrRecovery) || readErr != nil || !bytes.Equal(current, mutated) {
		t.Fatalf("cleanup=%v current=%q read=%v", err, current, readErr)
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

func TestIntegrationReportsValidRetainedPredecessorEvidenceWithoutDrift(t *testing.T) {
	service, options, _, _ := retainedEvidenceFixture(t)
	status, err := service.Status(context.Background(), options)
	if err != nil || status.State != integration.StateInstalled || status.RetainedPredecessorCount != 1 || status.RetainedPredecessorPath != retainedPredecessorRoot(options.ConfigDir) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
func TestIntegrationReportsInvalidRetainedAnchorsWithoutDeletion(t *testing.T) {
	for name, create := range map[string]func(string, string) error{
		"orphan":  func(path, _ string) error { return os.WriteFile(path, []byte("orphan"), 0o600) },
		"unknown": func(path, _ string) error { return os.WriteFile(path, []byte("unknown"), 0o600) },
		"symlink": func(path, target string) error { return os.Symlink(target, path) },
	} {
		t.Run(name, func(t *testing.T) {
			if name == "symlink" && runtime.GOOS == "windows" {
				t.Skip("symlink privileges vary on Windows")
			}
			root := filepath.Join(t.TempDir(), "opencode")
			service, options := NewIntegration(), integration.Options{ConfigDir: root}
			if _, err := service.Install(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			if err := prepareRetainedPredecessorDirectories(root); err != nil {
				t.Fatal(err)
			}
			entryName := ".unexpected"
			if name == "orphan" {
				entryName = ".vgxness-previous-orphan.tmp"
			}
			path := filepath.Join(retainedAnchorRoot(root), entryName)
			if err := create(path, filepath.Join(root, "opencode.json")); err != nil {
				t.Fatal(err)
			}
			status, statusErr := service.Status(context.Background(), options)
			before, _ := os.ReadFile(path)
			_, installErr := service.Install(context.Background(), options)
			after, readErr := os.ReadFile(path)
			if statusErr != nil || status.State != integration.StateDrifted || status.RetainedPredecessorCount != 1 || !errors.Is(installErr, integration.ErrConflict) || readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("status=%+v statusErr=%v installErr=%v", status, statusErr, installErr)
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("anchor entry was deleted: %v", err)
			}
		})
	}
}

func TestIntegrationAcceptsPairedRetainedMarkerPublicationAlias(t *testing.T) {
	service, options, _, marker := retainedEvidenceFixture(t)
	alias := filepath.Join(retainedPredecessorRoot(options.ConfigDir), ".vgxness-retained-crash.tmp")
	if err := os.Link(marker, alias); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), options)
	if err != nil || status.State != integration.StateInstalled || status.RetainedPredecessorCount != 1 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestIntegrationRejectsInvalidRetainedMarkerPublicationAliasesWithoutDeletion(t *testing.T) {
	for _, test := range []struct {
		name      string
		wantCount int
		mutate    func(string, string, []byte) error
	}{
		{name: "unpaired", wantCount: 1, mutate: func(marker, alias string, _ []byte) error { return os.Rename(marker, alias) }},
		{name: "copy", wantCount: 2, mutate: func(_ string, alias string, body []byte) error { return os.WriteFile(alias, body, 0o600) }},
		{name: "malformed", wantCount: 2, mutate: func(_ string, alias string, _ []byte) error { return os.WriteFile(alias, []byte("{bad}\n"), 0o600) }},
		{name: "symlink", wantCount: 2, mutate: func(marker, alias string, _ []byte) error {
			if runtime.GOOS == "windows" {
				return nil
			}
			return os.Symlink(marker, alias)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "symlink" && runtime.GOOS == "windows" {
				t.Skip("symlink privileges vary on Windows")
			}
			service, options, _, marker := retainedEvidenceFixture(t)
			body, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(retainedPredecessorRoot(options.ConfigDir), ".vgxness-retained-invalid.tmp")
			if err := test.mutate(marker, alias, body); err != nil {
				t.Fatal(err)
			}
			if test.name == "unpaired" {
				if err := os.Remove(filepath.Join(retainedAnchorRoot(options.ConfigDir), ".vgxness-previous-retained.tmp")); err != nil {
					t.Fatal(err)
				}
			}
			status, err := service.Status(context.Background(), options)
			if err != nil || status.State != integration.StateDrifted || status.RetainedPredecessorCount != test.wantCount {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			if _, err := os.Lstat(alias); err != nil {
				t.Fatalf("invalid alias was deleted: %v", err)
			}
		})
	}
}

func retainedEvidenceFixture(t *testing.T) (*Integration, integration.Options, integration.Result, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "opencode")
	service, options := NewIntegration(), integration.Options{ConfigDir: root}
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRetainedPredecessorDirectories(root); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(retainedAnchorRoot(root), ".vgxness-previous-retained.tmp")
	predecessor, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(installed.Path, anchor); err != nil {
		t.Fatal(err)
	}
	marker, err := persistRetainedPredecessor(root, installed.Path, anchor, predecessor)
	if err != nil {
		t.Fatal(err)
	}
	return service, options, installed, marker
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
		if staged < 9 {
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
			assertPendingBlocksOrdinaryMutation(t, service, options)
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
			assertPendingBlocksOrdinaryMutation(t, service, options)
			after, statErr := os.Lstat(markerPath)
			data, readErr := os.ReadFile(markerPath)
			if statErr != nil || readErr != nil || !os.SameFile(before, after) || !bytes.Equal(data, body) {
				t.Fatalf("malformed evidence changed: stat=%v read=%v", statErr, readErr)
			}
		})
	}
}

func assertPendingBlocksOrdinaryMutation(t *testing.T, service *Integration, options integration.Options) {
	for index, mutation := range []func(context.Context, integration.Options) (integration.Result, error){service.Install, service.Uninstall} {
		if _, err := mutation(context.Background(), options); !errors.Is(err, integration.ErrRecovery) {
			t.Fatalf("mutation[%d] error = %v, want ErrRecovery", index, err)
		}
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
			root, err := openRootTransaction(configDirectory, false)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			layout, err := service.ManagedLayout(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.writeReinstallPendingAtRoot(context.Background(), root, layout); err != nil {
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
	root, err := openRootTransaction(configDirectory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	markerPath := filepath.Join(configDirectory, reinstallPendingName)
	if err := os.WriteFile(markerPath, []byte("expected"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := root.Lstat(reinstallPendingName)
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
	if err := clearReinstallPendingAtRoot(root, reinstallPendingEvidence{info: expected, digest: sha256.Sum256([]byte("expected"))}); !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("clearReinstallPendingAtRoot() error = %v, want ErrRecovery", err)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil || !bytes.Equal(data, replacement) {
		t.Fatalf("concurrent marker replacement changed: %q, %v", data, err)
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

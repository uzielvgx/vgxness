package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	if resolved, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		_ = os.Setenv("TMPDIR", resolved)
	}
	os.Exit(m.Run())
}

func TestValidRelative(t *testing.T) {
	tests := []struct {
		name     string
		relative string
		valid    bool
	}{
		{name: "nested slash path", relative: "agents/openai.yaml", valid: true},
		{name: "dot", relative: "."},
		{name: "traversal", relative: "agents/../openai.yaml"},
		{name: "absolute", relative: "/agents/openai.yaml"},
		{name: "backslash", relative: "agents\\openai.yaml"},
		{name: "colon", relative: "agents:openai.yaml"},
		{name: "empty", relative: ""},
		{name: "NUL", relative: "agents/\x00openai.yaml"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validRelative(test.relative); got != test.valid {
				t.Fatalf("validRelative(%q) = %t, want %t", test.relative, got, test.valid)
			}
		})
	}
}

func TestExplicitEmptyCatalogIsInvalidWhileDefaultLoadsBundle(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if _, err := (&Service{catalog: &catalog{}}).Preview(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("explicit empty catalog error=%v, want ErrInvalid", err)
	}
	preview, err := New().Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.FileCount == 0 {
		t.Fatalf("default preview=%+v err=%v", preview, err)
	}
}

func TestUninstallBeforeBackupFailureCleansEmptySession(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if _, err := New().Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	service := &Service{beforeBackup: func(string) error { return errors.New("injected backup failure") }}
	if _, err := service.Uninstall(context.Background(), Options{Dir: destination}); err == nil {
		t.Fatal("Uninstall() error=nil")
	}
	if _, err := os.Lstat(filepath.Join(destination, ".vgxness-backups", "uninstall-0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty session remains: %v", err)
	}
	if status, err := service.Status(context.Background(), Options{Dir: destination}); err != nil || status.State != StateInstalled {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	result, err := New().Uninstall(context.Background(), Options{Dir: destination})
	if err != nil || filepath.Base(result.BackupPath) != "uninstall-0" {
		t.Fatalf("retry result=%+v err=%v", result, err)
	}
}

func TestUninstallPruneFailureReturnsDurableBackupPath(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	installed, err := New().Install(context.Background(), Options{Dir: destination})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{beforePrune: func() error { return errors.New("injected prune failure") }}
	result, err := service.Uninstall(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrRecovery) || result.BackupPath == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for identity, want := range installed.Hashes {
		actual, readErr := os.ReadFile(filepath.Join(result.BackupPath, filepath.FromSlash(identity)))
		if readErr != nil || digest(actual) != want {
			t.Fatalf("backup %s digest=%s err=%v", identity, digest(actual), readErr)
		}
	}
}

func TestCatalogLifecycleAggregatesSkillsAndPreservesExtras(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := &Service{catalog: &catalog{definitions: []skillDefinition{
		{name: "alpha-skill", files: map[string][]byte{"guide.txt": []byte("alpha guide"), "SKILL.md": []byte("alpha skill")}},
		{name: "beta-skill", files: map[string][]byte{"notes.txt": []byte("beta notes"), "SKILL.md": []byte("beta skill")}},
	}}}

	preview, err := service.Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.State != StateAbsent || !preview.Changed || preview.FileCount != 4 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, ok := preview.Hashes["alpha-skill/SKILL.md"]; !ok {
		t.Fatalf("canonical hashes=%v", preview.Hashes)
	}
	installed, err := service.Install(context.Background(), Options{Dir: destination})
	if err != nil || installed.State != StateInstalled || !installed.Changed || installed.Path != destination {
		t.Fatalf("installed=%+v err=%v", installed, err)
	}
	status, err := service.Status(context.Background(), Options{Dir: destination})
	if err != nil || status.State != StateInstalled || status.Changed || status.FileCount != 4 || status.Path != destination {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	for _, identity := range []string{"alpha-skill/SKILL.md", "beta-skill/SKILL.md"} {
		if _, ok := status.Hashes[identity]; !ok {
			t.Fatalf("canonical hashes=%v", status.Hashes)
		}
	}
	extra := filepath.Join(destination, "alpha-skill", "local.txt")
	if err := os.WriteFile(extra, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := service.Uninstall(context.Background(), Options{Dir: destination})
	if err != nil || removed.State != StateAbsent || !removed.Changed || removed.BackupPath == "" {
		t.Fatalf("removed=%+v err=%v", removed, err)
	}
	for name, want := range installed.Hashes {
		actual, err := os.ReadFile(filepath.Join(removed.BackupPath, filepath.FromSlash(name)))
		if err != nil || digest(actual) != want {
			t.Fatalf("backup %s digest=%s err=%v", name, digest(actual), err)
		}
	}
	assertFileBytes(t, extra, []byte("keep"))
	if status, err := service.Status(context.Background(), Options{Dir: destination}); err != nil || status.State != StateAbsent {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestCatalogRejectsNonCanonicalManagedIdentities(t *testing.T) {
	for _, definition := range []skillDefinition{
		{name: "Bad", files: map[string][]byte{"SKILL.md": []byte("x")}},
		{name: "good-skill", files: map[string][]byte{"../SKILL.md": []byte("x")}},
	} {
		service := &Service{catalog: &catalog{definitions: []skillDefinition{definition}}}
		_, err := service.Preview(context.Background(), Options{Dir: filepath.Join(t.TempDir(), "skills")})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("definition=%+v err=%v", definition, err)
		}
	}
	service := &Service{catalog: &catalog{definitions: []skillDefinition{
		{name: "good-skill", files: map[string][]byte{"SKILL.md": []byte("x")}},
		{name: "good-skill", files: map[string][]byte{"SKILL.md": []byte("y")}},
	}}}
	if _, err := service.Preview(context.Background(), Options{Dir: filepath.Join(t.TempDir(), "skills")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate skill err=%v", err)
	}
}

func TestValidSkillName(t *testing.T) {
	for _, test := range []struct {
		name  string
		valid bool
	}{
		{name: "agent-skill-engineer", valid: true},
		{name: "Bad"}, {name: "."}, {name: "../skill"}, {name: "skill\\name"},
		{name: "skill:name"}, {name: "skill\x00name"}, {name: "/skill"}, {name: "-skill"},
	} {
		if got := validSkillName(test.name); got != test.valid {
			t.Fatalf("validSkillName(%q)=%t, want %t", test.name, got, test.valid)
		}
	}
}

func TestCatalogInstallRollsBackAcrossSkills(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := &Service{
		catalog: &catalog{definitions: []skillDefinition{
			{name: "alpha-skill", files: map[string][]byte{"SKILL.md": []byte("alpha")}},
			{name: "beta-skill", files: map[string][]byte{"SKILL.md": []byte("beta")}},
		}},
		afterPublish: func(identity string) error {
			if identity == "beta-skill/SKILL.md" {
				return errors.New("injected cross-skill failure")
			}
			return nil
		},
	}
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err == nil || errors.Is(err, ErrRecovery) {
		t.Fatalf("Install() err=%v", err)
	}
	for _, identity := range []string{"alpha-skill/SKILL.md", "beta-skill/SKILL.md"} {
		if _, err := os.Lstat(filepath.Join(destination, filepath.FromSlash(identity))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s publication remains: %v", identity, err)
		}
	}
}

func TestInstallCreatesAndVerifiesManagedPack(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := New()
	preview, err := service.Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.State != StateAbsent || !preview.Changed {
		t.Fatalf("initial preview=%+v err=%v", preview, err)
	}
	result, err := service.Install(context.Background(), Options{Dir: destination})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateInstalled || !result.Changed || result.FileCount == 0 {
		t.Fatalf("result=%+v", result)
	}
	status, err := service.Status(context.Background(), Options{Dir: destination})
	if err != nil || status.State != StateInstalled || status.Changed {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	preview, err = service.Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.Changed {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
}

func TestInstallRejectsSelectedRootReplacementAfterInspect(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "skills")
	external := filepath.Join(parent, "external")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	service := &Service{afterInspect: func() {
		if err := os.Rename(destination, filepath.Join(parent, "skills-original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, destination); err != nil {
			t.Fatal(err)
		}
	}}

	if _, err := service.Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Install() error = %v, want ErrConflict", err)
	}
	if _, err := os.Stat(filepath.Join(external, "agent-skill-engineer")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external publication err = %v, want not exist", err)
	}
}

func TestExactPredecessorIsUpdateNeededButNearMatchIsDrift(t *testing.T) {
	for relative, sourceDigest := range predecessorDigests {
		if !predecessorDigest(relative, sourceDigest) {
			t.Fatalf("known predecessor %s was not recognized", relative)
		}
		near := sourceDigest[:63] + "0"
		if sourceDigest[63] == '0' {
			near = sourceDigest[:63] + "1"
		}
		if predecessorDigest(relative, near) {
			t.Fatalf("near predecessor %s was recognized", relative)
		}
	}
	if predecessor("LICENSE.txt", []byte("not a predecessor")) {
		t.Fatal("near predecessor was recognized")
	}
}

func TestPredecessorIdentityDigests(t *testing.T) {
	want := map[string]string{
		"SKILL.md": "c327cbe5604210494c40d413b03fa5f4c26462785dc3c2926aa291500908d35d", "LICENSE.txt": "1efd6091b70b35e21e7eb2ac1db17892a22ea60d32074e3f613c973833ca6e24", "skill-manifest.json": "6a1d59a957de5d6cbd74500b2c24cae2f3e166886a15219077095a6f531c0bd3", "agents/openai.yaml": "8b438047a165e0d562bda9670bfb46db643bd3dff27c63d71a4005a2873bbbc6", "assets/SKILL.template.md": "4700c62c712bd1409c796a04564f1386d49ecc8c8bae98e24ca739c2269d1d6a", "assets/eval-cases.template.json": "a65222a4a57a0c64db5d1b40da070ced6796fe38e5449e983ebb68a0b6e18f05", "references/authoring-methodology.md": "05d63276f6fa728cbbba6bc8154d5c19094505b8778b8587eeb78f747a1eb0b0", "references/evaluation-methodology.md": "092a7e740cd4fd726cd4da16d3015f33873fac992208b5b166901783d0602904", "references/forward-testing.md": "7935b58f939924b751c5bbd0cada648175bf77ce91dc2dba63b8676c6e3bac12", "references/security-review.md": "a8ed9556520ce6678ed5ca5b3e9268aeb48a48e3849b69120c1bd3b07e73ee95", "scripts/generate_openai_yaml.py": "1ee4de86048f6731081e205b0a0211722dc6aef9198400c6d3fb8e133d550ef5", "scripts/init_skill.py": "1a43b96f60d8f542b945c4121f598ce99c115500e8b71753bbc9e041476694a8", "scripts/run_evals.py": "d7a23dc13113866a169120586670a09d14b3c6cff97343e84f05564c65e4b5c2", "scripts/skill_utils.py": "c6b8364928a67ec7c2b8d5d7fe1fcd07f8402840116c8473dad84b9a56500e6c", "scripts/validate_skill.py": "b6171b38c4c624a45f8c8a48e9a20ba7f52529f1b16f2b4b685b9e3182c8fe1d",
	}
	if !mapsEqual(predecessorDigests, want) {
		t.Fatalf("predecessor digest map=%v want=%v", predecessorDigests, want)
	}
}

func TestInstallPreservesExtras(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	entries, err := files()
	if err != nil {
		t.Fatal(err)
	}
	for relative, content := range entries {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, relative)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, relative), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	extra := filepath.Join(root, "local-note.txt")
	if err := os.WriteFile(extra, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New()
	preview, err := service.Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.UpdateNeeded || preview.Changed {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	installed, err := service.Install(context.Background(), Options{Dir: destination})
	if err != nil || installed.Changed || installed.UpdateNeeded {
		t.Fatalf("installed=%+v err=%v", installed, err)
	}
	if actual, err := os.ReadFile(extra); err != nil || !bytes.Equal(actual, []byte("keep")) {
		t.Fatalf("extra=%q err=%v", actual, err)
	}
}

func TestNearPredecessorAndManagedSymlinkAreRefused(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := New()
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(destination, "agent-skill-engineer")
	if err := os.WriteFile(filepath.Join(root, "LICENSE.txt"), []byte("near predecessor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrDrift) {
		t.Fatalf("near match err=%v", err)
	}
	if err := os.Remove(filepath.Join(root, "LICENSE.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "SKILL.md"), filepath.Join(root, "LICENSE.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Status(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("symlink err=%v", err)
	}
}

func TestManagedRootAndAncestorSymlinksAreRefused(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Preview(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("root symlink err=%v", err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "agents")); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Status(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("ancestor symlink err=%v", err)
	}
}

func TestInstallRejectsSelectedSkillsRootSymlinkBeforeWriting(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "skills")
	outside := t.TempDir()
	if err := os.Symlink(outside, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("install err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "agent-skill-engineer")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside target err=%v", err)
	}
}

func TestInstallRejectsIntermediateSkillsRootSymlinkBeforeWriting(t *testing.T) {
	parent := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(parent, "redirect")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(link, "skills")
	if _, err := New().Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "skills", "agent-skill-engineer")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside err=%v", err)
	}
}

func TestInstallCreatesMissingParentChainFromAnchoredRoot(t *testing.T) {
	ancestor := t.TempDir()
	destination := filepath.Join(ancestor, "missing", "portable", "skills")
	result, err := New().Install(context.Background(), Options{Dir: destination})
	if err != nil || result.State != StateInstalled {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, directory := range []string{
		filepath.Join(ancestor, "missing"),
		filepath.Join(ancestor, "missing", "portable"),
		destination,
	} {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("directory=%q info=%v err=%v", directory, info, err)
		}
	}
}

func TestInstallRejectsSelectedSkillsRootFileBeforeWriting(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if err := os.WriteFile(destination, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("install err=%v", err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil || string(actual) != "not a directory" {
		t.Fatalf("root=%q err=%v", actual, err)
	}
}

func TestInstallRollbackRemovesPublishedFilesAfterPublishFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	if _, err := New().Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "LICENSE.txt")); err != nil {
		t.Fatal(err)
	}
	service := &Service{afterPublish: func(string) error { return errors.New("injected publish failure") }}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if err == nil || errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	if _, readErr := os.Lstat(filepath.Join(root, "LICENSE.txt")); !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("license err=%v", readErr)
	}
}

func TestInstallRollsBackAfterRenameFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := &Service{afterRename: func(string) error { return errors.New("injected post-rename failure") }}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if err == nil || errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(destination, "agent-skill-engineer", "LICENSE.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("license err=%v", statErr)
	}
}

func TestRollbackRestoresPredecessorAfterRenameFailure(t *testing.T) {
	destination := t.TempDir()
	r, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	name := filepath.Join("agent-skill-engineer", "SKILL.md")
	if err := ensureDirectory(context.Background(), r, filepath.Dir(name)); err != nil {
		t.Fatal(err)
	}
	predecessor := []byte("exact predecessor bytes")
	if err := os.WriteFile(filepath.Join(destination, name), predecessor, 0o600); err != nil {
		t.Fatal(err)
	}
	published := []byte("new managed bytes")
	committed, err := publish(r, name, published, 0o600, predecessor, true, nil, func() error {
		return errors.New("injected post-rename failure")
	}, nil)
	if !committed || err == nil {
		t.Fatalf("committed=%t err=%v", committed, err)
	}
	if err := rollback(r, []original{{name: name, data: predecessor, published: published, mode: 0o600, existed: true}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(destination, name))
	if err != nil || !bytes.Equal(actual, predecessor) {
		t.Fatalf("actual=%q err=%v", actual, err)
	}
}

func TestRollbackPreservesConcurrentReplacementAfterRenameFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	service := &Service{afterRename: func(identity string) error {
		if err := os.WriteFile(filepath.Join(destination, filepath.FromSlash(identity)), []byte("external"), 0o600); err != nil {
			return err
		}
		return errors.New("injected post-rename failure")
	}}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	entries, err := files()
	if err != nil {
		t.Fatal(err)
	}
	for relative := range entries {
		actual, err := os.ReadFile(filepath.Join(root, relative))
		if err == nil && string(actual) == "external" {
			return
		}
	}
	t.Fatal("concurrent replacement was not preserved")
}

func TestInstallPreservesReplacementBeforePublication(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	service := &Service{beforePublish: func(identity string) error {
		return os.WriteFile(filepath.Join(destination, filepath.FromSlash(identity)), []byte("external"), 0o600)
	}}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	entries, readErr := files()
	if readErr != nil {
		t.Fatal(readErr)
	}
	for relative := range entries {
		actual, readErr := os.ReadFile(filepath.Join(root, relative))
		if readErr == nil && string(actual) == "external" {
			return
		}
	}
	t.Fatal("replacement before publication was not preserved")
}

func TestRollbackPreservesReplacementAtRollbackBoundary(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	service := &Service{
		afterPublish: func(string) error { return errors.New("force rollback") },
		beforeRollback: func(name string) error {
			return os.WriteFile(filepath.Join(destination, name), []byte("external"), 0o600)
		},
	}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	entries, readErr := files()
	if readErr != nil {
		t.Fatal(readErr)
	}
	for relative := range entries {
		actual, readErr := os.ReadFile(filepath.Join(root, relative))
		if readErr == nil && string(actual) == "external" {
			return
		}
	}
	t.Fatal("replacement at rollback boundary was not preserved")
}

func TestInstallRecoversOrphanedTransactionBeforePublishing(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	entries, err := files()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	name := filepath.Join("agent-skill-engineer", "LICENSE.txt")
	if err := ensureDirectory(context.Background(), r, filepath.Dir(name)); err != nil {
		t.Fatal(err)
	}
	previous := transactionPath(name, 0)
	if err := os.WriteFile(filepath.Join(destination, previous), entries["LICENSE.txt"], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Status(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrRecovery) {
		t.Fatalf("status before recovery err=%v", err)
	}
	result, err := New().Install(context.Background(), Options{Dir: destination})
	if err != nil || result.State != StateInstalled {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(destination, previous)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan transaction remains: %v", err)
	}
}

func TestInstallRollbackRemovesOnlyEmptyDirectoriesItCreated(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := &Service{afterPublish: func(string) error { return errors.New("injected publish failure") }}
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err == nil {
		t.Fatal("expected install failure")
	}
	if info, err := os.Lstat(filepath.Join(destination, "agent-skill-engineer")); err != nil || !info.IsDir() {
		t.Fatalf("managed root info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(destination); err != nil || !info.IsDir() {
		t.Fatalf("skills root info=%v err=%v", info, err)
	}
}

func TestUninstallRollbackAndExtrasPreservation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	if _, err := New().Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "local-note.txt")
	if err := os.WriteFile(extra, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := &Service{afterPublish: func(string) error { return errors.New("injected uninstall failure") }}
	if _, err := service.Uninstall(context.Background(), Options{Dir: destination}); err == nil {
		t.Fatal("expected uninstall failure")
	}
	if status, err := New().Status(context.Background(), Options{Dir: destination}); err != nil || status.State != StateInstalled {
		t.Fatalf("rollback status=%+v err=%v", status, err)
	}
	if _, err := New().Uninstall(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.ReadFile(extra); err != nil || string(actual) != "keep" {
		t.Fatalf("extra=%q err=%v", actual, err)
	}
}

func TestUninstallRollbackPreservesChangedBackup(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := New()
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	service.afterPublish = func(relative string) error {
		return errors.New("force rollback")
	}
	service.beforeRollback = func(stored string) error {
		return os.WriteFile(filepath.Join(destination, stored), []byte("external backup"), 0o600)
	}
	_, err := service.Uninstall(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	found := false
	err = filepath.WalkDir(filepath.Join(destination, ".vgxness-backups"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			actual, readErr := os.ReadFile(path)
			if readErr == nil && string(actual) == "external backup" {
				found = true
			}
		}
		return nil
	})
	if err != nil || !found {
		t.Fatalf("backup changed err=%v found=%t", err, found)
	}
}

func TestUninstallRetainsDurableBackupOutsideSkillTree(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := New()
	installed, err := service.Install(context.Background(), Options{Dir: destination})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Uninstall(context.Background(), Options{Dir: destination})
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath == "" || filepath.Dir(result.BackupPath) != filepath.Join(destination, ".vgxness-backups") {
		t.Fatalf("backup path=%q", result.BackupPath)
	}
	for relative, want := range installed.Hashes {
		actual, err := os.ReadFile(filepath.Join(result.BackupPath, relative))
		if err != nil || digest(actual) != want {
			t.Fatalf("backup %s digest=%s err=%v", relative, digest(actual), err)
		}
	}
	if _, err := os.Lstat(filepath.Join(destination, "agent-skill-engineer", ".vgxness-backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hidden skill backup err=%v", err)
	}
}

func TestUninstallDoesNotAllocateBackupSessionAfterCancellation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if _, err := New().Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Uninstall(ctx, Options{Dir: destination})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(destination, ".vgxness-backups")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled uninstall created a backup root: %v", statErr)
	}
}

func TestUninstallReturnsConflictWhenBackupSessionsAreExhausted(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if _, err := New().Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(destination, ".vgxness-backups")
	if err := os.Mkdir(backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		if err := os.Mkdir(filepath.Join(backupRoot, fmt.Sprintf("uninstall-%d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := New().Uninstall(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(backupRoot, "uninstall-128")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("uninstall allocated an unbounded backup session: %v", statErr)
	}
}

func TestStatusIsAbsentAfterUninstallWithOrWithoutExtras(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(fmt.Sprintf("extra=%t", extra), func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "skills")
			service := New()
			if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
				t.Fatal(err)
			}
			if extra {
				if err := os.WriteFile(filepath.Join(destination, "agent-skill-engineer", "local.txt"), []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := service.Uninstall(context.Background(), Options{Dir: destination}); err != nil {
				t.Fatal(err)
			}
			status, err := service.Status(context.Background(), Options{Dir: destination})
			if err != nil || status.State != StateAbsent {
				t.Fatalf("status=%+v err=%v", status, err)
			}
		})
	}
}

func TestPreviewAndStatusRejectSelectedSkillsRootNotDirectory(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if err := os.WriteFile(destination, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, inspect := range []func(context.Context, Options) (Result, error){New().Preview, New().Status} {
		if _, err := inspect(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
			t.Fatalf("err=%v", err)
		}
	}
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func TestRejectsRelativeRootAndCancellation(t *testing.T) {
	if _, err := New().Preview(context.Background(), Options{Dir: "relative"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("relative root err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Install(ctx, Options{Dir: filepath.Join(t.TempDir(), "skills")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation err=%v", err)
	}
}

func TestInstallDoesNotCreateRootWhenCancelledAfterInspection(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{afterInspect: cancel}
	if _, err := service.Install(ctx, Options{Dir: destination}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Install() error=%v", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled install created selected root: %v", err)
	}
}

func TestInstallResumesExactPartialPack(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	entries, err := files()
	if err != nil {
		t.Fatal(err)
	}
	for relative, content := range entries {
		if relative == "LICENSE.txt" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, relative)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, relative), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := New().Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.State != StatePartial || !preview.Changed {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	result, err := New().Install(context.Background(), Options{Dir: destination})
	if err != nil || result.State != StateInstalled || !result.Changed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPartialUnknownContentIsRefused(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	entries, err := files()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(destination, "agent-skill-engineer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "agent-skill-engineer", "LICENSE.txt"), []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrDrift) || len(entries) == 0 {
		t.Fatalf("err=%v", err)
	}
}

func TestRollbackPreservesConcurrentReplacement(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "agent-skill-engineer")
	service := &Service{afterPublish: func(identity string) error {
		if err := os.WriteFile(filepath.Join(destination, filepath.FromSlash(identity)), []byte("external"), 0o644); err != nil {
			return err
		}
		return errors.New("injected failure")
	}}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	entries, readErr := files()
	if readErr != nil {
		t.Fatal(readErr)
	}
	for relative := range entries {
		if actual, readErr := os.ReadFile(filepath.Join(root, relative)); readErr == nil && string(actual) == "external" {
			return
		}
	}
	t.Fatal("concurrent replacement was not preserved")
}

func TestOpenedRootSurvivesSelectedRootReplacementWithoutEscaping(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "skills")
	moved := filepath.Join(parent, "moved-skills")
	service := &Service{afterPublish: func(string) error {
		if err := os.Rename(destination, moved); err != nil {
			return err
		}
		if err := os.Mkdir(destination, 0o755); err != nil {
			return err
		}
		return errors.New("replace selected root")
	}}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if err == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err := os.Stat(filepath.Join(destination, "agent-skill-engineer")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement root received writes: %v", err)
	}
}

func TestPublishPreservesTransactionDestinationCreatedAtMoveBoundary(t *testing.T) {
	destination := t.TempDir()
	r, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	name := filepath.Join("agent-skill-engineer", "SKILL.md")
	if err := ensureDirectory(context.Background(), r, filepath.Dir(name)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, name), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := transactionPath(name, 0)
	_, err = publish(r, name, []byte("new"), 0o600, []byte("managed"), true, nil, nil, func(_, transactionDestination string) error {
		return os.WriteFile(filepath.Join(destination, transactionDestination), []byte("external transaction"), 0o600)
	})
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	assertFileBytes(t, filepath.Join(destination, previous), []byte("external transaction"))
	assertFileBytes(t, filepath.Join(destination, name), []byte("managed"))
}

func TestRemoveExpectedPreservesTransactionDestinationConflict(t *testing.T) {
	destination := t.TempDir()
	r, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	name := "managed"
	if err := os.WriteFile(filepath.Join(destination, name), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := transactionPath(name, 0)
	err = removeExpected(r, name, []byte("managed"), func(_, transactionDestination string) error {
		return os.WriteFile(filepath.Join(destination, transactionDestination), []byte("external transaction"), 0o600)
	})
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	assertFileBytes(t, filepath.Join(destination, previous), []byte("external transaction"))
	assertFileBytes(t, filepath.Join(destination, name), []byte("managed"))
}

func TestUninstallPreservesStoredPathCreatedConcurrently(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := New()
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	service.beforeBackup = func(stored string) error {
		return os.WriteFile(filepath.Join(destination, stored), []byte("external backup"), 0o600)
	}
	_, err := service.Uninstall(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	var found bool
	if err := filepath.WalkDir(filepath.Join(destination, ".vgxness-backups"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			data, _ := os.ReadFile(path)
			found = found || bytes.Equal(data, []byte("external backup"))
		}
		return err
	}); err != nil || !found {
		t.Fatalf("external backup found=%t err=%v", found, err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("path=%s got=%q want=%q err=%v", path, got, want, err)
	}
}

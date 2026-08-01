package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestUninstallSessionDirectoriesRejectsUnreachableRoot(t *testing.T) {
	_, err := uninstallSessionDirectories(
		filepath.Join(".vgxness-backups", "uninstall-0"),
		filepath.Join("other", "session"),
	)
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("uninstallSessionDirectories() error=%v, want ErrRecovery", err)
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

func TestBundledCatalogHasThirteenCanonicalSkillsAndOneLegacyMigration(t *testing.T) {
	catalog, err := bundledCatalog()
	if err != nil || len(catalog.definitions) != 13 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	definition := catalog.definitions[0]
	if definition.name != "skills-creator" || definition.source != "skills-creator" || len(definition.legacy) != 1 || definition.legacy[0].name != "agent-skill-engineer" {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[1]; definition.name != "stacked-pr" || definition.source != "stacked-pr" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[2]; definition.name != "cross-platform" || definition.source != "cross-platform" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[3]; definition.name != "installer-lifecycle" || definition.source != "installer-lifecycle" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[4]; definition.name != "agent-evaluation" || definition.source != "agent-evaluation" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[5]; definition.name != "ci-triage" || definition.source != "ci-triage" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[6]; definition.name != "security-boundary" || definition.source != "security-boundary" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[7]; definition.name != "documentation-strategy" || definition.source != "documentation-strategy" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[8]; definition.name != "product-requirements" || definition.source != "product-requirements" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[9]; definition.name != "software-architecture-docs" || definition.source != "software-architecture-docs" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[10]; definition.name != "user-documentation" || definition.source != "user-documentation" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[11]; definition.name != "api-documentation" || definition.source != "api-documentation" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	if definition = catalog.definitions[12]; definition.name != "quality-test-documentation" || definition.source != "quality-test-documentation" || len(definition.legacy) != 0 {
		t.Fatalf("definition=%+v", definition)
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

func TestCatalogRejectsUnownedDigestPathsAndDuplicateInstalledNames(t *testing.T) {
	invalid := []catalog{
		{definitions: []skillDefinition{{name: "skills-creator", files: map[string][]byte{"SKILL.md": []byte("x")}, predecessors: map[string]string{"missing.txt": "digest"}}}},
		{definitions: []skillDefinition{{name: "skills-creator", files: map[string][]byte{"SKILL.md": []byte("x")}, legacy: []legacyDefinition{{name: "agent-skill-engineer", digests: map[string]string{"missing.txt": "digest"}}}}}},
		{definitions: []skillDefinition{
			{name: "alpha-skill", files: map[string][]byte{"SKILL.md": []byte("x")}, legacy: []legacyDefinition{{name: "beta-skill", digests: map[string]string{"SKILL.md": "digest"}}}},
			{name: "beta-skill", files: map[string][]byte{"SKILL.md": []byte("x")}},
		}},
	}
	for _, value := range invalid {
		service := &Service{catalog: &value}
		if _, err := service.Preview(context.Background(), Options{Dir: filepath.Join(t.TempDir(), "skills")}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("catalog=%+v err=%v", value, err)
		}
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
	if result.State != StateInstalled || !result.Changed || result.FileCount != 34 {
		t.Fatalf("result=%+v", result)
	}
	for _, name := range []string{"skills-creator", "stacked-pr", "cross-platform", "installer-lifecycle", "agent-evaluation", "ci-triage", "security-boundary", "documentation-strategy", "product-requirements", "software-architecture-docs", "user-documentation", "api-documentation", "quality-test-documentation"} {
		if _, err := os.Lstat(filepath.Join(destination, name, "SKILL.md")); err != nil {
			t.Fatalf("canonical %s activation file: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(destination, "agent-skill-engineer")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy agent-skill-engineer remains active: %v", err)
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

func TestInstallMigratesCompleteAndPartialRecognizedLegacyPackage(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "skills")
			service := syntheticMigrationService()
			legacy := filepath.Join(destination, "agent-skill-engineer")
			if err := os.MkdirAll(filepath.Join(legacy, "nested"), 0o755); err != nil {
				t.Fatal(err)
			}
			assertWrite(t, filepath.Join(legacy, "SKILL.md"), []byte("old skill"))
			if !partial {
				assertWrite(t, filepath.Join(legacy, "nested", "guide.txt"), []byte("old guide"))
			}
			extra := filepath.Join(legacy, "local.txt")
			assertWrite(t, extra, []byte("keep"))
			preview, err := service.Preview(context.Background(), Options{Dir: destination})
			if err != nil || !preview.Changed || !preview.UpdateNeeded {
				t.Fatalf("preview=%+v err=%v", preview, err)
			}
			result, err := service.Install(context.Background(), Options{Dir: destination})
			if err != nil || !result.Changed || result.BackupPath == "" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			assertFileBytes(t, filepath.Join(destination, "skills-creator", "SKILL.md"), []byte("new skill"))
			if _, err := os.Lstat(filepath.Join(legacy, "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("legacy active=%v", err)
			}
			assertFileBytes(t, extra, []byte("keep"))
			if _, err := os.Lstat(filepath.Join(legacy, "nested")); err != nil {
				t.Fatalf("managed nested directory=%v", err)
			}
			assertFileBytes(t, filepath.Join(result.BackupPath, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
			guide := filepath.Join(result.BackupPath, "agent-skill-engineer", "nested", "guide.txt")
			if partial {
				if _, err := os.Lstat(guide); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("partial backup guide=%v", err)
				}
			} else {
				assertFileBytes(t, guide, []byte("old guide"))
			}
		})
	}
}

func TestInstallLegacyDisappearsBeforeMoveHasNoBackup(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := syntheticMigrationService()
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(destination, "agent-skill-engineer", "SKILL.md")
	assertWrite(t, legacy, []byte("old skill"))
	service.afterInspect = func() { _ = os.Remove(legacy) }
	result, err := service.Install(context.Background(), Options{Dir: destination})
	if err != nil || result.Changed || result.BackupPath != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(destination, ".vgxness-backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup=%v", err)
	}
}

func TestInstallRejectsUnknownLegacyBytesBeforeDesiredPublication(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	assertWrite(t, filepath.Join(destination, "agent-skill-engineer", "SKILL.md"), []byte("foreign"))
	service := syntheticMigrationService()
	if _, err := service.Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrDrift) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "skills-creator")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new writes=%v", err)
	}
}

func TestDesiredPathRecognizesConfiguredLegacyDigestAsUpdate(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := syntheticMigrationService()
	assertWrite(t, filepath.Join(destination, "skills-creator", "SKILL.md"), []byte("old skill"))
	preview, err := service.Preview(context.Background(), Options{Dir: destination})
	if err != nil || preview.State != StatePartial || !preview.UpdateNeeded {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, filepath.Join(destination, "skills-creator", "SKILL.md"), []byte("new skill"))

	assertWrite(t, filepath.Join(destination, "skills-creator", "SKILL.md"), []byte("foreign"))
	if _, err := service.Preview(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrDrift) {
		t.Fatalf("unknown desired bytes err=%v", err)
	}
}

func TestPreviewRejectsLegacySymlinkRoot(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(destination, "agent-skill-engineer")); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Preview(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestLegacyIntermediateSymlinkRejectsBeforePublication(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	sibling := filepath.Join(destination, "sibling")
	assertWrite(t, filepath.Join(destination, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
	assertWrite(t, filepath.Join(sibling, "guide.txt"), []byte("old guide"))
	if err := os.Symlink(filepath.Join("..", "sibling"), filepath.Join(destination, "agent-skill-engineer", "nested")); err != nil {
		t.Fatal(err)
	}
	service := syntheticMigrationService()
	preview, err := service.Preview(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, err := service.Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Install() err=%v", err)
	}
	assertFileBytes(t, filepath.Join(sibling, "guide.txt"), []byte("old guide"))
	if _, err := os.Lstat(filepath.Join(destination, "skills-creator")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desired activation=%v", err)
	}
}

func TestInstallMigrationFailurePreservesReplacementSession(t *testing.T) {
	skipWindowsOpenDirectoryRenameRace(t)

	destination := filepath.Join(t.TempDir(), "skills")
	held := filepath.Join(filepath.Dir(destination), "held")
	assertWrite(t, filepath.Join(destination, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
	assertWrite(t, filepath.Join(destination, "agent-skill-engineer", "nested", "guide.txt"), []byte("old guide"))
	service := syntheticMigrationService()
	backups := 0
	service.beforeBackup = func(string) error {
		backups++
		if backups == 2 {
			session := filepath.Join(destination, ".vgxness-backups", "migrate-0")
			if err := os.Rename(session, held); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(session, 0o755); err != nil {
				t.Fatal(err)
			}
			return errors.New("injected migration failure")
		}
		return nil
	}
	if _, err := service.Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrRecovery) {
		t.Fatal("Install() error=nil")
	}
	assertFileBytes(t, filepath.Join(destination, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
	assertFileBytes(t, filepath.Join(destination, "agent-skill-engineer", "nested", "guide.txt"), []byte("old guide"))
	if _, err := os.Lstat(filepath.Join(destination, "skills-creator", "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desired active=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, ".vgxness-backups", "migrate-0")); err != nil {
		t.Fatalf("replacement session=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(held, "agent-skill-engineer", "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("held managed data=%v", err)
	}
}

func TestInstallMigrationFinalGateReturnsDurableConflict(t *testing.T) {
	skipWindowsOpenDirectoryRenameRace(t)

	parent := t.TempDir()
	destination := filepath.Join(parent, "skills")
	replaced := filepath.Join(parent, "replaced")
	assertWrite(t, filepath.Join(destination, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
	service := syntheticMigrationService()
	service.beforeFinal = func() {
		if err := os.Rename(destination, replaced); err != nil {
			t.Fatal(err)
		}
		assertWrite(t, filepath.Join(destination, "replacement"), []byte("keep"))
	}
	result, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) || !errors.Is(err, ErrRecovery) || !result.Changed || result.BackupPath != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertFileBytes(t, filepath.Join(replaced, ".vgxness-backups", "migrate-0", "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
	assertFileBytes(t, filepath.Join(destination, "replacement"), []byte("keep"))
}

func TestInstallMigrationSessionGateReturnsDurableConflict(t *testing.T) {
	skipWindowsOpenDirectoryRenameRace(t)

	parent := t.TempDir()
	destination, held := filepath.Join(parent, "skills"), filepath.Join(parent, "held")
	assertWrite(t, filepath.Join(destination, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
	service := syntheticMigrationService()
	service.beforeBackupGate = func() {
		session := filepath.Join(destination, ".vgxness-backups", "migrate-0")
		if err := os.Rename(session, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(session, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) || !errors.Is(err, ErrRecovery) || !result.Changed || result.BackupPath != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(destination, ".vgxness-backups", "migrate-0")); err != nil {
		t.Fatalf("replacement=%v", err)
	}
	assertFileBytes(t, filepath.Join(held, "agent-skill-engineer", "SKILL.md"), []byte("old skill"))
}

func skipWindowsOpenDirectoryRenameRace(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows denies renaming directories with live os.Root handles; this test requires Unix race interleaving")
	}
}

func syntheticMigrationService() *Service {
	legacy := map[string]string{"SKILL.md": digest([]byte("old skill")), "nested/guide.txt": digest([]byte("old guide"))}
	return &Service{catalog: &catalog{definitions: []skillDefinition{{
		name: "skills-creator", source: "skills-creator", files: map[string][]byte{"SKILL.md": []byte("new skill"), "nested/guide.txt": []byte("new guide")},
		legacy: []legacyDefinition{{name: "agent-skill-engineer", digests: legacy}},
	}}}}
}

func assertWrite(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
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
	if _, err := os.Stat(filepath.Join(external, "skills-creator")); !errors.Is(err, os.ErrNotExist) {
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

func TestLegacyV032DigestMapIsCompleteAndDistinctFromCanonicalPackage(t *testing.T) {
	want := map[string]string{
		"LICENSE.txt": "904c73d094910aff6f8e7f0bd06ab953f55f879264680363095d03e64e9a28d7", "SKILL.md": "ad5ce595583c57d5f1466fc1648d231143b4399734405139ac7cb64cb078539e", "agents/openai.yaml": "8b438047a165e0d562bda9670bfb46db643bd3dff27c63d71a4005a2873bbbc6", "assets/SKILL.template.md": "4700c62c712bd1409c796a04564f1386d49ecc8c8bae98e24ca739c2269d1d6a", "assets/eval-cases.template.json": "667412fa4210e93a9e31065a59536a179b6f2cb2dba8ec714b349cd33e73d4d0", "references/authoring-methodology.md": "05d63276f6fa728cbbba6bc8154d5c19094505b8778b8587eeb78f747a1eb0b0", "references/evaluation-methodology.md": "092a7e740cd4fd726cd4da16d3015f33873fac992208b5b166901783d0602904", "references/forward-testing.md": "2c7401c985f8cd77faa13004e07e890688493e1160b5f380d2d9ccddfe8cd04e", "references/security-review.md": "a72f2c0f111e4708469399aa45192b2018ee8ea5d379bfb19b0e6ffcf471d93d", "scripts/generate_openai_yaml.py": "2dc3dd5f118450fc1106f1146be1f78cde2d0b673f8c8583dca0cb8e05fe7088", "scripts/init_skill.py": "162e6ad532aca13c245d84a3b7164d9cf21c69526f300ad9dab8190943cff43e", "scripts/run_evals.py": "54a82f989e180d85662d013385c6033e6aab21e50ed90a6a9ee3b1230a07f7ae", "scripts/skill_utils.py": "8f793b14451a3894c784ecedf736dbeba6d47da9939b0cea66372901fd062dc5", "scripts/validate_skill.py": "b6171b38c4c624a45f8c8a48e9a20ba7f52529f1b16f2b4b685b9e3182c8fe1d", "skill-manifest.json": "df28f085bab7c4ff44a167ffb97dac3f99438fb0be716dbfd8422b42be73f7e1",
	}
	if !mapsEqual(legacyV032Digests, want) {
		t.Fatalf("legacy v0.3.2 digest map=%v want=%v", legacyV032Digests, want)
	}
	entries, err := files()
	if err != nil || len(entries) != len(legacyV032Digests) {
		t.Fatalf("canonical entries=%d legacy entries=%d err=%v", len(entries), len(legacyV032Digests), err)
	}
	if digest(entries["SKILL.md"]) == legacyV032Digests["SKILL.md"] || digest(entries["skill-manifest.json"]) == legacyV032Digests["skill-manifest.json"] {
		t.Fatal("canonical rename metadata must differ from the legacy v0.3.2 package")
	}
}

func TestInstallPreservesExtras(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "skills-creator")
	entries, err := New().entries()
	if err != nil {
		t.Fatal(err)
	}
	for identity, content := range entries {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(destination, native(identity))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, native(identity)), content, 0o644); err != nil {
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
	root := filepath.Join(destination, "skills-creator")
	service := New()
	if _, err := service.Install(context.Background(), Options{Dir: destination}); err != nil {
		t.Fatal(err)
	}
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
	root := filepath.Join(destination, "skills-creator")
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
	if _, err := os.Stat(filepath.Join(outside, "skills-creator")); !errors.Is(err, os.ErrNotExist) {
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
	if _, err := os.Stat(filepath.Join(outside, "skills", "skills-creator")); !errors.Is(err, os.ErrNotExist) {
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
	root := filepath.Join(destination, "skills-creator")
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
	if _, statErr := os.Lstat(filepath.Join(destination, "skills-creator", "LICENSE.txt")); !errors.Is(statErr, os.ErrNotExist) {
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
	name := filepath.Join("skills-creator", "SKILL.md")
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
	entries, err := New().entries()
	if err != nil {
		t.Fatal(err)
	}
	for identity := range entries {
		actual, err := os.ReadFile(filepath.Join(destination, native(identity)))
		if err == nil && string(actual) == "external" {
			return
		}
	}
	t.Fatal("concurrent replacement was not preserved")
}

func TestInstallPreservesReplacementBeforePublication(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	service := &Service{beforePublish: func(identity string) error {
		return os.WriteFile(filepath.Join(destination, filepath.FromSlash(identity)), []byte("external"), 0o600)
	}}
	_, err := service.Install(context.Background(), Options{Dir: destination})
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrRecovery) {
		t.Fatalf("err=%v", err)
	}
	entries, readErr := New().entries()
	if readErr != nil {
		t.Fatal(readErr)
	}
	for identity := range entries {
		actual, readErr := os.ReadFile(filepath.Join(destination, native(identity)))
		if readErr == nil && string(actual) == "external" {
			return
		}
	}
	t.Fatal("replacement before publication was not preserved")
}

func TestRollbackPreservesReplacementAtRollbackBoundary(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
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
	entries, readErr := New().entries()
	if readErr != nil {
		t.Fatal(readErr)
	}
	for identity := range entries {
		actual, readErr := os.ReadFile(filepath.Join(destination, native(identity)))
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
	name := filepath.Join("skills-creator", "LICENSE.txt")
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
	if info, err := os.Lstat(filepath.Join(destination, "agent-evaluation")); err != nil || !info.IsDir() {
		t.Fatalf("managed root info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(destination); err != nil || !info.IsDir() {
		t.Fatalf("skills root info=%v err=%v", info, err)
	}
}

func TestUninstallRollbackAndExtrasPreservation(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
	root := filepath.Join(destination, "skills-creator")
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
	if _, err := os.Lstat(filepath.Join(destination, "skills-creator", ".vgxness-backups")); !errors.Is(err, os.ErrNotExist) {
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
				if err := os.WriteFile(filepath.Join(destination, "skills-creator", "local.txt"), []byte("keep"), 0o644); err != nil {
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
	root := filepath.Join(destination, "skills-creator")
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
	if err := os.MkdirAll(filepath.Join(destination, "skills-creator"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "skills-creator", "LICENSE.txt"), []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Install(context.Background(), Options{Dir: destination}); !errors.Is(err, ErrDrift) || len(entries) == 0 {
		t.Fatalf("err=%v", err)
	}
}

func TestRollbackPreservesConcurrentReplacement(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "skills")
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
	entries, readErr := New().entries()
	if readErr != nil {
		t.Fatal(readErr)
	}
	for identity := range entries {
		if actual, readErr := os.ReadFile(filepath.Join(destination, native(identity))); readErr == nil && string(actual) == "external" {
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
	if _, err := os.Stat(filepath.Join(destination, "skills-creator")); !errors.Is(err, os.ErrNotExist) {
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
	name := filepath.Join("skills-creator", "SKILL.md")
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

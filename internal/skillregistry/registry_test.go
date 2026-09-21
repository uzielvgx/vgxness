package skillregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func manifest(name, description, body string) []byte {
	return []byte("---\nname: " + name + "\ndescription: " + description + "\ncompatibility: Agent Skills hosts\n---\n\n" + body + "\n")
}

func writeSkill(t *testing.T, root, directory, name, description, body string) string {
	t.Helper()
	path := filepath.Join(root, directory)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), manifest(name, description, body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func digestOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func baseOptions(t *testing.T) (Options, string, string) {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options := Options{
		Workspace: workspace,
		HomeDir:   home,
		CachePath: filepath.Join(home, "state", "skill-registry.json"),
		MaxAge:    time.Minute,
	}
	return options, workspace, home
}

func TestScanDiscoversAuthorizedRootsWithExtractedMetadata(t *testing.T) {
	options, workspace, home := baseOptions(t)
	projectRoot := filepath.Join(workspace, ".agents", "skills")
	globalRoot := filepath.Join(home, ".agents", "skills")
	writeSkill(t, projectRoot, "alpha", "alpha", "Project alpha workflow", "alpha body")
	skill := writeSkill(t, globalRoot, "beta", "beta", "Global beta workflow", "beta body")

	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if registry.SchemaVersion != SchemaVersion || registry.Workspace != workspace {
		t.Fatalf("registry identity: %+v", registry)
	}
	if len(registry.Roots) != 2 || registry.Roots[0].Scope != ScopeProject || registry.Roots[1].Scope != ScopeGlobal {
		t.Fatalf("roots: %+v", registry.Roots)
	}
	if !registry.Roots[0].Present || !registry.Roots[1].Present {
		t.Fatalf("roots must be present: %+v", registry.Roots)
	}
	if len(registry.Entries) != 2 {
		t.Fatalf("entries: %+v", registry.Entries)
	}
	byName := map[string]Entry{}
	for _, entry := range registry.Entries {
		byName[entry.Name] = entry
	}
	alpha, beta := byName["alpha"], byName["beta"]
	if alpha.Status != StatusOK || beta.Status != StatusOK {
		t.Fatalf("statuses: alpha=%q beta=%q", alpha.Status, beta.Status)
	}
	if alpha.Description != "Project alpha workflow" || beta.Description != "Global beta workflow" {
		t.Fatalf("descriptions must be extracted verbatim: %+v", byName)
	}
	content, err := os.ReadFile(filepath.Join(skill, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if beta.SHA256 != digestOf(content) {
		t.Fatalf("beta sha mismatch: %s", beta.SHA256)
	}
}

func TestDuplicateNamesAreExplicitAndDeterministic(t *testing.T) {
	options, workspace, home := baseOptions(t)
	projectRoot := filepath.Join(workspace, ".agents", "skills")
	globalRoot := filepath.Join(home, ".agents", "skills")
	writeSkill(t, projectRoot, "shared", "shared", "project copy", "p")
	writeSkill(t, globalRoot, "shared", "shared", "global copy", "g")

	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	var winners, duplicates []Entry
	for _, entry := range registry.Entries {
		switch entry.Status {
		case StatusOK:
			winners = append(winners, entry)
		case StatusDuplicate:
			duplicates = append(duplicates, entry)
		}
	}
	if len(winners) != 1 || winners[0].Description != "project copy" {
		t.Fatalf("first root must win: %+v", winners)
	}
	if len(duplicates) != 1 || duplicates[0].DuplicateOf != winners[0].ID {
		t.Fatalf("duplicate must reference the winner: %+v", duplicates)
	}
	if duplicates[0].ID == winners[0].ID {
		t.Fatalf("entry IDs must be unique: %+v", registry.Entries)
	}
	foundDiagnostic := false
	for _, diagnostic := range registry.Diagnostics {
		if diagnostic.Kind == DiagDuplicate {
			foundDiagnostic = true
		}
	}
	if !foundDiagnostic {
		t.Fatalf("missing duplicate diagnostic: %+v", registry.Diagnostics)
	}
}

func TestMissingRootsProduceDiagnostics(t *testing.T) {
	options, _, _ := baseOptions(t)
	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Diagnostics) != 2 {
		t.Fatalf("diagnostics: %+v", registry.Diagnostics)
	}
	for _, diagnostic := range registry.Diagnostics {
		if diagnostic.Kind != DiagMissingRoot {
			t.Fatalf("kind: %+v", diagnostic)
		}
	}
	for _, root := range registry.Roots {
		if root.Present {
			t.Fatalf("missing root reported present: %+v", root)
		}
	}
}

func TestSymlinkSkillIsRejected(t *testing.T) {
	options, workspace, home := baseOptions(t)
	projectRoot := filepath.Join(workspace, ".agents", "skills")
	real := writeSkill(t, filepath.Join(home, "external"), "real", "real", "real", "body")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(projectRoot, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 1 || registry.Entries[0].Status != StatusSymlink {
		t.Fatalf("symlink entry: %+v", registry.Entries)
	}
}

func TestInvalidFrontmatterIsReportedNotDropped(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	root := filepath.Join(workspace, ".agents", "skills")
	directory := filepath.Join(root, "broken")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\ndescription: no name\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 1 || registry.Entries[0].Status != StatusInvalid {
		t.Fatalf("entries: %+v", registry.Entries)
	}
	found := false
	for _, diagnostic := range registry.Diagnostics {
		if diagnostic.Kind == DiagInvalid {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics: %+v", registry.Diagnostics)
	}
}

func TestOversizedManifestIsReported(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	options.MaxManifestBytes = 16
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "large", "large", "large", strings.Repeat("x", 64))
	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 1 || registry.Entries[0].Status != StatusOversized {
		t.Fatalf("entries: %+v", registry.Entries)
	}
}

func TestRefreshPublishesOutsideWorkspaceAndEnsureIsFresh(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha workflow", "body")
	service := New()
	registry, err := service.Refresh(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(registry.GeneratedAt, "0001") || registry.GeneratedAt == "" {
		t.Fatalf("generatedAt: %q", registry.GeneratedAt)
	}
	cache, err := CachePath(options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(cache, workspace+string(os.PathSeparator)) {
		t.Fatalf("cache %q must live outside the versioned workspace %q", cache, workspace)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("cache not published: %v", err)
	}
	loaded, usable, err := service.Load(context.Background(), options)
	if err != nil || !usable || loaded.GeneratedAt != registry.GeneratedAt {
		t.Fatalf("load: usable=%t err=%v", usable, err)
	}
	ensured, err := service.Ensure(context.Background(), options)
	if err != nil || ensured.GeneratedAt != registry.GeneratedAt {
		t.Fatalf("ensure must reuse fresh cache: %v", err)
	}
}

func TestCorruptAndFutureSchemaRebuild(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	service := New()
	if _, err := service.Refresh(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	cache, _ := CachePath(options)
	for _, corrupt := range [][]byte{[]byte("{not json"), []byte(`{"schemaVersion":2,"workspace":"x","entries":[]}`)} {
		if err := os.WriteFile(cache, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, usable, err := service.Load(context.Background(), options); err != nil || usable {
			t.Fatalf("corrupt cache must not be usable: usable=%t err=%v", usable, err)
		}
		rebuilt, err := service.Ensure(context.Background(), options)
		if err != nil || len(rebuilt.Entries) != 1 {
			t.Fatalf("rebuild: entries=%d err=%v", len(rebuilt.Entries), err)
		}
		if _, usable, _ := service.Load(context.Background(), options); !usable {
			t.Fatalf("rebuild must publish a usable cache")
		}
	}
}

func TestStaleCacheRefreshesAndPicksUpChanges(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	root := filepath.Join(workspace, ".agents", "skills")
	writeSkill(t, root, "alpha", "alpha", "alpha", "body")
	clock := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	options.now = func() time.Time { return clock }
	service := New()
	first, err := service.Refresh(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "beta", "beta", "beta", "body")
	options.now = func() time.Time { return clock.Add(2 * time.Minute) }
	second, err := service.Ensure(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if second.GeneratedAt == first.GeneratedAt || len(second.Entries) != 2 {
		t.Fatalf("stale cache was not rebuilt: first=%q second=%q entries=%d", first.GeneratedAt, second.GeneratedAt, len(second.Entries))
	}
}

func TestConcurrentRefreshPublishesValidJSONOnly(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	service := New()
	var waitGroup sync.WaitGroup
	errs := make([]error, 8)
	for index := 0; index < len(errs); index++ {
		waitGroup.Add(1)
		go func(slot int) {
			defer waitGroup.Done()
			_, errs[slot] = service.Refresh(context.Background(), options)
		}(index)
	}
	waitGroup.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent refresh: %v", err)
		}
	}
	cache, _ := CachePath(options)
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Registry
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.SchemaVersion != SchemaVersion {
		t.Fatalf("cache is not valid JSON: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(cache), ".skill-registry-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestChangedDuringScanIsDowngraded(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	root := filepath.Join(workspace, ".agents", "skills")
	writeSkill(t, root, "alpha", "alpha", "alpha", "body")
	options.beforePublish = func(string) error {
		return os.WriteFile(filepath.Join(root, "alpha", "SKILL.md"), manifest("alpha", "alpha", "mutated"), 0o600)
	}
	registry, err := New().Refresh(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 1 || registry.Entries[0].Status != StatusChanged {
		t.Fatalf("entries: %+v", registry.Entries)
	}
	found := false
	for _, diagnostic := range registry.Diagnostics {
		if diagnostic.Kind == DiagChanged {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics: %+v", registry.Diagnostics)
	}
}

func TestSearchAndResolveAreBounded(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	root := filepath.Join(workspace, ".agents", "skills")
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		writeSkill(t, root, name, name, name+" workflow", "body")
	}
	service := New()
	registry, err := service.Search(context.Background(), options, "a", 2)
	if err != nil || len(registry.Entries) != 2 {
		t.Fatalf("search: entries=%d err=%v", len(registry.Entries), err)
	}
	if _, err := service.Search(context.Background(), options, "", 2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty query must be invalid: %v", err)
	}
	entry, err := service.Resolve(context.Background(), options, "beta")
	if err != nil || entry.Name != "beta" || entry.Description != "beta workflow" {
		t.Fatalf("resolve: %+v err=%v", entry, err)
	}
	if _, err := service.Resolve(context.Background(), options, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown resolve must be not found: %v", err)
	}
}

func TestWorkspaceMismatchRebuilds(t *testing.T) {
	options, workspace, home := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	service := New()
	if _, err := service.Refresh(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(other, ".agents", "skills"), "beta", "beta", "beta", "body")
	options.Workspace = other
	options.HomeDir = home
	registry, err := service.Ensure(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Workspace != other || len(registry.Entries) != 1 || registry.Entries[0].Name != "beta" {
		t.Fatalf("workspace mismatch must rebuild: %+v", registry)
	}
}

func TestLoadRequiresWorkspaceRootBindingAndFreshness(t *testing.T) {
	options, workspace, home := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	service := New()
	if _, err := service.Refresh(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if _, usable, err := service.Load(context.Background(), options); err != nil || !usable {
		t.Fatalf("bound fresh cache must be usable: usable=%t err=%v", usable, err)
	}
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherOptions := options
	otherOptions.Workspace = other
	if registry, usable, _ := service.Load(context.Background(), otherOptions); usable || registry.SchemaVersion != SchemaVersion {
		t.Fatalf("other-workspace cache must be present but unusable: usable=%t present=%t", usable, registry.SchemaVersion == SchemaVersion)
	}
	custom := options
	custom.Roots = []RootSpec{{ID: "custom", Path: filepath.Join(home, "custom-skills"), Scope: ScopeConfigured, Provenance: "test"}}
	if _, usable, _ := service.Load(context.Background(), custom); usable {
		t.Fatal("cache refreshed under other roots must not be reusable")
	}
	stale := options
	base := time.Now()
	stale.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, usable, _ := service.Load(context.Background(), stale); usable {
		t.Fatal("stale cache must not be fresh")
	}
	futureClock := options
	futureClock.now = func() time.Time { return base.Add(-2 * time.Minute) }
	if _, usable, _ := service.Load(context.Background(), futureClock); usable {
		t.Fatal("future cache must not be fresh")
	}
}

func TestResolveRootsRejectsExcessRoots(t *testing.T) {
	directory := t.TempDir()
	roots := make([]RootSpec, 0, DefaultMaxRoots+1)
	for index := 0; index <= DefaultMaxRoots; index++ {
		roots = append(roots, RootSpec{ID: "r" + strconv.Itoa(index), Path: filepath.Join(directory, "r"+strconv.Itoa(index))})
	}
	if _, err := New().Scan(context.Background(), Options{Workspace: directory, Roots: roots}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("excess roots must be rejected: %v", err)
	}
}

func TestControlledSymlinkRootIsRejected(t *testing.T) {
	options, workspace, home := baseOptions(t)
	external := filepath.Join(home, "external")
	writeSkill(t, external, "leak", "leak", "leak", "body")
	if err := os.Symlink(external, filepath.Join(workspace, ".agents")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	registry, err := New().Scan(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 0 {
		t.Fatalf("workspace-controlled .agents symlink must not be scanned: %+v", registry.Entries)
	}
	rejected := false
	for _, diagnostic := range registry.Diagnostics {
		if diagnostic.Kind == DiagRootRejected {
			rejected = true
		}
	}
	if !rejected {
		t.Fatalf("expected a root-rejected diagnostic: %+v", registry.Diagnostics)
	}

	// Nested case: a real .agents directory whose skills child is a symlink.
	nestedWorkspace := filepath.Join(home, "nested")
	if err := os.MkdirAll(filepath.Join(nestedWorkspace, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "skills"), filepath.Join(nestedWorkspace, ".agents", "skills")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	nested := options
	nested.Workspace = nestedWorkspace
	nestedRegistry, err := New().Scan(context.Background(), nested)
	if err != nil {
		t.Fatal(err)
	}
	if len(nestedRegistry.Entries) != 0 {
		t.Fatalf("nested workspace-controlled symlink must not be scanned: %+v", nestedRegistry.Entries)
	}
}

func TestManifestScalarsAreBounded(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	directory := filepath.Join(workspace, ".agents", "skills", "big")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: big\ndescription: " + strings.Repeat("d", 5000) + "\ncompatibility: " + strings.Repeat("c", 5000) + "\n---\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := New().Scan(context.Background(), options)
	if err != nil || len(registry.Entries) != 1 {
		t.Fatalf("scan: %+v err=%v", registry.Entries, err)
	}
	if len(registry.Entries[0].Description) > maxDescriptionBytes || len(registry.Entries[0].Compatibility) > maxDescriptionBytes {
		t.Fatalf("scalars not bounded: desc=%d compat=%d", len(registry.Entries[0].Description), len(registry.Entries[0].Compatibility))
	}
}

func TestResolveDuplicateNameIsAmbiguousNotSilentWinner(t *testing.T) {
	options, workspace, home := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "shared", "shared", "project copy", "p")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "shared", "shared", "global copy", "g")
	service := New()
	registry, err := service.Ensure(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	var winnerPath, duplicateID, duplicatePath string
	for _, entry := range registry.Entries {
		if entry.Status == StatusOK {
			winnerPath = entry.Path
		}
		if entry.Status == StatusDuplicate {
			duplicateID, duplicatePath = entry.ID, entry.Path
		}
	}
	if _, err := service.Resolve(context.Background(), options, "shared"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate name must be ambiguous: %v", err)
	}
	if entry, err := service.Resolve(context.Background(), options, winnerPath); err != nil || entry.Path != winnerPath {
		t.Fatalf("explicit path must resolve the winner: %+v err=%v", entry, err)
	}
	if entry, err := service.Resolve(context.Background(), options, duplicateID); err != nil || entry.Status != StatusDuplicate {
		t.Fatalf("explicit duplicate id must resolve without becoming ok: %+v err=%v", entry, err)
	}
	if entry, err := service.Resolve(context.Background(), options, duplicatePath); err != nil || entry.Status != StatusDuplicate {
		t.Fatalf("explicit duplicate path must resolve: %+v err=%v", entry, err)
	}
}

func TestEnsureDetectsContentChangeWithoutClockAdvance(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	root := filepath.Join(workspace, ".agents", "skills")
	writeSkill(t, root, "alpha", "alpha", "alpha", "body")
	service := New()
	first, err := service.Ensure(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "beta", "beta", "beta", "body")
	second, err := service.Ensure(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 2 || second.GeneratedAt == first.GeneratedAt {
		t.Fatalf("content change must invalidate cache: entries=%d sameTimestamp=%t", len(second.Entries), second.GeneratedAt == first.GeneratedAt)
	}
}

func TestFutureTimestampIsNotFreshAndRebuilds(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	service := New()
	registry, err := service.Refresh(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	registry.GeneratedAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	cache, _ := CachePath(options)
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if Fresh(options, registry) {
		t.Fatalf("future timestamp must not be fresh")
	}
	ensured, err := service.Ensure(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if ensured.GeneratedAt == registry.GeneratedAt {
		t.Fatalf("future cache must be rebuilt: %q", ensured.GeneratedAt)
	}
}

func TestCacheCannotLegitimizePathsOutsideAuthorizedRoots(t *testing.T) {
	options, workspace, home := baseOptions(t)
	cache, err := CachePath(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	registry := Registry{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Workspace:     workspace,
		Roots:         []RootSpec{{ID: "project", Path: filepath.Join(workspace, ".agents", "skills"), Scope: ScopeProject}},
		Entries:       []Entry{{ID: "evil", Name: "evil", Path: filepath.Join(home, "outside"), SHA256: "0", Status: StatusOK}},
		Complete:      true,
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, usable, err := New().Load(context.Background(), options); err != nil || usable {
		t.Fatalf("outside-root cache must be unusable: usable=%t err=%v", usable, err)
	}
}

func TestSearchRetainsDiagnosticsAndMarksTruncation(t *testing.T) {
	options, workspace, home := baseOptions(t)
	root := filepath.Join(workspace, ".agents", "skills")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeSkill(t, root, name, name, name+" workflow", "body")
	}
	real := writeSkill(t, filepath.Join(home, "external"), "real", "real", "real", "body")
	if err := os.Symlink(real, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	registry, err := New().Search(context.Background(), options, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Complete {
		t.Fatalf("truncated search must set complete=false")
	}
	if len(registry.Diagnostics) == 0 {
		t.Fatalf("search must retain discovery diagnostics: %+v", registry)
	}
	found := false
	for _, diagnostic := range registry.Diagnostics {
		if diagnostic.Kind == DiagLimit || diagnostic.Kind == DiagSymlink {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics lost: %+v", registry.Diagnostics)
	}
}

func TestPublishRejectsSymlinkedStatePath(t *testing.T) {
	options, workspace, home := baseOptions(t)
	writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	realDirectory := filepath.Join(home, "real-state")
	if err := os.MkdirAll(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	linkFile := filepath.Join(home, "link-registry.json")
	if err := os.Symlink(filepath.Join(realDirectory, "registry.json"), linkFile); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	options.CachePath = linkFile
	if _, err := New().Refresh(context.Background(), options); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlinked cache file must be rejected: %v", err)
	}
	linkDirectory := filepath.Join(home, "link-dir")
	if err := os.Symlink(realDirectory, linkDirectory); err != nil {
		t.Skipf("symlinked directory unavailable: %v", err)
	}
	options.CachePath = filepath.Join(linkDirectory, "registry.json")
	if _, err := New().Refresh(context.Background(), options); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlinked cache directory must be rejected: %v", err)
	}
}

func TestPublishRefusesOversizedDocument(t *testing.T) {
	directory := t.TempDir()
	cache := filepath.Join(directory, "skill-registry.json")
	registry := Registry{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Workspace:     directory,
		Entries:       []Entry{{ID: "x", Name: "x", Path: directory, SHA256: "0", Status: StatusOK, Description: strings.Repeat("a", maxCacheBytes+1)}},
		Complete:      true,
	}
	if err := publish(context.Background(), cache, registry); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized document must be refused: %v", err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("refused document must not be written: %v", err)
	}
}

func TestLockReleaseNeverRemovesReplacement(t *testing.T) {
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := acquireLock(context.Background(), root, "skill-registry.lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "skill-registry.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "skill-registry.lock"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock.release(root)
	data, err := os.ReadFile(filepath.Join(directory, "skill-registry.lock"))
	if err != nil || string(data) != "replacement" {
		t.Fatalf("release removed a replacement lock: %q err=%v", data, err)
	}
}

func TestLockReleaseRemovesOwnLock(t *testing.T) {
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := acquireLock(context.Background(), root, "skill-registry.lock")
	if err != nil {
		t.Fatal(err)
	}
	lock.release(root)
	if _, err := os.Stat(filepath.Join(directory, "skill-registry.lock")); !os.IsNotExist(err) {
		t.Fatalf("a writer must remove the lock it owns: %v", err)
	}
}

func TestLockOwnershipRejectsSamePIDSuccessor(t *testing.T) {
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := acquireLock(context.Background(), root, "skill-registry.lock")
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(directory, "skill-registry.lock")
	acquired, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(acquired)), "\n", 2)
	if len(lines) != 2 || lines[0] != strconv.Itoa(os.Getpid()) || len(lines[1]) != 32 {
		t.Fatalf("lock body must be PID plus a 32-hex token: %q", acquired)
	}
	if pid, ok := lockPID(acquired); !ok || pid != os.Getpid() {
		t.Fatalf("token lock PID parse: pid=%d ok=%t", pid, ok)
	}
	lock.release(root)
	// A PID-only successor and a same-PID different-token successor must both
	// survive release: PID equality is not proof of ownership.
	for _, successor := range [][]byte{
		[]byte(strconv.Itoa(os.Getpid())),
		[]byte(strconv.Itoa(os.Getpid()) + "\n" + strings.Repeat("0", 32)),
	} {
		held, err := acquireLock(context.Background(), root, "skill-registry.lock")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(lockPath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockPath, successor, 0o600); err != nil {
			t.Fatal(err)
		}
		held.release(root)
		data, err := os.ReadFile(lockPath)
		if err != nil || string(data) != string(successor) {
			t.Fatalf("release removed a same-PID successor %q: got %q err=%v", successor, data, err)
		}
		_ = os.Remove(lockPath)
	}
}

func TestStaleLockRecoveryRequiresProvablyDeadOwner(t *testing.T) {
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lockPath := filepath.Join(directory, "skill-registry.lock")
	old := time.Now().Add(-2 * staleLockAge)
	if err := os.WriteFile(lockPath, []byte("1073741824"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if recovered, err := recoverStaleLock(root, "skill-registry.lock"); err != nil || !recovered {
		t.Fatalf("dead-owner stale lock must be recovered: recovered=%t err=%v", recovered, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("recovered lock must be removed: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if recovered, err := recoverStaleLock(root, "skill-registry.lock"); err != nil || recovered {
		t.Fatalf("live-owner stale lock must not be recovered: recovered=%t err=%v", recovered, err)
	}
}

func TestRevalidateRejectsChangedSource(t *testing.T) {
	options, workspace, _ := baseOptions(t)
	path := writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "alpha", "alpha", "alpha", "body")
	manifestPath := filepath.Join(path, manifestName)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := options.resolveRoots(workspace)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{ID: "alpha", Name: "alpha", Path: path, SHA256: digest(content), Status: StatusOK}
	if _, err := revalidate(entry, roots, options); err != nil {
		t.Fatalf("unchanged source must revalidate: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest("alpha", "alpha", "mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := revalidate(entry, roots, options); !errors.Is(err, ErrStale) {
		t.Fatalf("changed source must be stale: %v", err)
	}
}

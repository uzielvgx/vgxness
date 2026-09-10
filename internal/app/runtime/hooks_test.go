package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/memory"
)

func TestMemoryHooksConstructors(t *testing.T) {
	dispatcher := hooks.New()
	if got := NewMemoryWithHooks("test", false, dispatcher); got.hooks != dispatcher {
		t.Fatal("hook emitter not retained")
	}

}

func TestMemoryHooksRememberForget(t *testing.T) {
	d := hooks.NewForTest(func() time.Time { return time.Now() }, func() (string, error) { return "event-1", nil })
	var events []hooks.Event
	if err := d.Register("test", func(_ context.Context, event hooks.Event) error { events = append(events, event); return nil }, hooks.NameMemorySaved, hooks.NameMemoryForgotten); err != nil {
		t.Fatal(err)
	}
	r := NewMemoryWithHooks("test", false, d)
	opts := config.Options{StorageRoot: t.TempDir()}
	entry, err := r.Remember(context.Background(), opts, memory.Remember{Content: "secret", Project: "project", Scope: memory.ScopeProject})
	if err != nil || len(events) != 1 || events[0].Subject().ID() != entry.ID {
		t.Fatalf("remember=%+v err=%v events=%d", entry, err, len(events))
	}
	if got, ok := events[0].MemoryEntry(); !ok || got.Scope() != string(entry.Scope) || got.Type() != entry.Type || got.State() != string(entry.State) || !got.CreatedAt().Equal(entry.CreatedAt) || !got.UpdatedAt().Equal(entry.UpdatedAt) {
		t.Fatal("memory result mapping mismatch")
	}
	encoded, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret", "title", "content", "preview", "references", "session", "producer", "sourceProvider", "sourceId"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("event leaked %q: %s", secret, encoded)
		}
	}
	forgotten, err := r.Forget(context.Background(), opts, memory.Forget{ID: entry.ID, Project: entry.Project, Scope: entry.Scope})
	if err != nil || forgotten.State != "archived" || len(events) != 2 || events[1].Name() != hooks.NameMemoryForgotten {
		t.Fatalf("forget=%+v err=%v events=%d", forgotten, err, len(events))
	}
}

type panickingEmitter struct{}

func (panickingEmitter) Emit(context.Context, hooks.Draft) { panic("emitter") }

func TestMemoryHooksSuppressInvalidCanceledAndEmitterPanic(t *testing.T) {
	opts := config.Options{StorageRoot: t.TempDir()}
	var events []hooks.Event
	d := hooks.New()
	if err := d.Register("test", func(_ context.Context, e hooks.Event) error { events = append(events, e); return nil }, hooks.NameMemorySaved); err != nil {
		t.Fatal(err)
	}
	r := NewMemoryWithHooks("test", false, d)
	if _, err := r.Remember(context.Background(), opts, memory.Remember{}); err == nil {
		t.Error("invalid remember succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Remember(ctx, opts, memory.Remember{Content: "secret"}); err == nil {
		t.Error("canceled remember succeeded")
	}
	if len(events) != 0 {
		t.Error("failed memory operation emitted")
	}
	entry, err := NewMemoryWithHooks("test", false, panickingEmitter{}).Remember(context.Background(), opts, memory.Remember{Content: "safe", Project: "project"})
	if err != nil || entry.ID == "" {
		t.Fatalf("panic emitter changed result: %+v %v", entry, err)
	}
	plain, err := NewMemory("test", false).Remember(context.Background(), opts, memory.Remember{Content: "plain", Project: "project"})
	if err != nil || plain.ID == "" {
		t.Fatalf("default constructor changed behavior: %+v %v", plain, err)
	}
}

func TestMemorySyncHooksMapAllNilResultsAndSuppressInvalidIdentity(t *testing.T) {
	workspace := t.TempDir()
	canonicalWorkspace, err := canonicalInvocationWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := memory.StableProjectID(canonicalWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []memory.SyncResult{
		{Status: memory.SyncStatusAbsent}, {Status: memory.SyncStatusDisabled}, {Status: memory.SyncStatusUnavailable},
		{Status: memory.SyncStatusUnreachable, Retried: 1}, {Status: memory.SyncStatusCredentialMissing},
		{Status: memory.SyncStatusCredentialUnavailable}, {Status: memory.SyncStatusIncompatible}, {Status: memory.SyncStatusInvalid},
		{Status: memory.SyncStatusUnauthorized}, {Status: memory.SyncStatusPartial, Batches: 1}, {Status: memory.SyncStatusRejected, Rejected: 1},
		{Status: memory.SyncStatusConflict, Conflicts: 1}, {Status: memory.SyncStatusSynced, Pushed: 1, PreviouslyAccepted: 2, Rejected: 3, Retried: 4, Conflicts: 5, Batches: 6},
	} {
		t.Run(string(result.Status), func(t *testing.T) {
			var got hooks.Event
			d := hooks.New()
			if err := d.Register("sync", func(_ context.Context, event hooks.Event) error { got = event; return nil }, hooks.NameMemorySyncCompleted); err != nil {
				t.Fatal(err)
			}
			r := NewMemoryWithHooks("test", false, d)
			r.emitMemorySync(context.Background(), workspace, result)
			mapped, ok := got.MemorySync()
			if !ok || got.Subject().ID() != projectID || mapped.Status() != string(result.Status) || mapped.Pushed() != int64(result.Pushed) || mapped.PreviouslyAccepted() != int64(result.PreviouslyAccepted) || mapped.Rejected() != int64(result.Rejected) || mapped.Retried() != int64(result.Retried) || mapped.Conflicts() != int64(result.Conflicts) || mapped.Batches() != int64(result.Batches) {
				t.Fatalf("event=%+v result=%+v", got, result)
			}
		})
	}
	for _, workspace := range []string{filepath.Join(t.TempDir(), "missing"), "/"} {
		var events []hooks.Event
		d := hooks.New()
		if err := d.Register("sync", func(_ context.Context, event hooks.Event) error { events = append(events, event); return nil }, hooks.NameMemorySyncCompleted); err != nil {
			t.Fatal(err)
		}
		NewMemoryWithHooks("test", false, d).emitMemorySync(context.Background(), workspace, memory.SyncResult{Status: memory.SyncStatusUnavailable})
		if len(events) != 0 {
			t.Fatalf("workspace %q emitted %d events", workspace, len(events))
		}
	}
	NewMemoryWithHooks("test", false, panickingEmitter{}).emitMemorySync(context.Background(), workspace, memory.SyncResult{Status: memory.SyncStatusSynced})
}

func TestMemorySyncEmitsOnceOnlyAfterNilError(t *testing.T) {
	var events []hooks.Event
	d := hooks.New()
	if err := d.Register("sync", func(_ context.Context, event hooks.Event) error { events = append(events, event); return nil }, hooks.NameMemorySyncCompleted); err != nil {
		t.Fatal(err)
	}
	r := NewMemoryWithHooks("test", false, d)
	workspace := t.TempDir()
	result, err := r.Sync(context.Background(), config.Options{ProjectDir: workspace, ProjectLocal: true})
	if err != nil || result.Status != memory.SyncStatusUnavailable || len(events) != 1 {
		t.Fatalf("sync result=%+v err=%v events=%d", result, err, len(events))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Sync(ctx, config.Options{ProjectDir: workspace}); err == nil || len(events) != 1 {
		t.Fatalf("errored sync emitted: err=%v events=%d", err, len(events))
	}
}

func TestMemorySyncRejectsMissingOrRelativeWorkspaceBeforeEffects(t *testing.T) {
	for _, workspace := range []string{"", "relative"} {
		t.Run(workspace, func(t *testing.T) {
			var events []hooks.Event
			d := hooks.New()
			if err := d.Register("sync", func(_ context.Context, event hooks.Event) error { events = append(events, event); return nil }, hooks.NameMemorySyncCompleted); err != nil {
				t.Fatal(err)
			}
			credentialCalls := 0
			r := NewMemoryWithHooks("test", false, d)
			r.credential = func(string) (string, error) { credentialCalls++; return "", nil }
			result, err := r.Sync(context.Background(), config.Options{StorageRoot: t.TempDir(), ProjectDir: workspace})
			if !errors.Is(err, memory.ErrInvalid) || result.Status != memory.SyncStatusUnavailable || len(events) != 0 || credentialCalls != 0 {
				t.Fatalf("workspace=%q result=%+v err=%v events=%d credentialCalls=%d", workspace, result, err, len(events), credentialCalls)
			}
		})
	}
}

func TestMemorySyncAbsoluteWorkspaceEmitsCompleted(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalInvocationWorkspace(workingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	wantProject, err := memory.StableProjectID(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var events []hooks.Event
	d := hooks.New()
	if err := d.Register("sync", func(_ context.Context, event hooks.Event) error { events = append(events, event); return nil }, hooks.NameMemorySyncCompleted); err != nil {
		t.Fatal(err)
	}
	result, err := NewMemoryWithHooks("test", false, d).Sync(context.Background(), config.Options{ProjectDir: workingDirectory, ProjectLocal: true})
	if err != nil || result.Status != memory.SyncStatusUnavailable || len(events) != 1 || events[0].Subject().ID() != wantProject {
		t.Fatalf("sync result=%+v err=%v events=%d wantProject=%q", result, err, len(events), wantProject)
	}
}

func TestCanonicalInvocationWorkspaceCanonicalizesRelativeAndSymlinkPaths(t *testing.T) {
	workspace, err := os.MkdirTemp(".", "canonical-workspace-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDirectory, workspace)
	if err != nil {
		t.Fatal(err)
	}
	fromRelative, err := canonicalInvocationWorkspace(relative)
	if err != nil {
		t.Fatal(err)
	}
	fromAbsolute, err := canonicalInvocationWorkspace(workspace)
	if err != nil || fromRelative != fromAbsolute {
		t.Fatalf("relative=%q absolute=%q err=%v", fromRelative, fromAbsolute, err)
	}
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(workspace, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	fromLink, err := canonicalInvocationWorkspace(link)
	if err != nil || fromLink != fromAbsolute {
		t.Fatalf("link=%q absolute=%q err=%v", fromLink, fromAbsolute, err)
	}
}

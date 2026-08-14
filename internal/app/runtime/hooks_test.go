package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestMemoryHooksConstructors(t *testing.T) {
	dispatcher := hooks.New()
	if got := NewMemoryWithHooks("test", false, dispatcher); got.hooks != dispatcher {
		t.Fatal("hook emitter not retained")
	}
	if got := NewSDDWithHooks(dispatcher); got.hooks != dispatcher {
		t.Fatal("SDD hook emitter not retained")
	}
}

func TestSDDHooksLifecycle(t *testing.T) {
	d := hooks.New()
	var events []hooks.Event
	if err := d.Register("sdd", func(_ context.Context, e hooks.Event) error { events = append(events, e); return nil }, hooks.NameChangeCreated, hooks.NameRevisionAccepted, hooks.NameProjectionRecorded, hooks.NameChangeTransitioned); err != nil {
		t.Fatal(err)
	}
	r := NewSDDWithHooks(d)
	opts := config.Options{StorageRoot: t.TempDir()}
	change, err := r.CreateChange(context.Background(), opts, sdd.CreateChangeRequest{Project: "project", IdempotencyKey: "hooks-sdd-1", Title: "Change", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := r.SaveRevision(context.Background(), opts, sdd.SaveRevisionRequest{Project: "project", ChangeID: change.ID, Artifact: sdd.PhaseExplore, Content: []byte("secret-content"), ExpectedStateVersion: change.StateVersion})
	if err != nil || len(events) != 1 {
		t.Fatalf("save=%v events=%d", err, len(events))
	}
	accepted, err := r.AcceptRevision(context.Background(), opts, sdd.AcceptRevisionRequest{Project: "project", ChangeID: change.ID, RevisionID: revision.ID, ExpectedStateVersion: revision.StateVersion})
	if err != nil {
		t.Fatal(err)
	}
	location, err := sdd.OpenSpecProjectionPath(change.ID, accepted.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := r.RecordProjection(context.Background(), opts, sdd.RecordProjectionRequest{Project: "project", ChangeID: change.ID, ArtifactID: accepted.ArtifactID, RevisionID: accepted.ID, Status: sdd.ProjectionCurrent, Digest: accepted.Digest, Location: location, ExpectedStateVersion: accepted.StateVersion})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := r.TransitionChange(context.Background(), opts, sdd.TransitionChangeRequest{Project: "project", ChangeID: change.ID, TargetPhase: sdd.PhaseProposal, ExpectedStateVersion: projection.StateVersion})
	if err != nil || len(events) != 4 {
		t.Fatalf("transition=%v events=%d", err, len(events))
	}
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"secret-content", "title", "content", "inputs", "externalLocation", "location"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("leak %q", secret)
			}
		}
	}
	if events[0].Name() != hooks.NameChangeCreated || events[0].Subject().ID() != change.ID {
		t.Fatal("change event mapping")
	}
	if got, ok := events[1].Revision(); !ok || got.ChangeID() != accepted.ChangeID || got.ArtifactID() != accepted.ArtifactID || got.Digest() != string(accepted.Digest) || got.InputDigest() != string(accepted.InputDigest) || got.StateVersion() != accepted.StateVersion {
		t.Fatal("revision event mapping")
	}
	if got, ok := events[2].Projection(); !ok || got.ChangeID() != projection.ChangeID || got.ArtifactID() != projection.ArtifactID || got.RevisionID() != projection.RevisionID || got.Digest() != string(projection.Digest) || got.StateVersion() != projection.StateVersion {
		t.Fatal("projection event mapping")
	}
	if got, ok := events[3].Change(); !ok || got.Phase() != string(transition.Phase) || got.Status() != string(transition.Status) || got.StateVersion() != transition.StateVersion {
		t.Fatal("transition event mapping")
	}
	if _, err := r.CreateChange(context.Background(), opts, sdd.CreateChangeRequest{}); err == nil || len(events) != 4 {
		t.Fatal("invalid create emitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.CreateChange(ctx, opts, sdd.CreateChangeRequest{Project: "project", IdempotencyKey: "hooks-sdd-canceled", Title: "Change", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium}); err == nil || len(events) != 4 {
		t.Fatal("canceled create emitted")
	}
}

func TestSDDHooksPanickingEmitterDoesNotChangeCreate(t *testing.T) {
	result, err := NewSDDWithHooks(panickingEmitter{}).CreateChange(context.Background(), config.Options{StorageRoot: t.TempDir()}, sdd.CreateChangeRequest{Project: "project", IdempotencyKey: "hooks-sdd-panic", Title: "Change", Backend: sdd.BackendMemory, InteractionMode: sdd.InteractionAutomatic, Plan: sdd.PlanMedium})
	if err != nil || result.ID == "" || result.StateVersion < 1 {
		t.Fatalf("result=%+v err=%v", result, err)
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

package pi_test

import (
	"context"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/providers/pi/tools"
)

func TestDispatcherRejectsMissingAndUnknownPayloads(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, true)
	if _, err := d.Dispatch(context.Background(), pi.Request{Operation: "memory.recall"}); err == nil {
		t.Fatal("missing payload accepted")
	}
	if _, err := d.Dispatch(context.Background(), pi.Request{Operation: "unknown"}); err == nil {
		t.Fatal("unknown operation accepted")
	}
}

func TestOperationCatalogIsCompleteAndClosed(t *testing.T) {
	want := []string{
		"model.resolve", "memory.remember", "memory.recall", "memory.recent", "memory.get", "memory.forget", "memory.project.resolve", "memory.project.initialize", "memory.sync.configure", "memory.sync.status", "memory.sync", "memory.sync.backfill", "memory.sync.repair_project", "memory.sync.reseed", "memory.sync.rejoin", "memory.session.start", "memory.session.checkpoint", "memory.session.renew", "memory.session.end", "memory.session.context", "memory.session.draft_save",
	}
	got := tools.OperationNames()
	if len(got) != len(want) {
		t.Fatalf("operation count=%d want=%d: %v", len(got), len(want), got)
	}
	for index, operation := range want {
		if got[index] != operation {
			t.Fatalf("operation[%d]=%q want=%q", index, got[index], operation)
		}
	}
}

func TestDispatcherRejectsForeignProjectAndUnknownOperationFields(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, false)
	for _, request := range []pi.Request{
		{Operation: "memory.recall", Payload: []byte(`{"project":"foreign","query":"x"}`)},
		{Operation: "memory.recall", Payload: []byte(`{"query":"x","unexpected":true}`)},
	} {
		if _, err := d.Dispatch(context.Background(), request); err == nil {
			t.Fatalf("accepted unbound payload %s", request.Payload)
		}
	}
}

func TestDispatcherUsesBoundProjectForMemoryAndManagerSDD(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, false)
	ctx := context.Background()
	if _, err := d.Dispatch(ctx, pi.Request{Role: "manager", Operation: "memory.remember", Payload: []byte(`{"title":"fixture","content":"bound project memory"}`)}); err != nil {
		t.Fatalf("remember: %v", err)
	}
	result, err := d.Dispatch(ctx, pi.Request{Role: "manager", Operation: "memory.recall", Payload: []byte(`{"query":"bound project"}`)})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if entries, ok := result.([]memory.Entry); !ok || len(entries) != 1 || entries[0].Project == "" {
		t.Fatalf("bound recall=%#v", result)
	}
	if _, err := d.Dispatch(ctx, pi.Request{Role: "general", Operation: "sdd.create", Payload: []byte(`{"idempotencyKey":"pi-general","title":"denied","backend":"memory","interactionMode":"automatic","plan":"low"}`)}); err == nil {
		t.Fatal("non-manager SDD create was accepted")
	}
	if _, err := d.Dispatch(ctx, pi.Request{Role: "manager", Operation: "sdd.create", Payload: []byte(`{"idempotencyKey":"pi-manager","title":"accepted","backend":"memory","interactionMode":"automatic","plan":"low"}`)}); err == nil {
		t.Fatal("retired manager SDD create accepted")
	}
}

func TestSyncConfigureRejectsBearerAndRequiresHostCredentialFile(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, false)
	for _, payload := range [][]byte{[]byte(`{"endpoint":"https://sync.example.test","deviceId":"device","bearer":"secret"}`), []byte(`{"endpoint":"https://sync.example.test","deviceId":"device"}`)} {
		if _, err := d.Dispatch(context.Background(), pi.Request{Role: "manager", Operation: "memory.sync.configure", Payload: payload}); err == nil {
			t.Fatalf("unsafe or unreachable configure payload accepted: %s", payload)
		}
	}
}

func TestDispatcherRejectsPersonalScope(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, false)
	if _, err := d.Dispatch(context.Background(), pi.Request{Role: "manager", Operation: "memory.remember", Payload: []byte(`{"title":"no","content":"no","scope":"personal"}`)}); err == nil {
		t.Fatal("personal scope accepted")
	}
}

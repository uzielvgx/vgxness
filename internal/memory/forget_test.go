package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestNativeMemoryRememberRecallGetForgetLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewMemoryService(store, "test", nil)
	remembered, err := service.Remember(ctx, Remember{Content: "native searchable token", Project: "project", Scope: ScopeProject, TopicKey: "native/lifecycle"})
	if err != nil || remembered.ID == "" {
		t.Fatalf("remember: entry=%#v err=%v", remembered, err)
	}
	found, err := service.Recall(ctx, Recall{Query: "searchable", Project: "project", Scope: ScopeProject})
	if err != nil || len(found) != 1 || found[0].ID != remembered.ID {
		t.Fatalf("recall: entries=%#v err=%v", found, err)
	}
	forgotten, err := service.Forget(ctx, Forget{ID: remembered.ID, Project: "project", Scope: ScopeProject})
	if err != nil || forgotten.State != StateArchived {
		t.Fatalf("forget: entry=%#v err=%v", forgotten, err)
	}
	found, err = service.Recall(ctx, Recall{Query: "searchable", Project: "project", Scope: ScopeProject, States: []State{StateArchived}})
	if err != nil || len(found) != 0 {
		t.Fatalf("forgotten entry remained in FTS: entries=%#v err=%v", found, err)
	}
	archived, err := service.Get(ctx, Lookup{ID: remembered.ID, Project: "project", Scope: ScopeProject})
	if err != nil || archived.State != StateArchived || archived.Content != remembered.Content {
		t.Fatalf("archived row compatibility: entry=%#v err=%v", archived, err)
	}
	again, err := service.Forget(ctx, Forget{ID: remembered.ID, Project: "project", Scope: ScopeProject})
	if err != nil || again.State != StateArchived {
		t.Fatalf("idempotent forget: entry=%#v err=%v", again, err)
	}
}

func TestNativeMemoryForgetValidatesBoundaryBeforeStorage(t *testing.T) {
	service := NewMemoryService(&fakeMemoryStore{}, "test", nil)
	_, err := service.Forget(context.Background(), Forget{ID: "id", Project: "project", Scope: "foreign"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid forget boundary: %v", err)
	}
}

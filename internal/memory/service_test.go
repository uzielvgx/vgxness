package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

type fakeMemoryStore struct {
	saved  Observation
	search Search
	items  []Observation
	item   Observation
	err    error
	calls  int
}

func (f *fakeMemoryStore) Save(_ context.Context, item Observation) (Observation, error) {
	f.calls++
	f.saved = item
	return item, f.err
}
func (f *fakeMemoryStore) Search(_ context.Context, query Search) ([]Observation, error) {
	f.calls++
	f.search = query
	return f.items, f.err
}
func (f *fakeMemoryStore) Get(context.Context, string, string, Scope) (Observation, error) {
	f.calls++
	return f.item, f.err
}

func TestMemoryService_DelegatesValidatedCommandsAndCancellation(t *testing.T) {
	store := &fakeMemoryStore{}
	service := NewMemoryService(store, "cli", nil)
	_, err := service.Save(context.Background(), SaveRequest{Content: "valid"})
	testutil.Require(t, err == nil && store.calls == 1 && store.saved.Content == "valid", "save delegation: %+v %v", store, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Search(ctx, SearchRequest{Query: "valid", Project: "p", Scope: ScopeProject})
	testutil.Require(t, errors.Is(err, context.Canceled) && store.calls == 1, "cancelled call reached store: %v", err)
}

func TestMemoryService_SaveDefaultsAndCallerFieldBoundary(t *testing.T) {
	store := &fakeMemoryStore{}
	_, err := NewMemoryService(store, "cli", nil).Save(context.Background(), SaveRequest{Title: "Title", Content: "body", Session: "s", TopicKey: "topic"})
	got := store.saved
	testutil.Require(t, err == nil && got.Title == "Title" && got.Project == "default" && got.Scope == ScopeProject && got.Type == "learning" && got.State == StateActive && got.Provenance == (Provenance{Producer: "cli"}) && got.Session == "s" && got.TopicKey == "topic", "defaults/boundary: %+v %v", got, err)
}

func TestMemoryService_GetMissingHidesForeignMetadata(t *testing.T) {
	store := &fakeMemoryStore{err: errors.Join(ErrNotFound, errors.New("observation missing"))}
	_, err := NewMemoryService(store, "cli", nil).Get(context.Background(), GetRequest{ID: "foreign-secret", Project: "p", Scope: ScopeProject})
	testutil.Require(t, errors.Is(err, ErrNotFound) && !strings.Contains(err.Error(), "foreign-secret"), "missing get leaked metadata: %v", err)
}

func TestMemoryService_SearchRejectsUnsafeFTSBeforeStore(t *testing.T) {
	for _, query := range []string{"", `"broken`, "a OR b", "topic:*", "a-b"} {
		store := &fakeMemoryStore{}
		_, err := NewMemoryService(store, "cli", nil).Search(context.Background(), SearchRequest{Query: query, Project: "p", Scope: ScopeProject})
		testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "query %q reached store: %v", query, err)
	}
}

func TestMemoryService_SearchPreviewBudgetsAreDeterministic(t *testing.T) {
	items := make([]Observation, 20)
	for i := range items {
		items[i] = observation(string(rune('a'+i)), "p", strings.Repeat("x", 400))
	}
	store := &fakeMemoryStore{items: items}
	service := NewMemoryService(store, "cli", nil)
	first, err := service.Search(context.Background(), SearchRequest{Query: "token", Project: "p", Scope: ScopeProject})
	second, err2 := service.Search(context.Background(), SearchRequest{Query: "token", Project: "p", Scope: ScopeProject})
	total := 0
	for _, item := range first {
		testutil.Require(t, len([]rune(item.Preview)) <= 256 && item.Content == "", "unbounded preview/content: %+v", item)
		total += len([]rune(item.Preview))
	}
	testutil.Require(t, err == nil && err2 == nil && store.search.Limit == 20 && total <= 4096 && len(first) == len(second) && first[19].Preview == second[19].Preview, "budget/order mismatch: %d %+v %+v", total, first, second)
}

func TestMemoryService_SourceClaimsRequireTrustedVerification(t *testing.T) {
	store := &fakeMemoryStore{}
	for _, request := range []SaveRequest{{Content: "paired", SourceProvider: "chronicle", SourceID: "event-1"}, {Content: "unpaired", SourceProvider: "chronicle"}} {
		_, err := NewMemoryService(store, "cli", nil).Save(context.Background(), request)
		testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "untrusted claim reached store: %+v %v", store.saved, err)
	}
	verify := func(_ context.Context, provider, id string) error {
		if provider == "chronicle" && id == "event-1" {
			return nil
		}
		return errors.New("unverified")
	}
	trusted := NewMemoryService(store, "trusted-agent", verify)
	_, err := trusted.Save(context.Background(), SaveRequest{Content: "correlation", SourceProvider: "chronicle", SourceID: "event-1"})
	testutil.Require(t, err == nil && store.calls == 1 && store.saved.Provenance == (Provenance{Producer: "trusted-agent", SourceProvider: "chronicle", SourceID: "event-1"}), "verified correlation rejected: %+v %v", store.saved, err)
	_, err = trusted.Save(context.Background(), SaveRequest{Content: "forged", SourceProvider: "chronicle", SourceID: "event-2"})
	testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 1, "unverified correlation reached store: %+v %v", store.saved, err)
}

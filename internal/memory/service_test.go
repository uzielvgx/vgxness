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
	recent Recent
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
func (f *fakeMemoryStore) Recent(_ context.Context, request Recent) ([]Observation, error) {
	f.calls++
	f.recent = request
	return f.items, f.err
}

func TestMemoryService_DelegatesValidatedCommandsAndCancellation(t *testing.T) {
	store := &fakeMemoryStore{}
	service := NewMemoryService(store, "cli", nil)
	_, err := service.Remember(context.Background(), Remember{Content: "valid"})
	testutil.Require(t, err == nil && store.calls == 1 && store.saved.Content == "valid", "save delegation: %+v %v", store, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Recall(ctx, Recall{Query: "valid", Project: "p", Scope: ScopeProject})
	testutil.Require(t, errors.Is(err, context.Canceled) && store.calls == 1, "cancelled call reached store: %v", err)
}

func TestMemoryService_RememberDefaultsAndCallerFieldBoundary(t *testing.T) {
	store := &fakeMemoryStore{}
	_, err := NewMemoryService(store, "cli", nil).Remember(context.Background(), Remember{Title: "Title", Content: "body", Session: "s", TopicKey: "topic", References: []string{"prior"}})
	got := store.saved
	testutil.Require(t, err == nil && got.Title == "Title" && got.Project == "default" && got.Scope == ScopeProject && got.Type == "learning" && got.State == StateActive && got.Provenance == (Provenance{Producer: "cli"}) && got.Session == "s" && got.TopicKey == "topic" && len(got.References) == 1 && got.References[0] == "prior", "defaults/boundary: %+v %v", got, err)
}

func TestMemoryService_RememberRejectsWhitespaceOnlyTitleBeforeStore(t *testing.T) {
	for _, test := range []struct {
		name, title string
		calls       int
	}{
		{"empty remains compatible", "", 1},
		{"whitespace", " \t", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeMemoryStore{}
			_, err := NewMemoryService(store, "cli", nil).Remember(context.Background(), Remember{Title: test.title, Content: "body"})
			if test.calls == 0 {
				testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "whitespace title reached store: %q %v", test.title, err)
				return
			}
			testutil.Require(t, err == nil && store.calls == 1, "empty title no longer compatible: %v", err)
		})
	}
}

func TestMemoryService_AllowsGovernedNeedsReviewLifecycle(t *testing.T) {
	store := &fakeMemoryStore{}
	service := NewMemoryService(store, "controlplane", nil)
	_, err := service.Remember(context.Background(), Remember{Content: "candidate", State: StateNeedsReview})
	testutil.Require(t, err == nil && store.saved.State == StateNeedsReview, "needs-review save: %+v %v", store.saved, err)
	_, err = service.Recall(context.Background(), Recall{
		Query: "candidate", Project: "p", Scope: ScopeProject, States: []State{StateActive, StateNeedsReview},
	})
	testutil.Require(t, err == nil && len(store.search.States) == 2 && store.search.States[1] == StateNeedsReview, "governed states: %+v %v", store.search, err)
	_, err = service.Remember(context.Background(), Remember{Content: "candidate", State: StateArchived})
	testutil.Require(t, errors.Is(err, ErrInvalid), "caller created archived observation: %v", err)
}

func TestMemoryService_GetMissingHidesForeignMetadata(t *testing.T) {
	store := &fakeMemoryStore{err: errors.Join(ErrNotFound, errors.New("observation missing"))}
	_, err := NewMemoryService(store, "cli", nil).Get(context.Background(), Lookup{ID: "foreign-secret", Project: "p", Scope: ScopeProject})
	testutil.Require(t, errors.Is(err, ErrNotFound) && !strings.Contains(err.Error(), "foreign-secret"), "missing get leaked metadata: %v", err)
}

func TestMemoryService_SearchRejectsEmptyQueryBeforeStore(t *testing.T) {
	store := &fakeMemoryStore{}
	_, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{Query: "", Project: "p", Scope: ScopeProject})
	testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "empty query reached store: %v", err)
}

func TestMemoryService_SearchQuotesOperatorsAndConservativeTerms(t *testing.T) {
	for _, test := range []struct {
		name, query, want string
		matchAny          bool
	}{
		{"all", "alpha and beta", `"alpha" "and" "beta"`, false},
		{"any", "alpha OR beta", `"alpha" OR "OR" OR "beta"`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeMemoryStore{}
			_, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{Query: test.query, Project: "p", Scope: ScopeProject, MatchAny: test.matchAny})
			testutil.Require(t, err == nil && store.search.Query == test.want, "query=%q want=%q err=%v", store.search.Query, test.want, err)
		})
	}
}

func TestMemoryService_SearchNeutralizesFTSOperators(t *testing.T) {
	for _, test := range []struct{ query, want string }{{`"broken`, `"broken"`}, {"topic:*", `"topic"`}} {
		store := &fakeMemoryStore{}
		_, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{Query: test.query, Project: "p", Scope: ScopeProject})
		testutil.Require(t, err == nil && store.search.Query == test.want, "query %q parsed as %q: %v", test.query, store.search.Query, err)
	}
}

func TestMemoryService_SearchParsesHumanPunctuationSafely(t *testing.T) {
	for _, test := range []struct {
		query, want string
	}{
		{"slice 9/10", `"slice" "9" "10"`},
		{"Alpha.4", `"Alpha" "4"`},
		{"cold-start", `"cold" "start"`},
	} {
		store := &fakeMemoryStore{}
		_, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{Query: test.query, Project: "p", Scope: ScopeProject})
		testutil.Require(t, err == nil && store.calls == 1 && store.search.Query == test.want, "query %q parsed as %q: %v", test.query, store.search.Query, err)
	}
	store := &fakeMemoryStore{}
	_, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{Query: "---/...", Project: "p", Scope: ScopeProject})
	testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "punctuation-only query reached store: %v", err)
}

func TestMemoryService_RecallFindsQuotedAndTermInStore(t *testing.T) {
	store := openTestStore(t)
	mustSave(t, store, observation("legacy", "p", "legacy migration decision"))
	entries, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{Query: "legacy and migration", Project: "p", Scope: ScopeProject, MatchAny: true})
	testutil.Require(t, err == nil && len(entries) == 1 && entries[0].ID == "legacy", "entries=%+v err=%v", entries, err)
}

func TestMemoryService_SearchPreviewBudgetsAreDeterministic(t *testing.T) {
	items := make([]Observation, 20)
	for i := range items {
		items[i] = observation(string(rune('a'+i)), "p", strings.Repeat("x", 400))
	}
	store := &fakeMemoryStore{items: items}
	service := NewMemoryService(store, "cli", nil)
	first, err := service.Recall(context.Background(), Recall{Query: "token", Project: "p", Scope: ScopeProject})
	second, err2 := service.Recall(context.Background(), Recall{Query: "token", Project: "p", Scope: ScopeProject})
	total := 0
	for _, item := range first {
		testutil.Require(t, len([]rune(item.Preview)) <= 256 && item.Content == "", "unbounded preview/content: %+v", item)
		total += len([]rune(item.Preview))
	}
	testutil.Require(t, err == nil && err2 == nil && store.search.Limit == 20 && total <= 4096 && len(first) == len(second) && first[19].Preview == second[19].Preview, "budget/order mismatch: %d %+v %+v", total, first, second)
}

func TestMemoryService_SearchCanUseBoundedAnyTermMatching(t *testing.T) {
	store := &fakeMemoryStore{}
	_, err := NewMemoryService(store, "cli", nil).Recall(context.Background(), Recall{
		Query: "architecture reliability", Project: "p", Scope: ScopeProject, MatchAny: true,
	})
	testutil.Require(t, err == nil && store.search.Query == `"architecture" OR "reliability"`, "match-any query=%q err=%v", store.search.Query, err)
}

func TestMemoryService_RecentDefaultsAndBoundsPreviews(t *testing.T) {
	store := &fakeMemoryStore{items: []Observation{
		observation("a", "p", strings.Repeat("x", 400)),
		observation("b", "p", strings.Repeat("y", 400)),
	}}
	results, err := NewMemoryService(store, "cli", nil).Recent(context.Background(), Recent{Project: "p", Scope: ScopeProject})
	testutil.Require(t, err == nil && store.recent.Limit == 20 && len(store.recent.States) == 1 && store.recent.States[0] == StateActive, "recent defaults: %+v %v", store.recent, err)
	testutil.Require(t, len(results) == 2 && len([]rune(results[0].Preview)) == previewLimit && results[0].Content == "", "recent shape: %+v", results)

	_, err = NewMemoryService(store, "cli", nil).Recent(context.Background(), Recent{Project: "p", Scope: ScopeProject, Limit: 51})
	testutil.Require(t, errors.Is(err, ErrInvalid), "recent accepted excessive limit: %v", err)
}

func TestMemoryService_RememberRejectsInvalidReferencesBeforeStore(t *testing.T) {
	for _, references := range [][]string{{""}, {"same", "same"}} {
		store := &fakeMemoryStore{}
		_, err := NewMemoryService(store, "cli", nil).Remember(context.Background(), Remember{Content: "body", References: references})
		testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "invalid references reached store: %#v err=%v", references, err)
	}
}

func TestMemoryService_SourceClaimsRequireTrustedVerification(t *testing.T) {
	store := &fakeMemoryStore{}
	for _, request := range []Remember{{Content: "paired", SourceProvider: "chronicle", SourceID: "event-1"}, {Content: "unpaired", SourceProvider: "chronicle"}} {
		_, err := NewMemoryService(store, "cli", nil).Remember(context.Background(), request)
		testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 0, "untrusted claim reached store: %+v %v", store.saved, err)
	}
	verify := func(_ context.Context, provider, id string) error {
		if provider == "chronicle" && id == "event-1" {
			return nil
		}
		return errors.New("unverified")
	}
	trusted := NewMemoryService(store, "trusted-agent", verify)
	_, err := trusted.Remember(context.Background(), Remember{Content: "correlation", SourceProvider: "chronicle", SourceID: "event-1"})
	testutil.Require(t, err == nil && store.calls == 1 && store.saved.Provenance == (Provenance{Producer: "trusted-agent", SourceProvider: "chronicle", SourceID: "event-1"}), "verified correlation rejected: %+v %v", store.saved, err)
	_, err = trusted.Remember(context.Background(), Remember{Content: "forged", SourceProvider: "chronicle", SourceID: "event-2"})
	testutil.Require(t, errors.Is(err, ErrInvalid) && store.calls == 1, "unverified correlation reached store: %+v %v", store.saved, err)
}

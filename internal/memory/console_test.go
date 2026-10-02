package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uzielvgx/vgxness/internal/testutil"
)

func TestCountActiveIgnoresOtherProjectsAndForgottenEntries(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	service := NewMemoryService(store, "test", nil)
	var kept Entry
	for index, request := range []Remember{
		{Title: "a", Content: "first fact", Project: "p"},
		{Title: "b", Content: "second fact", Project: "p"},
		{Title: "c", Content: "other project", Project: "q"},
	} {
		entry, err := service.Remember(ctx, request)
		testutil.NoError(t, err)
		if index == 0 {
			kept = entry
		}
	}
	count, err := store.CountActive(ctx, "p")
	testutil.Require(t, err == nil && count == 2, "count=%d err=%v", count, err)
	_, err = service.Forget(ctx, Forget{ID: kept.ID, Project: "p", Scope: ScopeProject})
	testutil.NoError(t, err)
	count, err = store.CountActive(ctx, "p")
	testutil.Require(t, err == nil && count == 1, "after forget count=%d err=%v", count, err)
	count, err = store.CountActive(ctx, "empty")
	testutil.Require(t, err == nil && count == 0, "empty project count=%d err=%v", count, err)
	_, err = store.CountActive(ctx, " ")
	testutil.Require(t, errors.Is(err, ErrInvalid), "blank project err=%v", err)
}

func TestSessionHandoffsListCompletedSummariesNewestFirst(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	clock := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	complete := func(external, summary string) ProviderSession {
		t.Helper()
		started, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "claude-code", ExternalID: external})
		testutil.NoError(t, err)
		closed, err := store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: external, LeaseToken: started.LeaseToken, State: ProviderSessionCompleted, Summary: summary})
		testutil.NoError(t, err)
		return closed
	}
	first := complete("s1", "first handoff")
	second := complete("s2", "second handoff")
	open, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "claude-code", ExternalID: "s3"})
	testutil.NoError(t, err)
	_, err = store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: open.Handle, ExternalID: "s3", LeaseToken: open.LeaseToken, State: ProviderSessionInterrupted})
	testutil.NoError(t, err)

	handoffs, err := store.SessionHandoffs(ctx, "p", 0)
	testutil.Require(t, err == nil && len(handoffs) == 2, "handoffs=%+v err=%v", handoffs, err)
	testutil.Require(t, handoffs[0].Handle == second.Handle && handoffs[0].Summary == "second handoff" && handoffs[1].Handle == first.Handle, "order=%+v", handoffs)
	testutil.Require(t, handoffs[0].Provider == "claude-code" && handoffs[0].ObservationID == second.FinalObservationID && !handoffs[0].CompletedAt.Before(handoffs[0].StartedAt), "fields=%+v", handoffs[0])

	limited, err := store.SessionHandoffs(ctx, "p", 1)
	testutil.Require(t, err == nil && len(limited) == 1 && limited[0].Handle == second.Handle, "limited=%+v err=%v", limited, err)
	other, err := store.SessionHandoffs(ctx, "q", 5)
	testutil.Require(t, err == nil && len(other) == 0, "other project=%+v err=%v", other, err)
	for _, limit := range []int{-1, maxSessionHandoffs + 1} {
		_, err := store.SessionHandoffs(ctx, "p", limit)
		testutil.Require(t, errors.Is(err, ErrInvalid), "limit %d err=%v", limit, err)
	}
}

func TestUserVersionReportsTheMigrationHead(t *testing.T) {
	store := openTestStore(t)
	version, err := store.UserVersion(context.Background())
	testutil.Require(t, err == nil && version == SchemaVersion(), "version=%d head=%d err=%v", version, SchemaVersion(), err)
}

func TestTypeCountsOrderByUse(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	service := NewMemoryService(store, "test", nil)
	for _, request := range []Remember{
		{Title: "a", Content: "one", Type: "decision", Project: "p"},
		{Title: "b", Content: "two", Type: "decision", Project: "p"},
		{Title: "c", Content: "three", Type: "bugfix", Project: "p"},
		{Title: "d", Content: "four", Type: "bugfix", Project: "q"},
	} {
		_, err := service.Remember(ctx, request)
		testutil.NoError(t, err)
	}
	counts, err := store.TypeCounts(ctx, "p")
	testutil.Require(t, err == nil && len(counts) == 2 && counts[0] == (TypeCount{Type: "decision", Count: 2}) && counts[1] == (TypeCount{Type: "bugfix", Count: 1}), "counts=%+v err=%v", counts, err)
}

func TestSyncSummaryReportsBindingAndPendingMutations(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	workspace := t.TempDir()
	summary, err := store.SyncSummary(ctx, workspace)
	testutil.Require(t, err == nil && summary.PortableID == "" && summary.Pending == 0 && summary.LastPull.IsZero(), "fresh summary=%+v err=%v", summary, err)
	enableSync(t, store)
	project, err := store.ResolveProject(ctx, workspace)
	testutil.NoError(t, err)
	_, err = NewMemoryService(store, "test", nil).Remember(ctx, Remember{Title: "a", Content: "queued fact", Project: project})
	testutil.NoError(t, err)
	summary, err = store.SyncSummary(ctx, workspace)
	testutil.Require(t, err == nil && summary.Pending > 0 && summary.PortableID == "", "unbound summary=%+v err=%v", summary, err)
}

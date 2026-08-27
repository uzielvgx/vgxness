package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/testutil"
)

func TestProviderSessionRejectsBlankInputsWithoutResidue(t *testing.T) {
	store := openTestStore(t)
	for _, request := range []ProviderSessionStart{{Project: " ", Provider: "openai", ExternalID: "run"}, {Project: "p", Provider: " ", ExternalID: "run"}, {Project: "p", Provider: "openai", ExternalID: " "}} {
		_, err := store.StartProviderSession(context.Background(), request)
		testutil.Require(t, errors.Is(err, ErrInvalid), "start %+v: %v", request, err)
	}
	var sessions, projects int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM local_provider_sessions`).Scan(&sessions))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects))
	testutil.Require(t, sessions == 0 && projects == 0, "invalid input left sessions=%d projects=%d", sessions, projects)
}

func TestProviderSessionCompletedSummaryIsSanitizedAndSynced(t *testing.T) {
	store := openTestStore(t)
	enableSync(t, store)
	ctx := context.Background()
	started, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: " OpenAI ", ExternalID: "remote-run"})
	testutil.NoError(t, err)
	marked, err := store.MarkProviderSessionCheckpoint(ctx, "p", started.Handle)
	testutil.Require(t, err == nil && marked.Checkpointed, "checkpoint=%+v err=%v", marked, err)
	_, err = store.MarkProviderSessionCheckpoint(ctx, "other", started.Handle)
	testutil.Require(t, errors.Is(err, ErrNotFound), "cross-project checkpoint=%v", err)
	closed, err := store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: "remote-run", State: ProviderSessionCompleted, Summary: "durable handoff"})
	testutil.Require(t, err == nil && closed.State == ProviderSessionCompleted && closed.FinalObservationID != "", "close=%+v err=%v", closed, err)
	item, err := store.Get(ctx, closed.FinalObservationID, "p", ScopeProject)
	testutil.Require(t, err == nil && item.Session == "" && item.Type == "summary" && item.TopicKey == providerSessionSummaryTopic(item.ID) && item.Provenance == (Provenance{Producer: "provider-session"}) && item.State == StateActive, "summary=%+v err=%v", item, err)
	item.Content = "mutated"
	_, err = store.Update(ctx, item)
	testutil.Require(t, errors.Is(err, ErrConflict), "summary Store.Update=%v", err)
	_, err = store.UpdateObservation(ctx, ObservationUpdate{ID: item.ID, Project: item.Project, ExpectedUpdatedAt: item.UpdatedAt, Content: "mutated"})
	testutil.Require(t, errors.Is(err, ErrConflict), "summary UpdateObservation=%v", err)
	var hash []byte
	testutil.NoError(t, store.db.QueryRow(`SELECT external_id_hash FROM local_provider_sessions WHERE handle=?`, started.Handle).Scan(&hash))
	testutil.Require(t, len(hash) == 32 && string(hash) != "remote-run", "raw external id persisted")
	rows, err := store.db.Query(`SELECT record_kind,payload FROM sync_outbox ORDER BY id`)
	testutil.NoError(t, err)
	defer rows.Close()
	var payloads strings.Builder
	for rows.Next() {
		var kind string
		var payload []byte
		testutil.NoError(t, rows.Scan(&kind, &payload))
		testutil.Require(t, kind == "project" || kind == "observation", "local session entered sync as %q", kind)
		payloads.Write(payload)
	}
	testutil.NoError(t, rows.Err())
	testutil.Require(t, !strings.Contains(payloads.String(), "remote-run") && !strings.Contains(payloads.String(), started.Handle), "identity entered sync: %s", payloads.String())
}

func TestProviderSessionEndValidatesIdentitySummaryAndAtomicity(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	started, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "openai", ExternalID: "external"})
	testutil.NoError(t, err)
	for _, request := range []ProviderSessionEnd{
		{Project: "p", Handle: started.Handle, ExternalID: "wrong", State: ProviderSessionCompleted, Summary: "ok"},
		{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionCompleted, Summary: "uses external"},
		{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionCompleted, Summary: "uses " + started.Handle},
		{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionInterrupted, Summary: "not allowed"},
	} {
		_, err := store.EndProviderSession(ctx, request)
		testutil.Require(t, errors.Is(err, ErrInvalid), "end %+v: %v", request, err)
	}
	var state string
	var observations int
	testutil.NoError(t, store.db.QueryRow(`SELECT state FROM local_provider_sessions WHERE handle=?`, started.Handle).Scan(&state))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM observations`).Scan(&observations))
	testutil.Require(t, state == string(ProviderSessionActive) && observations == 0, "partial close state=%q observations=%d", state, observations)
	closed, err := store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionCompleted, Summary: "ok"})
	testutil.NoError(t, err)
	repeated, err := store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionCompleted, Summary: "ok"})
	testutil.Require(t, err == nil && repeated.FinalObservationID == closed.FinalObservationID, "repeat=%+v err=%v", repeated, err)
	_, err = store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionCancelled})
	testutil.Require(t, errors.Is(err, ErrConflict), "incompatible close=%v", err)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.EndProviderSession(cancelled, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: "external", State: ProviderSessionCompleted, Summary: "ok"})
	testutil.Require(t, errors.Is(err, context.Canceled), "cancelled close=%v", err)
}

func TestProviderSessionTerminalHandoffAndIsolation(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	prior, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "openai", ExternalID: "prior"})
	testutil.NoError(t, err)
	_, err = store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: prior.Handle, ExternalID: "prior", State: ProviderSessionCompleted, Summary: strings.Repeat("x", 4096)})
	testutil.NoError(t, err)
	for _, state := range []ProviderSessionState{ProviderSessionInterrupted, ProviderSessionCancelled} {
		started, startErr := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "openai", ExternalID: string(state)})
		testutil.NoError(t, startErr)
		closed, closeErr := store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: string(state), State: state})
		testutil.Require(t, closeErr == nil && closed.FinalObservationID == "", "terminal=%+v err=%v", closed, closeErr)
	}
	current, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "openai", ExternalID: "current"})
	testutil.NoError(t, err)
	handoff, err := store.ProviderSessionContext(ctx, "p", current.Handle)
	testutil.Require(t, err == nil && strings.HasPrefix(handoff.Handoff, "UNTRUSTED DATA\n") && len([]rune(handoff.Handoff)) == 4096, "handoff=%+v err=%v", handoff, err)
	other, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "other", Provider: "openai", ExternalID: "current"})
	testutil.NoError(t, err)
	isolation, err := store.ProviderSessionContext(ctx, "other", other.Handle)
	testutil.Require(t, err == nil && isolation.Handoff == "", "isolation=%+v err=%v", isolation, err)
}

func TestProviderSessionRepeatedCompletionAndOptimisticUpdate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ids := make(map[string]bool)
	for _, external := range []string{"one", "two"} {
		started, err := store.StartProviderSession(ctx, ProviderSessionStart{Project: "p", Provider: "openai", ExternalID: external})
		testutil.NoError(t, err)
		closed, err := store.EndProviderSession(ctx, ProviderSessionEnd{Project: "p", Handle: started.Handle, ExternalID: external, State: ProviderSessionCompleted, Summary: "durable handoff"})
		testutil.Require(t, err == nil && !ids[closed.FinalObservationID], "close=%+v err=%v", closed, err)
		ids[closed.FinalObservationID] = true
	}
	for _, delta := range []time.Duration{0, -time.Nanosecond} {
		item := mustSave(t, store, observation("update"+delta.String(), "p", "before"))
		store.now = func() time.Time { return item.UpdatedAt.Add(delta) }
		updated, err := store.UpdateObservation(ctx, ObservationUpdate{ID: item.ID, Project: "p", ExpectedUpdatedAt: item.UpdatedAt, Content: "after"})
		testutil.Require(t, err == nil && updated.Content == "after" && updated.UpdatedAt.After(item.UpdatedAt), "delta=%s update=%+v err=%v", delta, updated, err)
		_, err = store.UpdateObservation(ctx, ObservationUpdate{ID: item.ID, Project: "p", ExpectedUpdatedAt: item.UpdatedAt, Content: "stale"})
		testutil.Require(t, errors.Is(err, ErrConflict), "delta=%s stale update=%v", delta, err)
	}
}

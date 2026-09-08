package pi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/providers/pi/tools"
)

func TestSessionResultsDoNotExposeLeaseTokensOrExternalIDs(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, false)
	ctx := context.Background()
	result, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.start", Payload: []byte(`{"externalId":"raw-transcript-session"}`)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "leaseToken") || strings.Contains(string(encoded), "raw-transcript-session") {
		t.Fatalf("session response leaked private data: %s", encoded)
	}
	var started struct {
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal(encoded, &started); err != nil || started.Handle == "" {
		t.Fatalf("safe handle=%s err=%v", encoded, err)
	}
	if _, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.checkpoint", Payload: []byte(`{"handle":"` + started.Handle + `"}`)}); err != nil {
		t.Fatalf("checkpoint through retained lease: %v", err)
	}
	if _, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.end", Payload: []byte(`{"handle":"` + started.Handle + `","state":"completed","summary":"sanitized handoff"}`)}); err != nil {
		t.Fatalf("end through retained identity: %v", err)
	}
	next, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.start", Payload: []byte(`{"externalId":"next"}`)})
	if err != nil {
		t.Fatal(err)
	}
	nextJSON, _ := json.Marshal(next)
	var active struct {
		Handle string `json:"handle"`
	}
	_ = json.Unmarshal(nextJSON, &active)
	contextResult, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.context", Payload: []byte(`{"handle":"` + active.Handle + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	contextJSON, _ := json.Marshal(contextResult)
	if !strings.Contains(string(contextJSON), "sanitized handoff") || strings.Contains(string(contextJSON), "raw-transcript-session") || strings.Contains(string(contextJSON), "leaseToken") {
		t.Fatalf("unsafe handoff: %s", contextJSON)
	}
}

func TestFailedSessionEndRetainsLocalLeaseForRecovery(t *testing.T) {
	d := tools.New(config.Options{ProjectDir: t.TempDir(), StorageRoot: t.TempDir()}, false)
	ctx := context.Background()
	started, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.start", Payload: []byte(`{"externalId":"recover"}`)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(started)
	var value struct {
		Handle string `json:"handle"`
	}
	_ = json.Unmarshal(encoded, &value)
	if _, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.end", Payload: []byte(`{"handle":"` + value.Handle + `","state":"completed","summary":""}`)}); err == nil {
		t.Fatal("invalid finalization accepted")
	}
	if _, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.checkpoint", Payload: []byte(`{"handle":"` + value.Handle + `"}`)}); err != nil {
		t.Fatalf("retained lease did not checkpoint: %v", err)
	}
	if _, err := d.Dispatch(ctx, pi.Request{Operation: "memory.session.end", Payload: []byte(`{"handle":"` + value.Handle + `","state":"completed","summary":"handoff"}`)}); err != nil {
		t.Fatalf("lease lost after failed end: %v", err)
	}
}

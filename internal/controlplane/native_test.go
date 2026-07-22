package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/orchestrator"
	"github.com/vgxness/vgxness/internal/providers"
	"github.com/vgxness/vgxness/internal/registry"
)

func TestNativeDispatchPreparesChildAndAcceptsIdempotentlyWithoutRunningAdapter(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
		NewID: func(prefix string) (string, error) {
			sequence++
			return prefix + "-native-" + strconv.Itoa(sequence), nil
		},
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-primary", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect the architecture with CodeGraph", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.OK || prepared.Prepared == nil || prepared.Prepared.Agent != "vgxness-explorer" || prepared.Prepared.TicketID == "" || prepared.Prepared.Prompt == "" || prepared.Status != "running" {
		t.Fatalf("unexpected prepared dispatch: %#v", prepared)
	}
	if prepared.Prepared.TicketID != "ticket-primary" {
		t.Fatalf("caller-owned ticket identity changed: %q", prepared.Prepared.TicketID)
	}
	if adapter.runs != 0 {
		t.Fatalf("prepare executed nested adapter %d times", adapter.runs)
	}
	if _, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-competing", Model: "openai/gpt-5.6-sol", Operation: bridge.WriteFiles,
		Goal: "Competing write", ParentSessionID: "ses_parent", ParentMessageID: "msg_competing", ChildSessionID: "ses_competing",
	}); !errors.Is(err, bridge.ErrUnavailable) {
		t.Fatalf("competing foreground dispatch was not denied: %v", err)
	}
	result := nativeResult(t, prepared.TaskID)
	completion := bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: result,
	}
	completed, err := service.Complete(context.Background(), workspace, completion)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.OK || completed.Status != "completed" || completed.Receipt == nil || completed.Receipt.EventCount != 3 || completed.TaskID != prepared.TaskID {
		t.Fatalf("unexpected completion: %#v", completed)
	}
	if adapter.runs != 0 {
		t.Fatalf("native completion executed nested adapter %d times", adapter.runs)
	}
	again, err := service.Complete(context.Background(), workspace, completion)
	if err != nil || string(again.Result) != string(completed.Result) || again.Receipt == nil || again.Receipt.ExecutionID != completed.Receipt.ExecutionID {
		t.Fatalf("idempotent completion changed: %#v err=%v", again, err)
	}
	_, paths, _, release, err := service.openNativeTicket(context.Background(), workspace, completion.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := acquireNativeLease(paths.Root, completion.TicketID, now.Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), workspace, completion); err != nil {
		t.Fatalf("terminal retry did not reconcile stale owner lease: %v", err)
	}
	if _, err := os.Lstat(nativeLeasePath(paths.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("terminal retry left stale lease: %v", err)
	}
	completion.ChildSessionID = "ses_forged"
	if _, err := service.Complete(context.Background(), workspace, completion); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("forged duplicate completion error=%v", err)
	}
	next, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-next", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Next sequential task", ParentSessionID: "ses_parent", ParentMessageID: "msg_next", ChildSessionID: "ses_next",
	})
	if err != nil {
		t.Fatalf("completed dispatch did not release foreground capacity: %v", err)
	}
	completion.ChildSessionID = "ses_child"
	if _, err := service.Complete(context.Background(), workspace, completion); err != nil {
		t.Fatalf("terminal retry disturbed successor lease: %v", err)
	}
	if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: next.Prepared.TicketID, ParentSessionID: "ses_parent", ChildSessionID: "ses_next", Category: "native-subagent-cancelled",
	}); err != nil {
		t.Fatalf("cleanup next dispatch: %v", err)
	}
}

func TestNativeContinuityPersistsOnlyAfterAcceptedChildResult(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	adapter := nativeTestAdapter(now)
	service := New(Options{
		StorageRoot: storage, Memory: sqliteContinuityMemory{}, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return adapter, nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-continuity", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect durable native continuity", Continuity: bridge.ContinuityStart,
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil || prepared.CapsuleID != "" || prepared.StateVersion != 0 {
		t.Fatalf("prepare prematurely completed continuity: %#v err=%v", prepared, err)
	}
	completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: nativeResult(t, prepared.TaskID),
	})
	if err != nil || completed.CapsuleID == "" || completed.StateVersion != 1 || len(completed.MemoryRefs) != 1 {
		t.Fatalf("accepted native continuity was not persisted: %#v err=%v", completed, err)
	}
	if adapter.runs != 0 {
		t.Fatalf("native continuity executed nested adapter %d times", adapter.runs)
	}
}

func TestNativeInvalidResultClosesTicketAsFailedIdempotently(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-invalid-result", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect safely", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	completion := bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", MessageID: "msg_child", Result: json.RawMessage(`{}`),
	}
	failed, err := service.Complete(context.Background(), workspace, completion)
	if err != nil || !failed.OK || failed.Status != "failed" {
		t.Fatalf("invalid child result did not close ticket: %#v err=%v", failed, err)
	}
	again, err := service.Complete(context.Background(), workspace, completion)
	if err != nil || again.Status != "failed" {
		t.Fatalf("failed completion was not idempotent: %#v err=%v", again, err)
	}
}

func TestNativeReadBrokerBindsTicketAndRejectsSensitiveAliases(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("SECRET=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git", "config"), []byte("extraheader = secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(workspace, "linked.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("internal", filepath.Join(workspace, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(workspace, ".env"), filepath.Join(workspace, "hardlinked.go")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-read", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Read one safe source file", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_child", Path: "internal/app.go", Limit: 8,
	})
	if err != nil || read.Read == nil || read.Read.Content != "package " || !read.Read.Truncated || read.Read.NextOffset != 8 {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	for _, path := range []string{".env", ".git/config", "linked.go", "linked-dir/app.go", "hardlinked.go"} {
		if _, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_child", Path: path,
		}); !errors.Is(err, bridge.ErrDenied) {
			t.Fatalf("path %q was not denied: %v", path, err)
		}
	}
	if _, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_forged", Path: "internal/app.go",
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("forged child read was not denied: %v", err)
	}
	if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent", ChildSessionID: "ses_child", Category: "native-subagent-failed",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeReadBrokerPagesUTF8WithoutRejectingSplitRune(t *testing.T) {
	workspace := t.TempDir()
	content := "1234567€tail"
	if err := os.WriteFile(filepath.Join(workspace, "utf8.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "invalid.txt"), []byte{'o', 'k', 0xff, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-utf8", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Read UTF-8 safely", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_child", Path: "utf8.txt", Limit: 8,
	})
	if err != nil || first.Read == nil || first.Read.Content != "1234567" || first.Read.NextOffset != 7 || !first.Read.Truncated {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_child", Path: "utf8.txt", Offset: first.Read.NextOffset, Limit: 8,
	})
	if err != nil || second.Read == nil || second.Read.Content != "€tail" || second.Read.Truncated {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	if _, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_child", Path: "invalid.txt", Limit: 8,
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("invalid UTF-8 was not denied: %v", err)
	}
}

func TestNativeReadBrokerRejectsReplacedWorkspaceRoot(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "safe.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-root", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Bind the workspace root", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(workspace, workspace+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "safe.txt"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadNative(context.Background(), workspace, bridge.NativeReadRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ChildSessionID: "ses_child", Path: "safe.txt",
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("replacement workspace root was not denied: %v", err)
	}
}

func TestNativeTicketBoundCoversWorstCaseAcceptedResult(t *testing.T) {
	count := (bridge.MaxNativeResultBytes - 2) / len("\u2028")
	result := json.RawMessage(`"` + strings.Repeat("\u2028", count) + `"`)
	document := nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-large", Workspace: "/workspace", WorkspaceID: "unix:1:1", State: "completed",
		Response: &bridge.Response{ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Status: "completed", Result: result},
	}
	encoded, err := json.Marshal(document)
	if err != nil || len(encoded) <= 4<<20 || len(encoded) > nativeTicketLimit {
		t.Fatalf("encoded ticket size=%d limit=%d err=%v", len(encoded), nativeTicketLimit, err)
	}
	root := t.TempDir()
	if err := createNativeTicket(root, document); err != nil {
		t.Fatal(err)
	}
	readback, err := readNativeTicket(root, document.TicketID)
	if err != nil || readback.Response == nil || !json.Valid(readback.Response.Result) {
		t.Fatalf("readback result=%d err=%v", len(readback.Response.Result), err)
	}
	var expectedText, actualText string
	if json.Unmarshal(result, &expectedText) != nil || json.Unmarshal(readback.Response.Result, &actualText) != nil || actualText != expectedText {
		t.Fatal("large accepted result changed across ticket persistence")
	}
	combined := nativeTicketDocument{
		SchemaVersion: nativeTicketVersion, TicketID: "ticket-combined", Workspace: "/workspace", WorkspaceID: "unix:1:1", State: "prepared",
		Coordinator: orchestrator.NativeTicket{Request: providers.Request{Packet: bytes.Repeat([]byte{'p'}, 12<<20)}},
	}
	if err := createNativeTicket(root, combined); err != nil {
		t.Fatalf("valid large prepared ticket was rejected: %v", err)
	}
	combined.State = "completed"
	combined.Response = &bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Status: "completed",
		Result: json.RawMessage(`"` + strings.Repeat("<", bridge.MaxNativeResultBytes-2) + `"`),
	}
	if err := writeNativeTicket(root, combined); err != nil {
		t.Fatalf("large prepared envelope plus worst-case terminal result was rejected: %v", err)
	}
}

func TestExpiredUnreturnedTicketIsTerminalizedBeforeNextPrepare(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	first, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-lost-response", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Simulate a lost prepare response", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, paths, _, releaseFirst, err := service.openNativeTicket(context.Background(), workspace, first.Prepared.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	releaseFirst()
	expiredLease, err := json.Marshal(nativeLease{SchemaVersion: nativeTicketVersion, TicketID: first.Prepared.TicketID, Deadline: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatalf("expire lost-response lease: %v", err)
	}
	if err := os.WriteFile(nativeLeasePath(paths.Root), expiredLease, 0o600); err != nil {
		t.Fatalf("expire lost-response lease: %v", err)
	}
	next, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-after-recovery", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Proceed after deterministic recovery", ParentSessionID: "ses_parent", ParentMessageID: "msg_next", ChildSessionID: "ses_next",
	})
	if err != nil {
		t.Fatalf("next prepare did not recover the expired owner: %v", err)
	}
	_, _, recovered, release, err := service.openNativeTicket(context.Background(), workspace, first.Prepared.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if recovered.State != "failed" || recovered.Response == nil || recovered.Response.Status != "failed" {
		t.Fatalf("lost-response ticket was not durably terminalized: %#v", recovered)
	}
	if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: next.Prepared.TicketID, ParentSessionID: "ses_parent", ChildSessionID: "ses_next", Category: "native-subagent-failed",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredPreStartTicketRecoversBeforeContinuityAdmission(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Memory: sqliteContinuityMemory{}, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	first, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-pre-start", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Simulate a crash before Chronicle task start", Continuity: bridge.ContinuityStart,
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, paths, document, release, err := service.openNativeTicket(context.Background(), workspace, first.Prepared.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	document.State = "preparing"
	document.Coordinator.Prepared = providers.Prepared{}
	document.Continuity.Snapshot = runSnapshot{}
	document.Continuity.Staged = chronicle.CurrentRun{}
	if err := writeNativeTicket(paths.Root, document); err != nil {
		release()
		t.Fatal(err)
	}
	log, err := chronicle.NewEventLog(paths.Root, document.RunID)
	if err != nil {
		release()
		t.Fatal(err)
	}
	events, err := log.Read(context.Background())
	if err != nil {
		release()
		t.Fatal(err)
	}
	kept := make([]byte, 0)
	for _, event := range events {
		if event.Type == "task.started" || event.Type == "background.started" {
			continue
		}
		kept = append(kept, event.Raw...)
		kept = append(kept, '\n')
	}
	if err := os.WriteFile(log.Path(), kept, 0o600); err != nil {
		release()
		t.Fatal(err)
	}
	expiredLease, err := json.Marshal(nativeLease{SchemaVersion: nativeTicketVersion, TicketID: document.TicketID, Deadline: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	if err != nil {
		release()
		t.Fatal(err)
	}
	if err := os.WriteFile(nativeLeasePath(paths.Root), expiredLease, 0o600); err != nil {
		release()
		t.Fatal(err)
	}
	release()

	recovery, prepareErr := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-after-pre-start", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Trigger recovery before continuity admission", Continuity: bridge.ContinuityStart,
		ParentSessionID: "ses_parent", ParentMessageID: "msg_next", ChildSessionID: "ses_next",
	})
	if prepareErr != nil || !recovery.OK || recovery.Status != "recovered" || recovery.RunID != document.RunID || recovery.CapsuleID == "" {
		t.Fatalf("recovered continuity identity was not returned: %#v err=%v", recovery, prepareErr)
	}
	_, _, recovered, releaseRecovered, err := service.openNativeTicket(context.Background(), workspace, document.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	releaseRecovered()
	if recovered.State != "failed" || recovered.Response == nil || recovered.Response.Status != "failed" {
		t.Fatalf("pre-start ticket was not recovered before continuity admission: %#v", recovered)
	}
	recoveredEvents, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states, err := chronicle.DeriveTaskStates(recoveredEvents)
	if err != nil {
		t.Fatalf("pre-start recovery corrupted Chronicle: %v", err)
	}
	if _, exists := states[document.TaskID]; exists {
		t.Fatal("pre-start recovery synthesized a terminal task without a start")
	}
	store, err := chronicle.NewSnapshotStore(paths.Root, document.RunID)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRecovery, err := store.Recover(context.Background())
	if err != nil || !snapshotRecovery.CurrentPresent || snapshotRecovery.Current.Status != "blocked" {
		t.Fatalf("pre-start continuity did not recover as blocked: %#v err=%v", snapshotRecovery.Current, err)
	}
	var snapshot runSnapshot
	if err := json.Unmarshal(snapshotRecovery.Run, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Status != "pending" || len(snapshot.Capsules) != 1 || snapshot.Capsules[0].Status != "blocked" {
		t.Fatalf("unexpected pre-start continuity mapping: tasks=%#v capsules=%#v", snapshot.Tasks, snapshot.Capsules)
	}
}

func TestImmediateFailOfPreStartTicketDoesNotAppendTerminalEvent(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-immediate-pre-start", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Simulate immediate cleanup before Chronicle start", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, paths, document, release, err := service.openNativeTicket(context.Background(), workspace, prepared.Prepared.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	document.State = "preparing"
	document.Coordinator.Prepared = providers.Prepared{}
	if err := writeNativeTicket(paths.Root, document); err != nil {
		release()
		t.Fatal(err)
	}
	log, err := chronicle.NewEventLog(paths.Root, document.RunID)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if err := os.WriteFile(log.Path(), nil, 0o600); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	failed, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: document.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", Category: "native-subagent-failed",
	})
	if err != nil || failed.Status != "failed" || failed.RunID != document.RunID {
		t.Fatalf("immediate pre-start cleanup failed: %#v err=%v", failed, err)
	}
	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states, err := chronicle.DeriveTaskStates(events)
	if err != nil {
		t.Fatalf("immediate pre-start cleanup corrupted Chronicle: %v", err)
	}
	if _, exists := states[document.TaskID]; exists {
		t.Fatal("immediate pre-start cleanup synthesized a terminal task without a start")
	}
}

func TestPrepareStartAppendFailureRetainsRecoverableTicketAndLease(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: storage, ProjectDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	log, err := chronicle.NewEventLog(paths.Root, "run-fixed")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(log.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	service := New(Options{
		StorageRoot: storage, Now: func() time.Time { return now }, NewID: func(prefix string) (string, error) { return prefix + "-fixed", nil },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	request := bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-start-append-failure", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Fail before task.started is committed", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	}
	if _, err := service.Prepare(context.Background(), workspace, request); err == nil {
		t.Fatal("prepare unexpectedly succeeded with an obstructed Chronicle log")
	}
	if err := verifyNativeLease(paths.Root, request.TicketID); err != nil {
		t.Fatalf("durable ticket lost its recovery lease: %v", err)
	}
	_, _, document, release, err := service.openNativeTicket(context.Background(), workspace, request.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if document.State != "preparing" || document.RunID != "run-fixed" {
		t.Fatalf("unexpected recovery ticket after start failure: %#v", document)
	}
	if err := os.Remove(log.Path()); err != nil {
		t.Fatal(err)
	}
	failed, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: request.TicketID, ParentSessionID: request.ParentSessionID,
		ChildSessionID: request.ChildSessionID, Category: "native-subagent-failed",
	})
	if err != nil || failed.Status != "failed" {
		t.Fatalf("retained pre-start ticket was not recoverable: %#v err=%v", failed, err)
	}
	if _, err := os.Lstat(nativeLeasePath(paths.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered start failure left its lease: %v", err)
	}
}

func TestImmediateFailReconcilesProviderPrepareFailure(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC().Add(time.Hour)
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	request := bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-provider-prepare-failure", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Fail provider preparation after Chronicle start", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	}
	if _, err := service.Prepare(context.Background(), workspace, request); err == nil {
		t.Fatal("future health evidence unexpectedly passed provider preparation")
	}
	failed, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: request.TicketID, ParentSessionID: request.ParentSessionID,
		ChildSessionID: request.ChildSessionID, Category: "native-subagent-failed",
	})
	if err != nil || failed.Status != "failed" {
		t.Fatalf("provider preparation failure was not reconciled: %#v err=%v", failed, err)
	}
	_, paths, document, release, err := service.openNativeTicket(context.Background(), workspace, request.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	log, err := chronicle.NewEventLog(paths.Root, document.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := log.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states, err := chronicle.DeriveTaskStates(events)
	if err != nil || states[document.TaskID].Status != chronicle.TaskFailed {
		t.Fatalf("provider preparation terminal state changed: state=%+v err=%v", states[document.TaskID], err)
	}
	terminalCount := 0
	for _, event := range events {
		if event.Type == "task.failed" || event.Type == "background.failed" {
			terminalCount++
		}
	}
	if terminalCount != 1 {
		t.Fatalf("provider preparation cleanup wrote %d terminal events", terminalCount)
	}
	if _, err := os.Lstat(nativeLeasePath(paths.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reconciled provider failure left its lease: %v", err)
	}
}

func TestTicketCollisionDoesNotStageContinuity(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Memory: sqliteContinuityMemory{}, Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	first, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-collision", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Create the retained ticket path", ParentSessionID: "ses_parent", ParentMessageID: "msg_first", ChildSessionID: "ses_first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: first.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_first", Category: "native-subagent-failed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-collision", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "A collision must fail before continuity publication", Continuity: bridge.ContinuityStart,
		ParentSessionID: "ses_parent", ParentMessageID: "msg_second", ChildSessionID: "ses_second",
	}); err == nil {
		t.Fatal("duplicate ticket unexpectedly replaced an existing terminal ticket")
	}
	paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: service.storageRoot, ProjectDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := chronicle.ReadCurrent(context.Background(), paths.CurrentRun); err != nil || present {
		t.Fatalf("ticket collision staged an unrecoverable continuity: present=%v err=%v", present, err)
	}
	if _, err := os.Lstat(nativeLeasePath(paths.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ticket collision retained a lease it does not own: %v", err)
	}
}

func TestProvisionalContinuityWithoutEventsCanFail(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: storage, ProjectDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	log, err := chronicle.NewEventLog(paths.Root, "run-fixed")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(log.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	service := New(Options{
		StorageRoot: storage, Memory: sqliteContinuityMemory{}, Now: func() time.Time { return now },
		NewID:          func(prefix string) (string, error) { return prefix + "-fixed", nil },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	request := bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-provisional-zero-events", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Fail continuity staging before its first event", Continuity: bridge.ContinuityStart,
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	}
	if _, err := service.Prepare(context.Background(), workspace, request); err == nil {
		t.Fatal("continuity staging unexpectedly succeeded with an obstructed event log")
	}
	if err := verifyNativeLease(paths.Root, request.TicketID); err != nil {
		t.Fatalf("provisional ticket lost its lease: %v", err)
	}
	if _, present, err := chronicle.ReadCurrent(context.Background(), paths.CurrentRun); err != nil || present {
		t.Fatalf("failed provisional stage published current-run: present=%v err=%v", present, err)
	}
	if err := os.Remove(log.Path()); err != nil {
		t.Fatal(err)
	}
	failed, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: request.TicketID, ParentSessionID: request.ParentSessionID,
		ChildSessionID: request.ChildSessionID, Category: "native-subagent-failed",
	})
	if err != nil || failed.Status != "failed" || failed.RunID != "run-fixed" {
		t.Fatalf("zero-event provisional ticket was not recoverable: %#v err=%v", failed, err)
	}
	if _, err := os.Lstat(nativeLeasePath(paths.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zero-event provisional recovery left its lease: %v", err)
	}
}

func TestExpiredProvisionalContinuityWithoutEventsReleasesCapacity(t *testing.T) {
	workspace := t.TempDir()
	storage := filepath.Join(t.TempDir(), "storage")
	now := time.Now().UTC()
	paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: storage, ProjectDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	log, err := chronicle.NewEventLog(paths.Root, "run-fixed")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(log.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	service := New(Options{
		StorageRoot: storage, Memory: sqliteContinuityMemory{}, Now: func() time.Time { return now },
		NewID: func(prefix string) (string, error) {
			sequence++
			if prefix == "run" && sequence == 1 {
				return "run-fixed", nil
			}
			return prefix + "-" + strconv.Itoa(sequence), nil
		},
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	request := bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-provisional-expired", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Expire continuity before its first event", Continuity: bridge.ContinuityStart,
		ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	}
	if _, err := service.Prepare(context.Background(), workspace, request); err == nil {
		t.Fatal("continuity staging unexpectedly succeeded with an obstructed event log")
	}
	if err := os.Remove(log.Path()); err != nil {
		t.Fatal(err)
	}
	expired, err := json.Marshal(nativeLease{SchemaVersion: nativeTicketVersion, TicketID: request.TicketID, Deadline: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativeLeasePath(paths.Root), expired, 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-after-provisional-expiry", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Continue after provisional expiry", ParentSessionID: "ses_parent", ParentMessageID: "msg_next", ChildSessionID: "ses_next",
	})
	if err != nil || next.Prepared == nil {
		t.Fatalf("expired provisional ticket did not release capacity: %#v err=%v", next, err)
	}
	_, _, recovered, release, err := service.openNativeTicket(context.Background(), workspace, request.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if recovered.State != "failed" || recovered.Response == nil || recovered.Response.Status != "failed" {
		t.Fatalf("expired provisional ticket was not terminalized: %#v", recovered)
	}
	if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: next.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_next", Category: "native-subagent-failed",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSharedLeaseUsesSiblingSlotUntilExpiredOwnerIsRecovered(t *testing.T) {
	root := t.TempDir()
	if err := acquireNativeLeaseMode(root, "expired", time.Now().Add(-time.Minute).Format(time.RFC3339Nano), true); err != nil {
		t.Fatal(err)
	}
	if err := acquireNativeLeaseMode(root, "replacement", time.Now().Add(time.Minute).Format(time.RFC3339Nano), true); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeLease(root, "expired"); err != nil {
		t.Fatalf("expired owner was reclaimed without terminal recovery: %v", err)
	}
	if err := verifyNativeLease(root, "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := releaseNativeLease(root, "expired"); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeLease(root, "replacement"); err != nil {
		t.Fatalf("old owner removed replacement lease: %v", err)
	}
}

func TestNativeLeaseGuardPreservesOwnerWhileParallelSlotAdmitsSuccessor(t *testing.T) {
	root := t.TempDir()
	if err := acquireNativeLeaseMode(root, "owner", time.Now().Add(-time.Minute).Format(time.RFC3339Nano), true); err != nil {
		t.Fatal(err)
	}
	guard, err := acquireOwnedNativeLeaseGuard(root, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := acquireNativeLeaseMode(root, "successor", time.Now().Add(time.Minute).Format(time.RFC3339Nano), true); err != nil {
		guard.Release()
		t.Fatalf("parallel slot was not admitted while the owner was terminalizing: %v", err)
	}
	if err := verifyNativeLease(root, "owner"); err != nil {
		guard.Release()
		t.Fatalf("guarded owner was reclaimed by parallel admission: %v", err)
	}
	guard.Release()
	if err := releaseNativeLease(root, "owner"); err != nil {
		t.Fatalf("guarded owner was not releasable after terminal work: %v", err)
	}
	if err := releaseNativeLease(root, "successor"); err != nil {
		t.Fatalf("parallel successor cleanup failed: %v", err)
	}
}

func TestNativeLeaseAdmissionSeparatesSharedReadsFromExclusiveWork(t *testing.T) {
	root := t.TempDir()
	deadline := time.Now().Add(time.Minute).Format(time.RFC3339Nano)
	if err := acquireNativeLeaseMode(root, "shared", deadline, true); err != nil {
		t.Fatal(err)
	}
	if err := acquireNativeLeaseMode(root, "exclusive-blocked", deadline, false); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("exclusive dispatch entered alongside shared read: %v", err)
	}
	if err := releaseNativeLease(root, "shared"); err != nil {
		t.Fatal(err)
	}
	if err := acquireNativeLeaseMode(root, "exclusive", deadline, false); err != nil {
		t.Fatal(err)
	}
	if err := acquireNativeLeaseMode(root, "shared-blocked", deadline, true); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("shared read entered alongside exclusive dispatch: %v", err)
	}
	if err := releaseNativeLease(root, "exclusive"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeLeasePoolMembershipWaitsForAdmissionGuard(t *testing.T) {
	root := t.TempDir()
	deadline := time.Now().Add(time.Minute).Format(time.RFC3339Nano)
	admissionGuard, err := orchestrator.AcquireFileLock(nativeAdmissionGuardPath(root))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	acquired := make(chan error, 1)
	go func() {
		close(started)
		acquired <- acquireNativeLeaseMode(root, "shared", deadline, true)
	}()
	<-started
	select {
	case err := <-acquired:
		admissionGuard.Release()
		t.Fatalf("lease publication bypassed the admission guard: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	admissionGuard.Release()
	if err := <-acquired; err != nil {
		t.Fatalf("lease was not admitted after the guard released: %v", err)
	}

	admissionGuard, err = orchestrator.AcquireFileLock(nativeAdmissionGuardPath(root))
	if err != nil {
		t.Fatal(err)
	}
	releaseStarted := make(chan struct{})
	released := make(chan error, 1)
	go func() {
		close(releaseStarted)
		released <- releaseNativeLease(root, "shared")
	}()
	<-releaseStarted
	select {
	case err := <-released:
		admissionGuard.Release()
		t.Fatalf("lease removal bypassed the admission guard: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	admissionGuard.Release()
	if err := <-released; err != nil {
		t.Fatalf("lease was not released after the guard opened: %v", err)
	}
}

func TestNativeLeaseAdmissionRejectsPreCanceledContext(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := acquireNativeLeaseModeContext(ctx, root, "canceled", time.Now().Add(time.Minute).Format(time.RFC3339Nano), true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled admission error=%v", err)
	}
	for _, path := range nativeLeasePaths(root) {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pre-canceled admission published %s: %v", path, err)
		}
	}
}

func TestNativeLeaseAdmissionNeverMixesSharedAndExclusiveUnderContention(t *testing.T) {
	for attempt := 0; attempt < 64; attempt++ {
		root := t.TempDir()
		deadline := time.Now().Add(time.Minute).Format(time.RFC3339Nano)
		start := make(chan struct{})
		type outcome struct {
			id         string
			sharedRead bool
			err        error
		}
		outcomes := make(chan outcome, nativeMaxConcurrentLeases+1)
		var workers sync.WaitGroup
		for index := 0; index < nativeMaxConcurrentLeases+1; index++ {
			index := index
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				sharedRead := index != nativeMaxConcurrentLeases
				id := fmt.Sprintf("candidate-%d", index)
				outcomes <- outcome{id: id, sharedRead: sharedRead, err: acquireNativeLeaseMode(root, id, deadline, sharedRead)}
			}()
		}
		close(start)
		workers.Wait()
		close(outcomes)
		sharedAdmitted, exclusiveAdmitted := 0, 0
		for result := range outcomes {
			if result.err != nil {
				if !errors.Is(result.err, bridge.ErrDenied) {
					t.Fatalf("attempt %d admission error for %s: %v", attempt, result.id, result.err)
				}
				continue
			}
			if result.sharedRead {
				sharedAdmitted++
			} else {
				exclusiveAdmitted++
			}
		}
		if exclusiveAdmitted > 1 || exclusiveAdmitted > 0 && sharedAdmitted > 0 {
			t.Fatalf("attempt %d mixed pool membership: shared=%d exclusive=%d", attempt, sharedAdmitted, exclusiveAdmitted)
		}
	}
}

func TestNativeDispatchSharesOnlyOneShotReads(t *testing.T) {
	if !nativeDispatchMayShareLease(bridge.DispatchRequest{Operation: bridge.ReadFiles, Continuity: bridge.ContinuitySingle}) {
		t.Fatal("one-shot read was not eligible for bounded fan-out")
	}
	for name, request := range map[string]bridge.DispatchRequest{
		"review":     {Operation: bridge.ReviewChanges, Continuity: bridge.ContinuitySingle},
		"continuity": {Operation: bridge.ReadFiles, Continuity: bridge.ContinuityStart},
		"write":      {Operation: bridge.WriteFiles, Continuity: bridge.ContinuitySingle},
	} {
		if nativeDispatchMayShareLease(request) {
			t.Fatalf("%s dispatch was incorrectly eligible for fan-out", name)
		}
	}
}

func TestNativeReadOnlyFanOutIsConcurrentAndBounded(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	type outcome struct {
		response bridge.Response
		err      error
	}
	start := make(chan struct{})
	results := make(chan outcome, nativeMaxConcurrentLeases)
	var workers sync.WaitGroup
	for index := 0; index < nativeMaxConcurrentLeases; index++ {
		index := index
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			response, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
				ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-parallel-" + strconv.Itoa(index), Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
				Goal: "Read independent file " + strconv.Itoa(index), ParentSessionID: "ses_parent", ParentMessageID: "msg_parallel", ChildSessionID: "ses_child_" + strconv.Itoa(index),
			})
			results <- outcome{response: response, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	prepared := make([]bridge.Response, 0, nativeMaxConcurrentLeases)
	for result := range results {
		if result.err != nil || result.response.Prepared == nil {
			t.Fatalf("parallel read-only prepare failed: %#v err=%v", result.response, result.err)
		}
		prepared = append(prepared, result.response)
	}
	if len(prepared) != nativeMaxConcurrentLeases {
		t.Fatalf("prepared %d parallel tickets, want %d", len(prepared), nativeMaxConcurrentLeases)
	}
	if _, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-over-capacity", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Exceed bounded fan-out", ParentSessionID: "ses_parent", ParentMessageID: "msg_over", ChildSessionID: "ses_child_over",
	}); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("over-capacity native fan-out error=%v", err)
	}
	resultIDs := map[string]bool{}
	for _, response := range prepared {
		index := strings.TrimPrefix(response.Prepared.TicketID, "ticket-parallel-")
		completed, err := service.Complete(context.Background(), workspace, bridge.NativeCompletionRequest{
			ProtocolVersion: bridge.ProtocolVersion, TicketID: response.Prepared.TicketID, ParentSessionID: "ses_parent",
			ChildSessionID: "ses_child_" + index, MessageID: "msg_child_" + index, Result: nativeResult(t, response.TaskID),
		})
		if err != nil || completed.Status != "completed" || completed.TaskID != response.TaskID {
			t.Fatalf("parallel ticket completion failed: %#v err=%v", completed, err)
		}
		var result agentResult
		if json.Unmarshal(completed.Result, &result) != nil || result.ResultID == "" || resultIDs[result.ResultID] {
			t.Fatalf("parallel result identity was not distinct: %#v", completed)
		}
		resultIDs[result.ResultID] = true
	}
}

func TestNativeSharedLeaseFailsClosedUntilCrashPartialIsRecovered(t *testing.T) {
	root := t.TempDir()
	path := nativeLeasePath(root)
	if err := os.WriteFile(path, []byte(`{"schemaVersion":"1"`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute).Format(time.RFC3339Nano)
	if err := acquireNativeLeaseMode(root, "blocked", deadline, true); !errors.Is(err, bridge.ErrDenied) {
		t.Fatalf("unreadable active lease did not fail closed: %v", err)
	}
	stale := time.Now().Add(-nativeLeaseRecoveryAge - time.Minute)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	service := New(Options{Now: time.Now})
	if _, err := service.recoverExpiredNativeLeasePath(context.Background(), config.Paths{Root: root}, root, path); err != nil {
		t.Fatalf("expired crash partial was not recovered: %v", err)
	}
	if err := acquireNativeLeaseMode(root, "replacement", deadline, true); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeLease(root, "replacement"); err != nil {
		t.Fatal(err)
	}
	leasePath, present, err := findNativeLease(root, "replacement")
	if err != nil || !present || leasePath != path {
		t.Fatalf("recovered slot was not safely reused: path=%q present=%t err=%v", leasePath, present, err)
	}
}

func TestNativeRecoverySurfacesMaterialSlotErrors(t *testing.T) {
	root := t.TempDir()
	leasePath := nativeLeasePath(root)
	if err := os.WriteFile(leasePath, []byte(`{"schemaVersion":"1"`), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-nativeLeaseRecoveryAge - time.Minute)
	if err := os.Chtimes(leasePath, stale, stale); err != nil {
		t.Fatal(err)
	}
	for _, guardPath := range []string{leasePath + ".guard", nativeAdmissionGuardPath(root)} {
		guard, err := orchestrator.AcquireFileLock(guardPath)
		if err != nil {
			t.Fatal(err)
		}
		guard.Release()
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o700)
	service := New(Options{Now: time.Now})
	if _, err := service.recoverExpiredNativeLease(context.Background(), config.Paths{Root: root}, root); !errors.Is(err, bridge.ErrExecution) {
		t.Fatalf("material recovery error was hidden: %v", err)
	}
}

func TestNativeFailureReplaysCrashTerminalization(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-replay", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect safely", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	root, paths, document, release, err := service.openNativeTicket(context.Background(), workspace, prepared.Prepared.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, _, err := service.nativeCoordinator(context.Background(), paths, root, document)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if _, err := coordinator.FailNative(context.Background(), document.Coordinator, "native-subagent-failed"); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if _, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", Category: "native-subagent-deadline",
	}); !errors.Is(err, orchestrator.ErrDurability) {
		t.Fatalf("mismatched crash failure category was not rejected: %v", err)
	}
	failure := bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", Category: "native-subagent-failed",
	}
	failed, err := service.Fail(context.Background(), workspace, failure)
	if err != nil || failed.Status != "failed" {
		t.Fatalf("crash-terminalized failure was not replayed: %#v err=%v", failed, err)
	}
	if _, err := service.Fail(context.Background(), workspace, failure); err != nil {
		t.Fatalf("persisted failure was not idempotent: %v", err)
	}
}

func TestPreparingRecoveryTicketCanTerminalizeStartedTask(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	service := New(Options{
		StorageRoot: filepath.Join(t.TempDir(), "storage"), Now: func() time.Time { return now },
		AdapterFactory: func(string) (providers.Adapter, error) { return nativeTestAdapter(now), nil },
	})
	prepared, err := service.Prepare(context.Background(), workspace, bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: "ticket-preparing", Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Recover a start-before-upgrade crash", ParentSessionID: "ses_parent", ParentMessageID: "msg_parent", ChildSessionID: "ses_child",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, paths, document, release, err := service.openNativeTicket(context.Background(), workspace, prepared.Prepared.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	document.State = "preparing"
	document.Coordinator.Prepared = providers.Prepared{}
	if err := writeNativeTicket(paths.Root, document); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	failed, err := service.Fail(context.Background(), workspace, bridge.NativeFailureRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: "ses_parent",
		ChildSessionID: "ses_child", Category: "native-subagent-failed",
	})
	if err != nil || failed.Status != "failed" {
		t.Fatalf("preparing recovery ticket was not terminalized: %#v err=%v", failed, err)
	}
	if _, err := os.Lstat(nativeLeasePath(paths.Root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preparing recovery left foreground lease: %v", err)
	}
}

func nativeTestAdapter(now time.Time) *fakeAdapter {
	return &fakeAdapter{
		descriptor: providers.Descriptor{
			Reference:        registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
			Source:           registry.SourceReference{Provider: "filesystem", ID: "opencode", Path: "/test/opencode"},
			InterfaceVersion: "1", Capabilities: []registry.Capability{{Capability: capabilityID, Version: "1"}},
		},
		health: providers.Health{Status: gatekeeper.AdapterHealthy, CheckedAt: now},
	}
}

func nativeResult(t *testing.T, taskID string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-" + taskID, "taskId": taskID,
		"agentId": agentID, "status": "success", "summary": "native bounded work completed", "artifacts": []any{},
		"nextRecommended": "inspect the receipt", "risks": []any{}, "errors": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

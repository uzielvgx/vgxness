package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeExecutor struct {
	result    commandResult
	args      []string
	directory string
}

func (executor *fakeExecutor) Run(_ context.Context, _ string, args []string, directory string, _ int) commandResult {
	executor.args = append([]string(nil), args...)
	executor.directory = directory
	return executor.result
}

func TestAdapterRunsOnlyBoundedProjectScopedOperations(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, ".codegraph"), 0o700); err != nil {
		t.Fatal(err)
	}
	executor := &fakeExecutor{result: commandResult{stdout: []byte("structural evidence")}}
	adapter, err := newAdapter("codegraph", executor, func(string) (string, error) { return "/usr/local/bin/codegraph", nil })
	if err != nil {
		t.Fatal(err)
	}
	tick := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	adapter.now = func() time.Time {
		tick = tick.Add(time.Millisecond)
		return tick
	}
	result, err := adapter.Query(context.Background(), workspace, Request{Operation: Explore, Query: "Dispatch native completion", MaxFiles: 6})
	if err != nil {
		t.Fatal(err)
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"explore", "--path", resolvedWorkspace, "--max-files", "6", "Dispatch native completion"}
	if !reflect.DeepEqual(executor.args, expected) || executor.directory != resolvedWorkspace || result.Format != "text" || result.Content != "structural evidence" || result.OutputSHA256 == "" || !result.FinishedAt.After(result.StartedAt) {
		t.Fatalf("args=%q directory=%q result=%#v", executor.args, executor.directory, result)
	}

	executor.result.stdout = []byte(`{"affected":["internal/controlplane/native_test.go"]}`)
	result, err = adapter.Query(context.Background(), workspace, Request{Operation: Affected, Files: []string{"internal/controlplane/native.go"}, Depth: 4})
	if err != nil || result.Format != "json" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	expected = []string{"affected", "--path", resolvedWorkspace, "--depth", "4", "--json", "internal/controlplane/native.go"}
	if !reflect.DeepEqual(executor.args, expected) {
		t.Fatalf("args=%q", executor.args)
	}
}

func TestAdapterRejectsUnavailableUnsafeAndInvalidEvidence(t *testing.T) {
	workspace := t.TempDir()
	executor := &fakeExecutor{result: commandResult{stdout: []byte(`{"ok":true}`)}}
	adapter, err := newAdapter("codegraph", executor, func(string) (string, error) { return "/usr/local/bin/codegraph", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Query(context.Background(), workspace, Request{Operation: Status}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing index err=%v", err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".codegraph"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{
		{Operation: Affected, Files: []string{"../outside.go"}},
		{Operation: Affected, Files: []string{".env"}},
		{Operation: Impact, Symbol: "bad\nsymbol"},
		{Operation: Explore, Query: ""},
	} {
		if _, err := adapter.Query(context.Background(), workspace, request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("request=%#v err=%v", request, err)
		}
	}
	executor.result.stdout = []byte("not-json")
	if _, err := adapter.Query(context.Background(), workspace, Request{Operation: Impact, Symbol: "Service"}); !errors.Is(err, ErrExecution) {
		t.Fatalf("invalid JSON err=%v", err)
	}
	executor.result = commandResult{stdout: []byte(`{"ok":true}`), overflow: true}
	if _, err := adapter.Query(context.Background(), workspace, Request{Operation: Status}); !errors.Is(err, ErrExecution) {
		t.Fatalf("overflow err=%v", err)
	}
	executor.result = commandResult{stdout: []byte(".env:1\nSECRET=value")}
	if _, err := adapter.Query(context.Background(), workspace, Request{Operation: Explore, Query: "configuration"}); !errors.Is(err, ErrExecution) {
		t.Fatalf("sensitive evidence err=%v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	executor.args = nil
	if _, err := adapter.Query(cancelled, workspace, Request{Operation: Status}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query err=%v", err)
	}
	if executor.args != nil {
		t.Fatalf("cancelled query reached executor: %q", executor.args)
	}
}

package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/testutil"
	"github.com/vgxness/vgxness/internal/tui"
)

type recordingTUIMemoryRuntime struct {
	recall     memory.Recall
	lookup     memory.Lookup
	references []string
}

func (*recordingTUIMemoryRuntime) ResolveProject(context.Context, config.Options, string) (string, error) {
	return "project-1", nil
}

func (*recordingTUIMemoryRuntime) Recent(context.Context, config.Options, memory.Recent) ([]memory.Entry, error) {
	return nil, nil
}

func (runtime *recordingTUIMemoryRuntime) Recall(_ context.Context, _ config.Options, request memory.Recall) ([]memory.Entry, error) {
	runtime.recall = request
	return []memory.Entry{{ID: "obs-1", Title: "Decision", Preview: "bounded preview", Type: "architecture", State: memory.StateActive}}, nil
}

func (runtime *recordingTUIMemoryRuntime) Get(_ context.Context, _ config.Options, request memory.Lookup) (memory.Entry, error) {
	runtime.lookup = request
	runtime.references = []string{"obs-prior"}
	return memory.Entry{
		ID: request.ID, Title: "Decision", Content: "Full durable content",
		Project: request.Project, Scope: request.Scope, Type: "architecture",
		State: memory.StateActive, References: runtime.references,
	}, nil
}

func TestMemoryRuntime_ReadAbsentStorageOperationalAndNonMutating(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"token","project":"p","scope":"project"}`), &out, &stderr)
	_, err := os.Stat(root)
	testutil.Require(t, code == 1 && out.Len() == 0 && os.IsNotExist(err), "exit=%d out=%q stat=%v", code, out.String(), err)
}

func TestMemoryRuntime_SaveCloseAndOfflineRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"memory", "save", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"title":"T","content":"restart token","project":"p"}`), &out, &stderr)
	testutil.Require(t, code == 0, "save exit=%d stderr=%q", code, stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"memory", "search", "--stdin", "--storage-root", root}, strings.NewReader(`{"schemaVersion":1,"query":"restart","project":"p","scope":"project"}`), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "T"), "restart exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestOpenCodeIntegrationRuntime_InstallStatusAndRecoverableUninstall(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"integrate", "opencode", "install", "--model", "openai/gpt-5.6-sol", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=installed") && stderr.Len() == 0, "install exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"integrate", "opencode", "status", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=installed") && strings.Contains(out.String(), "changed=false"), "status exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
	out.Reset()
	code = Run(context.Background(), []string{"integrate", "opencode", "uninstall", "--config-dir", configDirectory}, strings.NewReader(""), &out, &stderr)
	testutil.Require(t, code == 0 && strings.Contains(out.String(), "state=absent") && strings.Contains(out.String(), "backup="), "uninstall exit=%d out=%q stderr=%q", code, out.String(), stderr.String())
}

func TestVersionUsesLightweightPathWithoutWorkingDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.HasPrefix(stdout.String(), "version=dev\ncommit=unknown\ndate=unknown\n") || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunDispatchesExplicitTUIBeforeLegacyCLI(t *testing.T) {
	called := 0
	launcher := func(_ context.Context, _ io.Reader, _ io.Writer, _ io.Writer, backend tui.Backend, options tui.Options) int {
		called++
		if backend == nil || options.Workspace == "" {
			t.Fatalf("invalid TUI launch: backend=%v options=%+v", backend, options)
		}
		return 23
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"tui"}, bytes.NewReader(nil), &stdout, &stderr, launcher)
	if code != 23 || called != 1 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d called=%d stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestRunRejectsTUIArgumentsWithoutLaunching(t *testing.T) {
	called := 0
	launcher := func(context.Context, io.Reader, io.Writer, io.Writer, tui.Backend, tui.Options) int {
		called++
		return 0
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"tui", "--unknown"}, bytes.NewReader(nil), &stdout, &stderr, launcher)
	if code != 2 || called != 0 || stdout.Len() != 0 || stderr.String() != "usage: vgxness tui\n" {
		t.Fatalf("code=%d called=%d stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestTUIBackendSearchAndDetailStayProjectScoped(t *testing.T) {
	runtime := &recordingTUIMemoryRuntime{}
	backend := tuiBackend{memory: runtime}

	results, err := backend.Search(context.Background(), tui.MemorySearch{Workspace: "/workspace", Query: "architecture reliability", Limit: 12})
	testutil.Require(t, err == nil && len(results) == 1, "search results=%+v err=%v", results, err)
	testutil.Require(t, runtime.recall.Project == "project-1" && runtime.recall.Scope == memory.ScopeProject && runtime.recall.MatchAny && runtime.recall.Limit == 12, "recall=%+v", runtime.recall)
	testutil.Require(t, results[0].ID == "obs-1" && results[0].Preview == "bounded preview", "summary=%+v", results[0])

	detail, err := backend.GetMemory(context.Background(), tui.MemoryLookup{Workspace: "/workspace", ID: "obs-1"})
	testutil.Require(t, err == nil && detail.ID == "obs-1" && detail.Content == "Full durable content", "detail=%+v err=%v", detail, err)
	testutil.Require(t, runtime.lookup.Project == "project-1" && runtime.lookup.Scope == memory.ScopeProject && runtime.lookup.ID == "obs-1", "lookup=%+v", runtime.lookup)
	detail.References[0] = "changed"
	testutil.Require(t, runtime.references[0] == "obs-prior", "detail references alias runtime storage: %+v", runtime.references)
}

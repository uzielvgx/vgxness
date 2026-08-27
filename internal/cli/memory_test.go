package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/testutil"
)

type fakeMemoryRuntime struct {
	result     memory.Entry
	items      []memory.Entry
	recall     memory.Recall
	recent     memory.Recent
	project    string
	err        error
	calls      int
	sync       memory.SyncResult
	status     memory.SyncConfigurationStatus
	endpoint   string
	deviceID   string
	bearer     string
	opts       config.Options
	backfill   memory.SyncBackfillResult
	repair     memory.SyncProjectRepairResult
	transition memory.SyncProjectTransitionResult
	start      memory.ProviderSessionStart
	checkpoint string
	ended      memory.ProviderSessionEnd
}

func TestMemorySyncTransitionCommandsRequireExactModeConfirmation(t *testing.T) {
	workspace := t.TempDir()
	for _, args := range [][]string{
		{"memory", "sync", "reseed", "--workspace", workspace},
		{"memory", "sync", "reseed", "--workspace", workspace, "--confirm-cloud-empty", "--confirm-cloud-empty"},
		{"memory", "sync", "rejoin", "--workspace", workspace, "--confirm-cloud-empty"},
		{"memory", "sync", "rejoin", "--workspace", "relative", "--confirm-merge"},
		{"memory", "sync", "rejoin", "--workspace", workspace, "--confirm-merge", "--unknown"},
	} {
		runtime := &fakeMemoryRuntime{}
		code, out, stderr := runMemoryTest(args, "", runtime)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == 0 && stderr != "", "args=%q code=%d out=%q stderr=%q calls=%d", args, code, out, stderr, runtime.calls)
	}
	for _, tc := range []struct {
		verb, confirmation, mode string
	}{
		{"reseed", "--confirm-cloud-empty", string(memory.SyncProjectTransitionReseedSource)},
		{"rejoin", "--confirm-merge", string(memory.SyncProjectTransitionRejoinMerge)},
	} {
		runtime := &fakeMemoryRuntime{transition: memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionMode(tc.mode), Status: memory.SyncProjectTransitionPublishing, Projects: 1, Queued: 1}}
		code, out, stderr := runMemoryTest([]string{"memory", "sync", tc.verb, "--workspace", workspace, tc.confirmation, "--json"}, "", runtime)
		testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 1 && strings.Contains(out, `"schemaVersion":1`) && strings.Contains(out, `"mode":"`+tc.mode+`"`) && !strings.Contains(out, "credential") && !strings.Contains(out, "backup"), "verb=%s code=%d out=%q stderr=%q calls=%d", tc.verb, code, out, stderr, runtime.calls)
		runtime = &fakeMemoryRuntime{transition: memory.SyncProjectTransitionResult{SchemaVersion: 1, Mode: memory.SyncProjectTransitionMode(tc.mode), Status: memory.SyncProjectTransitionPublishing, Projects: 1, Queued: 1}}
		code, out, stderr = runMemoryTest([]string{"memory", "sync", tc.verb, "--workspace", workspace, tc.confirmation}, "", runtime)
		expected := "schema_version=1\nmode=" + tc.mode + "\nstatus=publishing\nprojects=1\nsessions=0\nobservations=0\nqueued=1\n"
		testutil.Require(t, code == 0 && stderr == "" && out == expected && runtime.calls == 1, "verb=%s code=%d out=%q stderr=%q calls=%d", tc.verb, code, out, stderr, runtime.calls)
	}
}

func TestMemorySyncRepairProjectRequiresExactConfirmationAndEmitsOnlyResult(t *testing.T) {
	runtime := &fakeMemoryRuntime{}
	workspace := t.TempDir()
	for _, args := range [][]string{{"memory", "sync", "repair-project", "--workspace", workspace, "--json"}, {"memory", "sync", "repair-project", "--workspace", "relative", "--confirm-remote-absent", "--json"}} {
		code, out, stderr := runMemoryTest(args, "", runtime)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == 0 && stderr != "", "args=%q code=%d out=%q stderr=%q", args, code, out, stderr)
	}
	code, out, stderr := runMemoryTest([]string{"memory", "sync", "repair-project", "--workspace", workspace, "--confirm-remote-absent", "--json"}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, `"schemaVersion":1`) && strings.Contains(out, `"status":"pending"`) && !strings.Contains(out, "credential"), "code=%d out=%q stderr=%q", code, out, stderr)
}

func (f *fakeMemoryRuntime) Remember(context.Context, config.Options, memory.Remember) (memory.Entry, error) {
	f.calls++
	return f.result, f.err
}
func (f *fakeMemoryRuntime) Recall(_ context.Context, _ config.Options, request memory.Recall) ([]memory.Entry, error) {
	f.calls++
	f.recall = request
	return f.items, f.err
}
func (f *fakeMemoryRuntime) Get(context.Context, config.Options, memory.Lookup) (memory.Entry, error) {
	f.calls++
	return f.result, f.err
}
func (f *fakeMemoryRuntime) Forget(context.Context, config.Options, memory.Forget) (memory.Entry, error) {
	f.calls++
	return f.result, f.err
}
func (f *fakeMemoryRuntime) ResolveProject(_ context.Context, opts config.Options, _ string) (string, error) {
	f.calls++
	f.opts = opts
	if f.project == "" {
		return "resolved-project", f.err
	}
	return f.project, f.err
}
func (f *fakeMemoryRuntime) InitializeProject(context.Context, config.Options, string) (string, error) {
	f.calls++
	if f.project == "" {
		return "initialized-project", f.err
	}
	return f.project, f.err
}

func (f *fakeMemoryRuntime) Recent(_ context.Context, _ config.Options, request memory.Recent) ([]memory.Entry, error) {
	f.calls++
	f.recent = request
	return f.items, f.err
}

func (f *fakeMemoryRuntime) Sync(_ context.Context, opts config.Options) (memory.SyncResult, error) {
	f.calls++
	f.opts = opts
	return f.sync, f.err
}
func (f *fakeMemoryRuntime) ConfigureSync(_ context.Context, opts config.Options, endpoint, deviceID, bearer string) (memory.SyncConfigurationStatus, error) {
	f.calls++
	f.opts = opts
	f.endpoint, f.deviceID, f.bearer = endpoint, deviceID, bearer
	return memory.SyncConfigurationStatus{Configured: true, Enabled: true, Credential: memory.SyncCredentialAvailable}, f.err
}

func TestMemoryCLI_SyncConfigurePassesCredentialFile(t *testing.T) {
	runtime := &fakeMemoryRuntime{}
	code, _, stderr := runMemoryTest([]string{"memory", "sync", "configure", "--credential-file", "/absolute/private/bearer", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.opts.CredentialFile == "/absolute/private/bearer" && runtime.bearer == "", "code=%d stderr=%q opts=%+v bearer=%q", code, stderr, runtime.opts, runtime.bearer)
}

func TestMemoryCLI_ProjectInit(t *testing.T) {
	runtime := &fakeMemoryRuntime{project: "550e8400-e29b-41d4-a716-446655440000"}
	workspace := t.TempDir()
	code, out, stderr := runMemoryTest([]string{"memory", "project", "init", "--workspace", workspace}, "", runtime)
	testutil.Require(t, code == 0 && out == "project=550e8400-e29b-41d4-a716-446655440000\n" && stderr == "" && runtime.calls == 1, "code=%d out=%q stderr=%q calls=%d", code, out, stderr, runtime.calls)
}
func (f *fakeMemoryRuntime) SyncStatus(context.Context, config.Options) (memory.SyncConfigurationStatus, error) {
	f.calls++
	if f.status == (memory.SyncConfigurationStatus{}) {
		f.status.Credential = memory.SyncCredentialNotConfigured
	}
	return f.status, f.err
}
func (f *fakeMemoryRuntime) BackfillSyncProject(context.Context, config.Options, string, int) (memory.SyncBackfillResult, error) {
	f.calls++
	return f.backfill, f.err
}
func (f *fakeMemoryRuntime) RepairSyncProject(_ context.Context, opts config.Options, _ string, _ bool) (memory.SyncProjectRepairResult, error) {
	f.calls++
	f.opts = opts
	if f.repair == (memory.SyncProjectRepairResult{}) {
		f.repair = memory.SyncProjectRepairResult{SchemaVersion: 1, Status: "pending", Queued: 1}
	}
	return f.repair, f.err
}

func (f *fakeMemoryRuntime) TransitionSyncProject(_ context.Context, opts config.Options, _ string, _ memory.SyncProjectTransitionMode) (memory.SyncProjectTransitionResult, error) {
	f.calls++
	f.opts = opts
	return f.transition, f.err
}
func (f *fakeMemoryRuntime) StartProviderSession(_ context.Context, _ config.Options, request memory.ProviderSessionStart) (memory.ProviderSession, error) {
	f.calls++
	f.start = request
	return memory.ProviderSession{Project: request.Project, Handle: "ps-test", State: memory.ProviderSessionActive}, f.err
}
func (f *fakeMemoryRuntime) MarkProviderSessionCheckpoint(_ context.Context, _ config.Options, project, handle string) (memory.ProviderSession, error) {
	f.calls++
	f.checkpoint = handle
	return memory.ProviderSession{Project: project, Handle: handle, State: memory.ProviderSessionActive, Checkpointed: true}, f.err
}
func (f *fakeMemoryRuntime) EndProviderSession(_ context.Context, _ config.Options, request memory.ProviderSessionEnd) (memory.ProviderSession, error) {
	f.calls++
	f.ended = request
	return memory.ProviderSession{Project: request.Project, Handle: request.Handle, State: request.State}, f.err
}

func runMemoryTest(args []string, input string, runtime MemoryRuntime) (int, string, string) {
	var out, stderr bytes.Buffer
	code := RunProductSDDRuntime(context.Background(), args, strings.NewReader(input), &out, &stderr, &fakeInspector{}, runtime, nil, nil, nil, nil, nil)
	return code, out.String(), stderr.String()
}

func TestMemoryCLI_StrictSingleSourceInput(t *testing.T) {
	valid := `{"schemaVersion":1,"content":"body"}`
	file := filepath.Join(t.TempDir(), "request.json")
	testutil.NoError(t, os.WriteFile(file, []byte(valid), 0o600))
	link := filepath.Join(t.TempDir(), "request.json")
	testutil.NoError(t, os.Symlink(file, link))
	cases := []struct {
		name  string
		args  []string
		input string
	}{
		{"valid stdin", []string{"memory", "save", "--stdin"}, valid},
		{"unknown", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","unknown":1}`},
		{"duplicate", []string{"memory", "save", "--stdin", "--project", "p"}, `{"schemaVersion":1,"content":"x","project":"p"}`},
		{"duplicate empty project", []string{"memory", "save", "--stdin", "--project", "p"}, `{"schemaVersion":1,"content":"x","project":""}`},
		{"duplicate zero limit", []string{"memory", "search", "--stdin", "--limit", "10"}, `{"schemaVersion":1,"query":"x","project":"p","scope":"project","limit":0}`},
		{"duplicate JSON key", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","content":"y"}`},
		{"caller producer", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","producer":"agent"}`},
		{"caller source", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","sourceProvider":"chronicle","sourceId":"event-1"}`},
		{"malformed", []string{"memory", "save", "--stdin"}, `{`},
		{"oversized", []string{"memory", "save", "--stdin"}, strings.Repeat("x", 65538)},
		{"symlink", []string{"memory", "save", "--input", link}, ""},
		{"both", []string{"memory", "save", "--stdin", "--input", file}, valid},
		{"neither", []string{"memory", "save"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &fakeMemoryRuntime{}
			code, out, _ := runMemoryTest(tc.args, tc.input, runtime)
			validCase := tc.name == "valid stdin"
			testutil.Require(t, code == map[bool]int{true: 0, false: 2}[validCase] && out == "" == !validCase && runtime.calls == map[bool]int{true: 1}[validCase], "exit=%d calls=%d out=%q", code, runtime.calls, out)
		})
	}
}

func TestMemoryCLI_ResolvesWorkspaceWithoutCallerControlledProject(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeMemoryRuntime{project: "workspace-project", result: memory.Entry{Project: "workspace-project"}}
	code, _, stderr := runMemoryTest([]string{"memory", "save", "--stdin", "--workspace", workspace, "--json"}, `{"schemaVersion":1,"content":"durable fact"}`, runtime)
	testutil.Require(t, code == 0 && runtime.calls == 2 && stderr == "", "workspace save: code=%d calls=%d stderr=%q", code, runtime.calls, stderr)

	runtime = &fakeMemoryRuntime{}
	code, out, _ := runMemoryTest([]string{"memory", "search", "--stdin", "--workspace", workspace, "--project", "forged"}, `{"schemaVersion":1,"query":"durable"}`, runtime)
	testutil.Require(t, code == 2 && runtime.calls == 0 && out == "", "workspace accepted caller project: code=%d calls=%d out=%q", code, runtime.calls, out)
}

func TestMemoryCLI_RoutesRecentAndMatchAnyStrictly(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeMemoryRuntime{project: "workspace-project"}
	code, _, stderr := runMemoryTest([]string{"memory", "recent", "--stdin", "--workspace", workspace, "--json"}, `{"schemaVersion":1,"limit":7}`, runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 2 && runtime.recent.Project == "workspace-project" && runtime.recent.Scope == memory.ScopeProject && runtime.recent.Limit == 7, "recent route: code=%d calls=%d request=%+v stderr=%q", code, runtime.calls, runtime.recent, stderr)

	runtime = &fakeMemoryRuntime{}
	code, _, stderr = runMemoryTest([]string{"memory", "search", "--stdin"}, `{"schemaVersion":1,"query":"architecture reliability","project":"p","scope":"project","matchAny":true}`, runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 1 && runtime.recall.MatchAny, "matchAny route: code=%d calls=%d request=%+v stderr=%q", code, runtime.calls, runtime.recall, stderr)

	wrongVerbPayloads := map[string]string{
		"save":   `{"schemaVersion":1,"content":"x","matchAny":true}`,
		"get":    `{"schemaVersion":1,"id":"entry","project":"p","scope":"project","matchAny":true}`,
		"forget": `{"schemaVersion":1,"id":"entry","project":"p","scope":"project","matchAny":true}`,
		"recent": `{"schemaVersion":1,"project":"p","scope":"project","matchAny":true}`,
	}
	for verb, payload := range wrongVerbPayloads {
		runtime = &fakeMemoryRuntime{}
		code, out, _ := runMemoryTest([]string{"memory", verb, "--stdin"}, payload, runtime)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == 0, "%s accepted matchAny: code=%d calls=%d", verb, code, runtime.calls)
	}
}

func TestMemoryCLI_RenderStableSafeAndAtomicOutput(t *testing.T) {
	runtime := &fakeMemoryRuntime{result: memory.Entry{ID: "id", Title: "line\n\x1b", Content: "body"}}
	code, out, stderr := runMemoryTest([]string{"memory", "get", "--stdin", "--json"}, `{"schemaVersion":1,"id":"id","project":"p","scope":"project"}`, runtime)
	testutil.Require(t, code == 0 && strings.Contains(out, `"schemaVersion":1`) && strings.Contains(out, `"ID":"id"`) && stderr == "", "json=%q stderr=%q", out, stderr)
	code, out, _ = runMemoryTest([]string{"memory", "get", "--stdin"}, `{"schemaVersion":1,"id":"id","project":"p","scope":"project"}`, runtime)
	testutil.Require(t, code == 0 && strings.Contains(out, `line\n\x1b`), "unsafe human output: %q", out)
	runtime.err = errors.New("secret /path SELECT")
	code, out, stderr = runMemoryTest([]string{"memory", "get", "--stdin"}, `{"schemaVersion":1,"id":"id","project":"p","scope":"project"}`, runtime)
	testutil.Require(t, code == 1 && out == "" && !strings.Contains(stderr, "secret"), "unsafe failure: %d %q %q", code, out, stderr)
}

func TestMemoryCLI_ClassifiedErrorsAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{memory.ErrInvalid, 2}, {memory.ErrConflict, 1}, {memory.ErrNotFound, 1}, {memory.ErrCorrupt, 1}, {context.Canceled, 130}, {context.DeadlineExceeded, 130}} {
		runtime := &fakeMemoryRuntime{err: tc.err}
		code, out, _ := runMemoryTest([]string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x"}`, runtime)
		testutil.Require(t, code == tc.code && out == "", "error=%v exit=%d out=%q", tc.err, code, out)
	}
}

func TestMemoryCLI_UnsupportedOperationsNeverOpenOrMutateStorage(t *testing.T) {
	for _, verb := range []string{"list", "review", "delete", "relations"} {
		runtime := &fakeMemoryRuntime{}
		code, out, _ := runMemoryTest([]string{"memory", verb, "--stdin"}, `{}`, runtime)
		testutil.Require(t, code == 2 && runtime.calls == 0 && out == "", "%s exit=%d calls=%d", verb, code, runtime.calls)
	}
}

func TestMemoryCLI_SyncUsesNoInputAndRendersTokenFreeResult(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeMemoryRuntime{sync: memory.SyncResult{Status: memory.SyncStatusPartial, Pushed: 2, Retried: 1, Batches: 1}}
	code, out, stderr := runMemoryTest([]string{"memory", "sync", "--workspace", workspace}, "vgx1.550e8400-e29b-41d4-a716-446655440000.secret", runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 1 && out == "status=partial\npushed=2\npreviously_accepted=0\nrejected=0\nretried=1\nconflicts=0\nbatches=1\n" && !strings.Contains(out, "vgx1."), "sync output: code=%d calls=%d out=%q stderr=%q", code, runtime.calls, out, stderr)

	runtime = &fakeMemoryRuntime{err: context.Canceled}
	code, out, _ = runMemoryTest([]string{"memory", "sync", "--workspace", workspace}, "", runtime)
	testutil.Require(t, code == 130 && out == "" && runtime.calls == 1, "sync cancellation: code=%d calls=%d out=%q", code, runtime.calls, out)

	runtime = &fakeMemoryRuntime{sync: memory.SyncResult{Status: memory.SyncStatusSynced, Pushed: 1}}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "--workspace", workspace, "--json"}, "vgx1.550e8400-e29b-41d4-a716-446655440000.secret", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, `"schemaVersion":1`) && strings.Contains(out, `"status":"synced"`) && strings.Contains(out, `"pushed":1`) && !strings.Contains(out, "vgx1."), "sync json: code=%d out=%q stderr=%q", code, out, stderr)

	runtime = &fakeMemoryRuntime{sync: memory.SyncResult{Mode: memory.SyncModeProjectBidirectional, Status: memory.SyncStatusSynced, Pushed: 1}}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "--workspace", workspace}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.HasPrefix(out, "mode=project_bidirectional\nstatus=synced\n"), "project mode plain output: code=%d out=%q stderr=%q", code, out, stderr)
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "--workspace", workspace, "--json"}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, `"mode":"project_bidirectional"`), "project mode json output: code=%d out=%q stderr=%q", code, out, stderr)

	runtime = &fakeMemoryRuntime{sync: memory.SyncResult{Mode: memory.SyncModeProjectBidirectional, Status: memory.SyncStatusUnavailable}}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "--workspace", workspace}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.HasPrefix(out, "mode=project_bidirectional\nstatus=unavailable\n"), "preflight mode plain output: code=%d out=%q stderr=%q", code, out, stderr)

	runtime = &fakeMemoryRuntime{sync: memory.SyncResult{Status: memory.SyncStatusUnreachable, FailureOperation: "capabilities", FailureClass: "http_status", FailureHTTPStatus: 503}}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "--workspace", workspace}, "bearer-secret https://private.example/v1/sync/pull?project_id=project-secret raw-error", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, "failure_operation=capabilities\n") && strings.Contains(out, "failure_class=http_status\n") && strings.Contains(out, "failure_http_status=503\n") && !strings.Contains(out, "bearer-secret") && !strings.Contains(out, "private.example") && !strings.Contains(out, "project-secret") && !strings.Contains(out, "raw-error"), "diagnostic plain output: %q", out)
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "--workspace", workspace, "--json"}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, `"failureOperation":"capabilities"`) && strings.Contains(out, `"failureClass":"http_status"`) && strings.Contains(out, `"failureHttpStatus":503`) && !strings.Contains(out, "project-secret"), "diagnostic JSON output: %q", out)
}

func TestMemoryCLI_SyncPropagatesWorkspaceSelector(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeMemoryRuntime{}
	code, _, stderr := runMemoryTest([]string{"memory", "sync", "--workspace", workspace}, "", runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 1 && runtime.opts.ProjectDir == workspace, "code=%d stderr=%q calls=%d projectDir=%q", code, stderr, runtime.calls, runtime.opts.ProjectDir)

	runtime = &fakeMemoryRuntime{}
	code, _, _ = runMemoryTest([]string{"memory", "sync", "--workspace", workspace, "unexpected"}, "", runtime)
	testutil.Require(t, code == 2 && runtime.calls == 0, "invalid selector invocation: code=%d calls=%d", code, runtime.calls)

	for _, args := range [][]string{{"memory", "sync"}, {"memory", "sync", "--workspace", "relative"}} {
		runtime = &fakeMemoryRuntime{}
		code, out, stderr := runMemoryTest(args, "", runtime)
		testutil.Require(t, code == 2 && out == "" && stderr != "" && runtime.calls == 0, "args=%q code=%d out=%q stderr=%q calls=%d", args, code, out, stderr, runtime.calls)
	}
}

func TestMemoryCLI_SyncConfigureAndStatusAreStrictAndTokenFree(t *testing.T) {
	bearer := "vgx1.550e8400-e29b-41d4-a716-446655440000.secret"
	runtime := &fakeMemoryRuntime{}
	code, out, stderr := runMemoryTest([]string{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"}, bearer+"\n", runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, "configured=true") && runtime.endpoint == "https://sync.example.test" && runtime.deviceID == "550e8400-e29b-41d4-a716-446655440000" && runtime.bearer == bearer && !strings.Contains(out, bearer), "configure: code=%d calls=%d out=%q stderr=%q", code, runtime.calls, out, stderr)

	runtime = &fakeMemoryRuntime{}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"}, bearer+"\r\n", runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.bearer == bearer, "CRLF configure: code=%d bearer=%q stderr=%q", code, runtime.bearer, stderr)

	runtime = &fakeMemoryRuntime{}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"}, bearer, runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.bearer == bearer, "exact configure: code=%d bearer=%q stderr=%q", code, runtime.bearer, stderr)

	for _, args := range [][]string{
		{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000", "--bearer", bearer},
		{"memory", "sync", "configure", "--endpoint", "http://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"},
		{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "not-a-device"},
	} {
		runtime = &fakeMemoryRuntime{}
		code, out, stderr = runMemoryTest(args, bearer, runtime)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == 0 && !strings.Contains(stderr, bearer), "invalid configure: code=%d calls=%d out=%q stderr=%q", code, runtime.calls, out, stderr)
	}
	for _, input := range []string{"", strings.Repeat("x", maxSyncBearerBytes+1)} {
		runtime = &fakeMemoryRuntime{}
		code, out, stderr = runMemoryTest([]string{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"}, input, runtime)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == 0 && !strings.Contains(stderr, bearer), "invalid stdin: code=%d calls=%d out=%q stderr=%q", code, runtime.calls, out, stderr)
	}
	for _, input := range []string{bearer + "\n\n", bearer + "\r", bearer + "\r\nx"} {
		runtime = &fakeMemoryRuntime{}
		code, out, stderr = runMemoryTest([]string{"memory", "sync", "configure", "--endpoint", "https://sync.example.test", "--device-id", "550e8400-e29b-41d4-a716-446655440000"}, input, runtime)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == 0 && !strings.Contains(stderr, bearer), "invalid line ending: code=%d calls=%d stderr=%q", code, runtime.calls, stderr)
	}
	runtime = &fakeMemoryRuntime{}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "unexpected"}, "", runtime)
	testutil.Require(t, code == 2 && out == "" && runtime.calls == 0, "unknown sync subcommand: code=%d calls=%d stderr=%q", code, runtime.calls, stderr)

	runtime = &fakeMemoryRuntime{}
	code, out, stderr = runMemoryTest([]string{"memory", "sync", "status", "--json"}, bearer, runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(out, `"configured":false`) && !strings.Contains(out, bearer), "status: code=%d calls=%d out=%q stderr=%q", code, runtime.calls, out, stderr)
}

func TestMemoryCLI_HookLifecycleIsVersionedStrictAndPrivate(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := filepath.Join(t.TempDir(), "memory")
	runtime := &fakeMemoryRuntime{project: "project-1"}
	call := func(input string) (int, string, string) {
		return runMemoryTest([]string{"memory", "hook", "--stdin", "--storage-root", storageRoot, "--project-local"}, input, runtime)
	}
	start := `{"schemaVersion":1,"operation":"start","workspace":"` + workspace + `","provider":"openai","external_id":"secret-run"}`
	code, out, stderr := call(start)
	testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 2 && runtime.opts.StorageRoot == storageRoot && runtime.opts.ProjectLocal && runtime.start == (memory.ProviderSessionStart{Project: "project-1", Provider: "openai", ExternalID: "secret-run"}) && strings.Contains(out, `"schemaVersion":1`) && strings.Contains(out, `"session_handle":"ps-test"`) && !strings.Contains(out, "secret-run"), "start=%d %q %q opts=%+v request=%+v", code, out, stderr, runtime.opts, runtime.start)
	code, out, stderr = call(`{"schemaVersion":1,"operation":"checkpoint","workspace":"` + workspace + `","session_handle":"ps-test"}`)
	testutil.Require(t, code == 0 && stderr == "" && runtime.checkpoint == "ps-test" && strings.Contains(out, `"checkpointed":true`), "checkpoint=%d %q %q", code, out, stderr)
	code, out, stderr = call(`{"schemaVersion":1,"operation":"end","workspace":"` + workspace + `","session_handle":"ps-test","external_id":"secret-run","state":"completed","summary":"safe"}`)
	testutil.Require(t, code == 0 && stderr == "" && runtime.ended == (memory.ProviderSessionEnd{Project: "project-1", Handle: "ps-test", ExternalID: "secret-run", State: memory.ProviderSessionCompleted, Summary: "safe"}) && strings.Contains(out, `"state":"completed"`) && !strings.Contains(out, "secret-run") && !strings.Contains(out, "safe"), "end=%d %q %q request=%+v", code, out, stderr, runtime.ended)
	for _, input := range []string{`{"operation":"start","workspace":"` + workspace + `","provider":"openai","external_id":"x"}`, `{"schemaVersion":2,"operation":"start","workspace":"` + workspace + `","provider":"openai","external_id":"x"}`, `{"schemaVersion":1,"operation":"start","workspace":"` + workspace + `","provider":"openai","external_id":"x","unknown":true}`, `{"schemaVersion":1,"schemaVersion":1,"operation":"start","workspace":"` + workspace + `","provider":"openai","external_id":"x"}`, start + ` {}`} {
		before := runtime.calls
		code, out, _ = call(input)
		testutil.Require(t, code == 2 && out == "" && runtime.calls == before, "invalid hook=%d %q calls=%d", code, out, runtime.calls)
	}
}

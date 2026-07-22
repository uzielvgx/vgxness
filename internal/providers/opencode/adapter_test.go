package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/prompts"
	"github.com/vgxness/vgxness/internal/providers"
	"github.com/vgxness/vgxness/internal/registry"
)

func TestAdapterRunInjectsExactPromptAndFailClosedPermissions(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG", "/user/supplied/opencode.json")
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"agent":{"vgxness-runtime":{"prompt":"drift"}}}`)
	t.Setenv("OPENCODE_DISABLE_DEFAULT_PLUGINS", "true")
	workspace := t.TempDir()
	executor := &fakeExecutor{results: []processResult{{Stdout: resultEvent(t, validAgentResult(t))}}}
	adapter := testAdapter(t, workspace, executor)
	invocation := validInvocation(t, workspace)
	canonicalWorkspace := adapter.workspace

	result, err := adapter.Run(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != string(validAgentResult(t)) || len(executor.requests) != 1 {
		t.Fatalf("unexpected execution: result=%s requests=%d", result, len(executor.requests))
	}
	request := executor.requests[0]
	if request.Executable != "/test/opencode" || request.Directory != canonicalWorkspace {
		t.Fatalf("unexpected process boundary: %+v", request)
	}
	wantArgs := []string{
		"run", "--pure", "--format", "json", "--agent", runtimeAgentID(invocation.ExecutionID),
		"--dir", canonicalWorkspace, "--title", "vgxness-execution-1", "--model", "openai/gpt-5.1-codex", runtimeMessage,
	}
	if !reflect.DeepEqual(request.Args, wantArgs) {
		t.Fatalf("unexpected args:\nwant=%q\n got=%q", wantArgs, request.Args)
	}
	for _, argument := range request.Args {
		if argument == "--auto" || argument == "--yolo" || argument == "--dangerously-skip-permissions" {
			t.Fatalf("unsafe approval flag present: %q", argument)
		}
	}
	environment := environmentMap(request.Environment)
	if _, inherited := environment["OPENCODE_CONFIG"]; inherited {
		t.Fatal("custom persistent OpenCode config leaked into the execution")
	}
	var configuration struct {
		Agent map[string]struct {
			Prompt     string         `json:"prompt"`
			Mode       string         `json:"mode"`
			Steps      int            `json:"steps"`
			Permission map[string]any `json:"permission"`
		} `json:"agent"`
	}
	if err := json.Unmarshal([]byte(environment["OPENCODE_CONFIG_CONTENT"]), &configuration); err != nil {
		t.Fatal(err)
	}
	runtimeAgent, ok := configuration.Agent[runtimeAgentID(invocation.ExecutionID)]
	if !ok || runtimeAgent.Prompt != invocation.Prompt.System || runtimeAgent.Mode != "primary" || runtimeAgent.Steps != defaultMaxSteps {
		t.Fatalf("prompt/config drift: %+v", runtimeAgent)
	}
	if runtimeAgent.Permission["task"] != "deny" || runtimeAgent.Permission["external_directory"] != "deny" || runtimeAgent.Permission["bash"] != "deny" {
		t.Fatalf("permissions broadened: %+v", runtimeAgent.Permission)
	}
	if _, ok := runtimeAgent.Permission["edit"].(map[string]any); !ok {
		t.Fatalf("write operation did not receive path-bounded edit permission: %+v", runtimeAgent.Permission["edit"])
	}
	if environment["OPENCODE_PERMISSION"] == "" || environment["OPENCODE_DISABLE_DEFAULT_PLUGINS"] != "false" || environment["OPENCODE_DISABLE_AUTOUPDATE"] != "true" {
		t.Fatalf("missing runtime isolation environment: %+v", environment)
	}
	if environment["OPENCODE_CONFIG_DIR"] == "" || environment["OPENCODE_DISABLE_CLAUDE_CODE"] != "true" || environment["OPENCODE_YOLO"] != "false" {
		t.Fatalf("missing transient config isolation: %+v", environment)
	}
	if _, err := os.Stat(environment["OPENCODE_CONFIG_DIR"]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transient OpenCode config directory was not removed: %v", err)
	}
}

func TestAdapterHealthProbesVersion(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name   string
		result processResult
		err    error
		want   gatekeeper.AdapterHealth
	}{
		{name: "healthy", result: processResult{Stdout: []byte("1.18.4\n")}, want: gatekeeper.AdapterHealthy},
		{name: "newer compatible", result: processResult{Stdout: []byte("1.19.0\n")}, want: gatekeeper.AdapterHealthy},
		{name: "new major", result: processResult{Stdout: []byte("v2.0.0\n")}, want: gatekeeper.AdapterIncompatible},
		{name: "old", result: processResult{Stdout: []byte("1.18.3\n")}, want: gatekeeper.AdapterIncompatible},
		{name: "invalid", result: processResult{Stdout: []byte("development\n")}, want: gatekeeper.AdapterIncompatible},
		{name: "unavailable", err: errors.New("not found"), want: gatekeeper.AdapterUnavailable},
		{name: "overflow", result: processResult{StdoutOverflow: true}, want: gatekeeper.AdapterUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{results: []processResult{test.result}, errors: []error{test.err}}
			adapter := testAdapter(t, workspace, executor)
			checkedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
			adapter.now = func() time.Time { return checkedAt }
			health := adapter.Health(context.Background())
			if health.Status != test.want || !health.CheckedAt.Equal(checkedAt) {
				t.Fatalf("unexpected health: %+v", health)
			}
			if len(executor.requests) != 1 || !reflect.DeepEqual(executor.requests[0].Args, []string{"--version"}) {
				t.Fatalf("unexpected health probe: %+v", executor.requests)
			}
		})
	}
}

func TestAdapterRejectsScopePromptAndAuthorizationDrift(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	tests := []struct {
		name   string
		mutate func(*providers.Invocation)
		want   providers.FailureCategory
	}{
		{name: "prompt checksum", mutate: func(value *providers.Invocation) { value.Prompt.System += " " }, want: providers.FailureInvalidResult},
		{name: "prompt reference", mutate: func(value *providers.Invocation) { value.Prompt.PromptRef.Version = "other" }, want: providers.FailureInvalidResult},
		{name: "provider", mutate: func(value *providers.Invocation) { value.Agent.Provider.ID = "other" }, want: providers.FailureInvalidResult},
		{name: "current operation", mutate: func(value *providers.Invocation) { value.Operation = gatekeeper.Push }, want: providers.FailureInvalidResult},
		{name: "outside workspace", mutate: func(value *providers.Invocation) {
			value.Packet = packetFor(t, value.WorkUnitID, []string{outside}, nil, nil)
		}, want: providers.FailurePermissionDenied},
		{name: "multiple roots", mutate: func(value *providers.Invocation) {
			value.Packet = packetFor(t, value.WorkUnitID, []string{workspace, outside}, nil, nil)
		}, want: providers.FailureInvalidResult},
		{name: "background write", mutate: func(value *providers.Invocation) { value.Mode = chronicle.TaskBackground }, want: providers.FailurePermissionDenied},
		{name: "denied edit tool", mutate: func(value *providers.Invocation) { value.Agent.Permissions.DeniedTools = []string{"edit"} }, want: providers.FailurePermissionDenied},
		{name: "denied shell tool", mutate: func(value *providers.Invocation) {
			value.Operation = gatekeeper.RunCommand
			value.AuthorizedOperations = append(value.AuthorizedOperations, gatekeeper.RunCommand)
			value.Agent.Permissions.DeniedTools = []string{"shell", "git"}
		}, want: providers.FailurePermissionDenied},
		{name: "model flag injection", mutate: func(value *providers.Invocation) { value.Agent.Model = "--auto/model" }, want: providers.FailureIncompatible},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{}
			adapter := testAdapter(t, workspace, executor)
			invocation := validInvocation(t, workspace)
			test.mutate(&invocation)
			_, err := adapter.Run(context.Background(), invocation)
			assertFailure(t, err, test.want)
			if len(executor.requests) != 0 {
				t.Fatal("invalid invocation reached OpenCode")
			}
		})
	}
}

func TestAdapterUsesOpenCodeDefaultWhenModelIsUnspecified(t *testing.T) {
	workspace := t.TempDir()
	executor := &fakeExecutor{results: []processResult{{Stdout: resultEvent(t, validAgentResult(t))}}}
	config := testConfig(workspace)
	config.DefaultModel = ""
	adapter, err := newAdapter(config, executor, func(string) (string, error) { return "/test/opencode", nil })
	if err != nil {
		t.Fatal(err)
	}
	invocation := validInvocation(t, workspace)
	invocation.Agent.Model = ""
	if _, err := adapter.Run(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	for _, argument := range executor.requests[0].Args {
		if argument == "--model" {
			t.Fatalf("unspecified model emitted an OpenCode model override: %q", executor.requests[0].Args)
		}
	}
}

func TestAdapterPermissionsFollowExactOperation(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name      string
		operation gatekeeper.OperationClass
		mutate    func(*providers.Invocation)
		check     func(*testing.T, map[string]any)
	}{
		{
			name: "background read only", operation: gatekeeper.ReadFiles,
			mutate: func(value *providers.Invocation) { value.Mode = chronicle.TaskBackground },
			check: func(t *testing.T, permission map[string]any) {
				if permission["read"] == "deny" || permission["edit"] != "deny" || permission["bash"] != "deny" || permission["task"] != "deny" {
					t.Fatalf("background permission broadening: %+v", permission)
				}
			},
		},
		{
			name: "run command without commit", operation: gatekeeper.RunCommand,
			check: func(t *testing.T, permission map[string]any) {
				rules := permission["bash"].(map[string]any)
				if rules["*"] != "allow" || rules["git commit*"] != "deny" || rules["git push*"] != "deny" || rules["npm install *"] != "deny" {
					t.Fatalf("command permission broadening: %+v", rules)
				}
			},
		},
		{
			name: "commit only", operation: gatekeeper.Commit,
			mutate: func(value *providers.Invocation) { value.Agent.Permissions.MayCommit = true },
			check: func(t *testing.T, permission map[string]any) {
				rules := permission["bash"].(map[string]any)
				if rules["*"] != "deny" || rules["git commit*"] != "allow" || rules["git push*"] != "deny" {
					t.Fatalf("commit permission broadening: %+v", rules)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{results: []processResult{{Stdout: resultEvent(t, validAgentResult(t))}}}
			adapter := testAdapter(t, workspace, executor)
			invocation := validInvocation(t, workspace)
			invocation.Operation = test.operation
			invocation.AuthorizedOperations = append(invocation.AuthorizedOperations, test.operation)
			if test.mutate != nil {
				test.mutate(&invocation)
			}
			if _, err := adapter.Run(context.Background(), invocation); err != nil {
				t.Fatal(err)
			}
			environment := environmentMap(executor.requests[0].Environment)
			var permission map[string]any
			if err := json.Unmarshal([]byte(environment["OPENCODE_PERMISSION"]), &permission); err != nil {
				t.Fatal(err)
			}
			test.check(t, permission)
		})
	}
}

func TestAdapterReviewChangesDeniesAllFileReadsAndDiscoveryTools(t *testing.T) {
	workspace := t.TempDir()
	executor := &fakeExecutor{results: []processResult{{Stdout: resultEvent(t, validAgentResult(t))}}}
	adapter := testAdapter(t, workspace, executor)
	invocation := validInvocation(t, workspace)
	invocation.Operation = gatekeeper.ReadFiles
	invocation.Packet = packetForInputs(t, invocation.WorkUnitID, []string{workspace}, nil, []string{filepath.Join(workspace, "excluded")}, map[string]any{
		"operation": "review-changes", "git": map[string]any{"statusShort": "?? untracked.go\n"},
	})
	if _, err := adapter.Run(context.Background(), invocation); err != nil {
		t.Fatal(err)
	}
	var permission map[string]any
	environment := environmentMap(executor.requests[0].Environment)
	if err := json.Unmarshal([]byte(environment["OPENCODE_PERMISSION"]), &permission); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"read", "grep", "glob", "list"} {
		if permission[tool] != "deny" {
			t.Fatalf("review tool %q was not denied: %+v", tool, permission)
		}
	}
}

func TestAdapterRejectsAmbiguousOrFailedOutput(t *testing.T) {
	workspace := t.TempDir()
	valid := validAgentResult(t)
	tests := []struct {
		name     string
		result   processResult
		runError error
		want     providers.FailureCategory
	}{
		{name: "malformed event", result: processResult{Stdout: []byte("not-json\n")}, want: providers.FailureInvalidResult},
		{name: "error event", result: processResult{Stdout: []byte(`{"type":"error","error":{"name":"ProviderError"}}` + "\n")}, want: providers.FailureUnavailable},
		{name: "invalid terminal text after intermediate", result: processResult{Stdout: append(resultEvent(t, valid), resultEvent(t, []byte("done"))...)}, want: providers.FailureInvalidResult},
		{name: "markdown fence", result: processResult{Stdout: resultEvent(t, append([]byte("```json\n"), append(valid, []byte("\n```")...)...))}, want: providers.FailureInvalidResult},
		{name: "trailing object", result: processResult{Stdout: resultEvent(t, append(append([]byte{}, valid...), valid...))}, want: providers.FailureInvalidResult},
		{name: "stdout overflow", result: processResult{StdoutOverflow: true}, want: providers.FailureInvalidResult},
		{name: "stderr overflow", result: processResult{StderrOverflow: true}, want: providers.FailureInvalidResult},
		{name: "process failure", runError: errors.New("exit 1"), want: providers.FailureUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{results: []processResult{test.result}, errors: []error{test.runError}}
			adapter := testAdapter(t, workspace, executor)
			_, err := adapter.Run(context.Background(), validInvocation(t, workspace))
			assertFailure(t, err, test.want)
		})
	}
}

func TestAdapterAcceptsIntermediateTextWhenTerminalTextIsExactJSON(t *testing.T) {
	workspace := t.TempDir()
	valid := validAgentResult(t)
	stdout := append(resultEvent(t, []byte("Inspecting the bounded workspace.")), resultEvent(t, valid)...)
	executor := &fakeExecutor{results: []processResult{{Stdout: stdout}}}
	adapter := testAdapter(t, workspace, executor)
	result, err := adapter.Run(context.Background(), validInvocation(t, workspace))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != string(valid) {
		t.Fatalf("terminal result=%s want=%s", result, valid)
	}
}

func TestAdapterPropagatesCancellation(t *testing.T) {
	workspace := t.TempDir()
	executor := blockingExecutor{}
	adapter := testAdapter(t, workspace, executor)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Run(ctx, validInvocation(t, workspace)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	if _, err := adapter.Run(ctx, validInvocation(t, workspace)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected in-flight cancellation, got %v", err)
	}
}

func TestCommandExecutorKillsCanceledProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := (commandExecutor{}).Run(ctx, processRequest{
		Executable: os.Args[0], Args: []string{"-test.run=TestCommandExecutorHelper"},
		Directory: t.TempDir(), Environment: append(os.Environ(), "VGXNESS_OPENCODE_HELPER=1"), MaxBytes: 4096,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected process deadline cancellation, got %v result=%+v", err, result)
	}
}

func TestCommandExecutorHelper(t *testing.T) {
	if os.Getenv("VGXNESS_OPENCODE_HELPER") != "1" {
		return
	}
	time.Sleep(time.Hour)
}

func TestCommandExecutorKillsDescendants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses direct child termination")
	}
	directory := t.TempDir()
	sentinel := filepath.Join(directory, "survived")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := (commandExecutor{}).Run(ctx, processRequest{
		Executable: os.Args[0], Args: []string{"-test.run=TestCommandExecutorParentHelper"}, Directory: directory,
		Environment: append(os.Environ(), "VGXNESS_OPENCODE_PARENT_HELPER=1", "VGXNESS_OPENCODE_SENTINEL="+sentinel), MaxBytes: 4096,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected process deadline cancellation, got %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider descendant survived cancellation: %v", err)
	}
}

func TestCommandExecutorParentHelper(t *testing.T) {
	if os.Getenv("VGXNESS_OPENCODE_PARENT_HELPER") != "1" {
		return
	}
	child := exec.Command(os.Args[0], "-test.run=TestCommandExecutorGrandchildHelper")
	child.Env = append(os.Environ(), "VGXNESS_OPENCODE_GRANDCHILD_HELPER=1")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	time.Sleep(time.Hour)
}

func TestCommandExecutorGrandchildHelper(t *testing.T) {
	if os.Getenv("VGXNESS_OPENCODE_GRANDCHILD_HELPER") != "1" {
		return
	}
	time.Sleep(250 * time.Millisecond)
	if err := os.WriteFile(os.Getenv("VGXNESS_OPENCODE_SENTINEL"), []byte("survived"), 0o600); err != nil {
		os.Exit(3)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	workspace := t.TempDir()
	base := testConfig(workspace)
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "provider", mutate: func(value *Config) { value.Reference.Provider = "other" }},
		{name: "version", mutate: func(value *Config) { value.MinimumVersion = "latest" }},
		{name: "model", mutate: func(value *Config) { value.DefaultModel = "invalid" }},
		{name: "model flag injection", mutate: func(value *Config) { value.DefaultModel = "--auto/model" }},
		{name: "oversized model", mutate: func(value *Config) { value.DefaultModel = "openai/" + strings.Repeat("x", 512) }},
		{name: "variant", mutate: func(value *Config) { value.Variant = "high max" }},
		{name: "variant flag injection", mutate: func(value *Config) { value.Variant = "--auto" }},
		{name: "steps", mutate: func(value *Config) { value.MaxSteps = 257 }},
		{name: "output", mutate: func(value *Config) { value.MaxOutputBytes = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.mutate(&config)
			if _, err := newAdapter(config, &fakeExecutor{}, func(string) (string, error) { return "/test/opencode", nil }); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected invalid config, got %v", err)
			}
		})
	}
	if _, err := newAdapter(base, &fakeExecutor{}, func(string) (string, error) { return "", errors.New("missing") }); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected missing executable rejection, got %v", err)
	}
}

type fakeExecutor struct {
	results  []processResult
	errors   []error
	requests []processRequest
}

func (f *fakeExecutor) Run(_ context.Context, request processRequest) (processResult, error) {
	f.requests = append(f.requests, request)
	index := len(f.requests) - 1
	var result processResult
	if index < len(f.results) {
		result = f.results[index]
	}
	var err error
	if index < len(f.errors) {
		err = f.errors[index]
	}
	return result, err
}

type blockingExecutor struct{}

func (blockingExecutor) Run(ctx context.Context, _ processRequest) (processResult, error) {
	<-ctx.Done()
	return processResult{}, ctx.Err()
}

func testAdapter(t *testing.T, workspace string, executor processExecutor) *Adapter {
	t.Helper()
	adapter, err := newAdapter(testConfig(workspace), executor, func(string) (string, error) { return "/test/opencode", nil })
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testConfig(workspace string) Config {
	return Config{
		Reference:        registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
		InterfaceVersion: "1",
		Capabilities:     []registry.Capability{{Capability: "implementation", Version: "1"}},
		Executable:       "opencode", WorkingDirectory: workspace,
	}
}

func validInvocation(t *testing.T, workspace string) providers.Invocation {
	t.Helper()
	promptRef := registry.PromptReference{
		Kind: "prompt.reference", SchemaVersion: "1", ID: "forge-apply", Version: "1",
		Checksum: registry.Checksum{Algorithm: "sha256", Value: strings.Repeat("a", 64)},
	}
	system := `{"contract":"vgxness.prompt.bundle/v1","work":{"taskId":"work-1"}}`
	digest := sha256.Sum256([]byte(system))
	return providers.Invocation{
		ExecutionID: "execution-1", CorrelationID: "execution-1", WorkUnitID: "work-1",
		Mode: chronicle.TaskForeground, Operation: gatekeeper.WriteFiles,
		AuthorizedOperations: []gatekeeper.OperationClass{gatekeeper.ReadFiles, gatekeeper.WriteFiles},
		Packet:               packetFor(t, "work-1", []string{workspace}, []string{"shell", "git"}, nil),
		Agent: registry.Agent{
			ID: "forge", Provider: registry.ProviderReference{Provider: "opencode", ID: "primary", Version: "1"},
			PromptRef: promptRef, Model: "openai/gpt-5.1-codex",
			Permissions: registry.Permissions{
				MayReadFiles: true, MayWriteFiles: true, MayRunCommands: true,
				AllowedTools: []string{"shell", "git"}, AllowedPaths: []string{workspace},
			},
		},
		Prompt: prompts.Bundle{
			SchemaVersion: "1", AgentID: "forge", Audience: "subagent", PromptRef: promptRef,
			PromptRegistryVersion: "prompts-v1", System: system, SHA256: hex.EncodeToString(digest[:]),
		},
	}
}

func packetFor(t *testing.T, taskID string, allowedPaths, allowedTools, excluded []string) []byte {
	return packetForInputs(t, taskID, allowedPaths, allowedTools, excluded, nil)
}

func packetForInputs(t *testing.T, taskID string, allowedPaths, allowedTools, excluded []string, inputs map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"kind": "execution.packet", "schemaVersion": "1",
		"context": map[string]any{
			"taskId": taskID, "allowedPaths": allowedPaths, "allowedTools": allowedTools,
			"scope": map[string]any{"excluded": excluded}, "inputs": inputs,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func validAgentResult(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-1", "taskId": "work-1", "agentId": "forge",
		"status": "success", "summary": "done", "artifacts": []any{}, "nextRecommended": "verify", "risks": []any{}, "errors": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func resultEvent(t *testing.T, text []byte) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type": "text", "timestamp": 1, "sessionID": "session-1",
		"part": map[string]any{"type": "text", "text": string(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func environmentMap(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	return values
}

func assertFailure(t *testing.T, err error, category providers.FailureCategory) {
	t.Helper()
	var failure *providers.Failure
	if !errors.As(err, &failure) || failure.Category != category {
		t.Fatalf("expected %s failure, got %v", category, err)
	}
}

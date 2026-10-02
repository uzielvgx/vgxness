package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uzielvgx/vgxness/internal/config"
	"github.com/uzielvgx/vgxness/internal/memory"
)

// claudeSessionRuntime fakes only the session operations the hook adapter
// uses and records every call, including the retry path for completed ids.
type claudeSessionRuntime struct {
	fakeMemoryRuntime
	starts       []memory.ProviderSessionStart
	completedIDs map[string]bool
	draft        bool
	handoff      string
	resolveErr   error
	contextErr   error
}

func (f *claudeSessionRuntime) ResolveProject(_ context.Context, opts config.Options, workspace string) (string, error) {
	f.opts = opts
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return "project:" + workspace, nil
}

func (f *claudeSessionRuntime) StartProviderSession(_ context.Context, _ config.Options, request memory.ProviderSessionStart) (memory.ProviderSession, error) {
	f.starts = append(f.starts, request)
	if f.completedIDs[request.ExternalID] {
		return memory.ProviderSession{Project: request.Project, Handle: "ps-done", State: memory.ProviderSessionCompleted}, nil
	}
	return memory.ProviderSession{Project: request.Project, Handle: "ps-" + request.ExternalID, State: memory.ProviderSessionActive, LeaseToken: "lease-1", DraftPresent: f.draft}, f.err
}

func (f *claudeSessionRuntime) ProviderSessionContext(_ context.Context, _ config.Options, project, handle string) (memory.ProviderSessionContext, error) {
	if f.contextErr != nil {
		return memory.ProviderSessionContext{}, f.contextErr
	}
	return memory.ProviderSessionContext{Session: memory.ProviderSession{Project: project, Handle: handle}, Handoff: f.handoff}, nil
}

func runClaudeTest(t *testing.T, args []string, input string, runtime MemoryRuntime) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := RunProductRuntime(context.Background(), args, strings.NewReader(input), &out, &stderr, &fakeInspector{}, runtime)
	return code, out.String(), stderr.String()
}

func decodeContext(t *testing.T, stdout, wantEvent string) string {
	t.Helper()
	var output claudeHookOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("stdout %q is not hook JSON: %v", stdout, err)
	}
	if output.HookSpecificOutput.HookEventName != wantEvent {
		t.Fatalf("hookEventName = %q, want %q", output.HookSpecificOutput.HookEventName, wantEvent)
	}
	return output.HookSpecificOutput.AdditionalContext
}

const sessionStartInput = `{"session_id":"sess-1","transcript_path":"/t.jsonl","cwd":"/work","hook_event_name":"SessionStart","source":"startup","future_field":{"nested":true}}`

func TestClaudeSessionStartInjectsPolicyHandleAndHandoff(t *testing.T) {
	t.Setenv(projectDirEnv, "")
	policy := filepath.Join(t.TempDir(), "manager.md")
	if err := os.WriteFile(policy, []byte("  Manager policy text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &claudeSessionRuntime{handoff: "UNTRUSTED DATA\nprior_completed_summary=obs-1\nfinished the parser"}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "hook", "session-start", "--policy", policy}, sessionStartInput, runtime)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	got := decodeContext(t, stdout, "SessionStart")
	for _, want := range []string{"Manager policy text.", "session_handle for memory_context, memory_save and memory_session_summary is ps-sess-1.", "untrusted data", "finished the parser"} {
		if !strings.Contains(got, want) {
			t.Fatalf("context lacks %q:\n%s", want, got)
		}
	}
	if strings.HasPrefix(got, " ") || len(runtime.starts) != 1 || runtime.starts[0] != (memory.ProviderSessionStart{Project: "project:/work", Provider: "claude-code", ExternalID: "sess-1"}) || runtime.opts.ProjectDir != "/work" {
		t.Fatalf("context=%q starts=%+v opts=%+v", got, runtime.starts, runtime.opts)
	}
}

func TestClaudeSessionStartWithoutHandoffOrPolicyStillAnnouncesHandle(t *testing.T) {
	t.Setenv(projectDirEnv, "/env-project")
	runtime := &claudeSessionRuntime{}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "hook", "session-start"}, sessionStartInput, runtime)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	got := decodeContext(t, stdout, "SessionStart")
	if !strings.Contains(got, "No completed VGXNESS session handoff exists") || !strings.Contains(got, "ps-sess-1") || runtime.starts[0].Project != "project:/env-project" {
		t.Fatalf("context=%q starts=%+v", got, runtime.starts)
	}
}

func TestClaudeSessionStartBoundsContextAtClaudeLimit(t *testing.T) {
	t.Setenv(projectDirEnv, "")
	policy := filepath.Join(t.TempDir(), "manager.md")
	if err := os.WriteFile(policy, bytes.Repeat([]byte("p"), claudePolicyLimit+500), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &claudeSessionRuntime{handoff: strings.Repeat("h", 5_000)}
	_, stdout, _ := runClaudeTest(t, []string{"claude-code", "hook", "session-start", "--policy", policy}, sessionStartInput, runtime)
	got := decodeContext(t, stdout, "SessionStart")
	if n := len([]rune(got)); n != claudeContextLimit || !strings.HasPrefix(got, strings.Repeat("p", claudePolicyLimit)+"\n\n") {
		t.Fatalf("context length = %d, want %d; prefix ok = %v", n, claudeContextLimit, strings.HasPrefix(got, strings.Repeat("p", claudePolicyLimit)))
	}
}

func TestClaudeSessionStartRetriesCompletedSessionIDs(t *testing.T) {
	t.Setenv(projectDirEnv, "")
	runtime := &claudeSessionRuntime{completedIDs: map[string]bool{"sess-1": true, "sess-1#2": true}}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "hook", "session-start"}, sessionStartInput, runtime)
	if code != 0 || stderr != "" || len(runtime.starts) != 3 || runtime.starts[2].ExternalID != "sess-1#3" || !strings.Contains(stdout, "ps-sess-1#3") {
		t.Fatalf("code=%d stderr=%q starts=%+v stdout=%q", code, stderr, runtime.starts, stdout)
	}
	exhausted := &claudeSessionRuntime{completedIDs: map[string]bool{"sess-1": true, "sess-1#2": true, "sess-1#3": true, "sess-1#4": true}}
	code, stdout, stderr = runClaudeTest(t, []string{"claude-code", "hook", "session-start"}, sessionStartInput, exhausted)
	if code != 0 || stdout != "" || !strings.Contains(stderr, "session-start skipped: conflict") || len(exhausted.starts) != claudeSessionRetries {
		t.Fatalf("code=%d stdout=%q stderr=%q starts=%d", code, stdout, stderr, len(exhausted.starts))
	}
}

func TestClaudePreCompactRenewsLeaseWithoutOutput(t *testing.T) {
	t.Setenv(projectDirEnv, "")
	runtime := &claudeSessionRuntime{}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "hook", "pre-compact"}, `{"session_id":"sess-1","cwd":"/work","hook_event_name":"PreCompact","trigger":"auto"}`, runtime)
	if code != 0 || stdout != "" || stderr != "" || len(runtime.starts) != 1 || runtime.ended.Handle != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q starts=%d ended=%+v", code, stdout, stderr, len(runtime.starts), runtime.ended)
	}
}

func TestClaudeSessionEndCompletesOnlyWhenADraftExists(t *testing.T) {
	t.Setenv(projectDirEnv, "")
	input := `{"session_id":"sess-1","cwd":"/work","hook_event_name":"SessionEnd","reason":"other"}`
	noDraft := &claudeSessionRuntime{}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "hook", "session-end"}, input, noDraft)
	if code != 0 || stdout != "" || stderr != "" || noDraft.ended.Handle != "" {
		t.Fatalf("no draft: code=%d stdout=%q stderr=%q ended=%+v", code, stdout, stderr, noDraft.ended)
	}
	withDraft := &claudeSessionRuntime{draft: true}
	code, stdout, stderr = runClaudeTest(t, []string{"claude-code", "hook", "session-end"}, input, withDraft)
	want := memory.ProviderSessionEnd{Project: "project:/work", Handle: "ps-sess-1", ExternalID: "sess-1", LeaseToken: "lease-1", State: memory.ProviderSessionCompleted}
	if code != 0 || stdout != "" || stderr != "" || withDraft.ended != want {
		t.Fatalf("draft: code=%d stdout=%q stderr=%q ended=%+v", code, stdout, stderr, withDraft.ended)
	}
}

func TestClaudeSubagentStartInjectsPolicyWithoutTouchingMemory(t *testing.T) {
	policy := filepath.Join(t.TempDir(), "worker.md")
	if err := os.WriteFile(policy, []byte("Worker contract."), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &claudeSessionRuntime{resolveErr: errors.New("must not be called")}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "hook", "subagent-start", "--policy", policy}, `{"session_id":"sess-1","cwd":"/work","hook_event_name":"SubagentStart","agent_type":"vgxness:explore"}`, runtime)
	if code != 0 || stderr != "" || decodeContext(t, stdout, "SubagentStart") != "Worker contract." || len(runtime.starts) != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runClaudeTest(t, []string{"claude-code", "hook", "subagent-start"}, `{"session_id":"sess-1","cwd":"/work"}`, runtime)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("no policy: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestClaudeHookFailsClosed(t *testing.T) {
	t.Setenv(projectDirEnv, "")
	for _, tc := range []struct {
		name    string
		args    []string
		input   string
		runtime MemoryRuntime
		stderr  string
	}{
		{name: "storage unavailable", args: []string{"claude-code", "hook", "session-start"}, input: sessionStartInput, runtime: &claudeSessionRuntime{resolveErr: memory.ErrCorrupt}, stderr: "session-start skipped: operational"},
		{name: "context unavailable", args: []string{"claude-code", "hook", "session-start"}, input: sessionStartInput, runtime: &claudeSessionRuntime{contextErr: memory.ErrConflict}, stderr: "session-start skipped: conflict"},
		{name: "empty stdin", args: []string{"claude-code", "hook", "session-start"}, input: "", runtime: &claudeSessionRuntime{}, stderr: "session-start skipped"},
		{name: "malformed stdin", args: []string{"claude-code", "hook", "session-start"}, input: "{not json", runtime: &claudeSessionRuntime{}, stderr: "session-start skipped"},
		{name: "missing session id", args: []string{"claude-code", "hook", "session-end"}, input: `{"cwd":"/work"}`, runtime: &claudeSessionRuntime{}, stderr: "session-end skipped"},
		{name: "missing policy file", args: []string{"claude-code", "hook", "subagent-start", "--policy", "/nonexistent/worker.md"}, input: `{"session_id":"s"}`, runtime: &claudeSessionRuntime{}, stderr: "subagent-start skipped"},
		{name: "nil runtime", args: []string{"claude-code", "hook", "session-start"}, input: sessionStartInput, runtime: nil, stderr: "session-start skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runClaudeTest(t, tc.args, tc.input, tc.runtime)
			if code != 0 || stdout != "" || !strings.Contains(stderr, tc.stderr) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestClaudeUsageErrorsAndSetup(t *testing.T) {
	for _, args := range [][]string{{"claude-code"}, {"claude-code", "hook"}, {"claude-code", "hook", "unknown-event"}, {"claude-code", "hook", "session-start", "extra"}, {"claude-code", "setup", "extra"}} {
		code, stdout, stderr := runClaudeTest(t, args, `{"session_id":"s","cwd":"/w"}`, &claudeSessionRuntime{})
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage: vgxness claude-code") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runClaudeTest(t, []string{"claude-code", "setup"}, "", &claudeSessionRuntime{})
	var rules map[string]map[string][]string
	if code != 0 || stderr != "" || json.Unmarshal([]byte(stdout), &rules) != nil || len(rules["permissions"]["allow"]) != 6 || len(rules["permissions"]["ask"]) != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, rule := range append(rules["permissions"]["allow"], rules["permissions"]["ask"]...) {
		if !strings.HasPrefix(rule, "mcp__plugin_vgxness_memory__memory_") {
			t.Fatalf("rule %q lacks the plugin tool prefix", rule)
		}
	}
}

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/navigator"
)

func TestOrchestrationCommandsExposeLifecycleWithoutRawBridgeInput(t *testing.T) {
	runtime := &fakeBridgeRuntime{response: bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode", Status: "running",
		Orchestration: &bridge.OrchestrationView{
			OrchestrationID: "orchestration-1", OwnerID: "owner-2", Status: "running", CurrentWave: 1,
			Plan: navigator.Plan{Decision: "parallel", Rationale: "two independent reads", Tasks: make([]navigator.Task, 2), Waves: make([]navigator.Wave, 1)},
		},
	}}
	workspace := t.TempDir()
	for _, test := range []struct {
		command string
		args    []string
		action  string
	}{
		{"status", []string{"status", "--workspace", workspace, "--id", "orchestration-1"}, "status"},
		{"resume", []string{"resume", "--workspace", workspace, "--id", "orchestration-1", "--owner", "owner-1"}, "resume"},
		{"cancel", []string{"cancel", "--workspace", workspace, "--id", "orchestration-1", "--owner", "owner-2"}, "cancel"},
		{"explain", []string{"explain", "--workspace", workspace, "--id", "orchestration-1"}, "status"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runOrchestration(context.Background(), test.args, &stdout, &stderr, runtime); code != 0 || runtime.action != test.action {
			t.Fatalf("%s code=%d action=%q stdout=%q stderr=%q", test.command, code, runtime.action, stdout.String(), stderr.String())
		}
		if test.command == "explain" && (!strings.Contains(stdout.String(), "decision=parallel") || !strings.Contains(stdout.String(), "tasks=2")) {
			t.Fatalf("explain output=%q", stdout.String())
		}
	}
}

func TestOrchestrationResumeRequiresCurrentOwner(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runOrchestration(context.Background(), []string{"resume", "--workspace", t.TempDir(), "--id", "orchestration-1"}, &stdout, &stderr, &fakeBridgeRuntime{})
	if code != 2 || !strings.Contains(stderr.String(), "invalid orchestration arguments") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

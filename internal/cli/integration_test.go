package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/testutil"
)

type fakeIntegrationRuntime struct {
	result  integration.Result
	err     error
	action  string
	options integration.Options
	calls   int
}

func (runtime *fakeIntegrationRuntime) Preview(_ context.Context, options integration.Options) (integration.Result, error) {
	return runtime.call("preview", options)
}
func (runtime *fakeIntegrationRuntime) Install(_ context.Context, options integration.Options) (integration.Result, error) {
	return runtime.call("install", options)
}
func (runtime *fakeIntegrationRuntime) Status(_ context.Context, options integration.Options) (integration.Result, error) {
	return runtime.call("status", options)
}
func (runtime *fakeIntegrationRuntime) Uninstall(_ context.Context, options integration.Options) (integration.Result, error) {
	return runtime.call("uninstall", options)
}
func (runtime *fakeIntegrationRuntime) call(action string, options integration.Options) (integration.Result, error) {
	runtime.calls++
	runtime.action = action
	runtime.options = options
	return runtime.result, runtime.err
}

func runIntegrationTest(args []string, runtime integration.Runtime) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := RunRuntime(context.Background(), args, strings.NewReader(""), &stdout, &stderr, &fakeInspector{}, nil, runtime)
	return code, stdout.String(), stderr.String()
}

func TestIntegrationCLI_RoutesEverySupportedAction(t *testing.T) {
	for _, action := range []string{"preview", "install", "status", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateInstalled, Bridge: integration.BridgeConfigured, Path: "/tmp/config/agent.md", ArtifactSHA256: strings.Repeat("a", 64), ToolPath: "/tmp/config/tools/vgxness.ts", ToolSHA256: strings.Repeat("b", 64), Changed: action == "install"}}
			args := []string{"integrate", "opencode", action, "--config-dir", "/tmp/config"}
			if action == "preview" || action == "install" {
				args = append(args, "--model", "openai/gpt-5.6-sol")
			}
			code, stdout, stderr := runIntegrationTest(args, runtime)
			modelOK := action != "preview" && action != "install" || runtime.options.Model == "openai/gpt-5.6-sol"
			testutil.Require(t, code == 0 && runtime.calls == 1 && runtime.action == action && runtime.options.ConfigDir == "/tmp/config" && modelOK && stderr == "", "exit=%d calls=%d action=%q options=%#v stderr=%q", code, runtime.calls, runtime.action, runtime.options, stderr)
			testutil.Require(t, strings.Contains(stdout, "provider=opencode\n") && strings.Contains(stdout, "state=installed\n") && strings.Contains(stdout, "bridge=configured\n") && strings.Contains(stdout, "tool_path=/tmp/config/tools/vgxness.ts\n") && strings.Contains(stdout, "changed="), "output=%q", stdout)
		})
	}
}

func TestIntegrationCLI_RejectsUnsupportedInputWithoutCallingRuntime(t *testing.T) {
	for _, args := range [][]string{
		{"integrate"},
		{"integrate", "codex", "status"},
		{"integrate", "opencode", "repair"},
		{"integrate", "opencode", "status", "extra"},
		{"integrate", "opencode", "status", "--unknown"},
	} {
		runtime := &fakeIntegrationRuntime{}
		code, stdout, _ := runIntegrationTest(args, runtime)
		testutil.Require(t, code == 2 && stdout == "" && runtime.calls == 0, "args=%v exit=%d calls=%d output=%q", args, code, runtime.calls, stdout)
	}
}

func TestIntegrationCLI_RequiresModelForPreviewAndInstall(t *testing.T) {
	for _, action := range []string{"preview", "install"} {
		runtime := &fakeIntegrationRuntime{}
		code, stdout, stderr := runIntegrationTest([]string{"integrate", "opencode", action}, runtime)
		testutil.Require(t, code == 2 && stdout == "" && runtime.calls == 0 && strings.Contains(stderr, "--model provider/model is required"), "action=%s exit=%d calls=%d stderr=%q", action, code, runtime.calls, stderr)
	}
}

func TestIntegrationCLI_ClassifiesErrorsAndKeepsOutputAtomic(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		text string
	}{
		{integration.ErrInvalid, 2, "invalid:"},
		{integration.ErrConflict, 1, "conflict:"},
		{integration.ErrDrift, 1, "drift:"},
		{context.Canceled, 130, "cancelled:"},
		{errors.New("secret /private/path"), 1, "io:"},
	} {
		runtime := &fakeIntegrationRuntime{err: tc.err}
		code, stdout, stderr := runIntegrationTest([]string{"integrate", "opencode", "install", "--model", "openai/gpt-5.6-sol"}, runtime)
		testutil.Require(t, code == tc.code && stdout == "" && strings.Contains(stderr, tc.text) && !strings.Contains(stderr, "secret"), "error=%v exit=%d stdout=%q stderr=%q", tc.err, code, stdout, stderr)
	}
}

func TestIntegrationCLI_EscapesPathsAndPrintsRecoverableBackup(t *testing.T) {
	runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateAbsent, Path: "/tmp/line\nagent", ArtifactSHA256: strings.Repeat("b", 64), ToolPath: "/tmp/tool\nfile", ToolSHA256: strings.Repeat("c", 64), Changed: true, BackupPath: "/tmp/backup\x1b", ToolBackupPath: "/tmp/tool-backup\x1b"}}
	code, stdout, stderr := runIntegrationTest([]string{"integrate", "opencode", "uninstall"}, runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(stdout, `path=/tmp/line\nagent`) && strings.Contains(stdout, `tool_path=/tmp/tool\nfile`) && strings.Contains(stdout, `backup=/tmp/backup\x1b`) && strings.Contains(stdout, `tool_backup=/tmp/tool-backup\x1b`), "exit=%d stdout=%q stderr=%q", code, stdout, stderr)
}

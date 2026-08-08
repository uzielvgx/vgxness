package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

type fakeIntegrationRuntime struct {
	result  integration.Result
	err     error
	action  string
	options integration.Options
	calls   int
}

func TestIntegrationCLI_ModelPlanFlagsAndResolvedOutput(t *testing.T) {
	runtime := &fakeIntegrationRuntime{result: integration.Result{
		Provider: "opencode", State: integration.StateAbsent, ModelPlan: sdd.PlanHigh, ModelProvider: "acme",
		ModelEfficient: "acme/fast", ModelBalanced: "acme/balanced", ModelFrontier: "acme/frontier",
		ManifestPath: "/tmp/config/vgxness/model-plan.json", RestartRequired: true, DirectoryDurability: "fsync",
	}}
	code, stdout, stderr := runIntegrationTest([]string{
		"integrate", "opencode", "preview", "--model-plan", "high",
		"--model-efficient", "acme/fast", "--model-balanced", "acme/balanced", "--model-frontier", "acme/frontier",
		"--model", "legacy/ignored",
	}, runtime)
	if code != 0 || stderr != "" || runtime.options.ModelPlan != sdd.PlanHigh || runtime.options.ModelEfficient != "acme/fast" {
		t.Fatalf("code=%d options=%+v stderr=%q", code, runtime.options, stderr)
	}
	for _, expected := range []string{"model_plan=high", "model_provider=acme", "model_efficient=acme/fast", "model_balanced=acme/balanced", "model_frontier=acme/frontier", "model_manifest=/tmp/config/vgxness/model-plan.json", "restart_required=true", "directory_durability=fsync"} {
		if !strings.Contains(stdout, expected+"\n") {
			t.Fatalf("output missing %q: %q", expected, stdout)
		}
	}
	if strings.Contains(stdout, "retained_predecessors=") {
		t.Fatalf("zero retained predecessors were rendered: %q", stdout)
	}
}

func TestIntegrationCLIRendersRetainedPredecessorEvidence(t *testing.T) {
	runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateInstalled, RetainedPredecessorCount: 2, RetainedPredecessorPath: "/config/vgxness/retained-predecessors"}}
	_, stdout, stderr := runIntegrationTest([]string{"integrate", "opencode", "status"}, runtime)
	if stderr != "" || !strings.Contains(stdout, "retained_predecessors=2\n") || !strings.Contains(stdout, "retained_predecessor_location=/config/vgxness/retained-predecessors\n") {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
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
func (runtime *fakeIntegrationRuntime) Reinstall(_ context.Context, options integration.Options) (integration.Result, error) {
	return runtime.call("reinstall", options)
}
func (*fakeIntegrationRuntime) ManagedLayout(context.Context, integration.Options) (integration.ManagedLayout, error) {
	return integration.ManagedLayout{}, nil
}
func (*fakeIntegrationRuntime) ReinstallPending(context.Context, integration.Options) (bool, error) {
	return false, nil
}
func (runtime *fakeIntegrationRuntime) call(action string, options integration.Options) (integration.Result, error) {
	runtime.calls++
	runtime.action = action
	runtime.options = options
	return runtime.result, runtime.err
}

func runIntegrationTest(args []string, runtime integration.Runtime) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := RunProductSDDRuntime(context.Background(), args, strings.NewReader(""), &stdout, &stderr, &fakeInspector{}, nil, runtime, runtime, nil, nil, nil)
	return code, stdout.String(), stderr.String()
}

func TestIntegrationCLI_RoutesCodexLifecycle(t *testing.T) {
	for _, action := range []string{"preview", "install", "status", "reinstall", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateInstalled}}
			code, stdout, stderr := runIntegrationTest([]string{"integrate", "codex", action, "--config-dir", "/tmp/codex"}, runtime)
			testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 1 && runtime.action == action && runtime.options.ConfigDir == "/tmp/codex", "exit=%d calls=%d action=%q options=%#v stderr=%q", code, runtime.calls, runtime.action, runtime.options, stderr)
			testutil.Require(t, strings.Contains(stdout, "provider=codex\n"), "output=%q", stdout)
		})
	}
}

func TestIntegrationCLI_CodexUsesHomeWhenConfigDirIsOmitted(t *testing.T) {
	runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateAbsent}}
	code, _, stderr := runIntegrationTest([]string{"integrate", "codex", "preview"}, runtime)
	testutil.Require(t, code == 0 && stderr == "" && runtime.calls == 1 && runtime.options.ConfigDir == "" && runtime.options.HomeDir != "", "exit=%d calls=%d options=%#v stderr=%q", code, runtime.calls, runtime.options, stderr)
}

func TestIntegrationCLI_RoutesEverySupportedAction(t *testing.T) {
	for _, action := range []string{"preview", "install", "status", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateInstalled, Path: "/tmp/config/agent.md", ArtifactSHA256: strings.Repeat("a", 64), Changed: action == "install"}}
			args := []string{"integrate", "opencode", action, "--config-dir", "/tmp/config"}
			code, stdout, stderr := runIntegrationTest(args, runtime)
			testutil.Require(t, code == 0 && runtime.calls == 1 && runtime.action == action && runtime.options.ConfigDir == "/tmp/config" && stderr == "", "exit=%d calls=%d action=%q options=%#v stderr=%q", code, runtime.calls, runtime.action, runtime.options, stderr)
			testutil.Require(t, strings.Contains(stdout, "provider=opencode\n") && strings.Contains(stdout, "state=installed\n") && strings.Contains(stdout, "projection=native+sdd-storage\n") && !strings.Contains(stdout, "storage_plugin=") && !strings.Contains(stdout, "model=") && strings.Contains(stdout, "changed="), "output=%q", stdout)
		})
	}
}

func TestIntegrationCLI_RejectsUnsupportedInputWithoutCallingRuntime(t *testing.T) {
	for _, args := range [][]string{
		{"integrate"},
		{"integrate", "opencode", "repair"},
		{"integrate", "opencode", "reinstall"},
		{"integrate", "opencode", "status", "extra"},
		{"integrate", "opencode", "status", "--unknown"},
		{"integrate", "opencode", "preview", "--model-plan", "extreme"},
	} {
		runtime := &fakeIntegrationRuntime{}
		code, stdout, _ := runIntegrationTest(args, runtime)
		testutil.Require(t, code == 2 && stdout == "" && runtime.calls == 0, "args=%v exit=%d calls=%d output=%q", args, code, runtime.calls, stdout)
	}
}

func TestIntegrationCLI_InvalidProviderArgumentsShowAccurateUsage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		contains []string
		absent   []string
	}{
		{
			name:     "opencode",
			args:     []string{"integrate", "opencode", "status", "--unknown"},
			contains: []string{"usage: vgxness integrate opencode <preview|install|status|uninstall>", "--model-plan", "--model-efficient"},
			absent:   []string{"reinstall"},
		},
		{
			name:     "codex",
			args:     []string{"integrate", "codex", "status", "--model-plan", "high"},
			contains: []string{"usage: vgxness integrate codex <preview|install|status|reinstall|uninstall>", "--config-dir PATH"},
			absent:   []string{"--model", "--model-plan", "--model-efficient"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &fakeIntegrationRuntime{}
			code, stdout, stderr := runIntegrationTest(tc.args, runtime)
			testutil.Require(t, code == 2 && stdout == "" && runtime.calls == 0, "exit=%d calls=%d stdout=%q", code, runtime.calls, stdout)
			for _, text := range tc.contains {
				testutil.Require(t, strings.Contains(stderr, text), "stderr missing %q: %q", text, stderr)
			}
			for _, text := range tc.absent {
				testutil.Require(t, !strings.Contains(stderr, text), "stderr unexpectedly contains %q: %q", text, stderr)
			}
		})
	}
}

func TestIntegrationCLI_DeprecatedModelFlagIsAcceptedAndIgnored(t *testing.T) {
	for _, action := range []string{"preview", "install"} {
		runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateAbsent}}
		code, _, stderr := runIntegrationTest([]string{"integrate", "opencode", action}, runtime)
		withoutModel := runtime.options
		testutil.Require(t, code == 0 && runtime.calls == 1 && stderr == "", "action=%s exit=%d calls=%d stderr=%q", action, code, runtime.calls, stderr)

		runtime = &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateAbsent}}
		code, _, stderr = runIntegrationTest([]string{"integrate", "opencode", action, "--model", "legacy/model"}, runtime)
		testutil.Require(t, code == 0 && runtime.calls == 1 && runtime.options == withoutModel && stderr == "", "compat action=%s exit=%d calls=%d options=%#v stderr=%q", action, code, runtime.calls, runtime.options, stderr)
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
		{errors.Join(integration.ErrConflict, integration.ErrRecovery), 1, "recovery:"},
		{errors.Join(context.Canceled, integration.ErrRecovery), 1, "recovery:"},
		{context.Canceled, 130, "cancelled:"},
		{errors.New("secret /private/path"), 1, "io:"},
	} {
		runtime := &fakeIntegrationRuntime{err: tc.err}
		code, stdout, stderr := runIntegrationTest([]string{"integrate", "opencode", "install"}, runtime)
		testutil.Require(t, code == tc.code && stdout == "" && strings.Contains(stderr, tc.text) && !strings.Contains(stderr, "secret"), "error=%v exit=%d stdout=%q stderr=%q", tc.err, code, stdout, stderr)
	}
}

func TestIntegrationCLI_EscapesPathsAndPrintsRecoverableBackup(t *testing.T) {
	runtime := &fakeIntegrationRuntime{result: integration.Result{Provider: "opencode", State: integration.StateAbsent, Path: "/tmp/line\nagent", ArtifactSHA256: strings.Repeat("b", 64), Changed: true, BackupPath: "/tmp/backup\x1b"}}
	code, stdout, stderr := runIntegrationTest([]string{"integrate", "opencode", "uninstall"}, runtime)
	testutil.Require(t, code == 0 && stderr == "" && strings.Contains(stdout, `path=/tmp/line\nagent`) && strings.Contains(stdout, `backup=/tmp/backup\x1b`) && !strings.Contains(stdout, `storage_plugin_backup=`), "exit=%d stdout=%q stderr=%q", code, stdout, stderr)
}

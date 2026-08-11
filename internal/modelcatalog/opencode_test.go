package modelcatalog

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

type runnerCall struct {
	executable string
	args       []string
}

type fakeRunner struct {
	stdout string
	stderr string
	err    error
	wait   bool
	calls  []runnerCall
}

func (runner *fakeRunner) Run(ctx context.Context, executable string, args []string, stdout, stderr io.Writer) error {
	runner.calls = append(runner.calls, runnerCall{executable: executable, args: append([]string(nil), args...)})
	if runner.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	_, _ = io.WriteString(stdout, runner.stdout)
	_, _ = io.WriteString(stderr, runner.stderr)
	return runner.err
}

func TestDiscoverUsesPureLocalCommandAndReturnsDeterministicSnapshot(t *testing.T) {
	runner := &fakeRunner{stdout: "zeta/model-b\r\n{\"variants\":{}}\r\nalpha/model-a\r\n{\"variants\":{}}\r\nzeta/model-a\r\n{\"variants\":{}}\r\n"}
	discovery := NewOpenCode("/opt/tools/opencode", runner, Options{})

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	wantCall := runnerCall{executable: "/opt/tools/opencode", args: []string{"models", "--pure", "--verbose"}}
	if !reflect.DeepEqual(runner.calls, []runnerCall{wantCall}) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, []runnerCall{wantCall})
	}
	want := Snapshot{
		Source:    SourceLocal,
		Providers: []string{"alpha", "zeta"},
		Models:    []string{"alpha/model-a", "zeta/model-a", "zeta/model-b"},
		Variants: map[string][]string{
			"alpha/model-a": {}, "zeta/model-a": {}, "zeta/model-b": {},
		},
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot = %#v, want %#v", snapshot, want)
	}
}

func TestDiscoverPreservesVerboseVariantsVerbatim(t *testing.T) {
	runner := &fakeRunner{stdout: "openai/gpt-5.6-terra\r\n{\"variants\":{\"xhigh\":{},\"max\":{},\"none\":{}}}\r\nopenai/gpt-5.6-luna\r\n{\"variants\":{}}\r\n"}
	discovery := NewOpenCode("opencode", runner, Options{})

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if !reflect.DeepEqual(snapshot.Variants, map[string][]string{
		"openai/gpt-5.6-terra": {"xhigh", "max", "none"},
		"openai/gpt-5.6-luna":  {},
	}) {
		t.Fatalf("Variants = %#v", snapshot.Variants)
	}
	if want := []string{"models", "--pure", "--verbose"}; !reflect.DeepEqual(runner.calls[0].args, want) {
		t.Fatalf("args = %#v, want %#v", runner.calls[0].args, want)
	}
}

func TestRefreshUsesExplicitRefreshFlag(t *testing.T) {
	runner := &fakeRunner{stdout: "alpha/model-a\n{\"variants\":{}}\n"}
	discovery := NewOpenCode("opencode-custom", runner, Options{})

	snapshot, err := discovery.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	wantCall := runnerCall{executable: "opencode-custom", args: []string{"models", "--pure", "--verbose", "--refresh"}}
	if !reflect.DeepEqual(runner.calls, []runnerCall{wantCall}) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, []runnerCall{wantCall})
	}
	if snapshot.Source != SourceRefreshed {
		t.Fatalf("Source = %q, want %q", snapshot.Source, SourceRefreshed)
	}
}

func TestValidReferenceUsesAuthoritativeBoundedGrammar(t *testing.T) {
	valid := []string{
		"provider/model",
		"provider/model:variant@host+feature/nested",
		strings.Repeat("a", maxSegmentBytes) + "/" + strings.Repeat("b", maxReferenceBytes-maxSegmentBytes-1),
	}
	for _, reference := range valid {
		if provider, ok := ValidReference(reference); !ok || provider != strings.Split(reference, "/")[0] {
			t.Errorf("ValidReference(%q) = (%q, %t), want provider and true", reference, provider, ok)
		}
	}

	invalid := []string{
		"model", "@provider/model", "provider//model", "provider/model name",
		"provider/model?query", "provider/model#fragment", "provider/model=value", `provider/model\path`,
		strings.Repeat("a", maxSegmentBytes+1) + "/model",
		strings.Repeat("a", maxSegmentBytes) + "/" + strings.Repeat("b", maxSegmentBytes),
	}
	for _, reference := range invalid {
		if provider, ok := ValidReference(reference); ok || provider != "" {
			t.Errorf("ValidReference(%q) = (%q, %t), want empty and false", reference, provider, ok)
		}
	}
}

func TestDiscoverRejectsMalformedOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{name: "empty", output: ""},
		{name: "blank line", output: "alpha/model\n\nbeta/model\n"},
		{name: "missing provider", output: "/model\n"},
		{name: "missing model", output: "provider/\n"},
		{name: "missing nested segment", output: "provider/model//variant\n"},
		{name: "missing separator", output: "model\n"},
		{name: "leading at", output: "@provider/model\n"},
		{name: "whitespace", output: "provider/model name\n"},
		{name: "control", output: "provider/model\x00\n"},
		{name: "ansi", output: "\x1b[31mprovider/model\x1b[0m\n"},
		{name: "oversized reference", output: "provider/" + strings.Repeat("m", maxReferenceBytes) + "\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{stdout: test.output}
			discovery := NewOpenCode("opencode", runner, Options{})

			if snapshot, err := discovery.Discover(context.Background()); !errors.Is(err, ErrInvalidOutput) || !reflect.DeepEqual(snapshot, Snapshot{}) {
				t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrInvalidOutput", snapshot, err)
			}
		})
	}
}

func TestDiscoverRejectsUnsafeVerboseRecords(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{name: "missing JSON", output: "openai/model\n"},
		{name: "missing variants", output: "openai/model\n{}\n"},
		{name: "trailing JSON", output: "openai/model\n{\"variants\":{}} {}\n"},
		{name: "duplicate reference", output: "openai/model\n{\"variants\":{}}\nopenai/model\n{\"variants\":{}}\n"},
		{name: "unsafe token", output: "openai/model\n{\"variants\":{\"max\\n\":{}}}\n"},
		{name: "oversized token", output: "openai/model\n{\"variants\":{\"" + strings.Repeat("x", maxVariantBytes+1) + "\":{}}}\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			discovery := NewOpenCode("opencode", &fakeRunner{stdout: test.output}, Options{})
			if snapshot, err := discovery.Discover(context.Background()); !errors.Is(err, ErrInvalidOutput) || !reflect.DeepEqual(snapshot, Snapshot{}) {
				t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrInvalidOutput", snapshot, err)
			}
		})
	}
}

func TestDiscoverFailsClosedWhenOutputExceedsLimits(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
	}{
		{name: "stdout", stdout: "provider/model\nprovider/another\n"},
		{name: "stderr", stdout: "provider/model\n", stderr: strings.Repeat("s", 17)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{stdout: test.stdout, stderr: test.stderr}
			discovery := NewOpenCode("opencode", runner, Options{MaxStdoutBytes: 16, MaxStderrBytes: 16})

			if snapshot, err := discovery.Discover(context.Background()); !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
				t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
			}
		})
	}
}

func TestDiscoverHonorsCancellationAndTimeout(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		runner := &fakeRunner{wait: true}
		discovery := NewOpenCode("opencode", runner, Options{Timeout: time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if snapshot, err := discovery.Discover(ctx); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(snapshot, Snapshot{}) {
			t.Fatalf("Discover() = (%#v, %v), want zero snapshot and context.Canceled", snapshot, err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		runner := &fakeRunner{wait: true}
		discovery := NewOpenCode("opencode", runner, Options{Timeout: time.Millisecond})

		if snapshot, err := discovery.Discover(context.Background()); !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(snapshot, Snapshot{}) {
			t.Fatalf("Discover() = (%#v, %v), want zero snapshot and context.DeadlineExceeded", snapshot, err)
		}
	})
}

func TestDiscoverDoesNotDiscloseProcessOutput(t *testing.T) {
	const sensitive = "token-secret-from-process"
	runner := &fakeRunner{stdout: sensitive, stderr: sensitive, err: errors.New(sensitive)}
	discovery := NewOpenCode("opencode", runner, Options{})

	snapshot, err := discovery.Discover(context.Background())
	if !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Fatalf("error disclosed process output: %q", err)
	}
}

func TestExecCommandBoundsPipeWaitAndPreservesDirectArgv(t *testing.T) {
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	args := []string{"models", "--pure"}

	command := newExecCommand(context.Background(), "opencode-test", args, stdout, stderr)

	if command.WaitDelay != defaultProcessWaitDelay || command.WaitDelay <= 0 {
		t.Fatalf("WaitDelay = %v, want bounded default %v", command.WaitDelay, defaultProcessWaitDelay)
	}
	if command.Cancel == nil {
		t.Fatal("command does not retain context cancellation")
	}
	if want := []string{"opencode-test", "models", "--pure"}; !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("Args = %#v, want direct argv %#v", command.Args, want)
	}
	if command.Stdout != stdout || command.Stderr != stderr {
		t.Fatal("command did not retain bounded output writers")
	}
}

func TestExecRunnerBoundsDescendantHeldPipeWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*defaultProcessWaitDelay)
	defer cancel()
	started := time.Now()

	err := (execRunner{}).Run(ctx, os.Args[0], []string{
		"-test.run=^TestExecRunnerWaitDelayHelper$", "--", "modelcatalog-helper=child",
	}, io.Discard, io.Discard)
	elapsed := time.Since(started)

	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Run() error = %v, want exec.ErrWaitDelay", err)
	}
	if elapsed < defaultProcessWaitDelay/2 || elapsed > 3*defaultProcessWaitDelay {
		t.Fatalf("Run() duration = %v, want bounded near %v", elapsed, defaultProcessWaitDelay)
	}
}

func TestExecRunnerWaitDelayHelper(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		mode = strings.TrimPrefix(arg, "modelcatalog-helper=")
		if mode != arg {
			break
		}
		mode = ""
	}
	switch mode {
	case "":
		return
	case "child":
		command := exec.Command(os.Args[0], "-test.run=^TestExecRunnerWaitDelayHelper$", "--", "modelcatalog-helper=grandchild")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if command.Start() != nil {
			os.Exit(2)
		}
		os.Exit(0)
	case "grandchild":
		deadline := time.Now().Add(5 * defaultProcessWaitDelay)
		for time.Now().Before(deadline) {
			if _, err := io.WriteString(os.Stdout, "."); err != nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(3)
	default:
		os.Exit(4)
	}
}

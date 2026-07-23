package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/bridge"
)

type fakeEditLifecycleRuntime struct {
	fakeBridgeRuntime
	result  bridge.NativeEditLifecycleResult
	inspect bridge.NativeEditInspectRequest
	action  bridge.NativeEditActionRequest
	method  string
}

func (runtime *fakeEditLifecycleRuntime) InspectNativeEdit(_ context.Context, _ string, request bridge.NativeEditInspectRequest) (bridge.NativeEditLifecycleResult, error) {
	runtime.inspect, runtime.method = request, "inspect"
	return runtime.result, runtime.err
}

func (runtime *fakeEditLifecycleRuntime) recordEditAction(method string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	runtime.action, runtime.method = request, method
	return runtime.result, runtime.err
}

func (runtime *fakeEditLifecycleRuntime) ApproveNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("approve", request)
}

func (runtime *fakeEditLifecycleRuntime) IntegrateNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("integrate", request)
}

func (runtime *fakeEditLifecycleRuntime) RetireNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("retire", request)
}

func (runtime *fakeEditLifecycleRuntime) DiscardNativeEdit(_ context.Context, _ string, request bridge.NativeEditActionRequest) (bridge.NativeEditLifecycleResult, error) {
	return runtime.recordEditAction("discard", request)
}

func TestEditLifecycleRoutesExactCommands(t *testing.T) {
	manifest := "sha256-" + strings.Repeat("a", 64)
	runtime := &fakeEditLifecycleRuntime{result: bridge.NativeEditLifecycleResult{
		TicketID: "ticket-1", State: "approved",
	}}
	workspace := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := runEditLifecycle(context.Background(), []string{
		"inspect", "--workspace", workspace, "--ticket", "ticket-1",
	}, &stdout, &stderr, runtime); code != 0 || runtime.method != "inspect" || runtime.inspect.TicketID != "ticket-1" ||
		!strings.Contains(stdout.String(), `"state":"approved"`) || stderr.Len() != 0 {
		t.Fatalf("inspect code=%d method=%q request=%#v stdout=%q stderr=%q", code, runtime.method, runtime.inspect, stdout.String(), stderr.String())
	}

	for _, method := range []string{"approve", "integrate", "retire", "discard"} {
		stdout.Reset()
		stderr.Reset()
		runtime.method = ""
		if code := runEditLifecycle(context.Background(), []string{
			method, "--workspace", workspace, "--ticket", "ticket-1", "--manifest", manifest, "--actor", "maintainer",
		}, &stdout, &stderr, runtime); code != 0 || runtime.method != method || runtime.action.TicketID != "ticket-1" ||
			runtime.action.ManifestSHA != manifest || runtime.action.Actor != "maintainer" || stderr.Len() != 0 {
			t.Fatalf("%s code=%d method=%q request=%#v stdout=%q stderr=%q", method, code, runtime.method, runtime.action, stdout.String(), stderr.String())
		}
	}
}

func TestEditLifecycleRejectsAmbiguousOrUnavailableCommands(t *testing.T) {
	workspace := t.TempDir()
	runtime := &fakeEditLifecycleRuntime{}
	var stdout, stderr bytes.Buffer
	if code := runEditLifecycle(context.Background(), []string{
		"inspect", "--workspace", workspace, "--ticket", "ticket-1", "--actor", "maintainer",
	}, &stdout, &stderr, runtime); code != 2 || runtime.method != "" {
		t.Fatalf("inspect accepted action flags: code=%d method=%q stderr=%q", code, runtime.method, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	baseRuntime := &fakeBridgeRuntime{}
	if code := runEditLifecycle(context.Background(), []string{
		"inspect", "--workspace", workspace, "--ticket", "ticket-1",
	}, &stdout, &stderr, baseRuntime); code != 1 || !strings.Contains(stderr.String(), "not configured") {
		t.Fatalf("missing runtime was not reported: code=%d stderr=%q", code, stderr.String())
	}
}

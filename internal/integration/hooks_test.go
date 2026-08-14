package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/hooks"
)

type hookRuntime struct {
	result Result
	err    error
	calls  int
}

func (r *hookRuntime) Preview(context.Context, Options) (Result, error) {
	r.calls++
	return r.result, r.err
}
func (r *hookRuntime) Install(context.Context, Options) (Result, error) {
	r.calls++
	return r.result, r.err
}
func (r *hookRuntime) Status(context.Context, Options) (Result, error) {
	r.calls++
	return r.result, r.err
}
func (r *hookRuntime) Uninstall(context.Context, Options) (Result, error) {
	r.calls++
	return r.result, r.err
}

type captureEmitter struct{ draft hooks.Draft }

func (e *captureEmitter) Emit(_ context.Context, d hooks.Draft) { e.draft = d }

func TestObserveHooksSuccessOnlyAndExactMapping(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(Runtime) (Result, error)
		want hooks.Name
	}{
		{"preview", func(r Runtime) (Result, error) { return r.Preview(context.Background(), Options{}) }, hooks.NameIntegrationPreviewCompleted},
		{"install", func(r Runtime) (Result, error) { return r.Install(context.Background(), Options{}) }, hooks.NameIntegrationInstallCompleted},
		{"status", func(r Runtime) (Result, error) { return r.Status(context.Background(), Options{}) }, hooks.NameIntegrationStatusCompleted},
		{"uninstall", func(r Runtime) (Result, error) { return r.Uninstall(context.Background(), Options{}) }, hooks.NameIntegrationUninstallCompleted},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := &hookRuntime{result: Result{Provider: "provider", State: StateInstalled, Changed: true, ArtifactSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ArtifactCount: 2, RestartRequired: true, Path: "secret-path", ToolPath: "secret-tool", BackupPath: "secret-backup", ToolBackupPath: "secret-tool-backup", ManifestPath: "secret-manifest", DefaultAgentPath: "secret-agent", ModelEfficient: "secret-efficient", ModelBalanced: "secret-balanced", ModelFrontier: "secret-frontier", RetainedPredecessorPath: "secret-retained"}}
			emit := hooks.New()
			var events []hooks.Event
			if err := emit.Register("test", func(_ context.Context, event hooks.Event) error { events = append(events, event); return nil }, test.want); err != nil {
				t.Fatal(err)
			}
			got, err := test.call(Observe(base, emit))
			if err != nil || got != base.result || base.calls != 1 || len(events) != 1 || events[0].Name() != test.want {
				t.Fatalf("result=%+v err=%v calls=%d", got, err, base.calls)
			}
			result, ok := events[0].Integration()
			if !ok || result.State() != string(base.result.State) || !result.Changed() || result.ArtifactSHA256() != base.result.ArtifactSHA256 || result.ArtifactCount() != int64(base.result.ArtifactCount) || !result.RestartRequired() {
				t.Fatal("scalar mapping")
			}
			if subject := events[0].Subject(); subject.Kind() != "integrationProvider" || subject.ID() != "provider" {
				t.Fatal("subject mapping")
			}
			encoded, _ := json.Marshal(events[0])
			for _, secret := range []string{"secret-", "path", "config", "assignments", "model"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("leak %q", secret)
				}
			}
		})
	}
}
func TestObserveHooksErrorNoEvent(t *testing.T) {
	base := &hookRuntime{err: errors.New("failure")}
	emit := &captureEmitter{}
	_, err := Observe(base, emit).Install(context.Background(), Options{})
	if err == nil || base.calls != 1 || emit.draft != (hooks.Draft{}) {
		t.Fatal("error changed or emitted")
	}
}

type panicEmitter struct{}

func (panicEmitter) Emit(context.Context, hooks.Draft) { panic("emitter") }

func TestObserveNilAndPanickingEmitterPreserveResult(t *testing.T) {
	base := &hookRuntime{result: Result{Provider: "provider", State: StateInstalled}}
	got, err := Observe(base, nil).Preview(context.Background(), Options{})
	if err != nil || got != base.result || base.calls != 1 {
		t.Fatal("nil emitter changed result")
	}
	base.calls = 0
	got, err = Observe(base, panicEmitter{}).Preview(context.Background(), Options{})
	if err != nil || got != base.result || base.calls != 1 {
		t.Fatal("panic emitter changed result")
	}
	if _, ok := Observe(base, panicEmitter{}).(ManagedRuntime); ok {
		t.Fatal("ordinary runtime gained managed capability")
	}
}

type managedHookRuntime struct {
	*hookRuntime
	layout       ManagedLayout
	pending      bool
	reinstall    Result
	managedCalls int
}

func (r *managedHookRuntime) ManagedLayout(context.Context, Options) (ManagedLayout, error) {
	r.managedCalls++
	return r.layout, nil
}
func (r *managedHookRuntime) ReinstallPending(context.Context, Options) (bool, error) {
	r.managedCalls++
	return r.pending, nil
}
func (r *managedHookRuntime) Reinstall(context.Context, Options) (Result, error) {
	r.managedCalls++
	return r.reinstall, nil
}
func TestObservePreservesManagedRuntimePassThrough(t *testing.T) {
	base := &managedHookRuntime{hookRuntime: &hookRuntime{}, layout: ManagedLayout{Root: "sentinel"}, pending: true, reinstall: Result{Provider: "provider", State: StateInstalled}}
	d := hooks.New()
	events := 0
	if err := d.Register("test", func(context.Context, hooks.Event) error { events++; return nil }, hooks.NameIntegrationPreviewCompleted, hooks.NameIntegrationInstallCompleted, hooks.NameIntegrationStatusCompleted, hooks.NameIntegrationUninstallCompleted); err != nil {
		t.Fatal(err)
	}
	observed, ok := Observe(base, d).(ManagedRuntime)
	if !ok {
		t.Fatal("managed capability lost")
	}
	layout, err := observed.ManagedLayout(context.Background(), Options{})
	if err != nil || layout.Root != base.layout.Root {
		t.Fatal("layout changed")
	}
	pending, err := observed.ReinstallPending(context.Background(), Options{})
	if err != nil || pending != base.pending {
		t.Fatal("pending changed")
	}
	result, err := observed.Reinstall(context.Background(), Options{})
	if err != nil || result != base.reinstall || base.managedCalls != 3 {
		t.Fatal("reinstall changed")
	}
	if events != 0 {
		t.Fatal("recovery operation emitted V1 event")
	}
}

func TestObserveDoesNotAddMemorySyncSurface(t *testing.T) {
	base := &hookRuntime{result: Result{Provider: "provider", State: StateInstalled}}
	events := 0
	d := hooks.New()
	if err := d.Register("sync", func(context.Context, hooks.Event) error { events++; return nil }, hooks.NameMemorySyncCompleted); err != nil {
		t.Fatal(err)
	}
	if _, err := Observe(base, d).Status(context.Background(), Options{}); err != nil || events != 0 {
		t.Fatalf("status err=%v sync events=%d", err, events)
	}
}

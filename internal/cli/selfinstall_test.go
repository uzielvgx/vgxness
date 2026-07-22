package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/selfinstall"
)

type fakeSelfInstaller struct {
	action  string
	options selfinstall.Options
	result  selfinstall.Result
	err     error
}

func (fake *fakeSelfInstaller) Preview(_ context.Context, options selfinstall.Options) (selfinstall.Result, error) {
	fake.action, fake.options = "preview", options
	return fake.result, fake.err
}
func (fake *fakeSelfInstaller) Install(_ context.Context, options selfinstall.Options) (selfinstall.Result, error) {
	fake.action, fake.options = "install", options
	return fake.result, fake.err
}
func (fake *fakeSelfInstaller) Status(_ context.Context, options selfinstall.Options) (selfinstall.Result, error) {
	fake.action, fake.options = "status", options
	return fake.result, fake.err
}
func (fake *fakeSelfInstaller) Rollback(_ context.Context, options selfinstall.Options) (selfinstall.Result, error) {
	fake.action, fake.options = "rollback", options
	return fake.result, fake.err
}

func TestRunSelfInstallRoutesAndRendersAtomicOutput(t *testing.T) {
	fake := &fakeSelfInstaller{result: selfinstall.Result{
		State: selfinstall.StateInstalled, LauncherPath: "/tmp/bin/vgxness", ManifestPath: "/tmp/bin/vgxness.launcher.json",
		DataDir: "/tmp/data", SourceSHA256: strings.Repeat("a", 64), ActiveSHA256: strings.Repeat("b", 64),
		PreviousSHA256: strings.Repeat("c", 64), UpdateAvailable: true, RollbackAvailable: true, Changed: true,
	}}
	var stdout, stderr bytes.Buffer
	code := runSelfInstall(context.Background(), []string{"install", "--bin-dir", "/tmp/bin", "--data-dir", "/tmp/data"}, &stdout, &stderr, fake)
	if code != 0 || fake.action != "install" || fake.options.BinDir != "/tmp/bin" || fake.options.DataDir != "/tmp/data" || stderr.Len() != 0 || !strings.Contains(stdout.String(), "state=installed\n") || !strings.Contains(stdout.String(), "rollback_available=true\nchanged=true\n") {
		t.Fatalf("code=%d action=%q options=%#v stdout=%q stderr=%q", code, fake.action, fake.options, stdout.String(), stderr.String())
	}
}

func TestRunSelfInstallRejectsInvalidArgumentsAndMapsErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runSelfInstall(context.Background(), []string{"install", "extra"}, &stdout, &stderr, &fakeSelfInstaller{}); code != 2 || stdout.Len() != 0 {
		t.Fatalf("invalid args code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	fake := &fakeSelfInstaller{err: ErrWrap{err: selfinstall.ErrDrift}}
	if code := runSelfInstall(context.Background(), []string{"status"}, &stdout, &stderr, fake); code != 1 || stdout.Len() != 0 || stderr.String() != "drift: managed self-install differs from its manifest\n" {
		t.Fatalf("drift code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type ErrWrap struct{ err error }

func (wrapped ErrWrap) Error() string { return "wrapped: " + wrapped.err.Error() }
func (wrapped ErrWrap) Unwrap() error { return wrapped.err }

var _ error = ErrWrap{err: errors.New("sentinel")}

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/selfinstall"
)

type fakeSelfInstaller struct {
	action  string
	options selfinstall.Options
	result  selfinstall.Result
	err     error
	gc      selfinstall.GCResult
	plan    string
}

func (fake *fakeSelfInstaller) GCPreview(_ context.Context, options selfinstall.Options) (selfinstall.GCResult, error) {
	fake.action, fake.options = "gc-preview", options
	return fake.gc, fake.err
}
func (fake *fakeSelfInstaller) GCApply(_ context.Context, options selfinstall.Options, plan string) (selfinstall.GCResult, error) {
	fake.action, fake.options = "gc-apply", options
	fake.plan = plan
	return fake.gc, fake.err
}
func (fake *fakeSelfInstaller) GCRecover(_ context.Context, options selfinstall.Options) (selfinstall.GCResult, error) {
	fake.action, fake.options = "gc-recover", options
	return fake.gc, fake.err
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

func TestFailureMapsSelfInstallRecovery(t *testing.T) {
	code, message := failure(errors.Join(selfinstall.ErrRecovery, selfinstall.ErrDrift))
	if code != 1 || !strings.Contains(message, "recovery:") || !strings.Contains(message, "self status") {
		t.Fatalf("code=%d message=%q", code, message)
	}
}

func TestRunSelfInstallGCRoutesAndRendersCanonicalOutput(t *testing.T) {
	fake := &fakeSelfInstaller{gc: selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: strings.Repeat("a", 64), Candidates: []string{strings.Repeat("b", 64), strings.Repeat("c", 64)}, Retained: []string{strings.Repeat("d", 64)}, Deleted: []string{strings.Repeat("b", 64), strings.Repeat("c", 64)}, Changed: true}}
	var stdout, stderr bytes.Buffer
	code := runSelfInstall(context.Background(), []string{"gc", "apply", "--plan-sha256=" + strings.Repeat("a", 64), "--bin-dir", "/bin", "--data-dir=/data"}, &stdout, &stderr, fake)
	want := "state=installed\ngc_plan_sha256=" + strings.Repeat("a", 64) + "\ngc_candidate_count=2\ngc_candidate_sha256=" + strings.Repeat("b", 64) + "\ngc_candidate_sha256=" + strings.Repeat("c", 64) + "\ngc_retained_count=1\ngc_retained_sha256=" + strings.Repeat("d", 64) + "\ngc_deleted_count=2\ngc_deleted_sha256=" + strings.Repeat("b", 64) + "\ngc_deleted_sha256=" + strings.Repeat("c", 64) + "\nchanged=true\n"
	if code != 0 || fake.action != "gc-apply" || fake.plan != strings.Repeat("a", 64) || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("code=%d action=%q plan=%q stdout=%q stderr=%q", code, fake.action, fake.plan, stdout.String(), stderr.String())
	}
}

func TestRunSelfInstallGCRejectsInvalidArgumentsWithoutRuntimeCall(t *testing.T) {
	invalid := [][]string{{"gc"}, {"gc", "apply"}, {"gc", "preview", "--plan-sha256", strings.Repeat("a", 64)}, {"gc", "recover", "--bin-dir", "/a", "--bin-dir=/b"}, {"gc", "apply", "--plan-sha256", strings.Repeat("A", 64)}}
	for _, args := range invalid {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			fake := &fakeSelfInstaller{}
			var stdout, stderr bytes.Buffer
			if code := runSelfInstall(context.Background(), args, &stdout, &stderr, fake); code != 2 || stdout.Len() != 0 || stderr.String() != "invalid self-install arguments\n" || fake.action != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q action=%q", code, stdout.String(), stderr.String(), fake.action)
			}
		})
	}
}

func TestRunSelfInstallGCRejectsMalformedRuntimeResult(t *testing.T) {
	fake := &fakeSelfInstaller{gc: selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: "not-a-digest"}}
	var stdout, stderr bytes.Buffer
	if code := runSelfInstall(context.Background(), []string{"gc", "preview"}, &stdout, &stderr, fake); code != 1 || stdout.Len() != 0 || stderr.String() != "operational: self-install garbage collection result is invalid\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunSelfInstallGCInvalidArgumentMatrix(t *testing.T) {
	plan := strings.Repeat("a", 64)
	cases := [][]string{{"gc", "unknown"}, {"gc", "preview", "extra"}, {"gc", "apply", "extra", "--plan-sha256", plan}, {"gc", "recover", "extra"}, {"gc", "preview", "--unknown", "x"}, {"gc", "preview", "--unknown=x"}, {"gc", "preview", "--bin-dir"}, {"gc", "preview", "--data-dir"}, {"gc", "apply", "--plan-sha256"}, {"gc", "preview", "--bin-dir", "/a", "--bin-dir=/b"}, {"gc", "preview", "--data-dir=/a", "--data-dir", "/b"}, {"gc", "apply", "--plan-sha256", plan, "--plan-sha256=" + plan}, {"gc", "preview", "--plan-sha256", plan}, {"gc", "recover", "--plan-sha256", plan}, {"gc", "apply"}, {"gc", "apply", "--plan-sha256="}, {"gc", "apply", "--plan-sha256=abc"}, {"gc", "apply", "--plan-sha256=" + strings.Repeat("A", 64)}, {"gc", "apply", "--plan-sha256=" + strings.Repeat("g", 64)}}
	for index, args := range cases {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			fake := &fakeSelfInstaller{}
			var stdout, stderr bytes.Buffer
			if code := runSelfInstall(context.Background(), args, &stdout, &stderr, fake); code != 2 || stdout.Len() != 0 || stderr.String() != "invalid self-install arguments\n" || fake.action != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q action=%q", code, stdout.String(), stderr.String(), fake.action)
			}
		})
	}
}

func TestRunSelfInstallGCRoutesAllActionsAndExactOutput(t *testing.T) {
	plan := strings.Repeat("a", 64)
	candidate, retained, recovered := strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("e", 64)
	cases := []struct {
		name         string
		args         []string
		action, want string
		result       selfinstall.GCResult
	}{
		{
			name: "preview", args: []string{"gc", "preview", "--bin-dir", "/bin", "--data-dir=/data"}, action: "gc-preview",
			want:   "state=installed\ngc_plan_sha256=" + plan + "\ngc_candidate_count=1\ngc_candidate_sha256=" + candidate + "\ngc_retained_count=1\ngc_retained_sha256=" + retained + "\nchanged=false\n",
			result: selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{candidate}, Retained: []string{retained}},
		},
		{
			name: "apply", args: []string{"gc", "apply", "--plan-sha256", plan}, action: "gc-apply",
			want:   "state=installed\ngc_plan_sha256=" + plan + "\ngc_candidate_count=1\ngc_candidate_sha256=" + candidate + "\ngc_retained_count=1\ngc_retained_sha256=" + retained + "\ngc_deleted_count=1\ngc_deleted_sha256=" + candidate + "\nchanged=true\n",
			result: selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{candidate}, Retained: []string{retained}, Deleted: []string{candidate}, Changed: true},
		},
		{
			name: "recover", args: []string{"gc", "recover"}, action: "gc-recover",
			want:   "state=installed\ngc_candidate_count=0\ngc_retained_count=0\ngc_recovered_count=1\ngc_recovered_sha256=" + recovered + "\nchanged=true\n",
			result: selfinstall.GCResult{State: selfinstall.StateInstalled, Recovered: []string{recovered}, Changed: true},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeSelfInstaller{gc: test.result}
			var stdout, stderr bytes.Buffer
			code := runSelfInstall(context.Background(), test.args, &stdout, &stderr, fake)
			if code != 0 || fake.action != test.action || stdout.String() != test.want || stderr.Len() != 0 {
				t.Fatalf("code=%d action=%q stdout=%q stderr=%q", code, fake.action, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunSelfInstallGCErrorsEmitNoStdout(t *testing.T) {
	plan := strings.Repeat("a", 64)
	cases := []struct {
		err     error
		code    int
		message string
	}{
		{err: selfinstall.ErrNoInstallation, code: 1, message: "not_found: no managed self-installation is available\n"},
		{err: selfinstall.ErrStaleGCPlan, code: 1, message: "conflict: self-install garbage-collection plan is stale; rerun `vgxness self gc preview`\n"},
		{err: selfinstall.ErrGCRecovery, code: 1, message: "recovery: self-install garbage collection is incomplete; run `vgxness self gc recover` without deleting retained evidence\n"},
		{err: selfinstall.ErrDrift, code: 1, message: "drift: managed self-install differs from its manifest\n"},
		{err: context.Canceled, code: 130, message: "cancelled: operation cancelled\n"},
	}
	for _, test := range cases {
		t.Run(test.message, func(t *testing.T) {
			fake := &fakeSelfInstaller{err: test.err}
			var stdout, stderr bytes.Buffer
			code := runSelfInstall(context.Background(), []string{"gc", "apply", "--plan-sha256", plan}, &stdout, &stderr, fake)
			if code != test.code || stdout.Len() != 0 || stderr.String() != test.message {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunSelfInstallGCApplyPartialFailurePrintsAuditResult(t *testing.T) {
	plan, first, second, retained := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
	fake := &fakeSelfInstaller{gc: selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{first, second}, Retained: []string{retained}, Deleted: []string{first}, Changed: true}, err: selfinstall.ErrGCRecovery}
	var stdout, stderr bytes.Buffer
	code := runSelfInstall(context.Background(), []string{"gc", "apply", "--plan-sha256", plan}, &stdout, &stderr, fake)
	want := "state=installed\ngc_plan_sha256=" + plan + "\ngc_candidate_count=2\ngc_candidate_sha256=" + first + "\ngc_candidate_sha256=" + second + "\ngc_retained_count=1\ngc_retained_sha256=" + retained + "\ngc_deleted_count=1\ngc_deleted_sha256=" + first + "\nchanged=true\n"
	if code != 1 || stdout.String() != want || stderr.String() != "recovery: self-install garbage collection is incomplete; run `vgxness self gc recover` without deleting retained evidence\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunSelfInstallGCApplyErrorPrintsFullOrderedAuditResult(t *testing.T) {
	plan, first, second := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	fake := &fakeSelfInstaller{gc: selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{first, second}, Deleted: []string{first, second}, Changed: true}, err: selfinstall.ErrGCRecovery}
	var stdout, stderr bytes.Buffer
	code := runSelfInstall(context.Background(), []string{"gc", "apply", "--plan-sha256", plan}, &stdout, &stderr, fake)
	if code != 1 || !strings.Contains(stdout.String(), "gc_deleted_sha256="+first+"\ngc_deleted_sha256="+second+"\n") || stderr.String() != "recovery: self-install garbage collection is incomplete; run `vgxness self gc recover` without deleting retained evidence\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunSelfInstallGCApplyErrorWithoutValidPartialPrintsNoAudit(t *testing.T) {
	plan := strings.Repeat("a", 64)
	cases := []selfinstall.GCResult{
		{},
		{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{strings.Repeat("b", 64)}, Deleted: []string{strings.Repeat("b", 64)}},
	}
	for index, result := range cases {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			fake := &fakeSelfInstaller{gc: result, err: selfinstall.ErrGCRecovery}
			var stdout, stderr bytes.Buffer
			if code := runSelfInstall(context.Background(), []string{"gc", "apply", "--plan-sha256", plan}, &stdout, &stderr, fake); code != 1 || stdout.Len() != 0 || stderr.String() != "recovery: self-install garbage collection is incomplete; run `vgxness self gc recover` without deleting retained evidence\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunSelfInstallLegacyStatusNeverCallsGC(t *testing.T) {
	fake := &fakeSelfInstaller{result: selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: "/bin/vgxness", ManifestPath: "/bin/vgxness.json", DataDir: "/data"}}
	var stdout, stderr bytes.Buffer
	if code := runSelfInstall(context.Background(), []string{"status"}, &stdout, &stderr, fake); code != 0 || fake.action != "status" || stderr.Len() != 0 || stdout.String() != "state=installed\nlauncher=/bin/vgxness\nmanifest=/bin/vgxness.json\ndata_dir=/data\nsource_sha256=\nactive_sha256=\nprevious_sha256=\nupdate_available=false\nrollback_available=false\nchanged=false\n" {
		t.Fatalf("code=%d action=%q stdout=%q stderr=%q", code, fake.action, stdout.String(), stderr.String())
	}
}

func TestRunSelfInstallGCMalformedResultMatrix(t *testing.T) {
	valid := strings.Repeat("a", 64)
	cases := []selfinstall.GCResult{
		{State: selfinstall.State("other"), PlanSHA256: valid},
		{State: selfinstall.StateInstalled, PlanSHA256: "bad"},
		{State: selfinstall.StateInstalled, PlanSHA256: valid, Candidates: []string{"bad"}},
		{State: selfinstall.StateInstalled, PlanSHA256: valid, Retained: []string{"bad"}},
		{State: selfinstall.StateInstalled, PlanSHA256: valid, Deleted: []string{"bad"}},
		{State: selfinstall.StateInstalled, PlanSHA256: valid, Recovered: []string{"bad"}},
	}
	for index, result := range cases {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			fake := &fakeSelfInstaller{gc: result}
			var stdout, stderr bytes.Buffer
			if code := runSelfInstall(context.Background(), []string{"gc", "preview"}, &stdout, &stderr, fake); code != 1 || stdout.Len() != 0 || stderr.String() != "operational: self-install garbage collection result is invalid\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunSelfInstallGCActionInvariantMatrix(t *testing.T) {
	plan := strings.Repeat("a", 64)
	digest := strings.Repeat("b", 64)
	cases := []struct {
		args   []string
		result selfinstall.GCResult
	}{
		{[]string{"gc", "preview"}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Changed: true}},
		{[]string{"gc", "preview"}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Deleted: []string{digest}}},
		{[]string{"gc", "apply", "--plan-sha256", plan}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Recovered: []string{digest}}},
		{[]string{"gc", "apply", "--plan-sha256", plan}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Deleted: []string{digest}, Changed: false}},
		{[]string{"gc", "apply", "--plan-sha256", plan}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{digest, strings.Repeat("c", 64)}, Deleted: []string{digest}, Changed: true}},
		{[]string{"gc", "apply", "--plan-sha256", plan}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{digest}, Deleted: []string{strings.Repeat("c", 64)}, Changed: true}},
		{[]string{"gc", "preview"}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{digest, digest}}},
		{[]string{"gc", "preview"}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{strings.Repeat("c", 64), digest}}},
		{[]string{"gc", "preview"}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan, Candidates: []string{digest}, Retained: []string{digest}}},
		{[]string{"gc", "recover"}, selfinstall.GCResult{State: selfinstall.StateInstalled, PlanSHA256: plan}},
		{[]string{"gc", "recover"}, selfinstall.GCResult{State: selfinstall.StateInstalled, Candidates: []string{digest}}},
		{[]string{"gc", "recover"}, selfinstall.GCResult{State: selfinstall.StateInstalled, Recovered: []string{digest}, Changed: false}},
	}
	for index, test := range cases {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			fake := &fakeSelfInstaller{gc: test.result}
			var stdout, stderr bytes.Buffer
			if code := runSelfInstall(context.Background(), test.args, &stdout, &stderr, fake); code != 1 || stdout.Len() != 0 || stderr.String() != "operational: self-install garbage collection result is invalid\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

type ErrWrap struct{ err error }

func (wrapped ErrWrap) Error() string { return "wrapped: " + wrapped.err.Error() }
func (wrapped ErrWrap) Unwrap() error { return wrapped.err }

var _ error = ErrWrap{err: errors.New("sentinel")}

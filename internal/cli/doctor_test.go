package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
)

func doctorFixture() *fakeUnifiedSetup {
	return &fakeUnifiedSetup{fakeSetupRuntime: &fakeSetupRuntime{}, sharedStatus: &setupflow.SharedPlan{
		Ready: true, Launcher: selfinstall.Result{State: selfinstall.StateInstalled}, Skills: skills.Result{State: skills.StateInstalled},
	}}
}

func TestDoctorAllReportsObservedHealthWithoutClaimingRuntimeCoverage(t *testing.T) {
	fake := doctorFixture()
	codex := &fakeIntegrationRuntime{result: integration.Result{Provider: "codex", State: integration.StateInstalled, ArtifactCount: 15}}
	var out, stderr bytes.Buffer
	code := RunProductRuntime(context.Background(), []string{"doctor", "--all", "--workspace", t.TempDir()}, nil, &out, &stderr,
		&fakeInspector{result: inspection.Result{Migration: 23}}, nil, nil, codex, nil, fake)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	for _, want := range []string{"storage=healthy", "launcher=installed", "skills=installed", "opencode.runtime=healthy", "codex.runtime=unobserved", "pi.runtime=unobserved", "model_access=unobserved", "doctor=incomplete"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "doctor=healthy") || fake.applyCalls != 0 || fake.planCalls != 0 || fake.piApplyCalls != 0 || codex.action != "status" {
		t.Fatalf("diagnosis mutated state or overstated evidence: %s", &out)
	}
}

func TestDoctorAllContinuesAfterIndependentFailures(t *testing.T) {
	fake := doctorFixture()
	fake.sharedStatusErr = errors.New("private diagnostic\x1b")
	codex := &fakeIntegrationRuntime{err: integration.ErrDrift}
	var out, stderr bytes.Buffer
	code := RunProductRuntime(context.Background(), []string{"doctor", "--all"}, nil, &out, &stderr,
		&fakeInspector{err: inspection.ErrCorrupt}, nil, nil, codex, nil, fake)
	if code != 1 || !strings.Contains(out.String(), "doctor=attention") || !strings.Contains(out.String(), "pi.artifacts=installed") || codex.calls != 1 {
		t.Fatalf("incomplete diagnosis code=%d out=%s stderr=%s", code, &out, &stderr)
	}
	if strings.Contains(out.String()+stderr.String(), "private diagnostic") {
		t.Fatal("raw internal error leaked")
	}
}

func TestDoctorAllUnavailableAdaptersAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		var out, stderr bytes.Buffer
		code := RunProductRuntime(ctx, []string{"doctor", "--all"}, nil, &out, &stderr, nil, nil, nil, nil, nil, nil)
		cancel()
		if cancelled {
			if code != 130 || out.Len() != 0 {
				t.Fatalf("cancelled: code=%d out=%s", code, &out)
			}
		} else if code != 1 || !strings.Contains(out.String(), "doctor=attention") {
			t.Fatalf("unavailable: code=%d out=%s", code, &out)
		}
	}
}

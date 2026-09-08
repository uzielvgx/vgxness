package pi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	setupflow "github.com/vgxness/vgxness/internal/setup"
)

func TestStatusInspectsInstalledPiWithoutReleaseDirectory(t *testing.T) {
	root := t.TempDir()
	options := Options{
		ReleaseDir:  tinyRelease(t, filepath.Join(root, "release"), "a"),
		AgentDir:    filepath.Join(root, "agent"),
		InstallRoot: filepath.Join(root, "managed"),
		GOOS:        "linux",
		GOARCH:      "amd64",
	}
	if _, err := Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	status, err := NewProvider(Options{AgentDir: options.AgentDir, InstallRoot: options.InstallRoot}).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || status.Ready || !status.Installed || status.ArtifactSHA256 == "" || status.Handshake.Status != "unavailable" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestStatusReportsOwnedRetainedReservation(t *testing.T) {
	root := t.TempDir()
	options := Options{AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed")}
	path := filepath.Join(options.InstallRoot, "packages", "pi-"+version+"-0123456789abcdef")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(rootManifest{Schema: 1, ManagedBy: "vgxness"})
	if err := os.WriteFile(filepath.Join(options.InstallRoot, rootManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".vgxness-pi-reservation"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := NewProvider(options).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || status.State != "partial" || !strings.Contains(status.Blocker, "reservation-unproven at "+path) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestStatusReportsPendingPublishedUpdateAndRetry(t *testing.T) {
	root := t.TempDir()
	options := Options{AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	options.ReleaseDir = tinyRelease(t, filepath.Join(root, "release-a"), "a")
	first, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	options.ReleaseDir = tinyRelease(t, filepath.Join(root, "release-b"), "b")
	installBoundaryHook = func(phase string) error {
		if phase == "published-inactive" {
			return errors.New("fixture")
		}
		return nil
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("update unexpectedly activated")
	}
	installBoundaryHook = nil
	status, err := NewProvider(Options{AgentDir: options.AgentDir, InstallRoot: options.InstallRoot}).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || status.State != "partial" || !strings.Contains(status.Blocker, "published-inactive") || !strings.Contains(status.Blocker, "pi-"+version+"-") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	second, err := Install(context.Background(), options)
	if err != nil || second.PackagePath == first.PackagePath {
		t.Fatalf("retry=%+v err=%v", second, err)
	}
	status, err = NewProvider(Options{AgentDir: options.AgentDir, InstallRoot: options.InstallRoot}).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || status.State != "installed" || strings.Contains(status.Blocker, "published-inactive") {
		t.Fatalf("status after retry=%+v err=%v", status, err)
	}
}

func TestStatusReportsActivatedFinalizationPending(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	reservationOperationHook = func(operation string) error {
		if operation == "remove" {
			return errors.New("fixture")
		}
		return nil
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("finalization failure missing")
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("retry finalization failure missing")
	}
	reservationOperationHook = nil
	status, err := NewProvider(Options{AgentDir: options.AgentDir, InstallRoot: options.InstallRoot}).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || status.State != "partial" || !strings.Contains(status.Blocker, "activated-finalization-pending") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRejectsFailedAndTimedOutChildren(t *testing.T) {
	previous := probeTimeout
	probeTimeout = 50 * time.Millisecond
	defer func() { probeTimeout = previous }()
	for _, test := range []struct {
		name, script string
		want         string
	}{
		{"healthy", "echo '{\"type\":\"hello\",\"protocol\":\"vgxness-pi/v1\"}'; read x; exit 0", "healthy"},
		{"incompatible", "echo broken; exit 0", "incompatible"},
		{"silent-timeout", "exec sleep 3", "unavailable"},
		{"hello-then-hang", "echo '{\"type\":\"hello\",\"protocol\":\"vgxness-pi/v1\"}'; read x; exec sleep 3", "unavailable"},
		{"hello-then-fail", "echo '{\"type\":\"hello\",\"protocol\":\"vgxness-pi/v1\"}'; read x; exit 7", "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			packagePath := fakeProbePackage(t, test.script)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if got := probe(ctx, Options{}, packagePath); got.Status.String() != test.want {
				t.Fatalf("probe=%+v want=%s", got, test.want)
			}
		})
	}
}

func fakeProbePackage(t *testing.T, script string) string {
	t.Helper()
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		t.Skip("shell probe fixture")
	}
	if arch == "amd64" {
		arch = "x64"
	}
	root := filepath.Join(t.TempDir(), "package", "node_modules", "@vgxness", "pi-backend-"+platform+"-"+arch)
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"binary":"bin/backend"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bin", "backend")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(root))))
}

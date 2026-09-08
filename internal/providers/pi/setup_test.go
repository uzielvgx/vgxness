package pi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	probeTimeout = 5 * time.Second
	defer func() { probeTimeout = previous }()
	for _, test := range []struct {
		name, script string
		want         string
	}{
		{"healthy", `console.log(JSON.stringify({type:"health",runtime:"typescript",schemaVersion:23,foreignKeys:true,fts5:true,bigint:true,backup:true}))`, "healthy"},
		{"incompatible", `console.log("broken")`, "incompatible"},
		{"silent-timeout", `setTimeout(()=>{},3000)`, "unavailable"},
		{"health-then-hang", `console.log(JSON.stringify({type:"health",runtime:"typescript",schemaVersion:23,foreignKeys:true,fts5:true,bigint:true,backup:true}));setTimeout(()=>{},3000)`, "unavailable"},
		{"health-then-fail", `console.log(JSON.stringify({type:"health",runtime:"typescript",schemaVersion:23,foreignKeys:true,fts5:true,bigint:true,backup:true}));process.exit(7)`, "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Output classification needs normal process-start headroom under a
			// loaded test runner. Only the explicit hang cases use a short deadline.
			probeTimeout = 5 * time.Second
			if test.name == "silent-timeout" || test.name == "health-then-hang" {
				probeTimeout = 300 * time.Millisecond
			}
			packagePath := fakeProbePackage(t, test.script)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if got := probe(ctx, Options{}, packagePath); got.Status.String() != test.want {
				t.Fatalf("probe=%+v want=%s", got, test.want)
			}
		})
	}
}

func fakeProbePackage(t *testing.T, script string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "package")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "probe.ts"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStatusReportsLegacyPackageAndUpdatePreservesIt(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release-a"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed")}
	installed, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(installed.PackagePath, "package.json")
	legacy := []byte(`{"name":"@vgxness/pi","version":"0.1.0","optionalDependencies":{"@vgxness/pi-backend-linux-arm64":"0.1.0"}}`)
	if err := os.WriteFile(metadataPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(installed.PackagePath, ".vgxness-pi.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest managedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files["package.json"] = checksum(legacy)
	data, _ = json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := NewProvider(Options{AgentDir: options.AgentDir, InstallRoot: options.InstallRoot}).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || status.Ready || !status.Installed || !strings.Contains(status.Blocker, "Legacy Go Pi package needs update") {
		t.Fatalf("legacy status=%+v err=%v", status, err)
	}
	options.ReleaseDir = tinyRelease(t, filepath.Join(root, "release-b"), "b")
	updated, err := Install(context.Background(), options)
	if err != nil || updated.PackagePath == installed.PackagePath {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	if data, err := os.ReadFile(metadataPath); err != nil || string(data) != string(legacy) {
		t.Fatalf("old package was changed: %v", err)
	}
}

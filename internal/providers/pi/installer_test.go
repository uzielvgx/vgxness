package pi

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/release"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

func TestMain(m *testing.M) {
	if runtime.GOOS == "darwin" {
		if directory, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
			_ = os.Setenv("TMPDIR", directory)
		}
	}
	os.Exit(m.Run())
}

func TestInstallReleasePreservesForeignSettingsAndIsIdempotent(t *testing.T) {
	repository := filepath.Join("..", "..", "..")
	releaseDir := filepath.Join(t.TempDir(), "release")
	if err := release.PackagePi(context.Background(), repository, releaseDir); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	options := Options{ReleaseDir: releaseDir, AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	if err := os.MkdirAll(options.AgentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(options.AgentDir, "settings.json"), []byte(`{"unknown":{"keep":true},"packages":["/foreign/package",{"path":"/foreign/object","enabled":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Install(context.Background(), options)
	if err != nil || !first.Changed || first.PackagePath == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := Install(context.Background(), options)
	if err != nil || second.Changed || second.PackagePath != first.PackagePath {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	settingsValue, err := readSettingsFixture(filepath.Join(options.AgentDir, "settings.json"))
	paths, pathsErr := settingsPackagePaths(filepath.Join(options.AgentDir, "settings.json"))
	if err != nil || pathsErr != nil || settingsValue.Unknown["keep"] != true || paths[first.PackagePath] != 1 || paths["/foreign/package"] != 1 || paths["/foreign/object"] != 1 {
		t.Fatalf("unknown=%v paths=%v err=%v pathsErr=%v", settingsValue.Unknown, paths, err, pathsErr)
	}
}

func TestLockSettingsCooperatesWithProperLockfileAndRefusesCompromise(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required for proper-lockfile interoperability: ", err)
	}
	command := exec.Command(node, "-e", `const l=require(process.argv[1]); l.lockSync(process.argv[2],{realpath:false,stale:10000,update:1000}); console.log("locked"); setTimeout(()=>{},10000)`, properLockfile(t), settings)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	if !bufio.NewScanner(stdout).Scan() {
		t.Fatal("proper-lockfile did not acquire lock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, _, err := lockSettings(ctx, settings); err == nil {
		t.Fatal("lockSettings acquired proper-lockfile lock")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	if err := os.Remove(settings + ".lock"); err != nil {
		t.Fatal(err)
	}
	check, release, err := lockSettings(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	lock := settings + ".lock"
	before, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	changed := before.ModTime().Add(-time.Hour)
	if err := os.Chtimes(lock, changed, changed); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	if after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("compromised lock timestamp unchanged: before=%v after=%v", before.ModTime(), after.ModTime())
	}
	if err := check(); err == nil {
		t.Fatal("compromised lock was accepted")
	}
	if err := release(); err == nil {
		t.Fatal("compromised lock was removed")
	}
	if _, err := os.Stat(settings + ".lock"); err != nil {
		t.Fatalf("compromised lock removed: %v", err)
	}
}

func TestInstallRejectsInvalidSettingsBeforeMutation(t *testing.T) {
	for _, settings := range []string{"null", `{"packages":{}}`} {
		t.Run(settings, func(t *testing.T) {
			root := t.TempDir()
			releaseDir := tinyRelease(t, filepath.Join(root, "release"), "a")
			options := Options{ReleaseDir: releaseDir, AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
			if err := os.MkdirAll(options.AgentDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(options.AgentDir, "settings.json"), []byte(settings), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(context.Background(), options); err == nil {
				t.Fatal("Install succeeded with malformed settings")
			}
			if _, err := os.Stat(options.InstallRoot); !os.IsNotExist(err) {
				t.Fatalf("installer mutated root: %v", err)
			}
		})
	}
}

func TestInstallDoesNotOverwriteOccupiedDestination(t *testing.T) {
	root := t.TempDir()
	releaseDir := tinyRelease(t, filepath.Join(root, "release"), "a")
	options := Options{ReleaseDir: releaseDir, AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	source, _, err := validateRelease(releaseDir, "linux-x64")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(options.InstallRoot, "packages", "pi-"+version+"-"+source[:16])
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("Install overwrote occupied destination")
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatalf("occupied destination changed: entries=%v err=%v", entries, err)
	}
}

func TestInstallRejectsOccupiedUnownedRoot(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	if err := os.MkdirAll(options.InstallRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(options.InstallRoot, "foreign"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("Install accepted occupied unowned root")
	}
	if data, err := os.ReadFile(filepath.Join(options.InstallRoot, "foreign")); err != nil || string(data) != "keep" {
		t.Fatalf("foreign root mutated: %q %v", data, err)
	}
}

func TestInstallCreatesMissingDedicatedParentsAndRejectsExtraProvenance(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "fresh", ".pi", "agent"), InstallRoot: filepath.Join(root, "fresh", ".pi", "agent", "nested", "managed"), GOOS: "linux", GOARCH: "amd64"}
	if result, err := Install(context.Background(), options); err != nil || !result.Changed {
		t.Fatalf("fresh nested install=%+v err=%v", result, err)
	}
	bad := tinyRelease(t, filepath.Join(root, "bad-release"), "b")
	if err := os.WriteFile(filepath.Join(bad, "PROVENANCE.json"), []byte(`{"name":"@vgxness/pi","version":"0.1.0","sourceSHA256":"`+strings.Repeat("b", 64)+`","provenance":"local source snapshot; unpublished","extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), Options{ReleaseDir: bad, AgentDir: filepath.Join(root, "bad-agent"), InstallRoot: filepath.Join(root, "bad-managed"), GOOS: "linux", GOARCH: "amd64"}); err == nil {
		t.Fatal("extra provenance field accepted")
	}
}

func TestLockSettingsHeartbeatPreventsProperLockfileSteal(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	check, release, err := lockSettings(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	time.Sleep(2500 * time.Millisecond)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required for proper-lockfile interoperability: ", err)
	}
	command := exec.Command(node, "-e", `const l=require(process.argv[1]);try{l.lockSync(process.argv[2],{realpath:false,stale:2000,update:1000});console.log("stolen")}catch(e){console.log(e.code)}`, properLockfile(t), settings)
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "ELOCKED" {
		t.Fatalf("proper-lockfile result=%q", data)
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
}

func properLockfile(t *testing.T) string {
	t.Helper()
	path, err := properLockfilePath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func properLockfilePath() (string, error) {
	path := os.Getenv("PI_PROPER_LOCKFILE")
	if path == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "npm", "root", "-g").Output()
		if err != nil {
			return "", errors.New("resolve global npm root for proper-lockfile fixture")
		}
		path = filepath.Join(strings.TrimSpace(string(output)), "@earendil-works", "pi-coding-agent", "node_modules", "proper-lockfile")
	}
	info, err := os.Stat(filepath.Join(path, "index.js"))
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("proper-lockfile fixture unavailable")
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return "", errors.New("read proper-lockfile fixture metadata")
	}
	var metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &metadata) != nil || metadata.Name != "proper-lockfile" || metadata.Version != "4.1.2" {
		return "", errors.New("proper-lockfile fixture must be version 4.1.2")
	}
	return path, nil
}

func TestProperLockfilePathRejectsWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proper-lockfile")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "index.js"), []byte("module.exports={}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "package.json"), []byte(`{"name":"proper-lockfile","version":"4.1.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_PROPER_LOCKFILE", path)
	if _, err := properLockfilePath(); err == nil {
		t.Fatal("wrong proper-lockfile version accepted")
	}
}

func TestInstallRecoversExactReservationOnRetry(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	source, _, err := validateRelease(options.ReleaseDir, "linux-x64")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(options.InstallRoot, "packages", "pi-"+version+"-"+source[:16])
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	rootData, _ := json.Marshal(rootManifest{Schema: 1, ManagedBy: "vgxness"})
	if err := os.WriteFile(filepath.Join(options.InstallRoot, rootManifestName), rootData, 0o600); err != nil {
		t.Fatal(err)
	}
	reservation, _ := json.Marshal(reservationManifest{Schema: 1, ManagedBy: "vgxness", SourceSHA: source, PackagePath: filepath.Join(destination, "package")})
	if err := os.WriteFile(filepath.Join(destination, ".vgxness-pi-reservation"), reservation, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := Install(context.Background(), options); err != nil || !result.Changed {
		t.Fatalf("retry=%+v err=%v", result, err)
	}
}

func TestRecoverReservationRetainsForeignChildAddedAtCleanupBoundary(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	packagePath := filepath.Join(destination, "package")
	source := strings.Repeat("a", 64)
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(reservationManifest{Schema: 1, ManagedBy: "vgxness", SourceSHA: source, PackagePath: packagePath})
	if err := os.WriteFile(filepath.Join(destination, ".vgxness-pi-reservation"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(destination, "foreign")
	reservationOperationHook = func(operation string) error {
		if operation == "recover" {
			return os.WriteFile(foreign, []byte("keep"), 0o600)
		}
		return nil
	}
	defer func() { reservationOperationHook = nil }()
	if recoverReservation(destination, packagePath, source) {
		t.Fatal("recovery accepted a directory with a concurrent foreign child")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "keep" {
		t.Fatalf("foreign child changed: %q %v", data, err)
	}
}

func TestInstallRetryPreservesReservationSymlinkAndTarget(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	source, _, err := validateRelease(options.ReleaseDir, "linux-x64")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(options.InstallRoot, "packages", "pi-"+version+"-"+source[:16])
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	rootData, _ := json.Marshal(rootManifest{Schema: 1, ManagedBy: "vgxness"})
	if err := os.WriteFile(filepath.Join(options.InstallRoot, rootManifestName), rootData, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "marker-target")
	valid, _ := json.Marshal(reservationManifest{Schema: 1, ManagedBy: "vgxness", SourceSHA: source, PackagePath: filepath.Join(destination, "package")})
	if err := os.WriteFile(target, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(destination, ".vgxness-pi-reservation")
	if err := os.Symlink(target, marker); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("symlink marker retried")
	}
	if info, err := os.Lstat(marker); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("marker changed: %v %v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != string(valid) {
		t.Fatalf("target changed: %q %v", data, err)
	}
}

func TestInstallReportsRetainedFailureBoundaries(t *testing.T) {
	for _, phase := range []string{"staged", "reserved", "published-inactive"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
			installBoundaryHook = func(value string) error {
				if value == phase {
					return errors.New("fixture failure")
				}
				return nil
			}
			defer func() { installBoundaryHook = nil }()
			result, err := Install(context.Background(), options)
			detail, ok := recoveryDetail(err)
			if err == nil || !ok || !strings.HasPrefix(detail, phase+" retained at ") || result.State != "" {
				t.Fatalf("result=%+v detail=%q err=%v", result, detail, err)
			}
		})
	}
}

func TestInstallReservationRecordFailurePreservesConflict(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	reservationOperationHook = func(operation string) error {
		if operation == "write" {
			return errors.New("fixture")
		}
		return nil
	}
	_, err := Install(context.Background(), options)
	detail, ok := recoveryDetail(err)
	reservationOperationHook = nil
	if !ok || !strings.HasPrefix(detail, "reservation-unproven retained at ") {
		t.Fatalf("detail=%q err=%v", detail, err)
	}
	entries, err := os.ReadDir(filepath.Join(options.InstallRoot, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	var stage, destination string
	for _, entry := range entries {
		path := filepath.Join(options.InstallRoot, "packages", entry.Name())
		if strings.HasPrefix(entry.Name(), ".pi-stage-") {
			stage = path
		} else if strings.HasPrefix(entry.Name(), "pi-"+version+"-") {
			destination = path
		}
	}
	if stage == "" || destination == "" {
		t.Fatalf("missing retained paths stage=%q destination=%q", stage, destination)
	}
	marker := filepath.Join(destination, ".vgxness-pi-reservation")
	if err := os.WriteFile(marker, []byte(`{"partial"`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := NewProvider(Options{AgentDir: options.AgentDir, InstallRoot: options.InstallRoot}).Status(context.Background(), setupflow.SharedPlan{})
	if err != nil || !strings.Contains(status.Blocker, stage) || !strings.Contains(status.Blocker, destination) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), options); err == nil {
		t.Fatal("unproven destination was retried")
	}
	after, err := os.ReadFile(marker)
	if err != nil || string(after) != string(before) {
		t.Fatalf("marker changed after refused retry: %q %v", after, err)
	}
}

func TestInstallReplacesOnlyOwnedEntryAndPreservesObjectFilters(t *testing.T) {
	root := t.TempDir()
	options := Options{AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	options.ReleaseDir = tinyRelease(t, filepath.Join(root, "release-a"), "a")
	first, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(options.AgentDir, "settings.json")
	fixture, err := json.Marshal(map[string]any{"packages": []any{map[string]any{"path": first.PackagePath, "enabled": true}, "/foreign/pi-0.1.0-lookalike"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	options.ReleaseDir = tinyRelease(t, filepath.Join(root, "release-b"), "b")
	second, err := Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	settingsValue, err := readSettingsFixture(settings)
	paths, pathsErr := settingsPackagePaths(settings)
	if err != nil || pathsErr != nil || !settingsPackageEnabled(settingsValue.Packages, second.PackagePath) || paths[second.PackagePath] != 1 || paths["/foreign/pi-0.1.0-lookalike"] != 1 {
		t.Fatalf("packages=%s paths=%v err=%v pathsErr=%v", settingsValue.Packages, paths, err, pathsErr)
	}
}

func TestInstallCancellationLeavesNoArtifacts(t *testing.T) {
	root := t.TempDir()
	options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Install(ctx, options); err == nil {
		t.Fatal("Install succeeded after cancellation")
	}
	if _, err := os.Stat(options.InstallRoot); !os.IsNotExist(err) {
		t.Fatalf("installer mutated root: %v", err)
	}
}

func TestInstallRejectsChecksumDriftAndDuplicateOwnedEntries(t *testing.T) {
	t.Run("checksum drift", func(t *testing.T) {
		root := t.TempDir()
		releaseDir := tinyRelease(t, filepath.Join(root, "release"), "a")
		archive := filepath.Join(releaseDir, "vgxness-pi-"+version+".tgz")
		if err := os.WriteFile(archive, []byte("corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		options := Options{ReleaseDir: releaseDir, AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
		if _, err := Install(context.Background(), options); err == nil {
			t.Fatal("Install accepted corrupt release archive")
		}
		if _, err := os.Stat(options.InstallRoot); !os.IsNotExist(err) {
			t.Fatalf("installer mutated root: %v", err)
		}
	})
	t.Run("duplicate owned entry", func(t *testing.T) {
		root := t.TempDir()
		options := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release-a"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
		first, err := Install(context.Background(), options)
		if err != nil {
			t.Fatal(err)
		}
		settings, err := json.Marshal(map[string]any{"packages": []string{first.PackagePath, first.PackagePath}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(options.AgentDir, "settings.json"), settings, 0o600); err != nil {
			t.Fatal(err)
		}
		options.ReleaseDir = tinyRelease(t, filepath.Join(root, "release-b"), "b")
		if _, err := Install(context.Background(), options); err == nil {
			t.Fatal("Install accepted duplicate managed settings entries")
		}
	})
}

func settingsPackagePaths(path string) (map[string]int, error) {
	settings, err := readSettingsFixture(path)
	if err != nil {
		return nil, err
	}
	paths := map[string]int{}
	for _, entry := range settings.Packages {
		var value string
		if json.Unmarshal(entry, &value) == nil {
			paths[value]++
			continue
		}
		var object struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(entry, &object); err != nil || object.Path == "" {
			return nil, errors.New("invalid package entry")
		}
		paths[object.Path]++
	}
	return paths, nil
}

type settingsFixture struct {
	Unknown  map[string]bool   `json:"unknown"`
	Packages []json.RawMessage `json:"packages"`
}

func readSettingsFixture(path string) (settingsFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return settingsFixture{}, err
	}
	var settings settingsFixture
	if err := json.Unmarshal(data, &settings); err != nil {
		return settingsFixture{}, err
	}
	return settings, nil
}

func settingsPackageEnabled(entries []json.RawMessage, path string) bool {
	for _, entry := range entries {
		var object struct {
			Path    string `json:"path"`
			Enabled bool   `json:"enabled"`
		}
		if json.Unmarshal(entry, &object) == nil && object.Path == path && object.Enabled {
			return true
		}
	}
	return false
}

func tinyRelease(t *testing.T, dir, source string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sums := make(map[string]string)
	main := "vgxness-pi-" + version + ".tgz"
	writeArchive := func(name string, files map[string][]byte) {
		path := filepath.Join(dir, name)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(file)
		tw := tar.NewWriter(gz)
		for path, data := range files {
			if err := tw.WriteHeader(&tar.Header{Name: "package/" + path, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sums[name] = checksum(data)
	}
	files := map[string][]byte{"package.json": []byte(`{"name":"@vgxness/pi","version":"0.1.0","type":"module","pi":{"extensions":["./src/extension.ts"]},"engines":{"node":">=22.19.0"},"peerDependencies":{"@earendil-works/pi-coding-agent":"^0.84.4","typebox":"1.3.7"}}`)}
	for _, name := range []string{"src/extension.ts", "src/probe.ts", "resources/migrations/manifest.json", "resources/prompts/manager.md", "resources/skills/sdd-lifecycle/SKILL.md", "LICENSE"} {
		files[name] = []byte("fixture")
	}
	migrations := []map[string]any{}
	for i := 1; i <= 23; i++ {
		name := fmt.Sprintf("%03d_fixture.sql", i)
		data := []byte("-- fixture")
		files["resources/migrations/"+name] = data
		migrations = append(migrations, map[string]any{"version": i, "file": name, "sha256": checksum(data)})
	}
	files["resources/migrations/manifest.json"], _ = json.Marshal(map[string]any{"version": 23, "migrations": migrations})
	writeArchive(main, files)
	var lines []string
	for _, name := range []string{main} {
		lines = append(lines, sums[name]+"  "+name)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provenance, _ := json.Marshal(map[string]string{"name": "@vgxness/pi", "version": version, "sourceSHA256": strings.Repeat(source, 64), "provenance": "local source snapshot; unpublished"})
	if err := os.WriteFile(filepath.Join(dir, "PROVENANCE.json"), provenance, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPortablePackageRejectsMigrationAndDependencyDrift(t *testing.T) {
	release := tinyRelease(t, filepath.Join(t.TempDir(), "release"), "a")
	_, sums, err := validateRelease(release, "linux-x64")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	name := "vgxness-pi-" + version + ".tgz"
	if err := extract(filepath.Join(release, name), root, sums[name]); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackage(root, "linux-x64"); err != nil {
		t.Fatal(err)
	}
	migration := filepath.Join(root, "resources", "migrations", "001_fixture.sql")
	if err := os.WriteFile(migration, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackage(root, "linux-x64"); err == nil || !strings.Contains(err.Error(), "migration hash") {
		t.Fatalf("migration drift=%v", err)
	}
	if err := os.WriteFile(migration, []byte("-- fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(root, "package.json")
	data, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	pkg["optionalDependencies"] = map[string]string{"@vgxness/pi-backend-linux-x64": version}
	data, _ = json.Marshal(pkg)
	if err := os.WriteFile(metadata, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackage(root, "linux-x64"); err == nil {
		t.Fatal("sidecar dependency accepted")
	}
}

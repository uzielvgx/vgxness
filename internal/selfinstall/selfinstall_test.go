package selfinstall

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/launcher"
)

func TestPreviewIsNonMutating(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root, "source-v1", "vgxness-v1")
	home := filepath.Join(root, "home")
	service := New(Config{SourceExecutable: source})
	result, err := service.Preview(context.Background(), Options{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateAbsent || !result.Changed || result.LauncherPath != filepath.Join(home, ".local", "bin", executableName()) || result.DataDir != filepath.Join(home, ".local", "share", "vgxness") {
		t.Fatalf("unexpected preview: %#v", result)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview mutated filesystem: %v", err)
	}
}

func TestInstallUpdateStatusAndRollback(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	dataDir := filepath.Join(root, "data")
	options := Options{BinDir: binDir, DataDir: dataDir}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	sourceV1 := writeSource(t, root, "source-v1", "vgxness-v1")
	serviceV1 := New(Config{SourceExecutable: sourceV1, Now: func() time.Time { return now }})

	installed, err := serviceV1.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	launcherBefore, err := os.Stat(installed.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	launcherDigest, err := launcher.FileSHA256(installed.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	if installed.State != StateInstalled || !installed.Changed || installed.ActiveSHA256 != installed.SourceSHA256 || installed.PreviousSHA256 != "" || launcherDigest != installed.SourceSHA256 {
		t.Fatalf("unexpected install: %#v launcher=%s", installed, launcherDigest)
	}
	status, err := serviceV1.Status(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := serviceV1.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateInstalled || status.UpdateAvailable || status.Changed || second.Changed {
		t.Fatalf("status=%#v second=%#v", status, second)
	}

	sourceV2 := writeSource(t, root, "source-v2", "vgxness-v2")
	serviceV2 := New(Config{SourceExecutable: sourceV2, Now: func() time.Time { return now.Add(time.Hour) }})
	preview, err := serviceV2.Preview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := serviceV2.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	launcherAfter, err := os.Stat(updated.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.UpdateAvailable || !preview.Changed || !updated.Changed || updated.ActiveSHA256 != updated.SourceSHA256 || updated.PreviousSHA256 != installed.ActiveSHA256 || !updated.RollbackAvailable || !os.SameFile(launcherBefore, launcherAfter) {
		t.Fatalf("preview=%#v updated=%#v", preview, updated)
	}
	for _, digest := range []string{installed.ActiveSHA256, updated.ActiveSHA256} {
		if actual, err := launcher.FileSHA256(launcher.VersionPath(dataDir, digest)); err != nil || actual != digest {
			t.Fatalf("version %s = %s, %v", digest, actual, err)
		}
	}

	rolledBack, err := serviceV2.Rollback(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !rolledBack.Changed || rolledBack.ActiveSHA256 != installed.ActiveSHA256 || rolledBack.PreviousSHA256 != "" || rolledBack.RollbackAvailable {
		t.Fatalf("unexpected rollback: %#v", rolledBack)
	}
	if _, err := serviceV2.Rollback(context.Background(), options); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("second rollback error = %v", err)
	}
}

func TestInstallRefusesForeignAndDriftedContent(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root, "source", "vgxness")
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	launcherPath := filepath.Join(options.BinDir, executableName())
	if err := os.MkdirAll(options.BinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcherPath, []byte("user-owned"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := New(Config{SourceExecutable: source})
	status, err := service.Status(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), options); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign install error = %v", err)
	}
	data, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateDrifted || string(data) != "user-owned" {
		t.Fatalf("status=%#v data=%q", status, data)
	}
}

func TestInstallRecoversExactLauncherWhenManifestWasNotPublished(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root, "source", "vgxness")
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	if err := os.MkdirAll(options.BinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	launcherPath := filepath.Join(options.BinDir, executableName())
	if err := os.WriteFile(launcherPath, content, 0o700); err != nil {
		t.Fatal(err)
	}
	service := New(Config{SourceExecutable: source})
	installed, err := service.Install(context.Background(), options)
	if err != nil || installed.State != StateInstalled || !installed.Changed {
		t.Fatalf("crash-left launcher was not recovered: %#v err=%v", installed, err)
	}
	if _, err := os.Stat(launcher.SidecarPath(launcherPath)); err != nil {
		t.Fatalf("manifest was not repaired: %v", err)
	}
}

func TestInstallPreservesExistingDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not portable to Windows")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dataDir, 0o711); err != nil {
		t.Fatal(err)
	}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	if _, err := service.Install(context.Background(), Options{BinDir: binDir, DataDir: dataDir}); err != nil {
		t.Fatal(err)
	}
	binInfo, err := os.Stat(binDir)
	if err != nil {
		t.Fatal(err)
	}
	dataInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if binInfo.Mode().Perm() != 0o755 || dataInfo.Mode().Perm() != 0o711 {
		t.Fatalf("existing directory modes changed to %o/%o", binInfo.Mode().Perm(), dataInfo.Mode().Perm())
	}
}

func TestInstallRefusesSymlinkDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	foreign := filepath.Join(root, "foreign")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, binDir); err != nil {
		t.Fatal(err)
	}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	_, err := service.Install(context.Background(), Options{BinDir: binDir, DataDir: filepath.Join(root, "data")})
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("symlink install error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(foreign, executableName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign directory was modified: %v", err)
	}
}

func TestInstallRefusesSymlinkAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	foreign := filepath.Join(root, "foreign")
	if err := os.MkdirAll(filepath.Join(foreign, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(foreign, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(foreign, linked); err != nil {
		t.Fatal(err)
	}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	_, err := service.Install(context.Background(), Options{BinDir: filepath.Join(linked, "bin"), DataDir: filepath.Join(root, "data")})
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("symlink-ancestor install error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(foreign, "bin", executableName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink ancestor was modified: %v", err)
	}
}

func TestInstallReportsRecoveryPendingAfterManifestPublicationSyncFailure(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{
		SourceExecutable:     writeSource(t, root, "source", "vgxness"),
		afterManifestPublish: func() error { return errors.New("injected post-publish failure") },
	})
	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || result.State != StateRecoveryPending || !result.Changed {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	service.afterManifestPublish = nil
	recovered, err := service.Install(context.Background(), options)
	if err != nil || recovered.State != StateInstalled || recovered.Changed {
		t.Fatalf("retry result=%#v err=%v", recovered, err)
	}
}

func TestInstallCancellationAfterManifestPublicationRequiresRecoveryAndRetry(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(Config{
		SourceExecutable: writeSource(t, root, "source", "vgxness"),
		afterManifestPublish: func() error {
			cancel()
			return nil
		},
	})
	result, err := service.Install(ctx, options)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRecovery) || result.State != StateRecoveryPending || !result.Changed {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	service.afterManifestPublish = nil
	retried, err := service.Install(context.Background(), options)
	if err != nil || retried.State != StateInstalled || retried.Changed {
		t.Fatalf("retry result=%#v err=%v", retried, err)
	}
}

func TestRollbackRefusesTamperedPreviousVersionWithoutChangingActiveManifest(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := New(Config{SourceExecutable: writeSource(t, root, "source-v1", "vgxness-v1")})
	v1, err := first.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceExecutable: writeSource(t, root, "source-v2", "vgxness-v2")})
	v2, err := second.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	previousPath := launcher.VersionPath(options.DataDir, v1.ActiveSHA256)
	if err := os.Chmod(previousPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previousPath, []byte("tampered"), 0o555); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(v2.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Rollback(context.Background(), options); !errors.Is(err, ErrDrift) {
		t.Fatalf("Rollback() error=%v, want ErrDrift", err)
	}
	manifestAfter, err := os.ReadFile(v2.ManifestPath)
	if err != nil || !bytes.Equal(manifestAfter, manifestBefore) {
		t.Fatalf("active manifest changed=%q err=%v", manifestAfter, err)
	}
	if digest, err := launcher.FileSHA256(launcher.VersionPath(options.DataDir, v2.ActiveSHA256)); err != nil || digest != v2.ActiveSHA256 {
		t.Fatalf("active version digest=%q err=%v", digest, err)
	}
	if data, err := os.ReadFile(previousPath); err != nil || string(data) != "tampered" {
		t.Fatalf("previous version restored=%q err=%v", data, err)
	}
}

func TestConcurrentInstallsSerializeWithoutCorruption(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	entered, release := make(chan struct{}), make(chan struct{})
	firstService := New(Config{
		SourceExecutable: writeSource(t, root, "source", "vgxness"),
		afterManifestPublish: func() error {
			close(entered)
			<-release
			return nil
		},
	})
	secondAnchored := make(chan struct{})
	secondService := New(Config{
		SourceExecutable: firstService.source,
		afterAnchorsOpen: func() error {
			close(secondAnchored)
			return nil
		},
	})
	firstDone := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := firstService.Install(context.Background(), options)
		firstDone <- struct {
			result Result
			err    error
		}{result, err}
	}()
	<-entered
	secondDone := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := secondService.Install(context.Background(), options)
		secondDone <- struct {
			result Result
			err    error
		}{result, err}
	}()
	<-secondAnchored
	close(release)
	first, second := <-firstDone, <-secondDone
	if first.err != nil || second.err != nil || first.result.State != StateInstalled || second.result.State != StateInstalled || second.result.Changed {
		t.Fatalf("first=%#v/%v second=%#v/%v", first.result, first.err, second.result, second.err)
	}
	status, err := firstService.Status(context.Background(), options)
	if err != nil || status.State != StateInstalled || status.ActiveSHA256 != status.SourceSHA256 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	versions, err := os.ReadDir(filepath.Join(options.DataDir, "versions"))
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
}

func TestWaitingInstallCancellationDoesNotMutateLockedTransaction(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	entered, release := make(chan struct{}), make(chan struct{})
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness"), afterManifestPublish: func() error {
		close(entered)
		<-release
		return nil
	}})
	firstDone := make(chan error, 1)
	go func() { _, err := service.Install(context.Background(), options); firstDone <- err }()
	<-entered
	manifestPath := filepath.Join(options.BinDir, executableName()+".launcher.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { _, err := service.Install(ctx, options); secondDone <- err }()
	cancel()
	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting Install() error=%v", err)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("waiting install mutated manifest=%q err=%v", after, err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if status, err := service.Status(context.Background(), options); err != nil || status.State != StateInstalled {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestRecoveryPendingPreservesConcurrentManifestReplacement(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(installed.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	foreign := []byte("concurrent replacement\n")
	if err := os.WriteFile(installed.ManifestPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolvePaths(options)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	if err := writeRecoveryRoot(anchors.data, manifestRecovery{Manifest: installed.ManifestPath, Expected: original, Published: []byte("published\n")}); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || status.State != StateRecoveryPending {
		t.Fatalf("Status() result=%#v err=%v", status, err)
	}
	_, err = service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("Install() error=%v", err)
	}
	current, readErr := os.ReadFile(installed.ManifestPath)
	if readErr != nil || string(current) != string(foreign) {
		t.Fatalf("concurrent manifest was overwritten: %q read=%v", current, readErr)
	}
}

func TestInstallRecoversManifestMovedBeforePublication(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := writeSource(t, root, "source-v1", "vgxness-v1")
	if _, err := New(Config{SourceExecutable: first}).Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	second := writeSource(t, root, "source-v2", "vgxness-v2")
	service := New(Config{SourceExecutable: second, afterManifestMove: func() error { return errors.New("stop after move") }})
	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || result.State != StateRecoveryPending {
		t.Fatalf("interrupted update result=%#v err=%v", result, err)
	}
	service.afterManifestMove = nil
	retried, err := service.Install(context.Background(), options)
	if err != nil || retried.State != StateInstalled || retried.ActiveSHA256 != retried.SourceSHA256 {
		t.Fatalf("retry result=%#v err=%v", retried, err)
	}
	if len(backupNames(t, options.BinDir)) == 0 {
		t.Fatal("verified recovery backup was deleted")
	}
}

func TestInstallPreservesManifestReplacedBetweenPrecheckAndMove(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := writeSource(t, root, "source-v1", "vgxness-v1")
	installed, err := New(Config{SourceExecutable: first}).Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	foreign := []byte("foreign manifest\n")
	foreignPath := filepath.Join(root, "foreign-manifest")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	second := writeSource(t, root, "source-v2", "vgxness-v2")
	service := New(Config{SourceExecutable: second, afterManifestPrecheck: func() error {
		return os.Rename(foreignPath, installed.ManifestPath)
	}})

	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	current, readErr := os.ReadFile(installed.ManifestPath)
	if readErr != nil || !bytes.Equal(current, foreign) {
		t.Fatalf("foreign manifest was not preserved: %q read=%v", current, readErr)
	}
	retry, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || retry.State != StateRecoveryPending {
		t.Fatalf("retry result=%#v err=%v", retry, err)
	}
	current, readErr = os.ReadFile(installed.ManifestPath)
	if readErr != nil || !bytes.Equal(current, foreign) {
		t.Fatalf("retry changed foreign manifest: %q read=%v", current, readErr)
	}
}

func TestInstallPreservesManifestReplacedAfterMove(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := writeSource(t, root, "source-v1", "vgxness-v1")
	installed, err := New(Config{SourceExecutable: first}).Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	foreign := []byte("foreign manifest\n")
	foreignPath := filepath.Join(root, "foreign-manifest")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	second := writeSource(t, root, "source-v2", "vgxness-v2")
	service := New(Config{SourceExecutable: second, afterManifestMove: func() error {
		return os.Rename(foreignPath, installed.ManifestPath)
	}})

	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	if current, readErr := os.ReadFile(installed.ManifestPath); readErr != nil || !bytes.Equal(current, foreign) {
		t.Fatalf("post-move replacement was not preserved: %q err=%v", current, readErr)
	}
}

func TestInstallPreservesByteIdenticalManifestReplacement(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := writeSource(t, root, "source-v1", "vgxness-v1")
	installed, err := New(Config{SourceExecutable: first}).Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(installed.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(root, "foreign-manifest")
	if err := os.WriteFile(foreignPath, expected, 0o600); err != nil {
		t.Fatal(err)
	}
	foreignInfo, err := os.Lstat(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	second := writeSource(t, root, "source-v2", "vgxness-v2")
	service := New(Config{SourceExecutable: second, afterManifestPrecheck: func() error {
		return os.Rename(foreignPath, installed.ManifestPath)
	}})

	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	currentInfo, statErr := os.Lstat(installed.ManifestPath)
	if statErr != nil || !os.SameFile(currentInfo, foreignInfo) {
		t.Fatalf("byte-identical foreign manifest was not restored: info=%v err=%v", currentInfo, statErr)
	}
	retry, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || retry.State != StateRecoveryPending {
		t.Fatalf("retry result=%#v err=%v", retry, err)
	}
	currentInfo, statErr = os.Lstat(installed.ManifestPath)
	if statErr != nil || !os.SameFile(currentInfo, foreignInfo) {
		t.Fatalf("retry changed byte-identical foreign manifest: info=%v err=%v", currentInfo, statErr)
	}
}

func TestInstallRejectsBackupReplacedAfterIdentitySampling(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := writeSource(t, root, "source-v1", "vgxness-v1")
	installed, err := New(Config{SourceExecutable: first}).Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(installed.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(root, "foreign-manifest")
	if err := os.WriteFile(foreignPath, expected, 0o600); err != nil {
		t.Fatal(err)
	}
	foreignInfo, err := os.Lstat(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	second := writeSource(t, root, "source-v2", "vgxness-v2")
	service := New(Config{SourceExecutable: second, afterManifestBackupSample: func(backup string) error {
		return os.Rename(foreignPath, filepath.Join(options.BinDir, backup))
	}})

	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	currentInfo, statErr := os.Lstat(installed.ManifestPath)
	if statErr != nil || !os.SameFile(currentInfo, foreignInfo) {
		t.Fatalf("candidate was published over byte-identical foreign manifest: info=%v err=%v", currentInfo, statErr)
	}
	retry, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || retry.State != StateRecoveryPending {
		t.Fatalf("retry result=%#v err=%v", retry, err)
	}
}

func TestInstallRefusesOccupiedManifestBackupTarget(t *testing.T) {
	for _, occupied := range []string{"file", "directory"} {
		t.Run(occupied, func(t *testing.T) {
			root := t.TempDir()
			options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
			first := writeSource(t, root, "source-v1", "vgxness-v1")
			installed, err := New(Config{SourceExecutable: first}).Install(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(installed.ManifestPath)
			if err != nil {
				t.Fatal(err)
			}
			second := writeSource(t, root, "source-v2", "vgxness-v2")
			var backupName string
			service := New(Config{SourceExecutable: second, beforeManifestMove: func(backup string) error {
				backupName = backup
				path := filepath.Join(options.BinDir, backup)
				if occupied == "directory" {
					return os.Mkdir(path, 0o700)
				}
				return os.WriteFile(path, []byte("foreign backup\n"), 0o600)
			}})

			result, err := service.Install(context.Background(), options)
			if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
				t.Fatalf("Install() result=%#v err=%v", result, err)
			}
			if current, readErr := os.ReadFile(installed.ManifestPath); readErr != nil || !bytes.Equal(current, expected) {
				t.Fatalf("manifest changed=%q err=%v", current, readErr)
			}
			if backupName == "" {
				t.Fatal("pre-move hook was not called")
			}
			info, statErr := os.Lstat(filepath.Join(options.BinDir, backupName))
			if statErr != nil || (occupied == "directory") != info.IsDir() {
				t.Fatalf("occupied backup changed: info=%v err=%v", info, statErr)
			}
		})
	}
}

func TestRecoveryClassifiesMismatchedBackupWithoutConflictMarker(t *testing.T) {
	published := []byte("published\n")
	foreign := []byte("foreign backup\n")
	fixture := newRecoveryFixture(t, false, true, published, foreign)
	requireRecoveryConflict(t, fixture)
	if data, err := os.ReadFile(filepath.Join(fixture.options.BinDir, fixture.backup)); err != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("mismatched backup was not preserved: %q err=%v", data, err)
	}
}

type recoveryFixture struct {
	root, backup string
	options      Options
	service      *Service
	installed    Result
	expected     []byte
}

func newRecoveryFixture(t *testing.T, missing, withBackup bool, published, backupData []byte) recoveryFixture {
	t.Helper()
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(installed.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		err = os.Remove(installed.ManifestPath)
	} else if published != nil {
		err = os.WriteFile(installed.ManifestPath, published, 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolvePaths(options)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(resolved)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(anchors.close)
	backup := ""
	if withBackup {
		backup = ".vgxness-manifest-backup-00112233445566778899aabb"
		if backupData == nil {
			backupData = expected
		}
		if err := writeRootFile(anchors.bin, backup, backupData, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if published == nil {
		published = expected
	}
	if err := writeRecoveryRoot(anchors.data, manifestRecovery{Manifest: installed.ManifestPath, Expected: expected, Published: published, Backup: backup}); err != nil {
		t.Fatal(err)
	}
	return recoveryFixture{root: root, backup: backup, options: options, service: service, installed: installed, expected: expected}
}

func requireRecoveryConflict(t *testing.T, fixture recoveryFixture) {
	t.Helper()
	result, err := fixture.service.Install(context.Background(), fixture.options)
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
}

func TestRecoveryRejectsBackupReplacedDuringRead(t *testing.T) {
	fixture := newRecoveryFixture(t, false, true, []byte("published\n"), nil)
	foreignPath := filepath.Join(fixture.root, "foreign-backup")
	foreign := []byte("foreign backup\n")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.service.afterRecoveryReadSample = func(name string) error {
		if name == fixture.backup {
			return os.Rename(foreignPath, filepath.Join(fixture.options.BinDir, fixture.backup))
		}
		return nil
	}

	requireRecoveryConflict(t, fixture)
	if data, err := os.ReadFile(filepath.Join(fixture.options.BinDir, fixture.backup)); err != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("late foreign backup was not preserved: %q err=%v", data, err)
	}
}

func TestRecoveryPreservesJournalReplacedBeforeArchive(t *testing.T) {
	fixture := newRecoveryFixture(t, false, false, nil, nil)
	foreignPath := filepath.Join(fixture.root, "foreign-journal")
	foreign := []byte("foreign journal\n")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.service.afterRecoveryJournalRead = func() error {
		return os.Rename(foreignPath, filepath.Join(fixture.options.DataDir, ".manifest-recovery.json"))
	}
	requireRecoveryConflict(t, fixture)
	if data, err := os.ReadFile(filepath.Join(fixture.options.DataDir, ".manifest-recovery.json")); err != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("foreign journal was not preserved: %q err=%v", data, err)
	}
}

func TestRecoveryRestoresExpectedWithoutLinkingLateBackupReplacement(t *testing.T) {
	fixture := newRecoveryFixture(t, true, true, []byte("published\n"), nil)
	foreignPath := filepath.Join(fixture.root, "foreign-backup")
	foreign := []byte("foreign backup\n")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.service.afterRecoveryBackupCheck = func(name string) error {
		if name == fixture.backup {
			return os.Rename(foreignPath, filepath.Join(fixture.options.BinDir, fixture.backup))
		}
		return nil
	}

	requireRecoveryConflict(t, fixture)
	if data, err := os.ReadFile(fixture.installed.ManifestPath); err != nil || !bytes.Equal(data, fixture.expected) {
		t.Fatalf("manifest was not restored from expected bytes: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(fixture.options.BinDir, fixture.backup)); err != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("late foreign backup was not preserved: %q err=%v", data, err)
	}
}

func TestRecoveryRejectsManifestReplacementAfterDirectRestore(t *testing.T) {
	fixture := newRecoveryFixture(t, true, true, []byte("published\n"), nil)
	foreignPath := filepath.Join(fixture.root, "foreign-manifest")
	foreign := []byte("foreign manifest\n")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.service.afterRecoveryManifestMake = func() error { return os.Rename(foreignPath, fixture.installed.ManifestPath) }
	requireRecoveryConflict(t, fixture)
	if data, err := os.ReadFile(fixture.installed.ManifestPath); err != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("foreign manifest was not preserved: %q err=%v", data, err)
	}
}

func TestRecoveryRejectsBackupReplacementBeforeArchive(t *testing.T) {
	fixture := newRecoveryFixture(t, false, true, nil, nil)
	foreignPath := filepath.Join(fixture.root, "foreign-backup")
	foreign := []byte("foreign backup\n")
	if err := os.WriteFile(foreignPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.service.beforeRecoveryArchive = func(string) error {
		return os.Rename(foreignPath, filepath.Join(fixture.options.BinDir, fixture.backup))
	}
	requireRecoveryConflict(t, fixture)
	if data, err := os.ReadFile(filepath.Join(fixture.options.BinDir, fixture.backup)); err != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("late foreign backup was not preserved: %q err=%v", data, err)
	}
}

func TestRecoveryRefusesOccupiedJournalArchiveTarget(t *testing.T) {
	fixture := newRecoveryFixture(t, false, false, nil, nil)
	fixture.service.beforeRecoveryArchive = func(archive string) error {
		return os.WriteFile(filepath.Join(fixture.options.DataDir, archive), []byte("foreign archive\n"), 0o600)
	}
	requireRecoveryConflict(t, fixture)
	if _, err := os.Stat(filepath.Join(fixture.options.DataDir, ".manifest-recovery.json")); err != nil {
		t.Fatalf("journal was changed after archive collision: %v", err)
	}
}

func TestRecoveryArchiveFailureRetainsEvidenceAndLeavesSafeState(t *testing.T) {
	fixture := newRecoveryFixture(t, false, false, nil, nil)
	fixture.service.afterRecoveryArchive = func() error { return errors.New("stop after archive") }

	result, err := fixture.service.Install(context.Background(), fixture.options)
	if !errors.Is(err, ErrRecovery) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	if len(recoveryArchiveNames(t, fixture.options.DataDir)) == 0 {
		t.Fatal("recovery archive was not retained")
	}
	fixture.service.afterRecoveryArchive = nil
	retry, err := fixture.service.Install(context.Background(), fixture.options)
	if err != nil || retry.State != StateInstalled || retry.Changed {
		t.Fatalf("retry result=%#v err=%v", retry, err)
	}
}

func recoveryArchiveNames(t *testing.T, dataDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".manifest-recovery-archive-") {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestInstallPreservesSymlinkManifestReplacement(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := writeSource(t, root, "source-v1", "vgxness-v1")
	installed, err := New(Config{SourceExecutable: first}).Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "foreign-target")
	if err := os.WriteFile(target, []byte("foreign target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(root, "foreign-manifest")
	if err := os.Symlink(target, foreignPath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	second := writeSource(t, root, "source-v2", "vgxness-v2")
	service := New(Config{SourceExecutable: second, afterManifestPrecheck: func() error {
		return os.Rename(foreignPath, installed.ManifestPath)
	}})

	result, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) || result.State != StateRecoveryPending {
		t.Fatalf("Install() result=%#v err=%v", result, err)
	}
	location := preservedSymlinkManifest(t, options.BinDir, filepath.Base(installed.ManifestPath))
	if link, err := os.Readlink(filepath.Join(options.BinDir, location)); err != nil || link != target {
		t.Fatalf("foreign symlink target=%q err=%v, want %q", link, err, target)
	}
	retry, err := service.Install(context.Background(), options)
	if !errors.Is(err, ErrRecovery) || retry.State != StateRecoveryPending {
		t.Fatalf("retry result=%#v err=%v", retry, err)
	}
	if got := preservedSymlinkManifest(t, options.BinDir, filepath.Base(installed.ManifestPath)); got != location {
		t.Fatalf("retry moved preserved symlink from %q to %q", location, got)
	}
	if link, err := os.Readlink(filepath.Join(options.BinDir, location)); err != nil || link != target {
		t.Fatalf("retry changed foreign symlink target=%q err=%v, want %q", link, err, target)
	}
}

func preservedSymlinkManifest(t *testing.T, binDir, manifestName string) string {
	t.Helper()
	for _, name := range append([]string{manifestName}, backupNames(t, binDir)...) {
		info, err := os.Lstat(filepath.Join(binDir, name))
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return name
		}
	}
	t.Fatal("foreign symlink was neither restored nor retained as recovery backup")
	return ""
}

func backupNames(t *testing.T, binDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".vgxness-manifest-backup-") {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestInstallRecoveryToleratesBackupCleanupBeforeJournalCleanup(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := New(Config{SourceExecutable: writeSource(t, root, "source-v1", "vgxness-v1")})
	installed, err := first.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(installed.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceExecutable: writeSource(t, root, "source-v2", "vgxness-v2")})
	updated, err := second.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(updated.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolvePaths(options)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	missingBackup := ".vgxness-manifest-backup-00112233445566778899aabb"
	if err := writeRecoveryRoot(anchors.data, manifestRecovery{Manifest: updated.ManifestPath, Expected: previous, Published: published, Backup: missingBackup}); err != nil {
		t.Fatal(err)
	}
	retried, err := second.Install(context.Background(), options)
	if err != nil || retried.State != StateInstalled || retried.Changed {
		t.Fatalf("retry result=%#v err=%v", retried, err)
	}
}

func TestInstallRecoveryRejectsArbitraryBackupName(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(installed.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(options.BinDir, "keep-me")
	if err := os.WriteFile(victim, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolvePaths(options)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	if err := writeRecoveryRoot(anchors.data, manifestRecovery{Manifest: installed.ManifestPath, Expected: []byte("foreign"), Published: manifest, Backup: "keep-me"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), options); !errors.Is(err, ErrRecovery) {
		t.Fatalf("Install() error=%v, want recovery rejection", err)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "foreign" {
		t.Fatalf("victim=%q err=%v", data, err)
	}
}

func TestPublishRootDirectoryDoesNotReplaceExistingDirectory(t *testing.T) {
	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("source", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("destination", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := publishRootDirectoryNoReplace(root, "source", "destination"); err == nil {
		t.Fatal("publish replaced an existing directory")
	}
	for _, name := range []string{"source", "destination"} {
		if info, err := root.Lstat(name); err != nil || !info.IsDir() {
			t.Fatalf("%s missing after failed publish: info=%v err=%v", name, info, err)
		}
	}
}

func TestInstallDoesNotFollowReplacementAfterAnchorsOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	binDir, dataDir := filepath.Join(parent, "bin"), filepath.Join(parent, "data")
	foreign := filepath.Join(root, "foreign")
	if err := os.MkdirAll(filepath.Join(foreign, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(foreign, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness"), afterAnchorsOpen: func() error {
		moved := filepath.Join(root, "anchored-parent")
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		return os.Symlink(foreign, parent)
	}})
	_, _ = service.Install(context.Background(), Options{BinDir: binDir, DataDir: dataDir})
	for _, path := range []string{filepath.Join(foreign, "bin", executableName()), filepath.Join(foreign, "data", "versions"), filepath.Join(foreign, "data", ".install.lock")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replacement redirected write into foreign hierarchy at %q: %v", path, err)
		}
	}
}

func TestInstallFailsClosedWhenAnchoredRootsLoseTheirNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming an open directory is not portable to Windows")
	}
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	binDir, dataDir := filepath.Join(parent, "bin"), filepath.Join(parent, "data")
	anchored := filepath.Join(root, "anchored-parent")
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness"), afterAnchorsOpen: func() error {
		if err := os.Rename(parent, anchored); err != nil {
			return err
		}
		if err := os.MkdirAll(binDir, 0o700); err != nil {
			return err
		}
		return os.MkdirAll(dataDir, 0o700)
	}})
	installed, err := service.Install(context.Background(), Options{BinDir: binDir, DataDir: dataDir})
	if !errors.Is(err, ErrDrift) || installed.State != "" {
		t.Fatalf("Install() result=%#v err=%v", installed, err)
	}
	for _, path := range []string{
		filepath.Join(binDir, executableName()),
		filepath.Join(binDir, executableName()+".launcher.json"),
		filepath.Join(dataDir, "versions"),
		filepath.Join(dataDir, ".install.lock"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replacement received write at %q: %v", path, err)
		}
	}
}

func TestCanceledPreviewDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "vgxness")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Preview(ctx, Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preview error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled preview mutated filesystem: %v", err)
	}
}

func writeSource(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

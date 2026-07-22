package selfinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func executableName() string {
	if runtime.GOOS == "windows" {
		return "vgxness.exe"
	}
	return "vgxness"
}

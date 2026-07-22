package selfinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/launcher"
)

var (
	ErrInvalid    = errors.New("invalid self-install request")
	ErrConflict   = errors.New("self-install conflicts with existing content")
	ErrDrift      = errors.New("managed self-install has drifted")
	ErrNoRollback = errors.New("no previous managed version is available")
)

type State string

const (
	StateAbsent    State = "absent"
	StateInstalled State = "installed"
	StateDrifted   State = "drifted"
)

type Options struct {
	BinDir  string
	DataDir string
	HomeDir string
}

type Result struct {
	State             State
	LauncherPath      string
	ManifestPath      string
	DataDir           string
	SourceSHA256      string
	ActiveSHA256      string
	PreviousSHA256    string
	UpdateAvailable   bool
	RollbackAvailable bool
	Changed           bool
}

type Runtime interface {
	Preview(context.Context, Options) (Result, error)
	Install(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Result, error)
	Rollback(context.Context, Options) (Result, error)
}

type Config struct {
	SourceExecutable string
	Now              func() time.Time
}

type Service struct {
	source string
	now    func() time.Time
}

type paths struct {
	binDir   string
	dataDir  string
	launcher string
	manifest string
	versions string
	lock     string
}

type inspection struct {
	result           Result
	paths            paths
	manifest         launcher.Manifest
	manifestRaw      []byte
	reusableLauncher bool
}

func New(config Config) *Service {
	source := strings.TrimSpace(config.SourceExecutable)
	if source == "" {
		source, _ = os.Executable()
	}
	if source != "" {
		source, _ = filepath.Abs(source)
		if resolved, err := filepath.EvalSymlinks(source); err == nil {
			source = resolved
		}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Service{source: source, now: now}
}

func (service *Service) Preview(ctx context.Context, options Options) (Result, error) {
	state, err := service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	state.result.Changed = state.result.State == StateAbsent || state.result.State == StateInstalled && state.result.UpdateAvailable
	return state.result, nil
}

func (service *Service) Status(ctx context.Context, options Options) (Result, error) {
	state, err := service.inspect(ctx, options)
	return state.result, err
}

func (service *Service) Install(ctx context.Context, options Options) (Result, error) {
	initial, err := service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	if initial.result.State == StateDrifted {
		return Result{}, ErrConflict
	}
	if initial.result.State == StateInstalled && !initial.result.UpdateAvailable {
		return initial.result, nil
	}
	if err := prepareDirectory(initial.paths.binDir); err != nil {
		return Result{}, fmt.Errorf("prepare launcher directory: %w", err)
	}
	if err := prepareDirectory(initial.paths.dataDir); err != nil {
		return Result{}, fmt.Errorf("prepare version data directory: %w", err)
	}
	if err := prepareDirectory(initial.paths.versions); err != nil {
		return Result{}, fmt.Errorf("prepare versions directory: %w", err)
	}
	lock, err := acquire(ctx, initial.paths.lock)
	if err != nil {
		return Result{}, err
	}
	defer lock.release()
	current, err := service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	if current.result.State == StateDrifted {
		return Result{}, ErrConflict
	}
	if current.result.State == StateInstalled && !current.result.UpdateAvailable {
		return current.result, nil
	}
	if err := installVersion(ctx, service.source, current.paths, current.result.SourceSHA256); err != nil {
		return Result{}, err
	}
	launcherTemporary := ""
	if current.result.State == StateAbsent && !current.reusableLauncher {
		launcherTemporary, err = installLauncher(ctx, service.source, current.paths.launcher, current.result.SourceSHA256)
		if err != nil {
			return Result{}, err
		}
		defer os.Remove(launcherTemporary)
	}
	launcherDigest := current.manifest.LauncherSHA256
	if current.result.State == StateAbsent {
		launcherDigest = current.result.SourceSHA256
	}
	previous := ""
	if current.result.State == StateInstalled {
		previous = current.result.ActiveSHA256
	}
	manifest := launcher.Manifest{
		SchemaVersion: launcher.SchemaVersion, ManagedBy: launcher.ManagedBy,
		LauncherPath: current.paths.launcher, LauncherSHA256: launcherDigest, DataDir: current.paths.dataDir,
		ActivePath: launcher.VersionPath(current.paths.dataDir, current.result.SourceSHA256), ActiveSHA256: current.result.SourceSHA256,
		PreviousSHA256: previous, UpdatedAt: service.now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeManifest(ctx, current.paths.manifest, manifest, current.manifestRaw); err != nil {
		return Result{}, err
	}
	verified, err := service.inspect(ctx, options)
	if err != nil || verified.result.State != StateInstalled || verified.result.ActiveSHA256 != current.result.SourceSHA256 {
		return Result{}, ErrDrift
	}
	verified.result.Changed = true
	return verified.result, nil
}

func (service *Service) Rollback(ctx context.Context, options Options) (Result, error) {
	initial, err := service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	if initial.result.State == StateDrifted {
		return Result{}, ErrDrift
	}
	if initial.result.State != StateInstalled || initial.result.PreviousSHA256 == "" {
		return Result{}, ErrNoRollback
	}
	lock, err := acquire(ctx, initial.paths.lock)
	if err != nil {
		return Result{}, err
	}
	defer lock.release()
	current, err := service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	if current.result.State != StateInstalled || current.result.PreviousSHA256 == "" {
		return Result{}, ErrNoRollback
	}
	previousPath := launcher.VersionPath(current.paths.dataDir, current.result.PreviousSHA256)
	previousDigest, err := launcher.FileSHA256(previousPath)
	if err != nil || previousDigest != current.result.PreviousSHA256 {
		return Result{}, ErrDrift
	}
	manifest := current.manifest
	manifest.ActivePath = previousPath
	manifest.ActiveSHA256 = previousDigest
	manifest.PreviousSHA256 = ""
	manifest.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
	if err := writeManifest(ctx, current.paths.manifest, manifest, current.manifestRaw); err != nil {
		return Result{}, err
	}
	verified, err := service.inspect(ctx, options)
	if err != nil || verified.result.State != StateInstalled || verified.result.ActiveSHA256 != previousDigest {
		return Result{}, ErrDrift
	}
	verified.result.Changed = true
	return verified.result, nil
}

func (service *Service) inspect(ctx context.Context, options Options) (inspection, error) {
	if err := ctx.Err(); err != nil {
		return inspection{}, err
	}
	resolved, err := resolvePaths(options)
	if err != nil {
		return inspection{}, err
	}
	sourceDigest, err := launcher.FileSHA256(service.source)
	if err != nil {
		return inspection{}, fmt.Errorf("%w: source executable", ErrInvalid)
	}
	state := inspection{paths: resolved, result: Result{
		State: StateAbsent, LauncherPath: resolved.launcher, ManifestPath: resolved.manifest,
		DataDir: resolved.dataDir, SourceSHA256: sourceDigest,
	}}
	launcherInfo, launcherErr := os.Lstat(resolved.launcher)
	manifestInfo, manifestErr := os.Lstat(resolved.manifest)
	launcherAbsent := errors.Is(launcherErr, os.ErrNotExist)
	manifestAbsent := errors.Is(manifestErr, os.ErrNotExist)
	if launcherAbsent && manifestAbsent {
		return state, nil
	}
	if !launcherAbsent && manifestAbsent && launcherErr == nil && launcherInfo.Mode()&os.ModeSymlink == 0 && launcherInfo.Mode().IsRegular() {
		if digest, digestErr := launcher.FileSHA256(resolved.launcher); digestErr == nil && digest == sourceDigest {
			state.reusableLauncher = true
			return state, nil
		}
	}
	if (launcherErr != nil && !launcherAbsent) || (manifestErr != nil && !manifestAbsent) ||
		launcherAbsent != manifestAbsent || launcherInfo.Mode()&os.ModeSymlink != 0 ||
		!launcherInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 ||
		!manifestInfo.Mode().IsRegular() {
		state.result.State = StateDrifted
		return state, nil
	}
	manifestRaw, err := readRegular(resolved.manifest, 64<<10)
	if err != nil {
		state.result.State = StateDrifted
		return state, nil
	}
	manifest, err := launcher.Load(resolved.launcher)
	if err != nil || filepath.Clean(manifest.DataDir) != resolved.dataDir {
		state.result.State = StateDrifted
		return state, nil
	}
	launcherDigest, err := launcher.FileSHA256(resolved.launcher)
	if err != nil || launcherDigest != manifest.LauncherSHA256 {
		state.result.State = StateDrifted
		return state, nil
	}
	activeDigest, err := launcher.FileSHA256(manifest.ActivePath)
	if err != nil || activeDigest != manifest.ActiveSHA256 {
		state.result.State = StateDrifted
		return state, nil
	}
	if manifest.PreviousSHA256 != "" {
		previousDigest, previousErr := launcher.FileSHA256(launcher.VersionPath(resolved.dataDir, manifest.PreviousSHA256))
		if previousErr != nil || previousDigest != manifest.PreviousSHA256 {
			state.result.State = StateDrifted
			return state, nil
		}
	}
	state.manifest = manifest
	state.manifestRaw = manifestRaw
	state.result.State = StateInstalled
	state.result.ActiveSHA256 = manifest.ActiveSHA256
	state.result.PreviousSHA256 = manifest.PreviousSHA256
	state.result.UpdateAvailable = sourceDigest != manifest.ActiveSHA256
	state.result.RollbackAvailable = manifest.PreviousSHA256 != ""
	return state, nil
}

func resolvePaths(options Options) (paths, error) {
	home := strings.TrimSpace(options.HomeDir)
	var err error
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return paths{}, ErrInvalid
		}
	}
	if !filepath.IsAbs(home) {
		return paths{}, ErrInvalid
	}
	binDir := strings.TrimSpace(options.BinDir)
	if binDir == "" {
		binDir = filepath.Join(home, ".local", "bin")
	}
	dataDir := strings.TrimSpace(options.DataDir)
	if dataDir == "" {
		dataDir = filepath.Join(home, ".local", "share", "vgxness")
	}
	if !filepath.IsAbs(binDir) || !filepath.IsAbs(dataDir) {
		return paths{}, ErrInvalid
	}
	name := "vgxness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	launcherPath := filepath.Join(filepath.Clean(binDir), name)
	return paths{
		binDir: filepath.Clean(binDir), dataDir: filepath.Clean(dataDir), launcher: launcherPath,
		manifest: launcher.SidecarPath(launcherPath), versions: filepath.Join(filepath.Clean(dataDir), "versions"),
		lock: filepath.Join(filepath.Clean(dataDir), ".install.lock"),
	}, nil
}

func installVersion(ctx context.Context, source string, target paths, digest string) error {
	versionDirectory := filepath.Dir(launcher.VersionPath(target.dataDir, digest))
	if info, err := os.Lstat(versionDirectory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrDrift
		}
		installedDigest, hashErr := launcher.FileSHA256(launcher.VersionPath(target.dataDir, digest))
		if hashErr != nil || installedDigest != digest {
			return ErrDrift
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.MkdirTemp(target.versions, ".version-*")
	if err != nil {
		return fmt.Errorf("create version directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	temporaryBinary := filepath.Join(temporary, filepath.Base(launcher.VersionPath(target.dataDir, digest)))
	if err := copyExecutable(ctx, source, temporaryBinary, 0o555, digest); err != nil {
		return err
	}
	if err := os.Rename(temporary, versionDirectory); err != nil {
		installedDigest, hashErr := launcher.FileSHA256(launcher.VersionPath(target.dataDir, digest))
		if hashErr == nil && installedDigest == digest {
			return nil
		}
		return fmt.Errorf("activate immutable version: %w", err)
	}
	return syncDirectory(target.versions)
}

func installLauncher(ctx context.Context, source, target, digest string) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".vgxness-launcher-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create launcher: %w", err)
	}
	temporaryPath := temporary.Name()
	_ = temporary.Close()
	_ = os.Remove(temporaryPath)
	if err := copyExecutable(ctx, source, temporaryPath, 0o755, digest); err != nil {
		_ = os.Remove(temporaryPath)
		return "", err
	}
	if err := os.Link(temporaryPath, target); err != nil {
		_ = os.Remove(temporaryPath)
		if errors.Is(err, os.ErrExist) {
			return "", ErrConflict
		}
		return "", fmt.Errorf("install launcher: %w", err)
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		removeIfSameFile(target, temporaryPath)
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("sync launcher directory: %w", err)
	}
	return temporaryPath, nil
}

func copyExecutable(ctx context.Context, source, target string, mode os.FileMode, digest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		_ = output.Close()
		if failed {
			_ = os.Remove(target)
		}
	}()
	if _, err := io.Copy(output, io.LimitReader(input, launcher.MaxBinarySize+1)); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := os.Chmod(target, mode); err != nil {
		return err
	}
	actual, err := launcher.FileSHA256(target)
	if err != nil || actual != digest {
		return ErrDrift
	}
	failed = false
	return nil
}

func writeManifest(ctx context.Context, path string, manifest launcher.Manifest, expected []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return ErrInvalid
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".vgxness-manifest-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if len(expected) == 0 {
		if err := os.Link(temporaryPath, path); err != nil {
			if errors.Is(err, os.ErrExist) {
				return ErrConflict
			}
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return ErrConflict
	}
	current, err := readRegular(path, 64<<10)
	if err != nil || !bytes.Equal(current, expected) {
		return ErrConflict
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, currentInfo) {
		return ErrConflict
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func prepareDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrDrift
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrDrift
	}
	return os.Chmod(path, 0o700)
}

func readRegular(path string, maximum int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maximum {
		return nil, ErrDrift
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, ErrDrift
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrDrift
	}
	return data, nil
}

func removeIfSameFile(target, expected string) {
	targetInfo, targetErr := os.Lstat(target)
	expectedInfo, expectedErr := os.Lstat(expected)
	if targetErr == nil && expectedErr == nil && os.SameFile(targetInfo, expectedInfo) {
		_ = os.Remove(target)
	}
}

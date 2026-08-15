package selfinstall

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	ErrInvalid        = errors.New("invalid self-install request")
	ErrConflict       = errors.New("self-install conflicts with existing content")
	ErrDrift          = errors.New("managed self-install has drifted")
	ErrNoRollback     = errors.New("no previous managed version is available")
	ErrRecovery       = errors.New("self-install recovery is required")
	ErrNoInstallation = errors.New("no managed self-install is available")
	ErrStaleGCPlan    = errors.New("self-install garbage collection plan is stale")
	ErrGCRecovery     = errors.Join(ErrRecovery, errors.New("self-install garbage collection recovery is required"))
)

type State string

const (
	StateAbsent          State = "absent"
	StateInstalled       State = "installed"
	StateDrifted         State = "drifted"
	StateRecoveryPending State = "recovery_pending"
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
	GCPreview(context.Context, Options) (GCResult, error)
	GCApply(context.Context, Options, string) (GCResult, error)
	GCRecover(context.Context, Options) (GCResult, error)
}

type Config struct {
	SourceExecutable          string
	Now                       func() time.Time
	afterManifestPublish      func() error        // package-test fault injection
	afterManifestMove         func() error        // package-test fault injection
	afterAnchorsOpen          func() error        // package-test fault injection
	afterGCJournal            func(gcState) error // package-test fault injection
	afterGCPreflight          func() error        // package-test synchronization
	afterGCDeleteOpened       func() error        // package-test synchronization
	afterGCSemanticValidation func() error        // package-test synchronization
	beforeGCRecoveryMutation  func() error        // package-test synchronization
	gcSync                    func(string, *os.Root) error
}

type Service struct {
	source                    string
	now                       func() time.Time
	afterManifestPublish      func() error
	afterManifestMove         func() error
	afterAnchorsOpen          func() error
	afterGCJournal            func(gcState) error
	afterGCPreflight          func() error
	afterGCDeleteOpened       func() error
	afterGCSemanticValidation func() error
	beforeGCRecoveryMutation  func() error
	gcSync                    func(string, *os.Root) error
}

type paths struct {
	binDir   string
	dataDir  string
	launcher string
	manifest string
	versions string
	lock     string
	recovery string
}

type inspection struct {
	result           Result
	paths            paths
	manifest         launcher.Manifest
	manifestRaw      []byte
	reusableLauncher bool
}

type installAnchors struct {
	bin  *os.Root
	data *os.Root
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "vgxness.exe"
	}
	return "vgxness"
}

func (anchors installAnchors) close() {
	if anchors.bin != nil {
		_ = anchors.bin.Close()
	}
	if anchors.data != nil {
		_ = anchors.data.Close()
	}
}

func openAnchors(target paths) (installAnchors, error) {
	bin, err := openInstallRoot(target.binDir, false)
	if err != nil {
		return installAnchors{}, fmt.Errorf("%w: open launcher anchor: %v", ErrDrift, err)
	}
	data, err := openInstallRoot(target.dataDir, false)
	if err != nil {
		_ = bin.Close()
		return installAnchors{}, fmt.Errorf("%w: open data anchor: %v", ErrDrift, err)
	}
	if err := requireRootDirectory(data, "versions", false); err != nil {
		_ = bin.Close()
		_ = data.Close()
		return installAnchors{}, err
	}
	return installAnchors{bin: bin, data: data}, nil
}

func prepareAnchors(target paths) (installAnchors, error) {
	bin, err := openInstallRoot(target.binDir, true)
	if err != nil {
		return installAnchors{}, fmt.Errorf("prepare launcher directory: %w", err)
	}
	data, err := openInstallRoot(target.dataDir, true)
	if err != nil {
		_ = bin.Close()
		return installAnchors{}, fmt.Errorf("prepare version data directory: %w", err)
	}
	if err := requireRootDirectory(data, "versions", true); err != nil {
		_ = bin.Close()
		_ = data.Close()
		return installAnchors{}, fmt.Errorf("prepare versions directory: %w", err)
	}
	return installAnchors{bin: bin, data: data}, nil
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
	return &Service{source: source, now: now, afterManifestPublish: config.afterManifestPublish, afterManifestMove: config.afterManifestMove, afterAnchorsOpen: config.afterAnchorsOpen, afterGCJournal: config.afterGCJournal, afterGCPreflight: config.afterGCPreflight, afterGCDeleteOpened: config.afterGCDeleteOpened, afterGCSemanticValidation: config.afterGCSemanticValidation, beforeGCRecoveryMutation: config.beforeGCRecoveryMutation, gcSync: config.gcSync}
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
	if err != nil && !errors.Is(err, ErrRecovery) {
		return Result{}, err
	}
	pendingRecovery := errors.Is(err, ErrRecovery)
	if !pendingRecovery && initial.result.State == StateDrifted {
		return Result{}, ErrConflict
	}
	if !pendingRecovery && initial.result.State == StateInstalled && !initial.result.UpdateAvailable {
		return initial.result, nil
	}
	anchors, err := prepareAnchors(initial.paths)
	if err != nil {
		return Result{}, err
	}
	defer anchors.close()
	if service.afterAnchorsOpen != nil {
		if err := service.afterAnchorsOpen(); err != nil {
			return Result{}, err
		}
	}
	lockFile, err := anchors.data.OpenFile(".install.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Result{}, err
	}
	lock, err := acquireFile(ctx, lockFile)
	if err != nil {
		return Result{}, err
	}
	defer lock.release()
	if result, err := recoverManifestRoot(anchors, initial.paths); err != nil {
		return result, err
	}
	current := initial
	if !anchorsStillNamed(anchors, initial.paths) {
		if pendingRecovery {
			return recoveryResult(initial.result, fmt.Errorf("%w: install roots moved while recovery was pending", ErrRecovery))
		}
		return Result{}, ErrDrift
	}
	current, err = service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	if !anchorsStillNamed(anchors, initial.paths) {
		return Result{}, ErrDrift
	}
	if current.result.State == StateDrifted {
		return Result{}, ErrConflict
	}
	if current.result.State == StateInstalled && !current.result.UpdateAvailable {
		return current.result, nil
	}
	if err := installVersionRoot(ctx, service.source, anchors.data, current.result.SourceSHA256); err != nil {
		return Result{}, err
	}
	launcherTemporary := ""
	if current.result.State == StateAbsent && !current.reusableLauncher {
		launcherTemporary, err = installLauncherRoot(ctx, service.source, anchors.bin, executableName(), current.result.SourceSHA256)
		if err != nil {
			return Result{}, err
		}
		defer anchors.bin.Remove(launcherTemporary)
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
	if !anchorsStillNamed(anchors, current.paths) {
		return Result{}, ErrDrift
	}
	if err := service.writeManifestRoot(ctx, anchors, current.paths, manifest, current.manifestRaw); err != nil {
		return recoveryResult(current.result, err)
	}
	result := current.result
	result.State, result.ActiveSHA256, result.PreviousSHA256, result.RollbackAvailable, result.UpdateAvailable, result.Changed = StateInstalled, current.result.SourceSHA256, previous, previous != "", false, true
	if !anchorsStillNamed(anchors, current.paths) {
		result.State = StateDrifted
		return result, ErrDrift
	}
	return result, nil
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
	anchors, err := openAnchors(initial.paths)
	if err != nil {
		return Result{}, err
	}
	defer anchors.close()
	lockFile, err := anchors.data.OpenFile(".install.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Result{}, err
	}
	lock, err := acquireFile(ctx, lockFile)
	if err != nil {
		return Result{}, err
	}
	defer lock.release()
	current := initial
	if result, err := recoverManifestRoot(anchors, initial.paths); err != nil {
		return result, err
	}
	if !anchorsStillNamed(anchors, initial.paths) {
		return Result{}, ErrDrift
	}
	current, err = service.inspect(ctx, options)
	if err != nil {
		return Result{}, err
	}
	if !anchorsStillNamed(anchors, initial.paths) {
		return Result{}, ErrDrift
	}
	if current.result.State != StateInstalled || current.result.PreviousSHA256 == "" {
		return Result{}, ErrNoRollback
	}
	previousPath := launcher.VersionPath(current.paths.dataDir, current.result.PreviousSHA256)
	previousDigest, err := fileSHA256Root(anchors.data, filepath.Join("versions", current.result.PreviousSHA256, executableName()))
	if err != nil || previousDigest != current.result.PreviousSHA256 {
		return Result{}, ErrDrift
	}
	manifest := current.manifest
	manifest.ActivePath = previousPath
	manifest.ActiveSHA256 = previousDigest
	manifest.PreviousSHA256 = ""
	manifest.UpdatedAt = service.now().UTC().Format(time.RFC3339Nano)
	if !anchorsStillNamed(anchors, current.paths) {
		return Result{}, ErrDrift
	}
	if err := service.writeManifestRoot(ctx, anchors, current.paths, manifest, current.manifestRaw); err != nil {
		return recoveryResult(current.result, err)
	}
	result := current.result
	result.ActiveSHA256, result.PreviousSHA256, result.RollbackAvailable, result.UpdateAvailable, result.Changed = previousDigest, "", false, false, true
	if !anchorsStillNamed(anchors, current.paths) {
		result.State = StateDrifted
		return result, ErrDrift
	}
	return result, nil
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
	if _, err := os.Lstat(resolved.recovery); err == nil {
		state.result.State = StateRecoveryPending
		return state, ErrRecovery
	} else if !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
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
		lock:     filepath.Join(filepath.Clean(dataDir), ".install.lock"),
		recovery: filepath.Join(filepath.Clean(dataDir), ".manifest-recovery.json"),
	}, nil
}

type manifestRecovery struct {
	Manifest  string `json:"manifest"`
	Expected  []byte `json:"expected"`
	Published []byte `json:"published"`
	Backup    string `json:"backup,omitempty"`
}

func recoveryResult(result Result, err error) (Result, error) {
	if !errors.Is(err, ErrRecovery) {
		return Result{}, err
	}
	result.State = StateRecoveryPending
	result.Changed = true
	return result, err
}

// openInstallRoot walks from the filesystem root one component at a time. Each
// component is checked before opening and then identity-checked against the
// opened child, closing the check/use window that MkdirAll would leave around
// caller-controlled symlink ancestors.
func openInstallRoot(path string, create bool) (*os.Root, error) {
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "darwin" && (clean == "/var" || strings.HasPrefix(clean, "/var/")) {
		clean = "/private" + clean
	}
	volumeRoot := filepath.VolumeName(clean) + string(os.PathSeparator)
	relative, err := filepath.Rel(volumeRoot, clean)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, ErrInvalid
	}
	root, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return root, nil
	}
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		before, statErr := root.Lstat(component)
		created := false
		if errors.Is(statErr, os.ErrNotExist) && create {
			if err := root.Mkdir(component, 0o700); err != nil {
				_ = root.Close()
				return nil, err
			}
			created = true
			before, statErr = root.Lstat(component)
		}
		if statErr != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			_ = root.Close()
			return nil, ErrDrift
		}
		child, err := root.OpenRoot(component)
		if err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("%w: open install ancestor: %v", ErrDrift, err)
		}
		after, err := child.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			_ = child.Close()
			_ = root.Close()
			return nil, ErrDrift
		}
		if created {
			if err := child.Chmod(".", 0o700); err != nil {
				_ = child.Close()
				_ = root.Close()
				return nil, err
			}
		}
		_ = root.Close()
		root = child
	}
	return root, nil
}

func requireRootDirectory(root *os.Root, name string, create bool) error {
	before, err := root.Lstat(name)
	created := false
	if errors.Is(err, os.ErrNotExist) && create {
		if err := root.Mkdir(name, 0o700); err != nil {
			return err
		}
		created = true
		before, err = root.Lstat(name)
	}
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return ErrDrift
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return ErrDrift
	}
	defer child.Close()
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return ErrDrift
	}
	if created {
		return child.Chmod(".", 0o700)
	}
	return nil
}

func anchorsStillNamed(anchors installAnchors, target paths) bool {
	binPath, binErr := os.Stat(target.binDir)
	binRoot, binRootErr := anchors.bin.Stat(".")
	dataPath, dataErr := os.Stat(target.dataDir)
	dataRoot, dataRootErr := anchors.data.Stat(".")
	return binErr == nil && binRootErr == nil && dataErr == nil && dataRootErr == nil &&
		os.SameFile(binPath, binRoot) && os.SameFile(dataPath, dataRoot)
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

func rootTemporaryName(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(bytes), nil
}

func readRegularRoot(root *os.Root, name string, maximum int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return nil, ErrDrift
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) {
		return nil, ErrDrift
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrDrift
	}
	return data, nil
}

func fileSHA256Root(root *os.Root, name string) (string, error) {
	data, err := readRegularRoot(root, name, launcher.MaxBinarySize)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func copyExecutableRoot(ctx context.Context, source string, root *os.Root, name string, mode os.FileMode, digest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		_ = output.Close()
		if failed {
			_ = root.Remove(name)
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
	if err := root.Chmod(name, mode); err != nil {
		return err
	}
	actual, err := fileSHA256Root(root, name)
	if err != nil || actual != digest {
		return ErrDrift
	}
	failed = false
	return nil
}

func installVersionRoot(ctx context.Context, source string, root *os.Root, digest string) error {
	directory := filepath.Join("versions", digest)
	binary := filepath.Join(directory, executableName())
	if info, err := root.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrDrift
		}
		installed, hashErr := fileSHA256Root(root, binary)
		if hashErr != nil || installed != digest {
			return ErrDrift
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := rootTemporaryName(".version-")
	if err != nil {
		return err
	}
	if err := root.Mkdir(temporary, 0o700); err != nil {
		return err
	}
	defer root.RemoveAll(temporary)
	temporaryRoot, err := root.OpenRoot(temporary)
	if err != nil {
		return err
	}
	defer temporaryRoot.Close()
	if err := copyExecutableRoot(ctx, source, temporaryRoot, executableName(), 0o555, digest); err != nil {
		return err
	}
	if err := publishRootDirectoryNoReplace(root, temporary, directory); err != nil {
		installed, hashErr := fileSHA256Root(root, binary)
		if hashErr == nil && installed == digest {
			return nil
		}
		return fmt.Errorf("activate immutable version: %w", err)
	}
	return syncRoot(root)
}

func installLauncherRoot(ctx context.Context, source string, root *os.Root, target, digest string) (string, error) {
	temporary, err := rootTemporaryName(".vgxness-launcher-")
	if err != nil {
		return "", err
	}
	if err := copyExecutableRoot(ctx, source, root, temporary, 0o755, digest); err != nil {
		return "", err
	}
	if err := root.Link(temporary, target); err != nil {
		_ = root.Remove(temporary)
		if errors.Is(err, os.ErrExist) {
			return "", ErrConflict
		}
		return "", fmt.Errorf("install launcher: %w", err)
	}
	if err := syncRoot(root); err != nil {
		removeRootIfSameFile(root, target, temporary)
		_ = root.Remove(temporary)
		return "", fmt.Errorf("sync launcher directory: %w", err)
	}
	return temporary, nil
}

func removeRootIfSameFile(root *os.Root, target, expected string) {
	targetInfo, targetErr := root.Lstat(target)
	expectedInfo, expectedErr := root.Lstat(expected)
	if targetErr == nil && expectedErr == nil && os.SameFile(targetInfo, expectedInfo) {
		_ = root.Remove(target)
	}
}

func writeRootFile(root *os.Root, name string, data []byte, mode os.FileMode) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := root.Chmod(name, mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func writeRecoveryRoot(root *os.Root, recovery manifestRecovery) error {
	data, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	temporary, err := rootTemporaryName(".manifest-recovery-")
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if err := writeRootFile(root, temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := root.Link(temporary, ".manifest-recovery.json"); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: recovery evidence already exists", ErrRecovery)
		}
		return err
	}
	return syncRoot(root)
}

func (service *Service) writeManifestRoot(ctx context.Context, anchors installAnchors, target paths, manifest launcher.Manifest, expected []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return ErrInvalid
	}
	data = append(data, '\n')
	temporary, err := rootTemporaryName(".vgxness-manifest-")
	if err != nil {
		return err
	}
	defer anchors.bin.Remove(temporary)
	if err := writeRootFile(anchors.bin, temporary, data, 0o600); err != nil {
		return err
	}
	recovery := manifestRecovery{Manifest: target.manifest, Expected: expected, Published: data}
	if len(expected) != 0 {
		recovery.Backup, err = rootTemporaryName(".vgxness-manifest-backup-")
		if err != nil {
			return err
		}
	}
	if err := writeRecoveryRoot(anchors.data, recovery); err != nil {
		return fmt.Errorf("%w: record manifest publication: %v", ErrRecovery, err)
	}
	manifestName := filepath.Base(target.manifest)
	if len(expected) == 0 {
		if err := anchors.bin.Link(temporary, manifestName); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%w: manifest appeared during publication", ErrRecovery)
			}
			return fmt.Errorf("%w: publish manifest: %v", ErrRecovery, err)
		}
		return service.finishManifestPublishRoot(ctx, anchors, target)
	}
	before, err := anchors.bin.Lstat(manifestName)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return errors.Join(ErrRecovery, ErrConflict)
	}
	current, err := readRegularRoot(anchors.bin, manifestName, 64<<10)
	if err != nil || !bytes.Equal(current, expected) {
		return errors.Join(ErrRecovery, ErrConflict)
	}
	currentInfo, err := anchors.bin.Lstat(manifestName)
	if err != nil || !os.SameFile(before, currentInfo) {
		return errors.Join(ErrRecovery, ErrConflict)
	}
	if err := anchors.bin.Rename(manifestName, recovery.Backup); err != nil {
		return fmt.Errorf("%w: retain manifest predecessor: %v", ErrRecovery, err)
	}
	if service.afterManifestMove != nil {
		if err := service.afterManifestMove(); err != nil {
			return fmt.Errorf("%w: manifest predecessor moved: %v", ErrRecovery, err)
		}
	}
	if err := anchors.bin.Link(temporary, manifestName); err != nil {
		return fmt.Errorf("%w: publish manifest without overwrite: %v", ErrRecovery, err)
	}
	return service.finishManifestPublishRoot(ctx, anchors, target)
}

func (service *Service) finishManifestPublishRoot(ctx context.Context, anchors installAnchors, target paths) error {
	if service.afterManifestPublish != nil {
		if err := service.afterManifestPublish(); err != nil {
			return fmt.Errorf("%w: manifest published; durability unknown: %v", ErrRecovery, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrRecovery, err)
	}
	if err := syncRoot(anchors.bin); err != nil {
		return fmt.Errorf("%w: manifest published; sync failed: %v", ErrRecovery, err)
	}
	_, err := recoverManifestRoot(anchors, target)
	return err
}

func recoverManifestRoot(anchors installAnchors, target paths) (Result, error) {
	data, err := readRegularRoot(anchors.data, ".manifest-recovery.json", 256<<10)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, nil
	}
	result := Result{State: StateRecoveryPending, LauncherPath: target.launcher, ManifestPath: target.manifest, DataDir: target.dataDir, Changed: true}
	if err != nil {
		return result, fmt.Errorf("%w: read recovery evidence: %v", ErrRecovery, err)
	}
	var recovery manifestRecovery
	if err := json.Unmarshal(data, &recovery); err != nil || recovery.Manifest != target.manifest || len(recovery.Published) == 0 {
		return result, fmt.Errorf("%w: invalid recovery evidence", ErrRecovery)
	}
	backup := recovery.Backup
	if backup != "" && filepath.IsAbs(backup) {
		if filepath.Dir(filepath.Clean(backup)) != filepath.Dir(target.manifest) {
			return result, fmt.Errorf("%w: invalid recovery evidence", ErrRecovery)
		}
		backup = filepath.Base(backup)
	}
	if backup != "" && filepath.Base(backup) != backup {
		return result, fmt.Errorf("%w: invalid recovery evidence", ErrRecovery)
	}
	if backup != "" && !validRecoveryBackup(backup) {
		return result, fmt.Errorf("%w: invalid recovery evidence", ErrRecovery)
	}
	manifestName := filepath.Base(target.manifest)
	current, err := readRegularRoot(anchors.bin, manifestName, 64<<10)
	if errors.Is(err, os.ErrNotExist) && backup != "" {
		previous, backupErr := readRegularRoot(anchors.bin, backup, 64<<10)
		if backupErr != nil || !bytes.Equal(previous, recovery.Expected) {
			return result, fmt.Errorf("%w: manifest predecessor retained at %q", ErrRecovery, recovery.Backup)
		}
		if linkErr := anchors.bin.Link(backup, manifestName); linkErr != nil {
			return result, fmt.Errorf("%w: restore manifest predecessor without overwrite: %v", ErrRecovery, linkErr)
		}
		if syncErr := syncRoot(anchors.bin); syncErr != nil {
			return result, fmt.Errorf("%w: sync restored manifest predecessor: %v", ErrRecovery, syncErr)
		}
		current, err = previous, nil
	}
	if err != nil || (!bytes.Equal(current, recovery.Published) && !bytes.Equal(current, recovery.Expected)) {
		return result, fmt.Errorf("%w: manifest changed while publication was pending", ErrRecovery)
	}
	if backup != "" {
		previous, err := readRegularRoot(anchors.bin, backup, 64<<10)
		if err == nil && !bytes.Equal(previous, recovery.Expected) {
			return result, fmt.Errorf("%w: manifest predecessor retained at %q", ErrRecovery, recovery.Backup)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("%w: inspect manifest predecessor at %q: %v", ErrRecovery, recovery.Backup, err)
		}
		if err == nil {
			if err := anchors.bin.Remove(backup); err != nil {
				return result, fmt.Errorf("%w: retain predecessor cleanup: %v", ErrRecovery, err)
			}
		}
		if err := syncRoot(anchors.bin); err != nil {
			return result, fmt.Errorf("%w: retain predecessor cleanup: %v", ErrRecovery, err)
		}
	}
	if err := anchors.data.Remove(".manifest-recovery.json"); err != nil {
		return result, fmt.Errorf("%w: remove recovery evidence: %v", ErrRecovery, err)
	}
	if err := syncRoot(anchors.data); err != nil {
		return result, fmt.Errorf("%w: sync recovery cleanup: %v", ErrRecovery, err)
	}
	return Result{}, nil
}

func validRecoveryBackup(name string) bool {
	const prefix = ".vgxness-manifest-backup-"
	suffix := strings.TrimPrefix(name, prefix)
	if suffix == name || len(suffix) != 24 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}

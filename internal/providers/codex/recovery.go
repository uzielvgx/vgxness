package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/opencodebackup"
)

// RecoveryOptions identifies the Codex configuration and its private backup root.
type RecoveryOptions struct {
	Integration integration.Options
	BackupRoot  string
	HomeDir     string
}

// Recovery is the provider-scoped recovery API. It deliberately returns only a
// snapshot identity, never a snapshot directory.
type Recovery struct {
	engine       *opencodebackup.Engine
	sourceRoot   string
	sourceInfo   os.FileInfo
	managedPaths []string
}

type Snapshot struct {
	ID     string
	source integration.SourceIdentity
}

func (s Snapshot) Source() integration.SourceIdentity { return s.source }

type sourceIdentity struct{ info os.FileInfo }

func (sourceIdentity) SourceIdentity() {}

// Source returns the root identity pinned while the managed source was
// inspected. It is intentionally opaque outside this provider.
func (r *Recovery) Source() integration.SourceIdentity { return sourceIdentity{info: r.sourceInfo} }

func NewRecovery(ctx context.Context, options RecoveryOptions) (*Recovery, error) {
	layout, err := NewIntegration().ManagedLayout(ctx, options.Integration)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(layout.Artifacts))
	for _, artifact := range layout.Artifacts {
		paths = append(paths, filepath.ToSlash(artifact.RelativePath))
	}
	sort.Strings(paths)
	backupRoot := options.BackupRoot
	if backupRoot == "" {
		home := options.HomeDir
		if home == "" {
			home = options.Integration.HomeDir
		}
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				return nil, err
			}
		}
		backupRoot = filepath.Join(home, ".local", "share", "vgxness", "backups", "codex")
	}
	sourceInfo, err := sourceRootIdentity(layout.Root)
	if err != nil {
		return nil, err
	}
	engine, err := opencodebackup.New(opencodebackup.Options{SourceRoot: layout.Root, BackupRoot: backupRoot, ManagedPaths: paths})
	if err != nil {
		return nil, err
	}
	if err := verifySourceRoot(layout.Root, sourceInfo); err != nil {
		return nil, err
	}
	return &Recovery{engine: engine, sourceRoot: layout.Root, sourceInfo: sourceInfo, managedPaths: paths}, nil
}

func (r *Recovery) List(ctx context.Context) ([]opencodebackup.Summary, error) {
	return r.engine.List(ctx)
}

func (r *Recovery) Create(ctx context.Context, mode opencodebackup.Mode) (Snapshot, error) {
	snapshot, err := r.engine.Create(ctx, mode)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: snapshot.Manifest.SnapshotID}, nil
}

func (r *Recovery) Verify(ctx context.Context, id string) (Snapshot, error) {
	snapshot, err := r.engine.Verify(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Manifest.SnapshotID != id {
		return Snapshot{}, fmt.Errorf("verify Codex snapshot identity")
	}
	if err := verifySourceRoot(r.sourceRoot, r.sourceInfo); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: id, source: r.Source()}, nil
}

func (r *Recovery) PreviewRestore(ctx context.Context, id string) (opencodebackup.RestorePreview, error) {
	return r.engine.PreviewRestore(ctx, id)
}

func (r *Recovery) Restore(ctx context.Context, request opencodebackup.RestoreRequest) (opencodebackup.RestoreResult, error) {
	return r.engine.Restore(ctx, request)
}

// ManagedPaths returns a defensive copy of the immutable provider contract.
func (r *Recovery) ManagedPaths() []string { return append([]string(nil), r.managedPaths...) }

// HasManagedFiles examines only managed paths. Missing entries are harmless;
// symlinks and unsafe types in a managed path fail closed.
func (r *Recovery) HasManagedFiles(ctx context.Context) (found bool, returnErr error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	root, err := os.OpenRoot(r.sourceRoot)
	if os.IsNotExist(err) {
		return false, fmt.Errorf("source root missing")
	}
	if err != nil {
		return false, err
	}
	defer func() {
		if err := root.Close(); err != nil && returnErr == nil {
			returnErr = err
		}
	}()
	if err := r.verifyHeldRoot(root); err != nil {
		return false, err
	}
	for _, relative := range r.managedPaths {
		components := strings.Split(relative, "/")
		for index := range components {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			info, err := root.Lstat(strings.Join(components[:index+1], "/"))
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return false, err
			}
			if info.Mode()&os.ModeSymlink != 0 || (index < len(components)-1 && !info.IsDir()) || (index == len(components)-1 && !info.Mode().IsRegular()) {
				return false, fmt.Errorf("unsafe managed path %q", relative)
			}
			if index == len(components)-1 {
				found = true
			}
		}
	}
	if err := r.verifyHeldRoot(root); err != nil {
		return false, err
	}
	return found, nil
}

func sourceRootIdentity(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("unsafe source root")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	held, statErr := root.Lstat(".")
	closeErr := root.Close()
	if statErr != nil {
		return nil, statErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if !os.SameFile(info, held) {
		return nil, fmt.Errorf("source root changed while opening")
	}
	return info, nil
}

func verifySourceRoot(path string, want os.FileInfo) error {
	info, err := sourceRootIdentity(path)
	if err != nil {
		return err
	}
	if !os.SameFile(want, info) {
		return fmt.Errorf("source root replaced")
	}
	return nil
}

func (r *Recovery) verifyHeldRoot(root *os.Root) error {
	held, err := root.Lstat(".")
	if err != nil {
		return err
	}
	path, err := os.Lstat(r.sourceRoot)
	if err != nil {
		return err
	}
	if path.Mode()&os.ModeSymlink != 0 || !path.IsDir() || !os.SameFile(r.sourceInfo, held) || !os.SameFile(r.sourceInfo, path) {
		return fmt.Errorf("source root replaced")
	}
	return nil
}

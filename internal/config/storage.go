package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrInvalid = errors.New("invalid")

type Options struct {
	StorageRoot  string
	ProjectDir   string
	ProjectLocal bool
	HomeDir      string
}

type Paths struct {
	Root           string
	Database       string
	LegacyDatabase string
}

func Prepare(ctx context.Context, opts Options) (Paths, error) {
	if err := ctx.Err(); err != nil {
		return Paths{}, err
	}
	root, err := resolve(opts)
	if err != nil {
		return Paths{}, err
	}
	if err := rejectSymlinkPath(root); err != nil {
		return Paths{}, err
	}
	root, err = canonicalizeSystemPath(root)
	if err != nil {
		return Paths{}, err
	}
	_, statErr := os.Stat(root)
	createdPath := ""
	if os.IsNotExist(statErr) {
		createdPath = firstMissing(root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		if createdPath != "" {
			cleanupCreatedRoot(root)
		}
		return Paths{}, fmt.Errorf("prepare storage root: %w", err)
	}
	if err := secureDirectory(root); err != nil {
		if createdPath != "" {
			cleanupCreatedRoot(root)
		}
		return Paths{}, err
	}
	if err := ctx.Err(); err != nil {
		if createdPath != "" {
			cleanupCreatedRoot(root)
		}
		return Paths{}, err
	}
	paths, err := pathsForRoot(opts, root)
	if err != nil {
		return Paths{}, err
	}
	if err := secureDirectory(filepath.Dir(paths.Database)); err != nil {
		return Paths{}, err
	}
	if err := rejectSymlinkPath(paths.Database); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

func PathsFor(opts Options) (Paths, error) {
	root, err := resolve(opts)
	if err != nil {
		return Paths{}, err
	}
	return pathsForRoot(opts, root)
}

func pathsForRoot(opts Options, root string) (Paths, error) {
	database := filepath.Join(root, "memory.db")
	legacy := ""
	if opts.StorageRoot == "" && !opts.ProjectLocal {
		home := opts.HomeDir
		var err error
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				return Paths{}, fmt.Errorf("resolve home: %w", err)
			}
		}
		database = filepath.Join(filepath.Dir(filepath.Dir(root)), "memory.db")
		if database != filepath.Join(root, "memory.db") {
			legacy = filepath.Join(root, "memory.db")
		}
	}
	return Paths{
		Root:           root,
		Database:       database,
		LegacyDatabase: legacy,
	}, nil
}

func cleanupCreatedRoot(root string) { _ = os.Remove(root) }

func secureDirectory(path string) error {
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("prepare storage root: %w", ErrInvalid)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure storage root: %w", err)
	}
	return nil
}

func rejectSymlinkPath(path string) error {
	for candidate := filepath.Clean(path); ; candidate = filepath.Dir(candidate) {
		info, err := os.Lstat(candidate)
		if err == nil && info.Mode()&os.ModeSymlink != 0 && !isSystemPathSymlink(candidate) {
			return fmt.Errorf("%w: storage path must not traverse symlinks", ErrInvalid)
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect storage path: %w", err)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return nil
		}
	}
}

// isSystemPathSymlink permits only macOS's documented /var compatibility
// symlink. All other symlinks in a configured storage path are rejected.
func isSystemPathSymlink(path string) bool {
	if runtime.GOOS != "darwin" || path != "/var" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && resolved == "/private/var"
}

func canonicalizeSystemPath(path string) (string, error) {
	clean := CanonicalizeExistingPathCase(path)
	ancestor := clean
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect storage path: %w", err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	canonical, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("resolve storage path: %w", err)
	}
	relative, err := filepath.Rel(ancestor, clean)
	if err != nil {
		return "", fmt.Errorf("resolve storage path: %w", err)
	}
	return filepath.Join(canonical, relative), nil
}

// CanonicalizeExistingPathCase preserves the spelling recorded by a
// case-insensitive filesystem. It only adjusts paths that the filesystem
// already resolves, so distinct directories on case-sensitive filesystems
// remain distinct.
func CanonicalizeExistingPathCase(path string) string {
	clean := filepath.Clean(path)
	if _, err := os.Stat(clean); err != nil {
		return clean
	}
	root := filepath.VolumeName(clean) + string(filepath.Separator)
	current := root
	for _, component := range strings.Split(strings.TrimPrefix(clean, root), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return clean
		}
		actual := component
		for _, entry := range entries {
			if entry.Name() == component {
				actual = entry.Name()
				break
			}
			if strings.EqualFold(entry.Name(), component) {
				actual = entry.Name()
			}
		}
		current = filepath.Join(current, actual)
	}
	return current
}

func firstMissing(path string) string {
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		return firstMissing(parent)
	}
	return path
}

func resolve(opts Options) (string, error) {
	if opts.StorageRoot != "" {
		if !filepath.IsAbs(opts.StorageRoot) {
			return "", fmt.Errorf("%w: storage root must be absolute", ErrInvalid)
		}
		return filepath.Clean(opts.StorageRoot), nil
	}
	project := opts.ProjectDir
	var err error
	if project == "" {
		project, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve project: %w", err)
		}
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	project = CanonicalizeExistingPathCase(project)
	if opts.ProjectLocal {
		root := filepath.Join(project, ".vgxness")
		if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: project-local storage root must not be a symlink", ErrInvalid)
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve project-local storage: %w", err)
		}
		return root, nil
	}
	home := opts.HomeDir
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
	}
	digest := sha256.Sum256([]byte(project))
	return filepath.Join(home, ".vgxness", "projects", fmt.Sprintf("%s-%x", filepath.Base(project), digest[:6])), nil
}

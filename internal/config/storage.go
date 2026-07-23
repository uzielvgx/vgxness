package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	CurrentRun     string
}

func Prepare(ctx context.Context, opts Options) (Paths, error) {
	if err := ctx.Err(); err != nil {
		return Paths{}, err
	}
	root, err := resolve(opts)
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
	if err := ctx.Err(); err != nil {
		if createdPath != "" {
			cleanupCreatedRoot(root)
		}
		return Paths{}, err
	}
	return pathsForRoot(opts, root)
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
		database = filepath.Join(filepath.Clean(home), ".vgxness", "memory.db")
		if database != filepath.Join(root, "memory.db") {
			legacy = filepath.Join(root, "memory.db")
		}
	}
	return Paths{
		Root:           root,
		Database:       database,
		LegacyDatabase: legacy,
		CurrentRun:     filepath.Join(root, "current-run.json"),
	}, nil
}

func cleanupCreatedRoot(root string) { _ = os.Remove(root) }

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

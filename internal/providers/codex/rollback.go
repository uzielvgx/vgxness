package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/vgxness/vgxness/internal/integration"
	"os"
	"path"
	"path/filepath"
	"time"
)

// restoreTimeout bounds reinstalling the previous package after a failed
// replacement, including when the caller's context was cancelled.
const restoreTimeout = 30 * time.Second

// retainPackage keeps only the predecessor actually observed on this machine.
// It contains no compiled historical policy. Repeated attempts never overwrite
// a changed backup or source; the root's anchored filesystem checks still apply.
func retainPackage(ctx context.Context, root *Root, pkg Package) (string, error) {
	base := "vgxness/rollback/" + pkg.SHA256
	for _, a := range pkg.Artifacts {
		body, _, err := root.Read(a.Path, maxArtifactBytes)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} // An already partial installation.
		if err != nil || !bytes.Equal(body, a.Bytes) {
			return "", errors.Join(integration.ErrDrift, err)
		}
		name := base + "/" + a.Path
		saved, _, readErr := root.Read(name, maxArtifactBytes)
		if readErr == nil {
			if !bytes.Equal(saved, body) {
				return "", integration.ErrDrift
			}
			continue
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return "", readErr
		}
		if err := root.Mkdir(path.Dir(name)); err != nil {
			return "", err
		}
		anchor, err := root.Publish(ctx, name, body)
		if err != nil {
			return "", err
		}
		if err = root.CommitAnchor(anchor); err != nil {
			return "", err
		}
	}
	return filepath.Join(root.Path, filepath.FromSlash(base)), nil
}
func (s *Integration) replacePackage(ctx context.Context, root *Root, previous, current Package, state inspection) (integration.Result, error) {
	backup, err := retainPackage(ctx, root, previous)
	if err != nil {
		return integration.Result{}, err
	}
	fail := func(err error) (integration.Result, error) {
		return integration.Result{BackupPath: backup}, fmt.Errorf("%w; previous Codex files retained at %q", err, backup)
	}
	if _, err = s.uninstall(ctx, root, previous, state); err != nil {
		return fail(err)
	}
	if err := s.check("previous-removed", previous.SHA256); err != nil {
		return fail(s.restorePrevious(ctx, root, previous, err))
	}
	next, err := inspectRoot(ctx, root, current)
	if err != nil {
		return fail(s.restorePrevious(ctx, root, previous, err))
	}
	result, err := s.installAndActivate(ctx, root, current, next)
	if err != nil {
		return fail(s.restorePrevious(ctx, root, previous, err))
	}
	result.BackupPath = backup
	return result, nil
}

// restorePrevious reinstalls the package that replacePackage just removed when
// the replacement failed without leaving pending evidence (for example when it
// was cancelled before its first write). Without it the root is left absent
// with nothing for Reinstall to recover. Pending evidence means the ordinary
// recovery path owns the root, so it is left untouched.
func (s *Integration) restorePrevious(ctx context.Context, root *Root, previous Package, cause error) error {
	pending, err := root.Pending()
	if err != nil {
		return errors.Join(cause, err)
	}
	if pending {
		return cause
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restoreTimeout)
	defer cancel()
	state, err := inspectRoot(restoreCtx, root, previous)
	if err == nil {
		_, err = s.installAndActivate(restoreCtx, root, previous, state)
	}
	if err != nil {
		return errors.Join(cause, integration.ErrRecovery, fmt.Errorf("restore previous Codex package: %w", err))
	}
	return fmt.Errorf("%w; previous Codex package restored", cause)
}

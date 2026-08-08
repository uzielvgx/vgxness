package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"

	"github.com/vgxness/vgxness/internal/integration"
)

const maxArtifactBytes = 512 << 10

type Integration struct {
	open       func(context.Context, integration.Options, bool) (*Root, error)
	checkpoint func(string, string) error
}
type inspectedArtifact struct {
	artifact Artifact
	info     os.FileInfo
	exact    bool
	present  bool
}
type inspection struct {
	result    integration.Result
	artifacts []inspectedArtifact
}

func NewIntegration() *Integration { return &Integration{} }
func (s *Integration) openRoot(ctx context.Context, options integration.Options, create bool) (*Root, error) {
	if s != nil && s.open != nil {
		return s.open(ctx, options, create)
	}
	return OpenRoot(ctx, options, create)
}
func (s *Integration) check(point, name string) error {
	if s != nil && s.checkpoint != nil {
		return s.checkpoint(point, name)
	}
	return nil
}

var _ integration.ManagedRuntime = (*Integration)(nil)

func codexPackage() (Package, error) { return Render("v0.0.0") }
func resultFor(root string, pkg Package) integration.Result {
	durability := "fsync"
	if runtime.GOOS == "windows" {
		durability = "file-sync-namespace-best-effort"
	}
	return integration.Result{Provider: "codex", Path: root, ArtifactSHA256: pkg.SHA256, ArtifactCount: len(pkg.Artifacts), DirectoryDurability: durability}
}
func inspectRoot(ctx context.Context, root *Root, pkg Package) (inspection, error) {
	state := inspection{result: resultFor(root.Path, pkg), artifacts: make([]inspectedArtifact, 0, len(pkg.Artifacts))}
	exact, absent, drifted := 0, 0, false
	if info, err := root.fs.Lstat("agents"); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			drifted = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		drifted = true
	}
	for _, artifact := range pkg.Artifacts {
		if err := ctx.Err(); err != nil {
			return inspection{}, err
		}
		body, info, err := root.Read(artifact.Path, maxArtifactBytes)
		item := inspectedArtifact{artifact: artifact, info: info}
		switch {
		case errors.Is(err, os.ErrNotExist):
			absent++
		case err != nil:
			drifted = true
		default:
			item.present = true
			item.exact = string(body) == string(artifact.Bytes)
			if item.exact {
				exact++
			} else {
				drifted = true
			}
		}
		state.artifacts = append(state.artifacts, item)
	}
	switch {
	case drifted:
		state.result.State = integration.StateDrifted
	case exact == len(pkg.Artifacts):
		state.result.State = integration.StateInstalled
	case absent == len(pkg.Artifacts):
		state.result.State = integration.StateAbsent
	default:
		state.result.State = integration.StatePartial
	}
	return state, nil
}
func (s *Integration) inspect(ctx context.Context, options integration.Options) (inspection, error) {
	pkg, err := codexPackage()
	if err != nil {
		return inspection{}, err
	}
	path, err := rootPath(options)
	if err != nil {
		return inspection{}, err
	}
	root, err := s.openRoot(ctx, options, false)
	if errors.Is(err, os.ErrNotExist) {
		return inspection{result: func() integration.Result { r := resultFor(path, pkg); r.State = integration.StateAbsent; return r }()}, nil
	}
	if err != nil {
		return inspection{}, err
	}
	defer root.Close()
	return inspectRoot(ctx, root, pkg)
}
func (s *Integration) Preview(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := s.inspect(ctx, options)
	if err != nil {
		return integration.Result{}, err
	}
	state.result.Changed = state.result.State == integration.StateAbsent || state.result.State == integration.StatePartial
	state.result.RestartRequired = state.result.Changed
	return state.result, nil
}
func (s *Integration) Status(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := s.inspect(ctx, options)
	return state.result, err
}
func (s *Integration) ManagedLayout(ctx context.Context, options integration.Options) (integration.ManagedLayout, error) {
	if err := ctx.Err(); err != nil {
		return integration.ManagedLayout{}, err
	}
	pkg, err := codexPackage()
	if err != nil {
		return integration.ManagedLayout{}, err
	}
	root, err := rootPath(options)
	if err != nil {
		return integration.ManagedLayout{}, err
	}
	items := make([]integration.ManagedArtifact, 0, len(pkg.Artifacts))
	for _, artifact := range pkg.Artifacts {
		sum := sha256.Sum256(artifact.Bytes)
		items = append(items, integration.ManagedArtifact{RelativePath: artifact.Path, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RelativePath < items[j].RelativePath })
	h := sha256.New()
	for _, item := range items {
		_, _ = io.WriteString(h, item.RelativePath)
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, item.SHA256)
		_, _ = h.Write([]byte{'\n'})
	}
	return integration.ManagedLayout{Root: root, Artifacts: items, AggregateSHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func pending(root *Root, pkg Package) (bool, error) {
	present, err := root.Pending()
	if err != nil || present {
		return present, err
	}
	for _, item := range pkg.Artifacts {
		for _, suffix := range []string{".vgxness-stage", ".vgxness-remove"} {
			_, err := root.fs.Lstat(item.Path + suffix)
			if err == nil {
				return true, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return false, recovery(err)
			}
		}
	}
	return false, nil
}
func (s *Integration) ReinstallPending(ctx context.Context, options integration.Options) (bool, error) {
	pkg, err := codexPackage()
	if err != nil {
		return false, err
	}
	root, err := s.openRoot(ctx, options, false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer root.Close()
	return pending(root, pkg)
}
func ensureArtifactDirs(root *Root, artifacts []inspectedArtifact) error {
	seen := map[string]bool{}
	for _, item := range artifacts {
		dir := ""
		for _, part := range splitDir(item.artifact.Path) {
			if dir == "" {
				dir = part
			} else {
				dir += "/" + part
			}
			if !seen[dir] {
				if err := root.Mkdir(dir); err != nil {
					return err
				}
				seen[dir] = true
			}
		}
	}
	return nil
}
func splitDir(path string) []string {
	var out []string
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			out = append(out, path[:i])
		}
	}
	return out
}
func (s *Integration) install(ctx context.Context, root *Root, pkg Package, state inspection) (integration.Result, error) {
	if state.result.State == integration.StateInstalled {
		return state.result, nil
	}
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts", integration.ErrConflict)
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	if err := root.MarkPending(); err != nil {
		return integration.Result{}, recovery(err)
	}
	anchors := []Anchor{}
	fail := func(err error) (integration.Result, error) {
		for i := len(anchors) - 1; i >= 0; i-- {
			err = errors.Join(err, root.RemoveAnchor(anchors[i]))
		}
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	if err := ensureArtifactDirs(root, state.artifacts); err != nil {
		return fail(err)
	}
	for _, item := range state.artifacts {
		if item.exact {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		a, err := root.Publish(ctx, item.artifact.Path, item.artifact.Bytes)
		if a.Info != nil {
			anchors = append(anchors, a)
		}
		if err != nil {
			return fail(err)
		}
		if err := s.check("published", item.artifact.Path); err != nil {
			return fail(err)
		}
	}
	verified, err := inspectRoot(ctx, root, pkg)
	if err != nil || verified.result.State != integration.StateInstalled {
		return fail(errors.Join(err, integration.ErrDrift))
	}
	for _, a := range anchors {
		if err := root.CommitAnchor(a); err != nil {
			return fail(err)
		}
	}
	verified, err = inspectRoot(ctx, root, pkg)
	if err != nil || verified.result.State != integration.StateInstalled {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err, integration.ErrDrift)
	}
	if err := root.ClearPending(); err != nil {
		return integration.Result{}, recovery(err)
	}
	verified.result.Changed, verified.result.RestartRequired = len(anchors) != 0, len(anchors) != 0
	return verified.result, nil
}
func (s *Integration) Install(ctx context.Context, options integration.Options) (integration.Result, error) {
	pkg, err := codexPackage()
	if err != nil {
		return integration.Result{}, err
	}
	root, err := s.openRoot(ctx, options, true)
	if err != nil {
		return integration.Result{}, err
	}
	defer root.Close()
	if p, err := pending(root, pkg); err != nil || p {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	state, err := inspectRoot(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, err
	}
	return s.install(ctx, root, pkg, state)
}
func recoverInstalled(ctx context.Context, root *Root, pkg Package) (integration.Result, error) {
	state, err := inspectRoot(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, recovery(err)
	}
	if err = ensureArtifactDirs(root, state.artifacts); err != nil {
		return integration.Result{}, recovery(err)
	}
	changed := false
	for _, item := range pkg.Artifacts {
		if err := ctx.Err(); err != nil {
			return integration.Result{}, err
		}
		name, data := item.Path, item.Bytes
		remove := name + ".vgxness-remove"
		stage := name + ".vgxness-stage"
		if _, err := root.fs.Lstat(remove); err == nil {
			if _, err := root.fs.Lstat(stage); err == nil {
				return integration.Result{}, recovery(conflict("both sidecars"))
			}
			changed = true
			b, info, err := root.Read(remove, len(data)+1)
			if err != nil || !bytes.Equal(b, data) {
				return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
			}
			target, _, err := root.Read(name, len(data)+1)
			if errors.Is(err, os.ErrNotExist) {
				err = root.Restore(Backup{Name: name, Sidecar: remove, Info: info, Bytes: data})
			} else if err != nil || !bytes.Equal(target, data) {
				return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
			}
			if err != nil {
				return integration.Result{}, recovery(err)
			}
			if err = root.RemoveBackup(Backup{Name: name, Sidecar: remove, Info: info, Bytes: data}); err != nil {
				return integration.Result{}, recovery(err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return integration.Result{}, recovery(err)
		}
		if _, err := root.fs.Lstat(stage); err == nil {
			changed = true
			b, info, err := root.Read(stage, len(data)+1)
			if err != nil || !bytes.Equal(b, data) {
				return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
			}
			if _, _, err = root.Read(name, len(data)+1); errors.Is(err, os.ErrNotExist) {
				err = root.fs.Link(stage, name)
			}
			if err != nil {
				return integration.Result{}, recovery(err)
			}
			if err = root.CommitAnchor(Anchor{Name: name, Temp: stage, Info: info, Bytes: data}); err != nil {
				return integration.Result{}, recovery(err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return integration.Result{}, recovery(err)
		}
		b, _, err := root.Read(name, len(data)+1)
		if errors.Is(err, os.ErrNotExist) {
			changed = true
			a, err := root.Publish(ctx, name, data)
			if err == nil {
				err = root.CommitAnchor(a)
			}
			if err != nil {
				return integration.Result{}, recovery(err)
			}
		} else if err != nil || !bytes.Equal(b, data) {
			return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
		}
	}
	verified, err := inspectRoot(ctx, root, pkg)
	if err != nil || verified.result.State != integration.StateInstalled {
		return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
	}
	if err = root.ClearPending(); err != nil {
		return integration.Result{}, recovery(err)
	}
	verified.result.Changed, verified.result.RestartRequired = changed, changed
	return verified.result, nil
}
func (s *Integration) Uninstall(ctx context.Context, options integration.Options) (integration.Result, error) {
	pkg, err := codexPackage()
	if err != nil {
		return integration.Result{}, err
	}
	path, err := rootPath(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := s.openRoot(ctx, options, false)
	if errors.Is(err, os.ErrNotExist) {
		r := resultFor(path, pkg)
		r.State = integration.StateAbsent
		return r, nil
	}
	if err != nil {
		return integration.Result{}, err
	}
	defer root.Close()
	if p, err := pending(root, pkg); err != nil || p {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	state, err := inspectRoot(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, err
	}
	if state.result.State == integration.StateAbsent {
		return state.result, nil
	}
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts", integration.ErrDrift)
	}
	if err := root.MarkPending(); err != nil {
		return integration.Result{}, recovery(err)
	}
	backups := []Backup{}
	fail := func(err error) (integration.Result, error) {
		for i := len(backups) - 1; i >= 0; i-- {
			err = errors.Join(err, root.Restore(backups[i]))
		}
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	for _, item := range state.artifacts {
		if !item.exact {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := s.check("before-backup", item.artifact.Path); err != nil {
			return fail(err)
		}
		b, err := root.BackupExact(item.artifact.Path, item.artifact.Bytes, item.info)
		if err != nil {
			return fail(err)
		}
		backups = append(backups, b)
		if err := root.RemoveTarget(b); err != nil {
			return fail(err)
		}
		if err := s.check("removed", item.artifact.Path); err != nil {
			return fail(err)
		}
	}
	verified, err := inspectRoot(ctx, root, pkg)
	if err != nil || verified.result.State != integration.StateAbsent {
		return fail(errors.Join(err, integration.ErrDrift))
	}
	for _, b := range backups {
		if err := s.check("cleanup", b.Name); err != nil {
			return integration.Result{}, recovery(err)
		}
		if err := root.RemoveBackup(b); err != nil {
			return integration.Result{}, recovery(err)
		}
	}
	verified, err = inspectRoot(ctx, root, pkg)
	if err != nil || verified.result.State != integration.StateAbsent {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err, integration.ErrDrift)
	}
	if err := root.ClearPending(); err != nil {
		return integration.Result{}, recovery(err)
	}
	verified.result.Changed, verified.result.RestartRequired = len(backups) != 0, len(backups) != 0
	return verified.result, nil
}
func (s *Integration) Reinstall(ctx context.Context, options integration.Options) (integration.Result, error) {
	pkg, err := codexPackage()
	if err != nil {
		return integration.Result{}, err
	}
	root, err := s.openRoot(ctx, options, false)
	if errors.Is(err, os.ErrNotExist) {
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts are absent", integration.ErrInvalid)
	}
	if err != nil {
		return integration.Result{}, err
	}
	defer root.Close()
	if p, err := pending(root, pkg); err != nil {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	} else if p {
		return recoverInstalled(ctx, root, pkg)
	}
	state, err := inspectRoot(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, err
	}
	switch state.result.State {
	case integration.StateInstalled:
		return state.result, nil
	case integration.StatePartial:
		return s.install(ctx, root, pkg, state)
	case integration.StateAbsent:
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts are absent", integration.ErrInvalid)
	default:
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts", integration.ErrDrift)
	}
}

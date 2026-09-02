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
	"path"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
)

const maxArtifactBytes = 512 << 10

type Integration struct {
	open       func(context.Context, integration.Options, bool) (*Root, error)
	checkpoint func(string, string) error
	runner     CommandRunner
	codexBin   string
}

var defaultRunner CommandRunner

// CommandRunner runs a Codex command. It permits embedders to supply an
// isolated command boundary without changing activation readback semantics.
type CommandRunner interface {
	Run(context.Context, string, []string, []string) ([]byte, error)
}

type commandRunner func(context.Context, string, []string, []string) ([]byte, error)

func (f commandRunner) Run(ctx context.Context, bin string, args, env []string) ([]byte, error) {
	return f(ctx, bin, args, env)
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
type partialCandidate struct {
	state inspection
	pkg   Package
}

func NewIntegration() *Integration { return &Integration{runner: defaultRunner} }

// NewIntegrationWithRunner creates an integration with an explicit command
// boundary. A nil runner retains production Codex CLI execution.
func NewIntegrationWithRunner(runner CommandRunner) *Integration { return &Integration{runner: runner} }

func logicalArtifactDir(name string) string { return path.Dir(name) }
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
var _ integration.ProtectedRuntime = (*Integration)(nil)

func verifyProtectedRoot(root *Root, source integration.SourceIdentity) error {
	identity, ok := source.(sourceIdentity)
	if !ok || identity.info == nil {
		return conflict("invalid protected source")
	}
	held, err := root.fs.Lstat(".")
	if err != nil {
		return err
	}
	if !held.IsDir() || !os.SameFile(identity.info, held) {
		return conflict("protected source root replaced")
	}
	return nil
}

func codexPackage(options integration.Options) (Package, error) {
	if options.ModelEfficient != "" || options.ModelBalanced != "" || options.ModelFrontier != "" ||
		options.ModelEfficientEffort != "" || options.ModelBalancedEffort != "" || options.ModelFrontierEffort != "" {
		return Package{}, fmt.Errorf("%w: Codex does not support model-slot customization", integration.ErrInvalid)
	}
	plan := options.ModelPlan
	if plan == "" {
		plan = sdd.PlanMedium
	}
	return RenderPlan("v0.0.0", plan)
}

func knownPackages() ([]Package, error) {
	packages := make([]Package, 0, 50)
	for _, plan := range []sdd.Plan{sdd.PlanLow, sdd.PlanMedium, sdd.PlanHigh, sdd.PlanUltra} {
		current, err := RenderPlan("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, current)
		v18PreTerminalClosure, err := renderActiveV18PreTerminalClosure("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v18PreTerminalClosure)
		v17, err := renderActiveV17("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v17)
		v16, err := renderActiveV16("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v16)
		v15, err := renderActiveV15("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v15)
		v14, err := renderActiveV14("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v14)
		v13, err := renderActiveV13("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v13)
		v12, err := renderActiveV12("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v12)
		v10, err := renderActiveV10("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v10)
		v9, err := renderActiveV9("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v9)
		v8, err := renderActiveV8("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v8)
		v7, err := renderActiveV7("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v7)
		v6, err := renderActiveV6("v0.0.0", plan)
		if err != nil {
			return nil, err
		}
		packages = append(packages, v6)
	}
	preConsolidation, err := renderPreConsolidationV4("v0.0.0", sdd.PlanMedium)
	if err != nil {
		return nil, err
	}
	packages = append(packages, preConsolidation)
	legacy, err := renderLegacy("v0.0.0")
	if err != nil {
		return nil, err
	}
	packages = append(packages, legacy)
	return packages, nil
}

func resultFor(root string, pkg Package) integration.Result {
	durability := "fsync"
	if runtime.GOOS == "windows" {
		durability = "file-sync-namespace-best-effort"
	}
	config := sdd.DefaultModelPlanConfig()
	plan := pkg.plan
	if pkg.legacy {
		plan = sdd.PlanMedium
	}
	return integration.Result{
		Provider: "codex", Path: root, ArtifactSHA256: pkg.SHA256, ArtifactCount: len(pkg.Artifacts), DirectoryDurability: durability,
		ModelPlan: plan, ModelProvider: config.Provider,
		ModelEfficient: config.Efficient, ModelBalanced: config.Balanced, ModelFrontier: config.Frontier,
	}
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
func inspectKnown(ctx context.Context, root *Root, preferred Package) (inspection, Package, error) {
	packages, err := knownPackages()
	if err != nil {
		return inspection{}, Package{}, err
	}
	partial := make([]partialCandidate, 0, len(packages))
	for _, pkg := range packages {
		state, err := inspectRoot(ctx, root, pkg)
		if err != nil {
			return inspection{}, Package{}, err
		}
		if state.result.State == integration.StateInstalled {
			return state, pkg, nil
		}
		if state.result.State == integration.StatePartial {
			partial = append(partial, partialCandidate{state: state, pkg: pkg})
		}
	}
	if len(partial) == 1 {
		return partial[0].state, partial[0].pkg, nil
	}
	if len(partial) > 1 {
		return collapsePartialCandidates(partial, preferred)
	}
	state, err := inspectRoot(ctx, root, preferred)
	return state, preferred, err
}

func isCurrentPackage(pkg Package) bool {
	if pkg.legacy || pkg.plan == "" {
		return false
	}
	current, err := RenderPlan("v0.0.0", pkg.plan)
	return err == nil && current.SHA256 == pkg.SHA256
}

func collapsePartialCandidates(candidates []partialCandidate, preferred Package) (inspection, Package, error) {
	present := make(map[string]bool)
	for _, candidate := range candidates {
		for _, artifact := range candidate.state.artifacts {
			present[artifact.artifact.Path] = present[artifact.artifact.Path] || artifact.present
		}
	}
	expected := make(map[string][]byte)
	for _, candidate := range candidates {
		for _, artifact := range candidate.pkg.Artifacts {
			if prior, ok := expected[artifact.Path]; ok && !bytes.Equal(prior, artifact.Bytes) {
				if present[artifact.Path] {
					return inspection{}, Package{}, conflict("ambiguous managed Codex package")
				}
				continue
			}
			expected[artifact.Path] = artifact.Bytes
		}
	}
	for _, candidate := range candidates {
		if candidate.pkg.SHA256 == preferred.SHA256 {
			return candidate.state, candidate.pkg, nil
		}
	}
	current := -1
	for index, candidate := range candidates {
		if packageUsesManager(candidate.pkg, activeManagerInstructions()) {
			if current != -1 {
				return inspection{}, Package{}, conflict("ambiguous managed Codex package")
			}
			current = index
		}
	}
	if current != -1 {
		return candidates[current].state, candidates[current].pkg, nil
	}
	for _, candidate := range candidates {
		if packageUsesManager(candidate.pkg, activeV9ManagerInstructions()) {
			return candidate.state, candidate.pkg, nil
		}
	}
	return inspection{}, Package{}, conflict("ambiguous managed Codex package")
}

func packageUsesManager(pkg Package, manager string) bool {
	for _, artifact := range pkg.Artifacts {
		if artifact.Path == "AGENTS.md" {
			return bytes.Equal(artifact.Bytes, []byte(manager))
		}
	}
	return false
}

func (s *Integration) inspect(ctx context.Context, options integration.Options) (inspection, error) {
	pkg, err := codexPackage(options)
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
	state, _, err := inspectKnown(ctx, root, pkg)
	return state, err
}
func (s *Integration) Preview(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := s.inspect(ctx, options)
	if err != nil {
		return integration.Result{}, err
	}
	if options.ModelPlan != "" && (state.result.State == integration.StateInstalled || state.result.State == integration.StatePartial) {
		pkg, err := codexPackage(options)
		if err != nil {
			return integration.Result{}, err
		}
		if state.result.State == integration.StatePartial || state.result.ArtifactSHA256 != pkg.SHA256 {
			state.result = resultFor(state.result.Path, pkg)
			state.result.State = integration.StatePartial
		}
	}
	state.result.Changed = state.result.State == integration.StateAbsent || state.result.State == integration.StatePartial
	state.result.RestartRequired = state.result.Changed
	return state.result, nil
}
func (s *Integration) Status(ctx context.Context, options integration.Options) (integration.Result, error) {
	pkg, err := codexPackage(options)
	if err != nil {
		return integration.Result{}, err
	}
	path, err := rootPath(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := s.openRoot(ctx, options, false)
	if errors.Is(err, os.ErrNotExist) {
		result := resultFor(path, pkg)
		result.State = integration.StateAbsent
		return result, nil
	}
	if err != nil {
		return integration.Result{}, err
	}
	defer root.Close()
	state, installed, err := inspectKnown(ctx, root, pkg)
	if err != nil {
		return state.result, err
	}
	if present, err := pending(root, pkg); err != nil || present {
		return state.result, errors.Join(integration.ErrRecovery, err)
	}
	if _, present, err := readActivationEvidence(root, installed); err != nil || present {
		return state.result, errors.Join(integration.ErrRecovery, err)
	}
	if options.ModelPlan != "" && state.result.State == integration.StateInstalled && state.result.ArtifactSHA256 != pkg.SHA256 {
		state.result = resultFor(state.result.Path, pkg)
		state.result.State = integration.StatePartial
		state.result.Changed = true
		state.result.RestartRequired = true
	}
	if state.result.State == integration.StateInstalled && !isCurrentPackage(installed) {
		state.result = resultFor(state.result.Path, installed)
		state.result.State = integration.StatePartial
		state.result.Changed = true
		state.result.RestartRequired = true
	}
	if !isCurrentPackage(installed) {
		return state.result, nil
	}
	activation, activationErr := s.activation(ctx, root)
	if activationErr != nil {
		return state.result, errors.Join(integration.ErrRecovery, activationErr)
	}
	if activation == activationDrifted {
		state.result.State = integration.StateDrifted
		return state.result, nil
	}
	if state.result.State == integration.StateInstalled && activation != activationActive {
		state.result.State = integration.StatePartial // artifacts are present but not activated.
		state.result.Changed, state.result.RestartRequired = true, true
	}
	return state.result, nil
}
func (s *Integration) ManagedLayout(ctx context.Context, options integration.Options) (integration.ManagedLayout, error) {
	if err := ctx.Err(); err != nil {
		return integration.ManagedLayout{}, err
	}
	pkg, err := codexPackage(options)
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
			_, err := root.lstat(item.Path + suffix)
			if err == nil {
				return true, nil
			}
			if errors.Is(err, integration.ErrInvalid) || errors.Is(err, integration.ErrDrift) {
				continue // inspection reports unsafe managed paths as drift, not recovery evidence.
			}
			if !errors.Is(err, os.ErrNotExist) {
				return false, recovery(err)
			}
		}
	}
	return false, nil
}
func (s *Integration) ReinstallPending(ctx context.Context, options integration.Options) (bool, error) {
	pkg, err := codexPackage(options)
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
	if err := root.MarkPending(pendingEvidence(pkg.SHA256)); err != nil {
		return integration.Result{}, recovery(err)
	}
	if err := s.check("pending", pkg.SHA256); err != nil {
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
		if dir := logicalArtifactDir(item.artifact.Path); dir != "." {
			if err := root.Mkdir(dir); err != nil {
				return fail(err)
			}
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
	verified.result.Changed, verified.result.RestartRequired = len(anchors) != 0, len(anchors) != 0
	return verified.result, nil
}

func (s *Integration) installAndActivate(ctx context.Context, root *Root, pkg Package, state inspection) (integration.Result, error) {
	result, err := s.install(ctx, root, pkg, state)
	if err != nil {
		return result, err
	}
	// Historical projections did not carry a marketplace manifest. They remain
	// inspectable/recoverable; a later normal reinstall upgrades them before CLI
	// activation rather than presenting an incomplete root to Codex.
	if !pkg.current {
		if p, err := root.Pending(); err != nil || p {
			if err == nil {
				err = root.ClearPending()
			}
			if err != nil {
				return integration.Result{}, recovery(err)
			}
		}
		return result, nil
	}
	evidence := activationEvidence(pkg, "activate")
	if err := root.MarkActivationPending(evidence.body); err != nil {
		return integration.Result{}, recovery(err)
	}
	if p, err := root.Pending(); err != nil || p {
		if err == nil {
			err = root.ClearPending()
		}
		if err != nil {
			return integration.Result{}, recovery(err)
		}
	}
	plugin, market, safeRollback, activationErr := s.activate(ctx, root)
	if activationErr == nil {
		if err := root.ClearActivationPending(evidence.body); err != nil {
			return integration.Result{}, recovery(err)
		}
		return result, nil
	}
	var rollbackErr error
	if safeRollback {
		rollbackErr = s.deactivate(ctx, root, plugin, market)
	}
	return integration.Result{}, errors.Join(integration.ErrRecovery, activationErr, rollbackErr)
}

func (s *Integration) recoverActivation(ctx context.Context, root *Root, pkg Package) (integration.Result, error) {
	evidence, present, err := readActivationEvidence(root, pkg)
	if err != nil || !present {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	if evidence.phase == "deactivate" {
		changed := false
		if p, err := root.Pending(); err != nil {
			return integration.Result{}, recovery(err)
		} else if p {
			if _, err := recoverInstalled(ctx, root, pkg); err != nil {
				return integration.Result{}, err
			}
			changed = true
		}
		state, err := inspectRoot(ctx, root, pkg)
		if err != nil {
			return integration.Result{}, recovery(err)
		}
		if state.result.State != integration.StateInstalled {
			return integration.Result{}, recovery(integration.ErrDrift)
		}
		activation, activationErr := s.activation(ctx, root)
		if activationErr != nil {
			return integration.Result{}, recovery(activationErr)
		}
		if activation != activationActive {
			if _, _, _, err := s.activate(ctx, root); err != nil {
				return integration.Result{}, recovery(err)
			}
			changed = true
		}
		if err := root.ClearActivationPending(evidence.body); err != nil {
			return integration.Result{}, recovery(err)
		}
		if changed {
			state.result.Changed, state.result.RestartRequired = true, true
		}
		return state.result, nil
	}
	if p, err := root.Pending(); err != nil {
		return integration.Result{}, recovery(err)
	} else if p {
		if _, err := recoverInstalled(ctx, root, pkg); err != nil {
			return integration.Result{}, err
		}
	}
	state, err := inspectRoot(ctx, root, pkg)
	if err != nil || state.result.State != integration.StateInstalled {
		return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
	}
	activation, activationErr := s.activation(ctx, root)
	if activationErr == nil && activation == activationActive {
		if err := root.ClearActivationPending(evidence.body); err != nil {
			return integration.Result{}, recovery(err)
		}
		return state.result, nil
	}
	if activationErr == nil && activation == activationDrifted {
		marketOnly, err := s.marketplaceCreatedExactly(ctx, root)
		if err != nil {
			return integration.Result{}, recovery(err)
		}
		if !marketOnly {
			pluginOnly, checkErr := s.pluginCreatedExactly(ctx, root)
			if checkErr != nil || !pluginOnly {
				return integration.Result{}, recovery(errors.Join(checkErr, integration.ErrDrift))
			}
			if _, err = s.command(ctx, root, "plugin", "marketplace", "add", root.Path, "--json"); err != nil {
				return integration.Result{}, errors.Join(integration.ErrRecovery, err)
			}
		} else if _, err = s.command(ctx, root, "plugin", "add", pluginID, "--json"); err != nil {
			return integration.Result{}, errors.Join(integration.ErrRecovery, err)
		}
		activation, err = s.activation(ctx, root)
		if err != nil || activation != activationActive {
			return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
		}
		if err = root.ClearActivationPending(evidence.body); err != nil {
			return integration.Result{}, recovery(err)
		}
		return state.result, nil
	}
	return s.installAndActivate(ctx, root, pkg, state)
}
func (s *Integration) Install(ctx context.Context, options integration.Options) (integration.Result, error) {
	return s.installProtected(ctx, options, nil, true)
}

func (s *Integration) InstallProtected(ctx context.Context, options integration.Options, source integration.SourceIdentity) (integration.Result, error) {
	if source == nil {
		return integration.Result{}, conflict("missing protected source")
	}
	return s.installProtected(ctx, options, source, false)
}

func (s *Integration) installProtected(ctx context.Context, options integration.Options, source integration.SourceIdentity, create bool) (integration.Result, error) {
	pkg, err := codexPackage(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := s.openRoot(ctx, options, create)
	if err != nil {
		return integration.Result{}, err
	}
	defer root.Close()
	if source != nil {
		if err := verifyProtectedRoot(root, source); err != nil {
			return integration.Result{}, err
		}
	}
	if evidencePkg, _, present, evidenceErr := readKnownActivationEvidence(root); evidenceErr != nil {
		return integration.Result{}, errors.Join(integration.ErrRecovery, evidenceErr)
	} else if present {
		return s.recoverActivation(ctx, root, evidencePkg)
	} else if p, err := pending(root, pkg); err != nil || p {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	state, installed, err := inspectKnown(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, err
	}
	if options.ModelPlan == "" && state.result.State == integration.StateInstalled {
		pkg = installed
	}
	if state.result.State == integration.StateInstalled && installed.SHA256 != pkg.SHA256 {
		if _, err := s.uninstall(ctx, root, installed, state); err != nil {
			return integration.Result{}, err
		}
		state, err = inspectRoot(ctx, root, pkg)
		if err != nil {
			return integration.Result{}, err
		}
	}
	return s.installAndActivate(ctx, root, pkg, state)
}

func recoveryPackage(root *Root, fallback Package, preferFallback bool) (Package, error) {
	packages, err := knownPackages()
	if err != nil {
		return Package{}, err
	}
	if body, present, evidenceErr := root.PendingEvidence(); evidenceErr != nil {
		return Package{}, evidenceErr
	} else if present && string(body) != "codex-pending\n" {
		sha := string(body[len("codex-pending-v2\nsha256=") : len("codex-pending-v2\nsha256=")+64])
		matches := make([]Package, 0, 1)
		for _, candidate := range packages {
			if candidate.SHA256 == sha {
				matches = append(matches, candidate)
			}
		}
		if len(matches) != 1 {
			return Package{}, recovery(conflict("unknown pending package"))
		}
		state, inspectErr := inspectRoot(context.Background(), root, matches[0])
		if inspectErr != nil || state.result.State == integration.StateDrifted {
			return Package{}, recovery(errors.Join(inspectErr, integration.ErrDrift))
		}
		for _, artifact := range matches[0].Artifacts {
			for _, suffix := range []string{".vgxness-stage", ".vgxness-remove"} {
				got, _, readErr := root.Read(artifact.Path+suffix, maxArtifactBytes)
				if !errors.Is(readErr, os.ErrNotExist) && (readErr != nil || !bytes.Equal(got, artifact.Bytes)) {
					return Package{}, recovery(errors.Join(readErr, integration.ErrDrift))
				}
			}
		}
		return matches[0], nil
	}
	matches := make([]Package, 0, len(packages))
	for index := range packages {
		pkg, matched := packages[index], false
		for _, artifact := range pkg.Artifacts {
			for _, suffix := range []string{".vgxness-stage", ".vgxness-remove"} {
				body, _, err := root.Read(artifact.Path+suffix, maxArtifactBytes)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil || !bytes.Equal(body, artifact.Bytes) {
					matched = false
					goto nextPackage
				}
				matched = true
			}
		}
		if matched {
			matches = append(matches, pkg)
		}
	nextPackage:
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		if preferFallback {
			for _, pkg := range matches {
				if pkg.SHA256 == fallback.SHA256 {
					return fallback, nil
				}
			}
		}
		return Package{}, recovery(conflict("ambiguous recovery package"))
	}
	if preferFallback {
		return fallback, nil
	}
	return Package{}, recovery(conflict("unidentified legacy pending package"))
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
		if _, err := root.lstat(remove); err == nil {
			if _, err := root.lstat(stage); err == nil {
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
		if _, err := root.lstat(stage); err == nil {
			changed = true
			b, info, err := root.Read(stage, len(data)+1)
			if err != nil || !bytes.Equal(b, data) {
				return integration.Result{}, recovery(errors.Join(err, integration.ErrDrift))
			}
			if _, _, err = root.Read(name, len(data)+1); errors.Is(err, os.ErrNotExist) {
				parent, base, openErr := root.parent(name)
				if openErr != nil {
					err = openErr
				} else {
					err = parent.Link(filepath.Base(stage), base)
					_ = parent.Close()
				}
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
			if dir := logicalArtifactDir(name); dir != "." {
				if err := root.Mkdir(dir); err != nil {
					return integration.Result{}, recovery(err)
				}
			}
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
	pkg, err := codexPackage(options)
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
	if evidencePkg, _, present, evidenceErr := readKnownActivationEvidence(root); evidenceErr != nil {
		return integration.Result{}, errors.Join(integration.ErrRecovery, evidenceErr)
	} else if present {
		state, inspectErr := inspectRoot(ctx, root, evidencePkg)
		if inspectErr != nil {
			return integration.Result{}, inspectErr
		}
		return s.uninstall(ctx, root, evidencePkg, state)
	} else if p, err := pending(root, pkg); err != nil || p {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	}
	state, installed, err := inspectKnown(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, err
	}
	return s.uninstall(ctx, root, installed, state)
}

func (s *Integration) uninstall(ctx context.Context, root *Root, pkg Package, state inspection) (integration.Result, error) {
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts", integration.ErrDrift)
	}
	var evidence activationPending
	if pkg.current {
		var present bool
		var err error
		evidence, present, err = readActivationEvidence(root, pkg)
		if err != nil {
			return integration.Result{}, errors.Join(integration.ErrRecovery, err)
		}
		if !present {
			evidence = activationEvidence(pkg, "deactivate")
			if err := root.MarkActivationPending(evidence.body); err != nil {
				return integration.Result{}, recovery(err)
			}
		}
		if err := s.deactivateExact(ctx, root); err != nil {
			return integration.Result{}, errors.Join(integration.ErrRecovery, err)
		}
	}
	if err := root.MarkPending(pendingEvidence(pkg.SHA256)); err != nil {
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
	if pkg.current {
		if err := root.ClearActivationPending(evidence.body); err != nil {
			return integration.Result{}, recovery(err)
		}
	}
	verified.result.Changed, verified.result.RestartRequired = len(backups) != 0, len(backups) != 0
	return verified.result, nil
}
func (s *Integration) Reinstall(ctx context.Context, options integration.Options) (integration.Result, error) {
	return s.reinstallProtected(ctx, options, nil, options.ModelPlan != "")
}

func (s *Integration) ReinstallProtected(ctx context.Context, options integration.Options, source integration.SourceIdentity) (integration.Result, error) {
	if source == nil {
		return integration.Result{}, conflict("missing protected source")
	}
	return s.reinstallProtected(ctx, options, source, false)
}

func (s *Integration) reinstallProtected(ctx context.Context, options integration.Options, source integration.SourceIdentity, create bool) (integration.Result, error) {
	pkg, err := codexPackage(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := s.openRoot(ctx, options, create)
	if errors.Is(err, os.ErrNotExist) {
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts are absent", integration.ErrInvalid)
	}
	if err != nil {
		return integration.Result{}, err
	}
	defer root.Close()
	if source != nil {
		if err := verifyProtectedRoot(root, source); err != nil {
			return integration.Result{}, err
		}
	}
	if evidencePkg, _, present, evidenceErr := readKnownActivationEvidence(root); evidenceErr != nil {
		return integration.Result{}, errors.Join(integration.ErrRecovery, evidenceErr)
	} else if present {
		return s.recoverActivation(ctx, root, evidencePkg)
	} else if p, err := pending(root, pkg); err != nil {
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	} else if p {
		state, installed, err := inspectKnown(ctx, root, pkg)
		if err != nil {
			return integration.Result{}, err
		}
		installedExact := state.result.State == integration.StateInstalled
		if installedExact {
			pkg = installed
		}
		pkg, err = recoveryPackage(root, pkg, installedExact || options.ModelPlan != "")
		if err != nil {
			return integration.Result{}, err
		}
		result, recoverErr := recoverInstalled(ctx, root, pkg)
		if recoverErr != nil {
			return result, recoverErr
		}
		if !pkg.current {
			return result, nil
		}
		state, inspectErr := inspectRoot(ctx, root, pkg)
		if inspectErr != nil {
			return integration.Result{}, inspectErr
		}
		activated, activationErr := s.installAndActivate(ctx, root, pkg, state)
		if activationErr == nil && result.Changed {
			activated.Changed, activated.RestartRequired = true, true
		}
		return activated, activationErr
	}
	state, installed, err := inspectKnown(ctx, root, pkg)
	if err != nil {
		return integration.Result{}, err
	}
	if options.ModelPlan == "" && (state.result.State == integration.StateInstalled || state.result.State == integration.StatePartial) {
		if !installed.legacy && installed.plan != "" {
			pkg, err = RenderPlan("v0.0.0", installed.plan)
			if err != nil {
				return integration.Result{}, err
			}
		} else {
			pkg = installed
		}
	}
	switch state.result.State {
	case integration.StateInstalled:
		if installed.SHA256 != pkg.SHA256 {
			if _, err := s.uninstall(ctx, root, installed, state); err != nil {
				return integration.Result{}, err
			}
			state, err = inspectRoot(ctx, root, pkg)
			if err != nil {
				return integration.Result{}, err
			}
			return s.installAndActivate(ctx, root, pkg, state)
		}
		return s.installAndActivate(ctx, root, pkg, state)
	case integration.StatePartial:
		if installed.SHA256 != pkg.SHA256 {
			if _, err := s.uninstall(ctx, root, installed, state); err != nil {
				return integration.Result{}, err
			}
			state, err = inspectRoot(ctx, root, pkg)
			if err != nil {
				return integration.Result{}, err
			}
		}
		return s.installAndActivate(ctx, root, pkg, state)
	case integration.StateAbsent:
		if options.ModelPlan != "" {
			return s.installAndActivate(ctx, root, pkg, state)
		}
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts are absent", integration.ErrInvalid)
	default:
		return integration.Result{}, fmt.Errorf("%w: managed Codex artifacts", integration.ErrDrift)
	}
}

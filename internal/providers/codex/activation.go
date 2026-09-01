package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
)

const (
	marketplaceName = "vgxness"
	pluginID        = "vgxness@vgxness"
	maxCLIOutput    = 64 << 10
	cliTimeout      = 10 * time.Second
)

// commandRunner is intentionally shell-free. Tests replace it with a bounded
// fake; production inherits cancellation and supplies an isolated CODEX_HOME.
func runCodex(ctx context.Context, bin string, args, env []string) ([]byte, error) {
	if bin == "" {
		bin = "codex"
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stdout, stderr cappedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("codex output exceeds bound")
	}
	if err != nil {
		return nil, errors.New("codex command failed")
	}
	return stdout.Bytes(), nil
}

type cappedOutput struct {
	bytes.Buffer
	exceeded bool
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > maxCLIOutput {
		w.exceeded = true
		return 0, io.ErrShortWrite
	}
	return w.Buffer.Write(p)
}
func filteredEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, value := range env {
		if len(value) < 11 || value[:11] != "CODEX_HOME=" {
			out = append(out, value)
		}
	}
	return out
}

type activationState uint8

const (
	activationAbsent activationState = iota
	activationActive
	activationDrifted
)

func (s *Integration) command(ctx context.Context, root *Root, args ...string) ([]byte, error) {
	var run CommandRunner = commandRunner(runCodex)
	if s.runner != nil {
		run = s.runner
	}
	env := append(filteredEnv(os.Environ()), "CODEX_HOME="+root.Path)
	return run.Run(ctx, s.codexBin, args, env)
}

type marketplaceRecord struct {
	Name              string `json:"name"`
	Root              string `json:"root"`
	MarketplaceSource struct {
		Source string `json:"source"`
	} `json:"marketplaceSource"`
}
type marketplaceList struct {
	Marketplaces []marketplaceRecord `json:"marketplaces"`
}
type pluginRecord struct {
	ID          string `json:"pluginId"`
	Name        string `json:"name"`
	Marketplace string `json:"marketplaceName"`
	Version     string `json:"version"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
}
type pluginList struct {
	Installed []pluginRecord `json:"installed"`
}

type activationPending struct {
	phase string
	body  []byte
}

func activationEvidence(pkg Package, phase string) activationPending {
	body := []byte("codex-activation-v1\nsha256=" + pkg.SHA256 + "\nphase=" + phase + "\n")
	return activationPending{phase: phase, body: body}
}

func readActivationEvidence(root *Root, pkg Package) (activationPending, bool, error) {
	body, present, err := root.ActivationPending()
	if err != nil || !present {
		return activationPending{}, present, err
	}
	for _, phase := range []string{"activate", "deactivate"} {
		evidence := activationEvidence(pkg, phase)
		if bytes.Equal(body, evidence.body) {
			return evidence, true, nil
		}
	}
	return activationPending{}, true, recovery(fmt.Errorf("%w: activation evidence", integration.ErrDrift))
}

func readKnownActivationEvidence(root *Root) (Package, activationPending, bool, error) {
	body, present, err := root.ActivationPending()
	if err != nil || !present {
		return Package{}, activationPending{}, present, err
	}
	packages, err := knownPackages()
	if err != nil {
		return Package{}, activationPending{}, true, err
	}
	for _, pkg := range packages {
		if !pkg.current {
			continue
		}
		for _, phase := range []string{"activate", "deactivate"} {
			evidence := activationEvidence(pkg, phase)
			if bytes.Equal(body, evidence.body) {
				return pkg, evidence, true, nil
			}
		}
	}
	return Package{}, activationPending{}, true, recovery(fmt.Errorf("%w: activation evidence", integration.ErrDrift))
}

func hasRoot(item marketplaceRecord, root string) bool {
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		want = root
	}
	got := item.Root
	if got == "" {
		got = item.MarketplaceSource.Source
	}
	if !filepath.IsAbs(got) {
		return false
	}
	real, err := filepath.EvalSymlinks(got)
	if err != nil {
		real = got
	}
	return filepath.Clean(real) == filepath.Clean(want)
}

func (s *Integration) activation(ctx context.Context, root *Root) (activationState, error) {
	markets, err := s.command(ctx, root, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return activationAbsent, err
	}
	var market marketplaceList
	if json.Unmarshal(markets, &market) != nil {
		return activationAbsent, errors.New("invalid marketplace list JSON")
	}
	plugins, err := s.command(ctx, root, "plugin", "list", "--json")
	if err != nil {
		return activationAbsent, err
	}
	var listed pluginList
	if json.Unmarshal(plugins, &listed) != nil {
		return activationAbsent, errors.New("invalid plugin list JSON")
	}
	if len(market.Marketplaces) == 0 && len(listed.Installed) == 0 {
		return activationAbsent, nil
	}
	if len(market.Marketplaces) != 1 || len(listed.Installed) != 1 || market.Marketplaces[0].Name != marketplaceName || !hasRoot(market.Marketplaces[0], root.Path) {
		return activationDrifted, nil
	}
	body, _, readErr := root.Read("plugins/vgxness/.codex-plugin/plugin.json", maxArtifactBytes)
	var manifest struct {
		Version string `json:"version"`
	}
	if readErr != nil || json.Unmarshal(body, &manifest) != nil || manifest.Version == "" {
		return activationDrifted, nil
	}
	p := listed.Installed[0]
	if p.ID != pluginID || p.Name != marketplaceName || p.Marketplace != marketplaceName || p.Version != manifest.Version || !p.Installed || !p.Enabled {
		return activationDrifted, nil
	}
	return activationActive, nil
}

func (s *Integration) marketplaceCreatedExactly(ctx context.Context, root *Root) (bool, error) {
	markets, err := s.command(ctx, root, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return false, err
	}
	var market marketplaceList
	if err := json.Unmarshal(markets, &market); err != nil {
		return false, errors.New("invalid marketplace list JSON")
	}
	plugins, err := s.command(ctx, root, "plugin", "list", "--json")
	if err != nil {
		return false, err
	}
	var listed pluginList
	if err := json.Unmarshal(plugins, &listed); err != nil {
		return false, errors.New("invalid plugin list JSON")
	}
	return len(market.Marketplaces) == 1 && market.Marketplaces[0].Name == marketplaceName && hasRoot(market.Marketplaces[0], root.Path) && len(listed.Installed) == 0, nil
}

func (s *Integration) pluginCreatedExactly(ctx context.Context, root *Root) (bool, error) {
	markets, err := s.command(ctx, root, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return false, err
	}
	plugins, err := s.command(ctx, root, "plugin", "list", "--json")
	if err != nil {
		return false, err
	}
	var market marketplaceList
	var listed pluginList
	if json.Unmarshal(markets, &market) != nil || json.Unmarshal(plugins, &listed) != nil {
		return false, errors.New("invalid Codex activation JSON")
	}
	body, _, err := root.Read("plugins/vgxness/.codex-plugin/plugin.json", maxArtifactBytes)
	var manifest struct {
		Version string `json:"version"`
	}
	if err != nil || json.Unmarshal(body, &manifest) != nil {
		return false, err
	}
	if len(market.Marketplaces) != 0 || len(listed.Installed) != 1 {
		return false, nil
	}
	p := listed.Installed[0]
	return p.ID == pluginID && p.Name == marketplaceName && p.Marketplace == marketplaceName && p.Version == manifest.Version && p.Installed && p.Enabled, nil
}

func (s *Integration) deactivateExact(ctx context.Context, root *Root) error {
	state, err := s.activation(ctx, root)
	if err != nil {
		return err
	}
	switch state {
	case activationAbsent:
		return nil
	case activationActive:
		return s.deactivate(ctx, root, true, true)
	}
	marketOnly, checkErr := s.marketplaceCreatedExactly(ctx, root)
	if checkErr != nil {
		return checkErr
	}
	if marketOnly {
		_, err = s.command(ctx, root, "plugin", "marketplace", "remove", marketplaceName, "--json")
		return err
	}
	pluginOnly, checkErr := s.pluginCreatedExactly(ctx, root)
	if checkErr != nil {
		return checkErr
	}
	if pluginOnly {
		_, err = s.command(ctx, root, "plugin", "remove", pluginID, "--json")
		return err
	}
	return fmt.Errorf("%w: Codex marketplace/plugin identity", integration.ErrDrift)
}

func (s *Integration) activate(ctx context.Context, root *Root) (createdPlugin, createdMarket, safeRollback bool, err error) {
	state, err := s.activation(ctx, root)
	// Codex 0.147 rejects list while a rendered local marketplace is not yet
	// registered. marketplace add is the authoritative conflict-safe preflight.
	if err != nil {
		state, err = activationAbsent, nil
	}
	if state == activationDrifted {
		return false, false, false, errors.Join(err, fmt.Errorf("%w: Codex marketplace/plugin identity", integration.ErrConflict))
	}
	if state == activationActive {
		return false, false, true, nil
	}
	if _, err = s.command(ctx, root, "plugin", "marketplace", "add", root.Path, "--json"); err != nil {
		created, checkErr := s.marketplaceCreatedExactly(ctx, root)
		if checkErr != nil || !created {
			return false, false, false, errors.Join(err, checkErr)
		}
		return false, true, true, err
	}
	createdMarket = true
	if _, err = s.command(ctx, root, "plugin", "add", pluginID, "--json"); err != nil {
		state, checkErr := s.activation(ctx, root)
		if checkErr != nil || state != activationActive {
			return false, false, false, errors.Join(err, checkErr)
		}
		return true, createdMarket, true, err
	}
	createdPlugin = true
	state, err = s.activation(ctx, root)
	if err != nil || state != activationActive {
		return createdPlugin, createdMarket, true, errors.Join(err, fmt.Errorf("%w: Codex activation readback", integration.ErrDrift))
	}
	return createdPlugin, createdMarket, true, nil
}

func (s *Integration) deactivate(ctx context.Context, root *Root, plugin, market bool) error {
	var err error
	if plugin {
		_, err = s.command(ctx, root, "plugin", "remove", pluginID, "--json")
	}
	if err == nil && market {
		_, err = s.command(ctx, root, "plugin", "marketplace", "remove", marketplaceName, "--json")
	}
	return err
}

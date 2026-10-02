// Package claudecli reads Claude Code state and runs plugin commands through
// the user's `claude` executable. It never edits Claude Code files directly.
package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// MarketplaceRepo is where the vgxness marketplace is published.
	MarketplaceRepo = "uzielvgx/vgxness"
	// MarketplaceName and PluginID identify the plugin once installed.
	MarketplaceName = "vgxness"
	PluginID        = "vgxness@vgxness"
	// MinimumVersion is the oldest Claude Code the plugin supports.
	MinimumVersion = "2.1.284"
)

// ErrUnavailable means the `claude` executable could not be found or run.
var ErrUnavailable = errors.New("claude executable unavailable")

// Runner executes one command and returns its stdout. Failing commands return
// an *ExitError carrying the captured stderr.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExitError reports a command that ran and exited with a non-zero status.
type ExitError struct {
	Command string
	Code    int
	Stderr  string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s exited with status %d", e.Command, e.Code)
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, path, args...)
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), &ExitError{Command: name + " " + strings.Join(args, " "), Code: exitErr.ExitCode(), Stderr: strings.TrimSpace(stderr.String())}
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

// Client talks to Claude Code through its CLI.
type Client struct {
	runner Runner
}

func New(runner Runner) Client {
	if runner == nil {
		runner = ExecRunner{}
	}
	return Client{runner: runner}
}

// InstalledPlugin is one entry of `claude plugin list --json`.
type InstalledPlugin struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	InstallPath string `json:"installPath"`
}

// Marketplace is one entry of `claude plugin marketplace list --json`.
type Marketplace struct {
	Name            string `json:"name"`
	Source          string `json:"source"`
	Repo            string `json:"repo"`
	InstallLocation string `json:"installLocation"`
}

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

// Version returns the semantic version printed by `claude --version`.
func (c Client) Version(ctx context.Context) (string, error) {
	out, err := c.runner.Run(ctx, "claude", "--version")
	if err != nil {
		return "", err
	}
	version := versionPattern.FindString(string(out))
	if version == "" {
		return "", fmt.Errorf("unrecognized claude --version output")
	}
	return version, nil
}

func (c Client) Plugins(ctx context.Context) ([]InstalledPlugin, error) {
	var plugins []InstalledPlugin
	return plugins, c.decode(ctx, &plugins, "plugin", "list", "--json")
}

func (c Client) Marketplaces(ctx context.Context) ([]Marketplace, error) {
	var marketplaces []Marketplace
	return marketplaces, c.decode(ctx, &marketplaces, "plugin", "marketplace", "list", "--json")
}

func (c Client) decode(ctx context.Context, target any, args ...string) error {
	out, err := c.runner.Run(ctx, "claude", args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, target); err != nil {
		return fmt.Errorf("decode claude %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// AddMarketplace, InstallPlugin, UpdatePlugin and ListMCP are the commands the
// console's plugin setup runs after the user confirms. Each returns the
// command's combined output for the setup log.
func (c Client) AddMarketplace(ctx context.Context) ([]byte, error) {
	return c.runner.Run(ctx, "claude", "plugin", "marketplace", "add", MarketplaceRepo)
}

func (c Client) InstallPlugin(ctx context.Context) ([]byte, error) {
	return c.runner.Run(ctx, "claude", "plugin", "install", PluginID)
}

func (c Client) UpdatePlugin(ctx context.Context) ([]byte, error) {
	return c.runner.Run(ctx, "claude", "plugin", "update", PluginID)
}

func (c Client) ListMCP(ctx context.Context) ([]byte, error) {
	return c.runner.Run(ctx, "claude", "mcp", "list")
}

// FindPlugin returns the installed vgxness plugin, if any.
func FindPlugin(plugins []InstalledPlugin) (InstalledPlugin, bool) {
	for _, plugin := range plugins {
		if plugin.ID == PluginID {
			return plugin, true
		}
	}
	return InstalledPlugin{}, false
}

// FindMarketplace returns the vgxness marketplace, if added.
func FindMarketplace(marketplaces []Marketplace) (Marketplace, bool) {
	for _, marketplace := range marketplaces {
		if marketplace.Name == MarketplaceName {
			return marketplace, true
		}
	}
	return Marketplace{}, false
}

// MarketplacePluginVersion reads the plugin version the local marketplace
// checkout offers; it is what `claude plugin update` would install.
func MarketplacePluginVersion(marketplace Marketplace) (string, error) {
	if marketplace.InstallLocation == "" {
		return "", os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(marketplace.InstallLocation, "plugins", "vgxness", ".claude-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version == "" {
		return "", fmt.Errorf("plugin.json has no version")
	}
	return manifest.Version, nil
}

// AtLeast reports whether version is greater than or equal to minimum; both
// are dotted numeric versions and a malformed one compares as older.
func AtLeast(version, minimum string) bool {
	have, ok := parse(version)
	want, wantOK := parse(minimum)
	if !ok || !wantOK {
		return false
	}
	for index := range want {
		if have[index] != want[index] {
			return have[index] > want[index]
		}
	}
	return true
}

func parse(version string) ([3]int, bool) {
	var parsed [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	if len(parts) < 3 {
		return parsed, false
	}
	for index := range parsed {
		value, err := strconv.Atoi(strings.SplitN(parts[index], "-", 2)[0])
		if err != nil || value < 0 {
			return parsed, false
		}
		parsed[index] = value
	}
	return parsed, true
}

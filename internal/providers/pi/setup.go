package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

// NewProvider adapts offline Pi provisioning to the shared setup coordinator.
// Pi owns only its package root and Pi settings; it does not start VGXNESS.
func NewProvider(options Options) setupflow.ProviderRuntime { return provider{options: options} }

type provider struct{ options Options }

var probeTimeout = 2 * time.Second

func (provider) Provider() setupflow.Provider { return setupflow.ProviderPi }

func (p provider) Plan(ctx context.Context, _ setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	if err := ctx.Err(); err != nil {
		return setupflow.ProviderPlan{}, err
	}
	options, target, err := normalize(p.options)
	if err != nil {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: err.Error()}, nil
	}
	source, _, err := validateRelease(options.ReleaseDir, target)
	if err != nil {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: err.Error()}, nil
	}
	path := filepath.Join(options.InstallRoot, "packages", "pi-"+version+"-"+source[:16], "package")
	installed, err := activePackage(options.AgentDir, path, source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: err.Error()}, nil
	}
	return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Ready: true, Changed: !installed, Installed: installed, State: piState(installed), ArtifactSHA256: source, ArtifactCount: 2}, nil
}

func (p provider) Status(ctx context.Context, shared setupflow.SharedPlan) (setupflow.ProviderPlan, error) {
	if p.options.ReleaseDir == "" {
		return installedStatus(ctx, p.options)
	}
	plan, err := p.Plan(ctx, shared)
	if err != nil || !plan.Ready {
		return plan, err
	}
	plan.Ready = plan.Installed
	if !plan.Ready {
		plan.Blocker = "Pi package is not activated"
		return plan, nil
	}
	plan.Handshake = probe(ctx, p.options, filepath.Join(p.options.InstallRoot, "packages", "pi-"+version+"-"+plan.ArtifactSHA256[:16], "package"))
	plan.Ready = plan.Handshake.OK
	if !plan.Ready {
		plan.Blocker = "Pi backend handshake is unavailable"
	}
	return plan, nil
}

func (p provider) Apply(ctx context.Context, plan setupflow.ProviderPlan, _ setupflow.SharedResult) (setupflow.ProviderResult, error) {
	result := setupflow.ProviderResult{Provider: setupflow.ProviderPi}
	if !plan.Ready || plan.Provider != setupflow.ProviderPi {
		return result, setupflow.ErrPrerequisite
	}
	installed, err := install(ctx, p.options, plan.ArtifactSHA256)
	if err != nil {
		if detail, ok := recoveryDetail(err); ok {
			result.Recovery = detail
		} else {
			result.Recovery = "Pi installation failed before publication; inspect Pi settings before retrying."
		}
		return result, err
	}
	status, err := p.Status(ctx, setupflow.SharedPlan{})
	if err != nil || !status.Ready || status.ArtifactSHA256 != plan.ArtifactSHA256 {
		result.Recovery = "Pi package publication needs status verification before retrying."
		if err == nil {
			err = fmt.Errorf("%w: Pi activation", setupflow.ErrVerification)
		}
		return result, err
	}
	result.Changed, result.Verified = installed.Changed, true
	return result, nil
}

// installedStatus is deliberately release-independent: --status must inspect
// the durable managed package and Pi settings rather than require the local
// release directory that was used for a prior installation.
func installedStatus(ctx context.Context, options Options) (setupflow.ProviderPlan, error) {
	if err := ctx.Err(); err != nil {
		return setupflow.ProviderPlan{}, err
	}
	if options.AgentDir == "" || options.InstallRoot == "" || !filepath.IsAbs(options.AgentDir) || !filepath.IsAbs(options.InstallRoot) {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: "Pi agent and managed roots must be absolute"}, nil
	}
	data, err := os.ReadFile(filepath.Join(options.AgentDir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		if plan := retainedStatus(options, ""); plan.Blocker != "" {
			return plan, nil
		}
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, State: integration.StateAbsent, Blocker: "Pi package is not activated"}, nil
	}
	if err != nil {
		return setupflow.ProviderPlan{}, err
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(data, &settings) != nil || settings == nil {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: "malformed Pi settings"}, nil
	}
	var packages []json.RawMessage
	if raw := settings["packages"]; raw != nil && json.Unmarshal(raw, &packages) != nil {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: "malformed Pi packages settings"}, nil
	}
	var path string
	for _, entry := range packages {
		candidate, ok := packageEntryPath(entry)
		if !ok || !isManagedPackagePath(candidate, options.InstallRoot) {
			continue
		}
		if path != "" {
			return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: "ambiguous Pi managed package entries"}, nil
		}
		path = candidate
	}
	if path == "" {
		if plan := retainedStatus(options, ""); plan.Blocker != "" {
			return plan, nil
		}
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, State: integration.StateAbsent, Blocker: "Pi package is not activated"}, nil
	}
	if err := verifyManaged(path, ""); err != nil {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: err.Error()}, nil
	}
	data, err = os.ReadFile(filepath.Join(path, ".vgxness-pi.json"))
	if err != nil {
		return setupflow.ProviderPlan{}, err
	}
	var manifest managedManifest
	if json.Unmarshal(data, &manifest) != nil || !validHex(manifest.SourceSHA) {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Blocker: "invalid Pi managed manifest"}, nil
	}
	plan := setupflow.ProviderPlan{Provider: setupflow.ProviderPi, Ready: true, Installed: true, State: integration.StateInstalled, ArtifactSHA256: manifest.SourceSHA, ArtifactCount: 2}
	if pending := retainedStatus(options, path); pending.Blocker != "" {
		return pending, nil
	}
	plan.Handshake = probe(ctx, options, path)
	plan.Ready = plan.Handshake.OK
	if !plan.Ready {
		plan.Blocker = "Pi backend handshake is unavailable"
	}
	return plan, nil
}

func retainedStatus(options Options, activePath string) setupflow.ProviderPlan {
	packages := filepath.Join(options.InstallRoot, "packages")
	entries, err := os.ReadDir(packages)
	if err != nil {
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi}
	}
	var found []string
	for _, entry := range entries {
		path := filepath.Join(packages, entry.Name())
		if strings.HasPrefix(entry.Name(), ".pi-stage-") && entry.IsDir() {
			found = append(found, "staged at "+path)
			continue
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "pi-"+version+"-") {
			continue
		}
		packagePath := filepath.Join(path, "package")
		markerPath := filepath.Join(path, ".vgxness-pi-reservation")
		if info, err := os.Lstat(markerPath); err == nil {
			data, readErr := os.ReadFile(markerPath)
			var marker reservationManifest
			valid := info.Mode().IsRegular() && readErr == nil && strictJSON(data, &marker) == nil && marker.Schema == 1 && marker.ManagedBy == "vgxness" && marker.PackagePath == packagePath && validHex(marker.SourceSHA)
			if !valid {
				found = append(found, "reservation-unproven at "+path)
				continue
			}
			if err := verifyManaged(packagePath, ""); err == nil {
				if packagePath == activePath {
					found = append(found, "activated-finalization-pending at "+filepath.Join(path, ".vgxness-pi-reservation"))
				} else {
					found = append(found, "published-inactive at "+packagePath)
				}
			} else {
				found = append(found, "reserved at "+path)
			}
		} else if _, err := os.Stat(packagePath); errors.Is(err, os.ErrNotExist) {
			found = append(found, "reservation-unproven at "+path)
		}
	}
	if len(found) != 1 {
		if len(found) > 1 {
			return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, State: integration.StatePartial, Blocker: "ambiguous Pi retained recovery state: " + strings.Join(found, "; ")}
		}
		return setupflow.ProviderPlan{Provider: setupflow.ProviderPi}
	}
	return setupflow.ProviderPlan{Provider: setupflow.ProviderPi, State: integration.StatePartial, Blocker: "Pi recovery " + found[0]}
}

// probe performs only the native protocol hello exchange in a fresh temporary
// workspace. It neither loads the extension nor touches a user's Pi storage.
func probe(ctx context.Context, options Options, packagePath string) integration.Handshake {
	limited, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	root := filepath.Join(packagePath, "node_modules", "@vgxness", "pi-backend-"+platform+"-"+arch)
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	var manifest struct {
		Binary string `json:"binary"`
	}
	if strictJSON(data, &manifest) != nil || manifest.Binary == "" {
		return integration.Handshake{Status: integration.HandshakeIncompatible}
	}
	binary := filepath.Join(root, manifest.Binary)
	if !within(root, binary) {
		return integration.Handshake{Status: integration.HandshakeIncompatible}
	}
	workspace, err := os.MkdirTemp("", "vgxness-pi-probe-")
	if err != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	defer os.RemoveAll(workspace)
	storage := filepath.Join(workspace, "storage")
	command := exec.CommandContext(limited, binary, "--protocol", "vgxness-pi/v1", "--workspace", workspace, "--storage-root", storage, "--mode", "read-only", "--role", "manager")
	command.Dir = workspace
	command.Env = []string{"HOME=" + workspace, "PATH=" + os.Getenv("PATH"), "PI_CODING_AGENT_DIR=" + workspace, "PI_CODING_AGENT_SESSION_DIR=" + filepath.Join(workspace, "session")}
	in, err := command.StdinPipe()
	if err != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	out, err := command.StdoutPipe()
	if err != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	if command.Start() != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	reaped := false
	reap := func() {
		if reaped {
			return
		}
		if command.ProcessState == nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
		reaped = true
	}
	defer reap()
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	line := scanner.Bytes()
	var hello map[string]any
	if json.Unmarshal(line, &hello) != nil || hello["type"] != "hello" || hello["protocol"] != "vgxness-pi/v1" {
		return integration.Handshake{Status: integration.HandshakeIncompatible}
	}
	if _, err := in.Write(append(append([]byte{}, line...), '\n')); err != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	if err := in.Close(); err != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	err = command.Wait()
	reaped = true
	if err != nil || limited.Err() != nil {
		return integration.Handshake{Status: integration.HandshakeUnavailable}
	}
	return integration.Handshake{OK: true, Status: integration.HandshakeHealthy}
}

func activePackage(agent, path, source string) (bool, error) {
	if err := verifyManaged(path, source); err != nil {
		return false, err
	}
	data, err := os.ReadFile(filepath.Join(agent, "settings.json"))
	if err != nil {
		return false, err
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(data, &settings) != nil {
		return false, errors.New("malformed Pi settings")
	}
	var packages []json.RawMessage
	if raw := settings["packages"]; raw != nil && json.Unmarshal(raw, &packages) != nil {
		return false, errors.New("malformed Pi packages settings")
	}
	count := 0
	for _, entry := range packages {
		if value, ok := packageEntryPath(entry); ok && value == path {
			count++
		}
	}
	if count > 1 {
		return false, errors.New("ambiguous Pi managed package entries")
	}
	return count == 1, nil
}

func piState(installed bool) integration.State {
	if installed {
		return integration.StateInstalled
	}
	return integration.StateAbsent
}

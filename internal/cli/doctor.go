package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
)

// runDoctorAll calls status operations only. Missing runtime observations remain
// explicit even when every managed artifact is installed correctly.
func runDoctorAll(ctx context.Context, stdout, stderr io.Writer, opts config.Options, inspector Inspector, setup setupflow.Runtime, codex integration.Runtime) int {
	if err := ctx.Err(); err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	workspace := opts.ProjectDir
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "operational: current workspace is unavailable")
			return 1
		}
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: workspace is invalid")
		return 2
	}
	opts.ProjectDir = workspace
	attention := false
	reportError := func(component string, err error) {
		attention = true
		_, message := failure(err)
		fmt.Fprintf(stdout, "%s=unavailable detail=%s\n", component, terminalSafe(message))
	}
	fmt.Fprintf(stdout, "workspace=%s\nprovider_roots=default\n", terminalSafe(workspace))
	if inspector == nil {
		reportError("storage", setupflow.ErrPrerequisite)
	} else if result, err := inspector.Doctor(ctx, opts); err != nil {
		reportError("storage", err)
	} else {
		fmt.Fprintf(stdout, "storage=healthy migration=%d database=%s\n", result.Migration, terminalSafe(result.Database))
	}
	options := setupflow.Options{Workspace: workspace}
	var shared setupflow.SharedPlan
	composite, ok := setup.(multiSetupRuntime)
	if !ok {
		reportError("shared", setupflow.ErrPrerequisite)
	} else if sharedRuntime := composite.Shared(options); sharedRuntime == nil {
		reportError("shared", setupflow.ErrPrerequisite)
	} else {
		shared, err = sharedRuntime.Status(ctx)
		if err != nil {
			reportError("shared", err)
		} else {
			fmt.Fprintf(stdout, "launcher=%s\nskills=%s\n", terminalSafe(string(shared.Launcher.State)), terminalSafe(string(shared.Skills.State)))
			attention = attention || shared.Launcher.State != selfinstall.StateInstalled || shared.Skills.State != skills.StateInstalled
		}
	}
	inspectProvider := func(name string, adapter setupflow.ProviderRuntime) {
		observedRuntime := "unobserved"
		if name == "opencode" {
			defer func() { fmt.Fprintf(stdout, "opencode.runtime=%s\n", terminalSafe(observedRuntime)) }()
		}
		if err := ctx.Err(); err != nil {
			reportError(name+".artifacts", err)
			return
		}
		if adapter == nil {
			reportError(name+".artifacts", setupflow.ErrPrerequisite)
			return
		}
		plan, err := adapter.Status(ctx, shared)
		if err != nil {
			reportError(name+".artifacts", err)
			return
		}
		fmt.Fprintf(stdout, "%s.artifacts=%s count=%d\n", name, terminalSafe(string(plan.State)), plan.ArtifactCount)
		attention = attention || !plan.Ready || plan.State != integration.StateInstalled
		if plan.Blocker != "" {
			fmt.Fprintf(stdout, "%s.detail=%s\n", name, terminalSafe(plan.Blocker))
		}
		if name == "opencode" {
			if plan.Handshake.Status != "" {
				observedRuntime = string(plan.Handshake.Status)
			}
			attention = attention || !plan.Handshake.OK || plan.Handshake.Status != integration.HandshakeHealthy
		}
	}
	if ok {
		inspectProvider("opencode", composite.OpenCodeProvider(options, nil))
	} else {
		inspectProvider("opencode", nil)
	}
	codexOptions, err := codexSetupOptions(integration.Options{}, "")
	if err != nil {
		reportError("codex.artifacts", err)
	} else if codex == nil {
		inspectProvider("codex", nil)
	} else {
		inspectProvider("codex", setupflow.NewIntegrationProvider(setupflow.ProviderCodex, codex, codexOptions))
	}
	fmt.Fprintln(stdout, "codex.runtime=unobserved")
	piOptions, err := resolvePiSetupOptions("", "", "")
	if err != nil {
		reportError("pi.artifacts", err)
	} else if factory, ok := setup.(piSetupRuntime); ok {
		inspectProvider("pi", factory.PiProvider(piOptions))
	} else {
		inspectProvider("pi", nil)
	}
	fmt.Fprintln(stdout, "pi.runtime=unobserved")
	workers := "unobserved"
	if runtime.GOOS == "windows" {
		workers = "unsupported"
	}
	fmt.Fprintf(stdout, "pi.workers=%s\nmodel_access=unobserved\n", workers)
	if err := ctx.Err(); err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	if attention {
		fmt.Fprintln(stdout, "doctor=attention")
		return 1
	}
	fmt.Fprintln(stdout, "doctor=incomplete")
	return 0
}

// Resolve the same installed Pi roots for setup status and doctor. This performs
// no acquisition, account inspection, or filesystem mutation.
func resolvePiSetupOptions(agent, root, release string) (pi.Options, error) {
	if agent == "" {
		agent = os.Getenv("PI_CODING_AGENT_DIR")
		if agent == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return pi.Options{}, err
			}
			agent = filepath.Join(home, ".pi", "agent")
		}
	}
	if root == "" {
		root = filepath.Join(agent, "vgxness-managed")
	}
	options := pi.Options{AgentDir: agent, InstallRoot: root, ReleaseDir: release}
	for _, path := range []*string{&options.AgentDir, &options.InstallRoot, &options.ReleaseDir} {
		if *path == "" {
			continue
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return pi.Options{}, err
		}
		*path = filepath.Clean(absolute)
	}
	return options, nil
}

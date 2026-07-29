package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
)

func runIntegration(ctx context.Context, args []string, stdout, stderr io.Writer, runtime integration.Runtime) int {
	if len(args) < 2 || args[0] != "opencode" || !integrationAction(args[1]) {
		fmt.Fprintln(stderr, "usage: vgxness integrate opencode <preview|install|status|uninstall> [--config-dir PATH] [--model-plan low|medium|high] [--model-efficient provider/model] [--model-balanced provider/model] [--model-frontier provider/model]")
		return 2
	}
	action := args[1]
	flags := flag.NewFlagSet("integrate opencode "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options integration.Options
	flags.StringVar(&options.ConfigDir, "config-dir", "", "OpenCode global config directory")
	flags.StringVar(&options.Model, "model", "", "deprecated compatibility flag; the native integration does not use a child model")
	flags.Var((*planFlag)(&options.ModelPlan), "model-plan", "active model plan: low, medium, or high")
	flags.StringVar(&options.ModelEfficient, "model-efficient", "", "exact provider/model for the efficient slot")
	flags.StringVar(&options.ModelBalanced, "model-balanced", "", "exact provider/model for the balanced slot")
	flags.StringVar(&options.ModelFrontier, "model-frontier", "", "exact provider/model for the frontier slot")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid integration arguments")
		return 2
	}
	if runtime == nil {
		fmt.Fprintln(stderr, "operational: integration runtime is unavailable")
		return 1
	}
	var result integration.Result
	var err error
	switch action {
	case "preview":
		result, err = runtime.Preview(ctx, options)
	case "install":
		result, err = runtime.Install(ctx, options)
	case "status":
		result, err = runtime.Status(ctx, options)
	case "uninstall":
		result, err = runtime.Uninstall(ctx, options)
	}
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	var output strings.Builder
	fmt.Fprintf(&output, "provider=%s\nstate=%s\nprojection=native+sdd-storage\nmanaged_artifacts=%d\npath=%s\nartifact_sha256=%s\nstorage_plugin=%s\nstorage_plugin_sha256=%s\nchanged=%t\n", terminalSafe(result.Provider), result.State, result.ArtifactCount, terminalSafe(result.Path), terminalSafe(result.ArtifactSHA256), terminalSafe(result.ToolPath), terminalSafe(result.ToolSHA256), result.Changed)
	fmt.Fprintf(&output, "model_plan=%s\nmodel_provider=%s\nmodel_efficient=%s\nmodel_balanced=%s\nmodel_frontier=%s\nmodel_manifest=%s\nmodel_manifest_sha256=%s\nrestart_required=%t\n", result.ModelPlan, terminalSafe(result.ModelProvider), terminalSafe(result.ModelEfficient), terminalSafe(result.ModelBalanced), terminalSafe(result.ModelFrontier), terminalSafe(result.ManifestPath), result.ManifestSHA256, result.RestartRequired)
	if result.BackupPath != "" {
		fmt.Fprintf(&output, "backup=%s\n", terminalSafe(result.BackupPath))
	}
	if result.ToolBackupPath != "" {
		fmt.Fprintf(&output, "storage_plugin_backup=%s\n", terminalSafe(result.ToolBackupPath))
	}
	_, _ = io.WriteString(stdout, output.String())
	return 0
}

type planFlag string

func (value *planFlag) String() string { return string(*value) }

func (value *planFlag) Set(input string) error {
	if input != "low" && input != "medium" && input != "high" {
		return integration.ErrInvalid
	}
	*value = planFlag(input)
	return nil
}

func integrationAction(value string) bool {
	switch value {
	case "preview", "install", "status", "uninstall":
		return true
	default:
		return false
	}
}

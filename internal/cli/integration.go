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
		fmt.Fprintln(stderr, "usage: vgxness integrate opencode <preview|install|status|uninstall> [--model PROVIDER/MODEL] [--config-dir PATH]")
		return 2
	}
	action := args[1]
	flags := flag.NewFlagSet("integrate opencode "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options integration.Options
	flags.StringVar(&options.ConfigDir, "config-dir", "", "OpenCode global config directory")
	flags.StringVar(&options.Model, "model", "", "explicit OpenCode execution model as provider/model")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid integration arguments")
		return 2
	}
	if (action == "preview" || action == "install") && strings.TrimSpace(options.Model) == "" {
		fmt.Fprintln(stderr, "invalid: --model provider/model is required for preview and install")
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
	fmt.Fprintf(&output, "provider=%s\nstate=%s\nbridge=%s\npath=%s\nartifact_sha256=%s\nchanged=%t\n", terminalSafe(result.Provider), result.State, result.Bridge, terminalSafe(result.Path), terminalSafe(result.ArtifactSHA256), result.Changed)
	fmt.Fprintf(&output, "tool_path=%s\ntool_sha256=%s\n", terminalSafe(result.ToolPath), terminalSafe(result.ToolSHA256))
	fmt.Fprintf(&output, "model=%s\n", terminalSafe(result.Model))
	if result.BackupPath != "" {
		fmt.Fprintf(&output, "backup=%s\n", terminalSafe(result.BackupPath))
	}
	if result.ToolBackupPath != "" {
		fmt.Fprintf(&output, "tool_backup=%s\n", terminalSafe(result.ToolBackupPath))
	}
	_, _ = io.WriteString(stdout, output.String())
	return 0
}

func integrationAction(value string) bool {
	switch value {
	case "preview", "install", "status", "uninstall":
		return true
	default:
		return false
	}
}

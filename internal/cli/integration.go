package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
	opencodeprovider "github.com/vgxness/vgxness/internal/providers/opencode"
)

func runIntegration(ctx context.Context, args []string, stdout, stderr io.Writer, opencode, codex integration.Runtime) int {
	if len(args) < 2 || !integrationProvider(args[0]) || !integrationAction(args[0], args[1]) {
		fmt.Fprintln(stderr, "usage: vgxness integrate <opencode|codex> <action> [--config-dir PATH]")
		return 2
	}
	provider := args[0]
	action := args[1]
	flags := flag.NewFlagSet("integrate "+provider+" "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options integration.Options
	var models agentModelFlags
	var repairOldExecutable, repairEntrySHA256 string
	flags.StringVar(&options.ConfigDir, "config-dir", "", provider+" config directory")
	if provider == "opencode" {
		models.register(flags, "")
		flags.StringVar(&options.ModelEfficient, "model-efficient", "", "exact provider/model for the efficient slot")
		flags.StringVar(&options.ModelBalanced, "model-balanced", "", "exact provider/model for the balanced slot")
		flags.StringVar(&options.ModelFrontier, "model-frontier", "", "exact provider/model for the frontier slot")
		flags.StringVar(&repairOldExecutable, "old-executable", "", "absolute obsolete managed MCP executable")
		flags.StringVar(&repairEntrySHA256, "expected-mcp-sha256", "", "SHA-256 of the canonical obsolete MCP entry")
	}
	flags.Var((*planFlag)(&options.ModelPlan), "model-plan", "active model plan: low, medium, high, or ultra")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid integration arguments")
		fmt.Fprintln(stderr, integrationUsage(provider))
		return 2
	}
	if provider == "opencode" {
		if options.ModelPlan != "" || hasSetupSlotRef(options) {
			fmt.Fprintln(stderr, "invalid: OpenCode uses --model-mode single|per-agent, not plans or slots")
			return 2
		}
		c, err := models.config()
		if err != nil {
			fmt.Fprintln(stderr, "invalid model selection")
			return 2
		}
		if c != nil {
			a, err := opencodeprovider.ExplicitModels(*c)
			if err != nil {
				fmt.Fprintln(stderr, "invalid model selection")
				return 2
			}
			options.ModelAssignments = &a
		}
	}
	if provider == "opencode" && action != "repair-mcp-preview" && action != "repair-mcp" {
		invalidRepairFlag := false
		flags.Visit(func(flag *flag.Flag) {
			if flag.Name == "old-executable" || flag.Name == "expected-mcp-sha256" {
				invalidRepairFlag = true
			}
		})
		if invalidRepairFlag {
			fmt.Fprintln(stderr, "invalid integration arguments")
			fmt.Fprintln(stderr, integrationUsage(provider))
			return 2
		}
	}
	if provider == "codex" && options.ConfigDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(stderr, "operational: resolve home directory")
			return 1
		}
		options.HomeDir = home
	}
	runtime := opencode
	if provider == "codex" {
		runtime = codex
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
	case "reinstall":
		managed, ok := runtime.(integration.ManagedRuntime)
		if !ok {
			fmt.Fprintln(stderr, "operational: integration runtime is unavailable")
			return 1
		}
		result, err = managed.Reinstall(ctx, options)
	case "uninstall":
		result, err = runtime.Uninstall(ctx, options)
	case "repair-mcp-preview", "repair-mcp":
		repair, ok := runtime.(integration.MCPRepairRuntime)
		if !ok || provider != "opencode" {
			fmt.Fprintln(stderr, "operational: MCP repair runtime is unavailable")
			return 1
		}
		proof := integration.MCPRepairProof{OldExecutable: repairOldExecutable, ExpectedEntrySHA256: repairEntrySHA256}
		if action == "repair-mcp-preview" {
			result, err = repair.PreviewMCPRepair(ctx, options, proof)
		} else {
			result, err = repair.RepairMCP(ctx, options, proof)
		}
	}
	if err != nil {
		if result.BackupPath != "" {
			fmt.Fprintf(stderr, "recovery_backup=%s\n", terminalSafe(result.BackupPath))
		}
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	var output strings.Builder
	fmt.Fprintf(&output, "provider=%s\nstate=%s\nprojection=native+memory\nmanaged_artifacts=%d\npath=%s\nartifact_sha256=%s\nchanged=%t\n", terminalSafe(result.Provider), result.State, result.ArtifactCount, terminalSafe(result.Path), terminalSafe(result.ArtifactSHA256), result.Changed)
	fmt.Fprintf(&output, "model_plan=%s\nmodel_provider=%s\nmodel_efficient=%s\nmodel_balanced=%s\nmodel_frontier=%s\nmodel_manifest=%s\nmodel_manifest_sha256=%s\nrestart_required=%t\n", result.ModelPlan, terminalSafe(result.ModelProvider), terminalSafe(result.ModelEfficient), terminalSafe(result.ModelBalanced), terminalSafe(result.ModelFrontier), terminalSafe(result.ManifestPath), result.ManifestSHA256, result.RestartRequired)
	fmt.Fprintf(&output, "directory_durability=%s\n", terminalSafe(result.DirectoryDurability))
	if result.RetainedPredecessorCount != 0 {
		fmt.Fprintf(&output, "retained_predecessors=%d\nretained_predecessor_location=%s\n", result.RetainedPredecessorCount, terminalSafe(result.RetainedPredecessorPath))
	}
	fmt.Fprintf(&output, "default_agent=%s\ndefault_agent_config=%s\n", terminalSafe(result.DefaultAgent), terminalSafe(result.DefaultAgentPath))
	if result.BackupPath != "" {
		fmt.Fprintf(&output, "backup=%s\n", terminalSafe(result.BackupPath))
	}
	if result.MCPRepairOldExecutable != "" {
		fmt.Fprintf(&output, "mcp_repair_old_executable=%s\nmcp_repair_entry_sha256=%s\nmcp_repair_mode=%s\n", terminalSafe(result.MCPRepairOldExecutable), result.MCPRepairEntrySHA256, terminalSafe(result.MCPRepairMode))
	}
	_, _ = io.WriteString(stdout, output.String())
	return 0
}

func integrationUsage(provider string) string {
	if provider == "opencode" {
		return "usage: vgxness integrate opencode <preview|install|status|uninstall|repair-mcp-preview|repair-mcp> [--config-dir PATH] [--old-executable ABSOLUTE_PATH --expected-mcp-sha256 SHA256] [--model-mode single|per-agent] [--model PROVIDER/MODEL | --agent-model ROLE=PROVIDER/MODEL ...] [--model-effort EFFORT]"
	}
	return "usage: vgxness integrate codex <preview|install|status|reinstall|uninstall> [--config-dir PATH] [--model-plan low|medium|high|ultra]"
}

type planFlag string

func (value *planFlag) String() string { return string(*value) }

func (value *planFlag) Set(input string) error {
	if input != "low" && input != "medium" && input != "high" && input != "ultra" {
		return integration.ErrInvalid
	}
	*value = planFlag(input)
	return nil
}

func integrationProvider(value string) bool { return value == "opencode" || value == "codex" }

func integrationAction(provider, value string) bool {
	switch value {
	case "preview", "install", "status", "uninstall":
		return true
	case "repair-mcp-preview", "repair-mcp":
		return provider == "opencode"
	case "reinstall":
		return provider == "codex"
	default:
		return false
	}
}

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelcatalog"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
)

type multiSetupRuntime interface {
	setupflow.Runtime
	Shared(setupflow.Options) setupflow.SharedRuntime
	OpenCodeProvider(setupflow.Options, setupflow.PreviewIntegrationFactory) setupflow.ProviderRuntime
}

func runSetup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime setupflow.Runtime, providers ...integration.Runtime) int {
	if len(providers) == 1 && len(args) > 0 && (args[0] == "opencode" || args[0] == "codex" || args[0] == "all") {
		return runMultiSetup(ctx, args, stdin, stdout, stderr, runtime, providers[0])
	}
	return runOpenCodeSetup(ctx, args, stdin, stdout, stderr, runtime)
}

func runOpenCodeSetup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime setupflow.Runtime) int {
	if len(args) == 0 || args[0] != "opencode" {
		fmt.Fprintln(stderr, "usage: vgxness setup opencode [--preview|--status] [--yes] [--workspace PATH] [--bin-dir PATH] [--data-dir PATH] [--config-dir PATH] [--model-plan low|medium|high|ultra] [--model-efficient PROVIDER/MODEL --model-efficient-effort EFFORT] [--model-balanced PROVIDER/MODEL --model-balanced-effort EFFORT] [--model-frontier PROVIDER/MODEL --model-frontier-effort EFFORT]")
		return 2
	}
	flags := flag.NewFlagSet("setup opencode", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var (
		preview, status, yes bool
		workspace            string
		deprecatedModel      string
		options              setupflow.Options
	)
	flags.BoolVar(&preview, "preview", false, "explain the complete plan without writing")
	flags.BoolVar(&status, "status", false, "inspect the complete setup without writing")
	flags.BoolVar(&yes, "yes", false, "approve the explained plan non-interactively")
	flags.StringVar(&workspace, "workspace", "", "workspace used for the live OpenCode handshake")
	flags.StringVar(&deprecatedModel, "model", "", "deprecated compatibility flag; the native integration does not use a child model")
	flags.Var((*planFlag)(&options.Integration.ModelPlan), "model-plan", "active model plan: low, medium, high, or ultra")
	flags.StringVar(&options.Integration.ModelEfficient, "model-efficient", "", "exact provider/model for the efficient slot")
	flags.StringVar(&options.Integration.ModelBalanced, "model-balanced", "", "exact provider/model for the balanced slot")
	flags.StringVar(&options.Integration.ModelFrontier, "model-frontier", "", "exact provider/model for the frontier slot")
	flags.Var(effortFlag{target: &options.Integration.ModelEfficientEffort}, "model-efficient-effort", "effort for the efficient mixed slot")
	flags.Var(effortFlag{target: &options.Integration.ModelBalancedEffort}, "model-balanced-effort", "effort for the balanced mixed slot")
	flags.Var(effortFlag{target: &options.Integration.ModelFrontierEffort}, "model-frontier-effort", "effort for the frontier mixed slot")
	flags.StringVar(&options.SelfInstall.BinDir, "bin-dir", "", "stable launcher directory")
	flags.StringVar(&options.SelfInstall.DataDir, "data-dir", "", "version data directory")
	flags.StringVar(&options.Integration.ConfigDir, "config-dir", "", "OpenCode configuration directory")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || preview && status || yes && (preview || status) {
		fmt.Fprintln(stderr, "invalid setup arguments")
		return 2
	}
	if runtime == nil {
		fmt.Fprintln(stderr, "operational: setup runtime is unavailable")
		return 1
	}
	if hasSetupSlotRef(options.Integration) || hasSetupSlotEffort(options.Integration) {
		if options.Integration.ModelEfficient == "" || options.Integration.ModelBalanced == "" || options.Integration.ModelFrontier == "" || !validSetupModelReference(options.Integration.ModelEfficient) || !validSetupModelReference(options.Integration.ModelBalanced) || !validSetupModelReference(options.Integration.ModelFrontier) {
			fmt.Fprintln(stderr, "invalid: model slots must be valid provider/model references")
			return 2
		}
		mixed := modelProvider(options.Integration.ModelEfficient) != modelProvider(options.Integration.ModelBalanced) || modelProvider(options.Integration.ModelEfficient) != modelProvider(options.Integration.ModelFrontier)
		if mixed && (options.Integration.ModelEfficientEffort == "" || options.Integration.ModelBalancedEffort == "" || options.Integration.ModelFrontierEffort == "") {
			fmt.Fprintln(stderr, "invalid: mixed model slots require all refs and efforts")
			return 2
		}
		if !mixed && hasSetupSlotEffort(options.Integration) {
			fmt.Fprintln(stderr, "invalid: per-slot efforts require mixed providers")
			return 2
		}
	}
	if workspace == "" {
		current, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "operational: current workspace is unavailable")
			return 1
		}
		workspace = current
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: workspace is invalid")
		return 2
	}
	options.Workspace = filepath.Clean(absWorkspace)

	if status {
		plan, statusErr := runtime.Status(ctx, options)
		if statusErr != nil {
			code, message := failure(statusErr)
			fmt.Fprintln(stderr, message)
			return code
		}
		renderSetupStatus(stdout, plan, options.Workspace)
		if !plan.Ready {
			return 1
		}
		return 0
	}
	plan, planErr := runtime.Plan(ctx, options)
	if planErr != nil {
		code, message := failure(planErr)
		fmt.Fprintln(stderr, message)
		return code
	}
	renderSetupPlan(stdout, plan, options.Workspace)
	if !plan.Ready {
		fmt.Fprintf(stdout, "\nResultado: bloqueado sin cambios. %s\n", terminalSafe(plan.Blocker))
		return 1
	}
	if preview {
		fmt.Fprintln(stdout, "\nResultado: preview completo; no se modificó ningún archivo.")
		return 0
	}
	if yes {
		fmt.Fprintln(stdout, "\nConfirmación: aceptada mediante --yes.")
	} else {
		fmt.Fprint(stdout, "\n¿Aplicar exactamente este plan? [s/N]: ")
		approved, approvalErr := setupApproval(stdin)
		if approvalErr != nil {
			fmt.Fprintln(stderr, "invalid: confirmation must be s/si/sí or n/no")
			return 2
		}
		if !approved {
			fmt.Fprintln(stdout, "Resultado: cancelado por el usuario; no se modificó ningún archivo.")
			return 0
		}
		fmt.Fprintln(stdout, "Confirmación: aceptada.")
	}
	fmt.Fprintln(stdout, "\nAplicando el plan aprobado y verificando cada resultado...")
	options.ExpectedPlanDigest = plan.Digest
	result, applyErr := runtime.Apply(ctx, options)
	if applyErr != nil {
		if result.Recovery != "" {
			fmt.Fprintf(stdout, "Recuperación: %s\n", terminalSafe(result.Recovery))
		}
		code, message := failure(applyErr)
		fmt.Fprintln(stderr, message)
		return code
	}
	fmt.Fprintf(stdout, "Paso 2: launcher verificado en %s\n", terminalSafe(result.SelfInstall.LauncherPath))
	fmt.Fprintln(stdout, "Paso 3: retiro verificado: sólo bytes v1-v10 reconocidos de vgxness.ts y vgxness-autonomous-stacked-pr.")
	fmt.Fprintf(stdout, "Paso 4: %d artefactos del proveedor verificados en %s\n", result.Integration.ArtifactCount, terminalSafe(result.Integration.ManifestPath))
	fmt.Fprintln(stdout, "Perfil de modelos aplicado:")
	renderModelSlots(stdout, result.Plan.Integration)
	if note := directoryDurabilityNote(result.Integration.DirectoryDurability); note != "" {
		fmt.Fprintln(stdout, note)
	}
	if result.Integration.RetainedPredecessorCount != 0 {
		fmt.Fprintf(stdout, "Recuperación retenida: %d anclas en %s\n", result.Integration.RetainedPredecessorCount, terminalSafe(result.Integration.RetainedPredecessorPath))
	}
	fmt.Fprintf(stdout, "Paso 5: catálogo global de %d archivos skills-creator + git-delivery + cross-platform + installer-lifecycle + agent-evaluation + ci-triage + security-boundary + documentation-strategy + product-requirements + software-architecture-docs + user-documentation + api-documentation + quality-test-documentation + operations-runbooks + governance-compliance-docs + release-lifecycle-docs + end-to-end-testing + memory-sync + sdd-lifecycle verificado en %s\n", result.Plan.Skills.FileCount, terminalSafe(result.Plan.Skills.Path))
	fmt.Fprintf(stdout, "Paso 6: handshake OpenCode=%s workspace=%s\n", terminalSafe(result.Handshake.Status.String()), terminalSafe(options.Workspace))
	if result.Integration.RetainedPredecessorCount == 0 {
		fmt.Fprintln(stdout, "Paso 7: no fue necesaria recuperación.")
	} else {
		fmt.Fprintln(stdout, "Paso 7: se retuvo evidencia de recuperación; no se eliminó automáticamente.")
	}
	fmt.Fprintf(stdout, "\nResultado: configuración completa; changed=%t. Reinicia OpenCode para cargar vgxness-manager como agente predeterminado.\n", result.Changed)
	return 0
}

func runMultiSetup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime setupflow.Runtime, codex integration.Runtime) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: vgxness setup <opencode|codex|all> [--preview|--status] [--yes] [--workspace PATH] [--bin-dir PATH] [--data-dir PATH] [--config-dir PATH] [--codex-home PATH] [--model-plan low|medium|high|ultra]")
		return 2
	}
	providers, ok := setupProviders(args[0])
	if !ok {
		fmt.Fprintln(stderr, "usage: vgxness setup <opencode|codex|all> [--preview|--status] [--yes] [--workspace PATH] [--bin-dir PATH] [--data-dir PATH] [--config-dir PATH] [--codex-home PATH] [--model-plan low|medium|high|ultra]")
		return 2
	}
	flags := flag.NewFlagSet("setup "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var preview, status, yes bool
	var workspace string
	var codexHome string
	var deprecatedModel string
	var options setupflow.Options
	flags.BoolVar(&preview, "preview", false, "explain the complete plan without writing")
	flags.BoolVar(&status, "status", false, "inspect the complete setup without writing")
	flags.BoolVar(&yes, "yes", false, "approve the explained plan non-interactively")
	flags.StringVar(&workspace, "workspace", "", "workspace used for the OpenCode handshake")
	flags.StringVar(&deprecatedModel, "model", "", "deprecated compatibility flag; the native integration does not use a child model")
	flags.Var((*planFlag)(&options.Integration.ModelPlan), "model-plan", "active model plan: low, medium, high, or ultra")
	flags.StringVar(&options.Integration.ModelEfficient, "model-efficient", "", "exact provider/model for the efficient slot")
	flags.StringVar(&options.Integration.ModelBalanced, "model-balanced", "", "exact provider/model for the balanced slot")
	flags.StringVar(&options.Integration.ModelFrontier, "model-frontier", "", "exact provider/model for the frontier slot")
	flags.Var(effortFlag{target: &options.Integration.ModelEfficientEffort}, "model-efficient-effort", "effort for the efficient mixed slot")
	flags.Var(effortFlag{target: &options.Integration.ModelBalancedEffort}, "model-balanced-effort", "effort for the balanced mixed slot")
	flags.Var(effortFlag{target: &options.Integration.ModelFrontierEffort}, "model-frontier-effort", "effort for the frontier mixed slot")
	flags.StringVar(&options.SelfInstall.BinDir, "bin-dir", "", "stable launcher directory")
	flags.StringVar(&options.SelfInstall.DataDir, "data-dir", "", "version data directory")
	flags.StringVar(&options.Integration.ConfigDir, "config-dir", "", "OpenCode configuration directory")
	flags.StringVar(&codexHome, "codex-home", "", "Codex home directory")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || preview && status || yes && (preview || status) {
		fmt.Fprintln(stderr, "invalid setup arguments")
		return 2
	}
	if !includesOpenCode(providers) && (hasSetupSlotRef(options.Integration) || hasSetupSlotEffort(options.Integration)) {
		fmt.Fprintln(stderr, "invalid: model slots apply only to OpenCode")
		return 2
	}
	if !includesOpenCode(providers) && options.Integration.ConfigDir != "" {
		fmt.Fprintln(stderr, "invalid: --config-dir applies only to OpenCode")
		return 2
	}
	if !includesCodex(providers) && codexHome != "" {
		fmt.Fprintln(stderr, "invalid: --codex-home applies only to Codex")
		return 2
	}
	if includesOpenCode(providers) && (hasSetupSlotRef(options.Integration) || hasSetupSlotEffort(options.Integration)) {
		if options.Integration.ModelEfficient == "" || options.Integration.ModelBalanced == "" || options.Integration.ModelFrontier == "" || !validSetupModelReference(options.Integration.ModelEfficient) || !validSetupModelReference(options.Integration.ModelBalanced) || !validSetupModelReference(options.Integration.ModelFrontier) {
			fmt.Fprintln(stderr, "invalid: model slots must be valid provider/model references")
			return 2
		}
		mixed := modelProvider(options.Integration.ModelEfficient) != modelProvider(options.Integration.ModelBalanced) || modelProvider(options.Integration.ModelEfficient) != modelProvider(options.Integration.ModelFrontier)
		if mixed && (options.Integration.ModelEfficientEffort == "" || options.Integration.ModelBalancedEffort == "" || options.Integration.ModelFrontierEffort == "") {
			fmt.Fprintln(stderr, "invalid: mixed model slots require all refs and efforts")
			return 2
		}
		if !mixed && hasSetupSlotEffort(options.Integration) {
			fmt.Fprintln(stderr, "invalid: per-slot efforts require mixed providers")
			return 2
		}
	}
	if runtime == nil || (!includesOpenCode(providers) && codex == nil) || (len(providers) == 2 && codex == nil) {
		fmt.Fprintln(stderr, "operational: setup runtime is unavailable")
		return 1
	}
	composite, ok := runtime.(multiSetupRuntime)
	if !ok {
		fmt.Fprintln(stderr, "operational: multi-provider setup runtime is unavailable")
		return 1
	}
	if workspace == "" {
		current, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "operational: current workspace is unavailable")
			return 1
		}
		workspace = current
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: workspace is invalid")
		return 2
	}
	options.Workspace = filepath.Clean(absWorkspace)
	runtimes := make([]setupflow.ProviderRuntime, 0, len(providers))
	for _, provider := range providers {
		if provider == setupflow.ProviderOpenCode {
			runtimes = append(runtimes, composite.OpenCodeProvider(options, func(path string) (integration.Runtime, error) {
				return opencode.NewPreviewIntegration(path)
			}))
			continue
		}
		codexOptions, err := codexSetupOptions(options.Integration, codexHome)
		if err != nil {
			fmt.Fprintln(stderr, "operational: resolve Codex home directory")
			return 1
		}
		runtimes = append(runtimes, setupflow.NewIntegrationProvider(provider, codex, codexOptions))
	}
	multi := setupflow.NewMultiWithShared(composite.Shared(options), runtimes...)
	if status {
		plan, err := multi.Status(ctx, setupflow.MultiOptions{Providers: providers})
		if err != nil {
			code, message := failure(err)
			fmt.Fprintln(stderr, message)
			return code
		}
		renderMultiSetupPlan(stdout, plan)
		if openCodeOnly(providers) {
			renderOpenCodeCompatibilityStatus(stdout, plan)
		}
		if !plan.Ready {
			fmt.Fprintln(stdout, "Resultado: requires attention.")
			return 1
		}
		if includesCodex(providers) {
			fmt.Fprintln(stdout, "Resultado: managed artifacts and shared setup are healthy; Codex MCP/runtime health remains unobserved.")
		} else {
			fmt.Fprintln(stdout, "Resultado: configuration is healthy.")
		}
		return 0
	}
	plan, err := multi.Plan(ctx, setupflow.MultiOptions{Providers: providers})
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	renderMultiSetupPlan(stdout, plan)
	if openCodeOnly(providers) {
		renderOpenCodeCompatibilityPlan(stdout)
	}
	if !plan.Ready {
		fmt.Fprintf(stdout, "\nResultado: bloqueado sin cambios. %s\n", terminalSafe(plan.Blocker))
		return 1
	}
	if preview {
		fmt.Fprintln(stdout, "\nResultado: preview completo; no se modificó ningún archivo.")
		return 0
	}
	if yes {
		fmt.Fprintln(stdout, "\nConfirmación: aceptada mediante --yes.")
	} else {
		fmt.Fprint(stdout, "\n¿Aplicar exactamente este plan? [s/N]: ")
		approved, approvalErr := setupApproval(stdin)
		if approvalErr != nil {
			fmt.Fprintln(stderr, "invalid: confirmation must be s/si/sí or n/no")
			return 2
		}
		if !approved {
			fmt.Fprintln(stdout, "Resultado: cancelado por el usuario; no se modificó ningún archivo.")
			return 0
		}
	}
	result, err := multi.Apply(ctx, setupflow.MultiOptions{Providers: providers, ExpectedPlanDigest: plan.Digest})
	if err != nil {
		renderMultiSetupResult(stdout, result)
		if result.Shared.Recovery != "" {
			fmt.Fprintf(stdout, "Recovery: %s\n", terminalSafe(result.Shared.Recovery))
		}
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	fmt.Fprintf(stdout, "Resultado: configuración completa; changed=%t.\n", result.Plan.Changed)
	renderMultiSetupResult(stdout, result)
	if openCodeOnly(providers) {
		fmt.Fprintln(stdout, "Paso 6: handshake OpenCode=healthy")
		fmt.Fprintln(stdout, "Reinicia OpenCode para cargar vgxness-manager como agente predeterminado.")
	}
	return 0
}

func setupProviders(value string) ([]setupflow.Provider, bool) {
	switch value {
	case "opencode":
		return []setupflow.Provider{setupflow.ProviderOpenCode}, true
	case "codex":
		return []setupflow.Provider{setupflow.ProviderCodex}, true
	case "all":
		return []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderCodex}, true
	default:
		return nil, false
	}
}

func includesOpenCode(providers []setupflow.Provider) bool {
	for _, provider := range providers {
		if provider == setupflow.ProviderOpenCode {
			return true
		}
	}
	return false
}

func includesCodex(providers []setupflow.Provider) bool {
	for _, provider := range providers {
		if provider == setupflow.ProviderCodex {
			return true
		}
	}
	return false
}

func openCodeOnly(providers []setupflow.Provider) bool {
	return len(providers) == 1 && providers[0] == setupflow.ProviderOpenCode
}

func codexSetupOptions(options integration.Options, home string) (integration.Options, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil || home == "" {
			return integration.Options{}, fmt.Errorf("resolve Codex home directory")
		}
	}
	if !filepath.IsAbs(home) {
		return integration.Options{}, fmt.Errorf("resolve Codex home directory")
	}
	return integration.Options{HomeDir: home, ModelPlan: options.ModelPlan}, nil
}

func renderMultiSetupPlan(writer io.Writer, plan setupflow.MultiPlan) {
	fmt.Fprintln(writer, "VGXNESS · Setup unificado")
	fmt.Fprintf(writer, "Plan digest: %s\n", terminalSafe(plan.Digest))
	fmt.Fprintln(writer, "Preflight compartido: launcher y skills")
	for _, provider := range plan.Providers {
		fmt.Fprintf(writer, "Provider %s: ready=%t changed=%t state=%s artifacts=%d\n", provider.Provider, provider.Ready, provider.Changed, provider.State, provider.ArtifactCount)
		if provider.Provider == setupflow.ProviderCodex {
			renderCodexDiagnostics(writer, provider)
		}
	}
}

// renderCodexDiagnostics reports only the managed projection inspected by this
// command. Codex's MCP/runtime configuration is operator-owned and unobserved.
func renderCodexDiagnostics(writer io.Writer, provider setupflow.ProviderPlan) {
	fmt.Fprintf(writer, "Provider codex: managed-artifacts state=%s count=%d\n", provider.State, provider.ArtifactCount)
	fmt.Fprintln(writer, "Provider codex: MCP/runtime health=unobserved (operator-managed config.toml; not inspected)")
	fmt.Fprintln(writer, "Recovery codex: repair managed artifacts with `vgxness integrate codex reinstall --config-dir <same-codex-home>`; review config.toml and restart Codex yourself.")
}

func renderMultiSetupResult(writer io.Writer, result setupflow.MultiResult) {
	for _, provider := range result.Providers {
		fmt.Fprintf(writer, "Provider %s: verified=%t changed=%t skipped=%t\n", provider.Provider, provider.Verified, provider.Changed, provider.Skipped)
		if provider.Recovery != "" {
			fmt.Fprintf(writer, "Recovery %s: %s\n", provider.Provider, terminalSafe(provider.Recovery))
		}
	}
}

func renderOpenCodeCompatibilityPlan(writer io.Writer) {
	steps := []string{
		"validar preflight y destinos",
		"publicar y verificar el launcher",
		"retirar artefactos heredados reconocidos",
		"instalar y verificar la integración OpenCode",
		"publicar y verificar las skills globales",
		"verificar el handshake OpenCode",
		"reportar recuperación y activación",
	}
	for index, step := range steps {
		fmt.Fprintf(writer, "Paso %d de %d — %s.\n", index+1, len(steps), step)
	}
}

func renderOpenCodeCompatibilityStatus(writer io.Writer, plan setupflow.MultiPlan) {
	var provider setupflow.ProviderPlan
	for _, item := range plan.Providers {
		if item.Provider == setupflow.ProviderOpenCode {
			provider = item
			break
		}
	}
	fmt.Fprintf(writer, "Launcher: state=%s\n", plan.SharedPlan.Launcher.State)
	fmt.Fprintf(writer, "Handshake: ok=%t status=%s\n", provider.Handshake.OK, terminalSafe(provider.Handshake.Status.String()))
	fmt.Fprintf(writer, "Plan de modelos: %s provider=%s manifest=%s\n", provider.Integration.ModelPlan, terminalSafe(provider.Integration.ModelProvider), terminalSafe(provider.Integration.ManifestPath))
}

func renderSetupPlan(writer io.Writer, plan setupflow.Plan, workspace string) {
	fmt.Fprintln(writer, "VGXNESS · Wizard guiado de OpenCode")
	fmt.Fprintln(writer, "\nAntes de cambiar nada, este es el recorrido completo:")
	for _, step := range plan.Steps {
		change := "solo lectura"
		if step.Mutates {
			change = "escritura explícita"
		}
		fmt.Fprintf(writer, "\nPaso %d de %d — %s [%s]\n  %s\n", step.Number, len(plan.Steps), step.Title, change, step.Explanation)
	}
	fmt.Fprintln(writer, "\nResumen de destinos y estado detectado:")
	fmt.Fprintf(writer, "  Lifecycle action: %s\n", setupLifecycleAction(plan))
	fmt.Fprintf(writer, "  Plan digest: %s\n", terminalSafe(plan.Digest))
	fmt.Fprintf(writer, "  Launcher: %s (estado=%s)\n", terminalSafe(plan.SelfInstall.LauncherPath), plan.SelfInstall.State)
	fmt.Fprintf(writer, "  Versiones: %s\n", terminalSafe(plan.SelfInstall.DataDir))
	fmt.Fprintf(writer, "  Manager: %s (estado=%s)\n", terminalSafe(plan.Integration.Path), plan.Integration.State)
	fmt.Fprintf(writer, "  Skills globales: %s (estado=%s, archivos=%d)\n", terminalSafe(plan.Skills.Path), plan.Skills.State, plan.Skills.FileCount)
	fmt.Fprintln(writer, "  Proyección: manager con workspace de solo lectura y operaciones Git aprobadas por el usuario + Explore + general escritor + verificador + tres perfiles CARE de revisión + seis agentes SDD + MCP --full administrado como único runtime")
	fmt.Fprintf(writer, "  Artefactos administrados: %d\n", plan.Integration.ArtifactCount)
	fmt.Fprintf(writer, "  Plan de modelos: %s provider=%s\n", plan.Integration.ModelPlan, terminalSafe(plan.Integration.ModelProvider))
	renderModelSlots(writer, plan.Integration)
	fmt.Fprintf(writer, "  Manifest: %s\n", terminalSafe(plan.Integration.ManifestPath))
	if note := directoryDurabilityNote(plan.Integration.DirectoryDurability); note != "" {
		fmt.Fprintf(writer, "  %s\n", note)
	}
	if plan.Integration.RetainedPredecessorCount != 0 {
		fmt.Fprintf(writer, "  Recuperación retenida: count=%d location=%s\n", plan.Integration.RetainedPredecessorCount, terminalSafe(plan.Integration.RetainedPredecessorPath))
	}
	fmt.Fprintf(writer, "  Agente predeterminado: %s (%s)\n", terminalSafe(plan.Integration.DefaultAgent), terminalSafe(plan.Integration.DefaultAgentPath))
	fmt.Fprintf(writer, "  Workspace de verificación: %s\n", terminalSafe(workspace))
	fmt.Fprintln(writer, "  Activación: reinicia OpenCode después de aplicar o cambiar el plan.")
	fmt.Fprintln(writer, "\nLímites: no se editará PATH, no se descargará software, no se sobrescribirá contenido ajeno y no se habilitará shell arbitrario.")
	fmt.Fprintln(writer, "Recuperación: una actualización binaria puede volver a la versión anterior; una primera instalación o integración escrita se conserva y se reporta para evitar borrados silenciosos.")
}

func renderSetupStatus(writer io.Writer, plan setupflow.Plan, workspace string) {
	fmt.Fprintln(writer, "VGXNESS · Estado completo del setup OpenCode")
	fmt.Fprintf(writer, "Launcher: state=%s path=%s active_sha256=%s\n", plan.SelfInstall.State, terminalSafe(plan.SelfInstall.LauncherPath), plan.SelfInstall.ActiveSHA256)
	fmt.Fprintf(writer, "Integración: state=%s projection=agents+mcp-full artifacts=%d manager=%s\n", plan.Integration.State, plan.Integration.ArtifactCount, terminalSafe(plan.Integration.Path))
	fmt.Fprintf(writer, "Skills globales: state=%s path=%s files=%d\n", plan.Skills.State, terminalSafe(plan.Skills.Path), plan.Skills.FileCount)
	fmt.Fprintf(writer, "Plan de modelos: %s provider=%s manifest=%s\n", plan.Integration.ModelPlan, terminalSafe(plan.Integration.ModelProvider), terminalSafe(plan.Integration.ManifestPath))
	renderModelSlots(writer, plan.Integration)
	fmt.Fprintf(writer, "Agente predeterminado: %s config=%s\n", terminalSafe(plan.Integration.DefaultAgent), terminalSafe(plan.Integration.DefaultAgentPath))
	if note := directoryDurabilityNote(plan.Integration.DirectoryDurability); note != "" {
		fmt.Fprintln(writer, note)
	}
	if plan.Integration.RetainedPredecessorCount != 0 {
		fmt.Fprintf(writer, "Recuperación retenida: count=%d location=%s\n", plan.Integration.RetainedPredecessorCount, terminalSafe(plan.Integration.RetainedPredecessorPath))
	}
	fmt.Fprintf(writer, "Handshake: ok=%t status=%s workspace=%s\n", plan.Handshake.OK, terminalSafe(plan.Handshake.Status.String()), terminalSafe(workspace))
	if plan.Ready {
		fmt.Fprintln(writer, "Resultado: configuración completa y saludable.")
	} else {
		fmt.Fprintf(writer, "Resultado: requiere atención. %s\n", terminalSafe(plan.Blocker))
	}
}

type effortFlag struct{ target *sdd.Effort }

func (value effortFlag) String() string { return string(*value.target) }
func (value effortFlag) Set(input string) error {
	effort := sdd.Effort(input)
	if !effort.Valid() {
		return setupflow.ErrInvalid
	}
	*value.target = effort
	return nil
}

func renderModelSlots(writer io.Writer, result integration.Result) {
	if result.ModelSchemaVersion == 3 && result.ModelAssignments != nil {
		for _, assignment := range result.ModelAssignments {
			fmt.Fprintf(writer, "  Assignment artifact_key=%s provider=%s model=%s requested_effort=%s effective_effort=%s variant=%s source=%s availability=%s", terminalSafe(assignment.ArtifactKey), terminalSafe(assignment.Provider), terminalSafe(assignment.Model), terminalSafe(string(assignment.RequestedEffort)), terminalSafe(string(assignment.Effort)), terminalSafe(string(assignment.Variant)), terminalSafe(string(assignment.Source)), terminalSafe(string(assignment.Availability)))
			if assignment.Degradation.Degraded {
				fmt.Fprintf(writer, " degradation=%s", terminalSafe(assignment.Degradation.Reason))
			}
			fmt.Fprintln(writer)
		}
		return
	}
	for _, slot := range []struct {
		name, ref    string
		effort       sdd.Effort
		source       sdd.ModelSlotSource
		availability sdd.ModelSlotAvailability
	}{
		{"efficient", result.ModelEfficient, result.ModelEfficientEffort, result.ModelEfficientSource, result.ModelEfficientAvailability},
		{"balanced", result.ModelBalanced, result.ModelBalancedEffort, result.ModelBalancedSource, result.ModelBalancedAvailability},
		{"frontier", result.ModelFrontier, result.ModelFrontierEffort, result.ModelFrontierSource, result.ModelFrontierAvailability},
	} {
		line := fmt.Sprintf("  Slot %s: provider=%s ref=%s effort=%s source=%s availability=%s", slot.name, terminalSafe(modelProvider(slot.ref)), terminalSafe(slot.ref), terminalSafe(string(slot.effort)), terminalSafe(string(slot.source)), terminalSafe(string(slot.availability)))
		if len(line) <= 80 {
			fmt.Fprintln(writer, line)
			continue
		}
		fmt.Fprintf(writer, "  Slot %s:\n", slot.name)
		renderSetupField(writer, "    provider=", modelProvider(slot.ref))
		renderSetupField(writer, "    ref=", slot.ref)
		renderSetupField(writer, "    effort=", string(slot.effort))
		renderSetupField(writer, "    source=", string(slot.source))
		renderSetupField(writer, "    availability=", string(slot.availability))
	}
}

func renderSetupField(writer io.Writer, prefix, value string) {
	value = terminalSafe(value)
	continuation := strings.Repeat(" ", len(prefix))
	for {
		room := 80 - len(prefix)
		if len(value) <= room {
			fmt.Fprintln(writer, prefix+value)
			return
		}
		cut := room
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		fmt.Fprintln(writer, prefix+value[:cut])
		value, prefix = value[cut:], continuation
	}
}

func setupLifecycleAction(plan setupflow.Plan) string {
	if plan.SelfInstall.State == selfinstall.StateAbsent || plan.Integration.State == integration.StateAbsent || plan.Skills.State == skills.StateAbsent {
		return "install"
	}
	if plan.SelfInstall.State == selfinstall.StateInstalled && plan.Integration.State == integration.StateInstalled && plan.Skills.State == skills.StateInstalled &&
		!plan.SelfInstall.Changed && !plan.SelfInstall.UpdateAvailable && !plan.Integration.Changed && !plan.Integration.RestartRequired && !plan.Skills.Changed && !plan.Skills.UpdateNeeded {
		return "no-change"
	}
	return "update/reinstall"
}

func modelProvider(reference string) string {
	provider, _, _ := strings.Cut(reference, "/")
	return provider
}
func hasSetupSlotEffort(options integration.Options) bool {
	return options.ModelEfficientEffort != "" || options.ModelBalancedEffort != "" || options.ModelFrontierEffort != ""
}
func hasSetupSlotRef(options integration.Options) bool {
	return options.ModelEfficient != "" || options.ModelBalanced != "" || options.ModelFrontier != ""
}
func validSetupModelReference(reference string) bool {
	_, valid := modelcatalog.ValidReference(reference)
	return valid
}

func directoryDurabilityNote(value string) string {
	switch value {
	case "fsync":
		return "Durabilidad de directorio: fsync."
	case "file-sync-namespace-best-effort":
		return "Durabilidad de directorio: mejor esfuerzo; no se reclama persistencia de entradas tras pérdida de energía."
	default:
		return ""
	}
}

func setupApproval(reader io.Reader) (bool, error) {
	if reader == nil {
		return false, nil
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16), 64)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	switch answer {
	case "", "n", "no":
		return false, nil
	case "s", "si", "sí", "y", "yes":
		return true, nil
	default:
		return false, setupflow.ErrInvalid
	}
}

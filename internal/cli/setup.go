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

	"github.com/vgxness/vgxness/internal/agentmodels"
	"github.com/vgxness/vgxness/internal/buildinfo"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/piartifact"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/providers/pi"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

type multiSetupRuntime interface {
	setupflow.Runtime
	Shared(setupflow.Options) setupflow.SharedRuntime
	OpenCodeProvider(setupflow.Options, setupflow.PreviewIntegrationFactory) setupflow.ProviderRuntime
}

type piSetupRuntime interface {
	PiProvider(pi.Options) setupflow.ProviderRuntime
}

var acquirePiRelease = pi.AcquireRelease

// setupUsage lists only the flags runSetup accepts; the retired model-slot
// flags are still parsed so they can be rejected explicitly.
const setupUsage = "usage: vgxness setup <opencode|codex|pi|all> [--preview|--status] [--yes] [--workspace PATH] [--bin-dir PATH] [--data-dir PATH] [--config-dir PATH] [--codex-home PATH] [--model-plan low|medium|high|ultra] [--model-mode single|per-agent] [--model PROVIDER/MODEL] [--agent-model ROLE=PROVIDER/MODEL]... [--model-effort EFFORT] [--pi-model-mode single|per-agent] [--pi-model PROVIDER/MODEL] [--pi-agent-model ROLE=PROVIDER/MODEL]... [--pi-model-effort EFFORT] [--pi-release-dir PATH|--pi-release-version vSemVer] [--pi-agent-dir PATH] [--pi-root PATH]"

func runSetup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime setupflow.Runtime, codex integration.Runtime) (exitCode int) {
	if len(args) == 0 {
		fmt.Fprintln(stderr, setupUsage)
		return 2
	}
	providers, ok := setupProviders(args[0])
	if !ok {
		fmt.Fprintln(stderr, setupUsage)
		return 2
	}
	flags := flag.NewFlagSet("setup "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var preview, status, yes bool
	var workspace string
	var codexHome string
	var piRelease, piReleaseVersion, piAgent, piRoot string
	var modelFlags, piModelFlags agentModelFlags
	var options setupflow.Options
	flags.BoolVar(&preview, "preview", false, "explain the complete plan without writing")
	flags.BoolVar(&status, "status", false, "inspect the complete setup without writing")
	flags.BoolVar(&yes, "yes", false, "approve the explained plan non-interactively")
	flags.StringVar(&workspace, "workspace", "", "workspace used for the OpenCode handshake")
	modelFlags.register(flags, "")
	piModelFlags.register(flags, "pi-")
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
	flags.StringVar(&piRelease, "pi-release-dir", "", "local Pi release directory")
	flags.StringVar(&piReleaseVersion, "pi-release-version", "", "pinned Pi release v-prefixed SemVer")
	flags.StringVar(&piAgent, "pi-agent-dir", "", "Pi agent settings directory")
	flags.StringVar(&piRoot, "pi-root", "", "VGXNESS-managed Pi package root")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || preview && status || yes && (preview || status) {
		fmt.Fprintln(stderr, "invalid setup arguments")
		return 2
	}
	selection, selectionErr := modelFlags.config()
	piSelection, piSelectionErr := piModelFlags.config()
	if selectionErr != nil || piSelectionErr != nil {
		fmt.Fprintln(stderr, "invalid: provide single or complete per-agent model selection")
		return 2
	}
	if hasSetupSlotRef(options.Integration) || hasSetupSlotEffort(options.Integration) {
		fmt.Fprintln(stderr, "invalid: model slots are retired; use --model-mode single|per-agent")
		return 2
	}
	if options.Integration.ModelPlan != "" && !includesCodex(providers) {
		fmt.Fprintln(stderr, "invalid: --model-plan applies only to Codex")
		return 2
	}
	codexPlan := options.Integration.ModelPlan
	options.Integration.ModelPlan = ""
	if selection != nil {
		switch args[0] {
		case "opencode", "all":
			assignments, err := opencode.ExplicitModels(*selection)
			if err != nil {
				fmt.Fprintln(stderr, "invalid model selection")
				return 2
			}
			options.Integration.ModelAssignments = &assignments
		case "pi":
			if piSelection != nil {
				fmt.Fprintln(stderr, "invalid: duplicate Pi selection")
				return 2
			}
			piSelection = selection
		default:
			fmt.Fprintln(stderr, "invalid: Codex uses --model-plan")
			return 2
		}
	}
	if piSelection != nil && !includesPi(providers) {
		fmt.Fprintln(stderr, "invalid: Pi model selection requires Pi")
		return 2
	}
	piVersionProvided := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "pi-release-version" {
			piVersionProvided = true
		}
	})
	if includesPi(providers) && !piVersionProvided {
		piReleaseVersion = buildinfo.Version
	}
	if !includesOpenCode(providers) && options.Integration.ConfigDir != "" {
		fmt.Fprintln(stderr, "invalid: --config-dir applies only to OpenCode")
		return 2
	}
	if !includesCodex(providers) && codexHome != "" {
		fmt.Fprintln(stderr, "invalid: --codex-home applies only to Codex")
		return 2
	}
	if !includesPi(providers) && (piRelease != "" || piVersionProvided || piAgent != "" || piRoot != "") {
		fmt.Fprintln(stderr, "invalid: Pi paths apply only to Pi")
		return 2
	}
	if includesPi(providers) && piVersionProvided {
		if _, err := piartifact.Filename(piReleaseVersion); err != nil {
			fmt.Fprintln(stderr, "invalid: --pi-release-version must be strict v-prefixed SemVer")
			return 2
		}
	}
	if includesPi(providers) && piRelease != "" && piVersionProvided {
		fmt.Fprintln(stderr, "invalid: --pi-release-dir and --pi-release-version cannot be combined")
		return 2
	}
	if runtime == nil || (includesCodex(providers) && codex == nil) {
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
	if includesPi(providers) {
		if _, ok := runtime.(piSetupRuntime); !ok {
			fmt.Fprintln(stderr, "operational: Pi setup runtime is unavailable")
			return 1
		}
	}
	var acquiredCleanup func() error
	if includesPi(providers) && piRelease == "" && !preview && !status {
		if piReleaseVersion == "dev" || piReleaseVersion == "" {
			fmt.Fprintln(stderr, "invalid: --pi-release-version is required for development builds")
			return 2
		}
		if _, err := piartifact.Filename(piReleaseVersion); err != nil {
			fmt.Fprintln(stderr, "invalid: --pi-release-version is required for non-release builds")
			return 2
		}
		var acquireErr error
		piRelease, acquiredCleanup, acquireErr = acquirePiRelease(ctx, piReleaseVersion)
		if acquireErr != nil {
			fmt.Fprintf(stderr, "operational: acquire Pi release: %v\n", acquireErr)
			return 1
		}
		defer func() {
			if acquiredCleanup != nil {
				if err := acquiredCleanup(); err != nil {
					fmt.Fprintf(stderr, "recovery: acquired Pi release retained: %v\n", err)
					if exitCode == 0 {
						exitCode = 1
					}
				}
			}
		}()
	}
	if includesPi(providers) && piRelease == "" && preview {
		fmt.Fprintf(stdout, "Pi release: acquisition required; pinned version=%s\n", terminalSafe(piReleaseVersion))
	}
	runtimes := make([]setupflow.ProviderRuntime, 0, len(providers))
	for _, provider := range providers {
		if provider == setupflow.ProviderOpenCode {
			runtimes = append(runtimes, composite.OpenCodeProvider(options, func(path string) (integration.Runtime, error) {
				return opencode.NewPreviewIntegration(path)
			}))
			continue
		}
		if provider == setupflow.ProviderPi {
			factory, ok := runtime.(piSetupRuntime)
			if !ok {
				fmt.Fprintln(stderr, "operational: Pi setup runtime is unavailable")
				return 1
			}
			piOptions, pathErr := resolvePiSetupOptions(piAgent, piRoot, piRelease)
			piOptions.Models = piSelection
			if pathErr != nil {
				fmt.Fprintln(stderr, "invalid: Pi path is invalid")
				return 2
			}
			runtimes = append(runtimes, factory.PiProvider(piOptions))
			continue
		}
		codexOptions, err := codexSetupOptions(integration.Options{ModelPlan: codexPlan}, codexHome)
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
	case "pi":
		return []setupflow.Provider{setupflow.ProviderPi}, true
	case "all":
		return []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderCodex, setupflow.ProviderPi}, true
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

func includesPi(providers []setupflow.Provider) bool {
	for _, provider := range providers {
		if provider == setupflow.ProviderPi {
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
		if provider.Models != nil {
			fmt.Fprintf(writer, "Provider %s model mode: %s\n", provider.Provider, provider.Models.Mode)
			for _, role := range agentmodels.Roles {
				a := provider.Models.Assignments[role]
				fmt.Fprintf(writer, "  %s=%s effort=%s\n", role, terminalSafe(a.Model), a.Effort)
			}
		}
		if provider.Integration.ModelAssignments != nil {
			renderModelSlots(writer, provider.Integration)
		}
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

type effortFlag struct{ target *modelplan.Effort }

func (value effortFlag) String() string { return string(*value.target) }
func (value effortFlag) Set(input string) error {
	effort := modelplan.Effort(input)
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
		effort       modelplan.Effort
		source       modelplan.ModelSlotSource
		availability modelplan.ModelSlotAvailability
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

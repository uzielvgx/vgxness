package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/selfinstall"
	"github.com/vgxness/vgxness/internal/skills"
)

var (
	ErrInvalid      = errors.New("invalid setup request")
	ErrPrerequisite = errors.New("setup prerequisite is unavailable")
	ErrVerification = errors.New("setup verification failed")
)

type Step struct {
	Number      int
	Title       string
	Explanation string
	Mutates     bool
}

type Options struct {
	SelfInstall        selfinstall.Options
	Integration        integration.Options
	Skills             skills.Options
	Workspace          string
	ExpectedPlanDigest string
}

type Plan struct {
	Digest      string
	Provider    string
	Workspace   string
	Steps       []Step
	SelfInstall selfinstall.Result
	Integration integration.Result
	Skills      skills.Result
	Handshake   integration.Handshake
	Ready       bool
	Blocker     string
}

type Result struct {
	Plan        Plan
	SelfInstall selfinstall.Result
	Integration integration.Result
	Handshake   integration.Handshake
	Recovery    string
	Changed     bool
}

type Runtime interface {
	Plan(context.Context, Options) (Plan, error)
	Apply(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Plan, error)
}

type IntegrationFactory func(string) (integration.Runtime, error)

// PreviewIntegrationFactory binds a planned launcher without asserting its
// on-disk ownership. Publication is always performed through the managed
// factory after shared launcher verification.
type PreviewIntegrationFactory func(string) (integration.Runtime, error)

type Prober interface {
	Probe(context.Context, string) (integration.Handshake, error)
}

type Service struct {
	installer           selfinstall.Runtime
	preview             integration.Runtime
	integrations        IntegrationFactory
	prober              Prober
	managedIntegrations ManagedIntegrationFactory
	backups             BackupEngineFactory
	skills              skills.Runtime
}

func New(installer selfinstall.Runtime, preview integration.Runtime, integrations IntegrationFactory, prober Prober) *Service {
	return &Service{installer: installer, preview: preview, integrations: integrations, prober: prober}
}

type serviceShared struct {
	service *Service
	options Options
}

// Shared exposes the launcher and global-skills boundary for a composite setup
// without changing Service's established single-provider transaction.
func (service *Service) Shared(options Options) SharedRuntime {
	return serviceShared{service: service, options: options}
}

// OpenCodeProvider builds the composite OpenCode runtime. It keeps the
// pre-write handshake and planned launcher binding with the setup service.
func (service *Service) OpenCodeProvider(options Options, previews PreviewIntegrationFactory) ProviderRuntime {
	return &openCodeProvider{service: service, options: options, previews: previews}
}

type openCodeProvider struct {
	service  *Service
	options  Options
	previews PreviewIntegrationFactory
}

func (*openCodeProvider) Provider() Provider { return ProviderOpenCode }

func (provider *openCodeProvider) Plan(ctx context.Context, shared SharedPlan) (ProviderPlan, error) {
	plan := ProviderPlan{Provider: ProviderOpenCode}
	if provider == nil || provider.service == nil || provider.previews == nil || provider.service.managedIntegrations == nil || provider.service.prober == nil || provider.options.Workspace == "" || !shared.Ready || shared.Launcher.LauncherPath == "" {
		return plan, ErrPrerequisite
	}
	handshake, err := provider.service.prober.Probe(ctx, provider.options.Workspace)
	if err != nil {
		return plan, err
	}
	if !handshake.OK {
		plan.Blocker = "OpenCode is unavailable or unhealthy."
		return plan, nil
	}
	preview, err := provider.previews(shared.Launcher.LauncherPath)
	if err != nil {
		return plan, err
	}
	result, err := preview.Preview(ctx, provider.options.Integration)
	if err != nil {
		return plan, err
	}
	plan.Installed = result.State == integration.StateInstalled
	plan.Changed = result.Changed || !plan.Installed
	plan.ArtifactSHA256 = result.ArtifactSHA256
	plan.ArtifactCount = result.ArtifactCount
	plan.State = result.State
	if result.Provider != "" && result.Provider != string(ProviderOpenCode) {
		plan.Blocker = "provider preview identity does not match selection"
	} else if result.State != integration.StateAbsent && result.State != integration.StateInstalled && result.State != integration.StatePartial {
		plan.Blocker = "provider integration has drift or is unsafe to overwrite"
	} else if result.ArtifactSHA256 == "" {
		plan.Blocker = "provider preview lacks managed artifact identity"
	}
	plan.Ready = plan.Blocker == ""
	return plan, nil
}

func (provider *openCodeProvider) Apply(ctx context.Context, plan ProviderPlan, shared SharedResult) (ProviderResult, error) {
	result := ProviderResult{Provider: ProviderOpenCode}
	if provider == nil || provider.service == nil || provider.previews == nil || provider.service.managedIntegrations == nil || provider.service.prober == nil || !plan.Ready || !shared.Verified || shared.Launcher.State != selfinstall.StateInstalled || shared.Launcher.LauncherPath == "" {
		return result, ErrPrerequisite
	}
	preview, err := provider.previews(shared.Launcher.LauncherPath)
	if err != nil {
		return result, err
	}
	current, err := preview.Preview(ctx, provider.options.Integration)
	if err != nil {
		return result, err
	}
	if current.ArtifactSHA256 != plan.ArtifactSHA256 || current.ArtifactCount != plan.ArtifactCount {
		return result, fmt.Errorf("%w: opencode preview identity", ErrVerification)
	}
	managed, err := provider.service.managedIntegrations(shared.Launcher.LauncherPath)
	if err != nil {
		return result, err
	}
	handshake, err := provider.service.prober.Probe(ctx, provider.options.Workspace)
	if err != nil || !handshake.OK {
		return result, fmt.Errorf("%w: opencode pre-write handshake", ErrVerification)
	}
	installed, err := managed.Install(ctx, provider.options.Integration)
	result.Changed = installed.Changed
	if err != nil {
		return result, err
	}
	status, err := managed.Status(ctx, provider.options.Integration)
	if err != nil {
		return result, err
	}
	if status.Provider != string(ProviderOpenCode) || status.State != integration.StateInstalled || status.ArtifactSHA256 != plan.ArtifactSHA256 || status.ArtifactCount != plan.ArtifactCount {
		return result, fmt.Errorf("%w: opencode integration identity", ErrVerification)
	}
	handshake, err = provider.service.prober.Probe(ctx, provider.options.Workspace)
	if err != nil || !handshake.OK {
		return result, fmt.Errorf("%w: opencode handshake", ErrVerification)
	}
	result.Verified = true
	return result, nil
}

func (shared serviceShared) Plan(ctx context.Context) (SharedPlan, error) {
	if shared.service == nil || shared.service.installer == nil || shared.options.Workspace == "" {
		return SharedPlan{}, ErrInvalid
	}
	launcher, err := shared.service.installer.Preview(ctx, shared.options.SelfInstall)
	if err != nil {
		return SharedPlan{}, err
	}
	plan := SharedPlan{Ready: true, Changed: launcher.Changed, Launcher: launcher, Skills: skills.Result{State: skills.StateInstalled}}
	if shared.service.skills != nil {
		plan.Skills, err = shared.service.skills.Preview(ctx, shared.options.Skills)
		if err != nil && !errors.Is(err, skills.ErrDrift) && !errors.Is(err, skills.ErrConflict) {
			return plan, err
		}
		plan.Changed = plan.Changed || plan.Skills.Changed || plan.Skills.UpdateNeeded
	}
	if launcher.State == selfinstall.StateDrifted || plan.Skills.State == skills.StateDrifted || plan.Skills.State == skills.StateConflict {
		plan.Ready = false
		plan.Blocker = "Managed launcher or shared skills have drift or a conflict."
	}
	return plan, nil
}

func (shared serviceShared) Apply(ctx context.Context, plan SharedPlan) (SharedResult, error) {
	if !plan.Ready {
		return SharedResult{}, ErrPrerequisite
	}
	installed, err := shared.service.installer.Install(ctx, shared.options.SelfInstall)
	result := SharedResult{Changed: installed.Changed}
	if err != nil {
		return result, err
	}
	status, err := shared.service.installer.Status(ctx, shared.options.SelfInstall)
	if err != nil || status.State != selfinstall.StateInstalled || status.ActiveSHA256 != installed.ActiveSHA256 {
		return result, fmt.Errorf("%w: shared launcher", ErrVerification)
	}
	result.Verified = true
	result.Launcher = status
	return result, nil
}

// Finalize publishes global skills only after every selected provider has
// completed its verified write. Provider references prevent launcher rollback.
func (shared serviceShared) Finalize(ctx context.Context, _ SharedPlan, result SharedResult) (SharedResult, error) {
	if shared.service == nil || !result.Verified {
		return result, ErrPrerequisite
	}
	if shared.service.skills == nil {
		return result, nil
	}
	installed, err := shared.service.skills.Install(ctx, shared.options.Skills)
	result.Changed = result.Changed || installed.Changed
	if err != nil {
		result.Recovery = "Launcher and provider integrations remain installed; run `vgxness skills status` then `vgxness skills install` to complete global skills."
		return result, err
	}
	status, err := shared.service.skills.Status(ctx, shared.options.Skills)
	if err != nil || status.State != skills.StateInstalled {
		result.Recovery = "Launcher and provider integrations remain installed; run `vgxness skills status` then `vgxness skills install` to repair global skills."
		return result, fmt.Errorf("%w: shared skills", ErrVerification)
	}
	return result, nil
}

func OpenCodeSteps() []Step {
	return []Step{
		{Number: 1, Title: "Revisar requisitos y estado actual", Explanation: "Comprobaré el binario candidato, los destinos, el workspace y que OpenCode esté disponible y sea compatible. Esta revisión no escribe archivos ni exige un modelo secundario."},
		{Number: 2, Title: "Instalar el launcher estable", Explanation: "Guardaré la versión exacta por SHA-256 y activaré el launcher permanente. No editaré PATH ni descargaré software.", Mutates: true},
		{Number: 3, Title: "Retirar el plugin y la skill heredados de OpenCode", Explanation: "Antes de publicar la skill portable, retiraré sólo bytes v1-v10 reconocidos del plugin heredado vgxness.ts y de la skill heredada vgxness-autonomous-stacked-pr. Bytes modificados o desconocidos bloquean el wizard sin sobrescritura.", Mutates: true},
		{Number: 4, Title: "Instalar los artefactos del proveedor OpenCode", Explanation: "Instalaré los 15 agentes enlazados al plan de modelos activo: vgxness-manager con workspace de solo lectura y operaciones Git aprobadas por el usuario, sustituciones administradas para Explore y general, un verificador independiente, cinco revisores de solo lectura y seis agentes SDD especializados. El manager conserva la autoridad exclusiva sobre estado y fases; general es el único escritor ordinario de código fuente.", Mutates: true},
		{Number: 5, Title: "Publicar las skills globales portables", Explanation: "Después del retiro seguro, instalaré o verificaré globalmente el catálogo de 18 skills y 42 archivos: skills-creator, stacked-pr, cross-platform, installer-lifecycle, agent-evaluation, ci-triage, security-boundary, documentation-strategy, product-requirements, software-architecture-docs, user-documentation, api-documentation, quality-test-documentation, operations-runbooks, governance-compliance-docs, release-lifecycle-docs, end-to-end-testing y sdd-lifecycle, en el directorio compartido de skills. Estas skills no pertenecen a OpenCode y su desinstalación no las elimina.", Mutates: true},
		{Number: 6, Title: "Verificar archivos y conexión", Explanation: "Leeré nuevamente todos los artefactos administrados, sus hashes y el manifiesto, y comprobaré el handshake real con OpenCode desde el workspace seleccionado. Los cambios de plan se cargan al reiniciar OpenCode."},
		{Number: 7, Title: "Explicar recuperación", Explanation: "Si una actualización falla antes de integrar OpenCode, intentaré volver a la versión anterior. Una primera instalación o una integración ya escrita se conserva para evitar borrados automáticos y se reporta cómo repararla."},
	}
}

func (service *Service) Plan(ctx context.Context, options Options) (plan Plan, err error) {
	plan = Plan{Provider: "opencode", Workspace: options.Workspace, Steps: OpenCodeSteps()}
	defer func() { plan.Digest = planDigest(plan) }()
	if service == nil || service.installer == nil || service.preview == nil || service.integrations == nil || service.prober == nil || options.Workspace == "" {
		return plan, ErrInvalid
	}
	selfResult, err := service.installer.Preview(ctx, options.SelfInstall)
	if err != nil {
		return plan, err
	}
	skillResult := skills.Result{State: skills.StateInstalled}
	if service.skills != nil {
		skillResult, err = service.skills.Preview(ctx, options.Skills)
		if err != nil && !errors.Is(err, skills.ErrDrift) && !errors.Is(err, skills.ErrConflict) {
			return plan, err
		}
	}
	integrationRuntime := service.preview
	if selfResult.State == selfinstall.StateInstalled {
		integrationRuntime, err = service.integrations(selfResult.LauncherPath)
		if err != nil {
			return plan, err
		}
	}
	integrationResult, err := integrationRuntime.Preview(ctx, options.Integration)
	if err != nil {
		return plan, err
	}
	plan.SelfInstall = selfResult
	plan.Integration = cloneIntegrationResult(integrationResult)
	plan.Skills = skillResult
	handshake, handshakeErr := service.prober.Probe(ctx, options.Workspace)
	plan.Handshake = handshake
	if handshakeErr != nil {
		return plan, handshakeErr
	}
	if handshake.Status == integration.HandshakeUnavailable {
		plan.Blocker = "OpenCode no está disponible o el workspace no es válido. Instala una versión compatible y vuelve a ejecutar el wizard."
		return plan, nil
	}
	if !handshake.OK {
		plan.Blocker = "OpenCode respondió, pero el adaptador no está saludable o la versión es incompatible. Corrige el requisito antes de continuar."
		return plan, nil
	}
	if selfResult.State == selfinstall.StateDrifted || integrationResult.State == integration.StateDrifted || skillResult.State == skills.StateDrifted || skillResult.State == skills.StateConflict {
		plan.Blocker = "Hay contenido administrado modificado o un destino en conflicto. El wizard no sobrescribirá esos archivos."
		return plan, nil
	}
	plan.Ready = true
	return plan, nil
}

func (service *Service) Status(ctx context.Context, options Options) (Plan, error) {
	plan := Plan{Provider: "opencode", Workspace: options.Workspace, Steps: OpenCodeSteps()}
	if service == nil || service.installer == nil || service.preview == nil || service.integrations == nil || service.prober == nil || options.Workspace == "" {
		return plan, ErrInvalid
	}
	selfResult, err := service.installer.Status(ctx, options.SelfInstall)
	if err != nil {
		return plan, err
	}
	skillResult := skills.Result{State: skills.StateInstalled}
	if service.skills != nil {
		skillResult, err = service.skills.Status(ctx, options.Skills)
		if err != nil && !errors.Is(err, skills.ErrDrift) && !errors.Is(err, skills.ErrConflict) {
			return plan, err
		}
	}
	integrationRuntime := service.preview
	if selfResult.State == selfinstall.StateInstalled {
		integrationRuntime, err = service.integrations(selfResult.LauncherPath)
		if err != nil {
			return plan, err
		}
	}
	integrationResult, err := integrationRuntime.Status(ctx, options.Integration)
	if err != nil {
		return plan, err
	}
	plan.SelfInstall = selfResult
	plan.Integration = cloneIntegrationResult(integrationResult)
	plan.Skills = skillResult
	handshake, handshakeErr := service.prober.Probe(ctx, options.Workspace)
	plan.Handshake = handshake
	if handshakeErr != nil {
		return plan, handshakeErr
	}
	if !handshake.OK {
		plan.Blocker = "OpenCode no está disponible, es incompatible o el workspace no es válido."
		return plan, nil
	}
	plan.Ready = selfResult.State == selfinstall.StateInstalled && integrationResult.State == integration.StateInstalled && skillResult.State == skills.StateInstalled
	if !plan.Ready {
		plan.Blocker = "La configuración todavía no está completa o presenta drift. Ejecuta el wizard para revisar el plan de reparación."
	}
	return plan, nil
}

func (service *Service) Apply(ctx context.Context, options Options) (Result, error) {
	plan, err := service.Plan(ctx, options)
	result := Result{Plan: plan}
	if err != nil {
		return result, err
	}
	if options.ExpectedPlanDigest == "" || options.ExpectedPlanDigest != plan.Digest {
		return result, fmt.Errorf("%w: confirmed preview no longer matches", ErrPrerequisite)
	}
	if !plan.Ready {
		return result, ErrPrerequisite
	}
	installed, err := service.installer.Install(ctx, options.SelfInstall)
	result.SelfInstall = installed
	result.Plan.SelfInstall = installed
	if err != nil {
		return result, err
	}
	managed, err := service.integrations(installed.LauncherPath)
	if err != nil {
		service.recoverBinary(ctx, options, plan, installed, &result)
		return result, err
	}
	integrated, err := managed.Install(ctx, options.Integration)
	result.Integration = preserveModelDetails(integrated, plan.Integration)
	result.Plan.Integration = cloneIntegrationResult(result.Integration)
	if err != nil {
		observed, statusErr := managed.Status(ctx, options.Integration)
		if observed != (integration.Result{}) {
			result.Integration = preserveModelDetails(observed, result.Plan.Integration)
			result.Plan.Integration = cloneIntegrationResult(result.Integration)
		}
		integrationRecoveryIncomplete := errors.Is(err, integration.ErrRecovery)
		service.recoverBinary(ctx, options, plan, installed, &result)
		if integrationRecoveryIncomplete {
			message := "La integración no pudo revertir todos los artefactos; inspecciona los backups administrados antes de reintentar."
			if result.Recovery != "" {
				message += " " + result.Recovery
			}
			result.Recovery = message
		}
		if statusErr != nil {
			return result, errors.Join(err, statusErr)
		}
		return result, err
	}
	skillInstalled := skills.Result{State: skills.StateInstalled}
	if service.skills != nil {
		skillInstalled, err = service.skills.Install(ctx, options.Skills)
		result.Plan.Skills = skillInstalled
		if err != nil {
			service.recoverBinary(ctx, options, plan, installed, &result)
			if errors.Is(err, skills.ErrRecovery) {
				service.discloseIncompleteSkills(&result)
			} else {
				service.discloseUnconfirmedGlobalSkills(&result)
			}
			return result, err
		}
	}
	selfStatus, err := service.installer.Status(ctx, options.SelfInstall)
	result.SelfInstall = selfStatus
	result.Plan.SelfInstall = selfStatus
	if err != nil || selfStatus.State != selfinstall.StateInstalled || selfStatus.ActiveSHA256 != installed.ActiveSHA256 {
		result.Recovery = "La instalación se conserva para no borrar archivos sin una identidad comprobada. Ejecuta `vgxness self status` y repara el drift antes de reintentar."
		service.discloseSkills(&result, skillInstalled)
		return result, fmt.Errorf("%w: self-install", ErrVerification)
	}
	integrationStatus, err := managed.Status(ctx, options.Integration)
	result.Integration = preserveModelDetails(integrationStatus, result.Plan.Integration)
	result.Plan.Integration = cloneIntegrationResult(result.Integration)
	if err != nil || integrationStatus.State != integration.StateInstalled {
		result.Recovery = "Los archivos instalados se conservan. Ejecuta `vgxness integrate opencode status` para inspeccionar y `uninstall` sólo si deseas retirarlos recuperablemente."
		service.discloseSkills(&result, skillInstalled)
		return result, fmt.Errorf("%w: integration", ErrVerification)
	}
	handshake, err := service.prober.Probe(ctx, options.Workspace)
	result.Handshake = handshake
	result.Plan.Handshake = handshake
	if err != nil || !handshake.OK {
		result.Recovery = "El launcher y la integración quedaron instalados, pero OpenCode no respondió saludablemente. Corrige OpenCode y ejecuta `vgxness setup opencode --status`."
		service.discloseSkills(&result, skillInstalled)
		return result, fmt.Errorf("%w: handshake", ErrVerification)
	}
	if service.skills != nil {
		skillStatus, err := service.skills.Status(ctx, options.Skills)
		if err != nil || skillStatus.State != skills.StateInstalled {
			result.Plan.Skills = skillStatus
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			service.discloseIncompleteSkills(&result)
			return result, errors.Join(fmt.Errorf("%w: skills", ErrVerification), skills.ErrRecovery, err)
		}
		result.Plan.Skills = skillStatus
	}
	result.Changed = installed.Changed || integrated.Changed || skillInstalled.Changed
	return result, nil
}

func planDigest(plan Plan) string {
	plan.Digest = ""
	encoded, _ := json.Marshal(plan)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func preserveModelDetails(result, fallback integration.Result) integration.Result {
	result = cloneIntegrationResult(result)
	fallback = cloneIntegrationResult(fallback)
	if result.ModelSchemaVersion == 0 {
		result.ModelSchemaVersion = fallback.ModelSchemaVersion
	}
	if result.ModelAssignments == nil {
		result.ModelAssignments = fallback.ModelAssignments
	}
	if result.ModelPlan == "" {
		result.ModelPlan = fallback.ModelPlan
	}
	if result.ModelProvider == "" {
		result.ModelProvider = fallback.ModelProvider
	}
	if result.ModelEfficient == "" {
		result.ModelEfficient = fallback.ModelEfficient
	}
	if result.ModelBalanced == "" {
		result.ModelBalanced = fallback.ModelBalanced
	}
	if result.ModelFrontier == "" {
		result.ModelFrontier = fallback.ModelFrontier
	}
	if result.ModelEfficientEffort == "" {
		result.ModelEfficientEffort = fallback.ModelEfficientEffort
	}
	if result.ModelBalancedEffort == "" {
		result.ModelBalancedEffort = fallback.ModelBalancedEffort
	}
	if result.ModelFrontierEffort == "" {
		result.ModelFrontierEffort = fallback.ModelFrontierEffort
	}
	if !result.ModelVariantsSpecified {
		result.ModelEfficientVariant = fallback.ModelEfficientVariant
		result.ModelBalancedVariant = fallback.ModelBalancedVariant
		result.ModelFrontierVariant = fallback.ModelFrontierVariant
		result.ModelVariantsSpecified = fallback.ModelVariantsSpecified
	}
	if result.ModelEfficientSource == "" {
		result.ModelEfficientSource = fallback.ModelEfficientSource
	}
	if result.ModelBalancedSource == "" {
		result.ModelBalancedSource = fallback.ModelBalancedSource
	}
	if result.ModelFrontierSource == "" {
		result.ModelFrontierSource = fallback.ModelFrontierSource
	}
	if result.ModelEfficientAvailability == "" {
		result.ModelEfficientAvailability = fallback.ModelEfficientAvailability
	}
	if result.ModelBalancedAvailability == "" {
		result.ModelBalancedAvailability = fallback.ModelBalancedAvailability
	}
	if result.ModelFrontierAvailability == "" {
		result.ModelFrontierAvailability = fallback.ModelFrontierAvailability
	}
	return result
}

func cloneIntegrationResult(result integration.Result) integration.Result {
	if result.ModelAssignments != nil {
		assignments := *result.ModelAssignments
		result.ModelAssignments = &assignments
	}
	return result
}

func (service *Service) discloseSkills(result *Result, installed skills.Result) {
	if !installed.Changed {
		return
	}
	message := "El paquete global de skills quedó instalado y verificado de forma independiente; adminístralo con `vgxness skills` (no se revierte automáticamente)."
	if result.Recovery != "" {
		message = result.Recovery + " " + message
	}
	result.Recovery = message
}

func (service *Service) discloseIncompleteSkills(result *Result) {
	message := "Sólo los bytes v1-v10 reconocidos del plugin heredado vgxness.ts y de la skill heredada vgxness-autonomous-stacked-pr podrían haberse retirado; la publicación global no está confirmada. Inspecciona con `vgxness skills status` y reintenta con `vgxness skills install`. El paquete global de skills puede haber quedado parcial o modificado; inspecciónalo con `vgxness skills status` antes de reintentar."
	if result.Recovery != "" {
		message = result.Recovery + " " + message
	}
	result.Recovery = message
}

func (service *Service) discloseUnconfirmedGlobalSkills(result *Result) {
	message := "Sólo los bytes v1-v10 reconocidos del plugin heredado vgxness.ts y de la skill heredada vgxness-autonomous-stacked-pr podrían haberse retirado; la publicación global no está confirmada. Inspecciona con `vgxness skills status` y reintenta con `vgxness skills install`."
	if result.Recovery != "" {
		message = result.Recovery + " " + message
	}
	result.Recovery = message
}

func (service *Service) recoverBinary(ctx context.Context, options Options, plan Plan, installed selfinstall.Result, result *Result) {
	if !installed.Changed || plan.SelfInstall.State != selfinstall.StateInstalled || !installed.RollbackAvailable {
		result.Recovery = "El launcher administrado se conserva; no existe una versión previa comprobada que pueda restaurarse automáticamente."
		return
	}
	recoveryContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	rolledBack, err := service.installer.Rollback(recoveryContext, options.SelfInstall)
	if err != nil {
		result.Recovery = "No fue posible restaurar la versión anterior automáticamente. Ejecuta `vgxness self status` antes de reintentar."
		return
	}
	result.SelfInstall = rolledBack
	result.Plan.SelfInstall = rolledBack
	result.Recovery = "La actualización del binario se revirtió a la versión administrada anterior."
}

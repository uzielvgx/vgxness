package setup

import (
	"context"
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
	SelfInstall selfinstall.Options
	Integration integration.Options
	Skills      skills.Options
	Workspace   string
}

type Plan struct {
	Provider    string
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

func OpenCodeSteps() []Step {
	return []Step{
		{Number: 1, Title: "Revisar requisitos y estado actual", Explanation: "Comprobaré el binario candidato, los destinos, el workspace y que OpenCode esté disponible y sea compatible. Esta revisión no escribe archivos ni exige un modelo secundario."},
		{Number: 2, Title: "Instalar el launcher estable", Explanation: "Guardaré la versión exacta por SHA-256 y activaré el launcher permanente. No editaré PATH ni descargaré software.", Mutates: true},
		{Number: 3, Title: "Retirar la skill autónoma heredada de OpenCode", Explanation: "Antes de publicar la skill portable, retiraré sólo bytes v1, v2 o v3 reconocidos de vgxness-autonomous-stacked-pr. Bytes modificados o desconocidos bloquean el wizard sin sobrescritura.", Mutates: true},
		{Number: 4, Title: "Instalar los artefactos del proveedor OpenCode", Explanation: "Instalaré los 15 agentes enlazados al plan de modelos activo: vgxness-manager con workspace de solo lectura y operaciones Git aprobadas por el usuario, sustituciones administradas para Explore y general, un verificador independiente, cinco revisores de solo lectura y seis agentes SDD especializados. El manager conserva la autoridad exclusiva sobre estado y fases; general es el único escritor ordinario de código fuente.", Mutates: true},
		{Number: 5, Title: "Publicar las skills globales portables", Explanation: "Después del retiro seguro, instalaré o verificaré globalmente el catálogo de 17 skills y 41 archivos: skills-creator, stacked-pr, cross-platform, installer-lifecycle, agent-evaluation, ci-triage, security-boundary, documentation-strategy, product-requirements, software-architecture-docs, user-documentation, api-documentation, quality-test-documentation, operations-runbooks, governance-compliance-docs, release-lifecycle-docs y end-to-end-testing, en el directorio compartido de skills. Estas skills no pertenecen a OpenCode y su desinstalación no las elimina.", Mutates: true},
		{Number: 6, Title: "Verificar archivos y conexión", Explanation: "Leeré nuevamente todos los artefactos administrados, sus hashes y el manifiesto, y comprobaré el handshake real con OpenCode desde el workspace seleccionado. Los cambios de plan se cargan al reiniciar OpenCode."},
		{Number: 7, Title: "Explicar recuperación", Explanation: "Si una actualización falla antes de integrar OpenCode, intentaré volver a la versión anterior. Una primera instalación o una integración ya escrita se conserva para evitar borrados automáticos y se reporta cómo repararla."},
	}
}

func (service *Service) Plan(ctx context.Context, options Options) (Plan, error) {
	plan := Plan{Provider: "opencode", Steps: OpenCodeSteps()}
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
	plan.Integration = integrationResult
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
	plan := Plan{Provider: "opencode", Steps: OpenCodeSteps()}
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
	plan.Integration = integrationResult
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
	if !plan.Ready {
		return result, ErrPrerequisite
	}
	installed, err := service.installer.Install(ctx, options.SelfInstall)
	result.SelfInstall = installed
	if err != nil {
		return result, err
	}
	managed, err := service.integrations(installed.LauncherPath)
	if err != nil {
		service.recoverBinary(ctx, options, plan, installed, &result)
		return result, err
	}
	integrated, err := managed.Install(ctx, options.Integration)
	result.Integration = integrated
	if err != nil {
		integrationRecoveryIncomplete := errors.Is(err, integration.ErrRecovery)
		service.recoverBinary(ctx, options, plan, installed, &result)
		if integrationRecoveryIncomplete {
			message := "La integración no pudo revertir todos los artefactos; inspecciona los backups administrados antes de reintentar."
			if result.Recovery != "" {
				message += " " + result.Recovery
			}
			result.Recovery = message
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
	if err != nil || selfStatus.State != selfinstall.StateInstalled || selfStatus.ActiveSHA256 != installed.ActiveSHA256 {
		result.Recovery = "La instalación se conserva para no borrar archivos sin una identidad comprobada. Ejecuta `vgxness self status` y repara el drift antes de reintentar."
		service.discloseSkills(&result, skillInstalled)
		return result, fmt.Errorf("%w: self-install", ErrVerification)
	}
	integrationStatus, err := managed.Status(ctx, options.Integration)
	if err != nil || integrationStatus.State != integration.StateInstalled {
		result.Recovery = "Los archivos instalados se conservan. Ejecuta `vgxness integrate opencode status` para inspeccionar y `uninstall` sólo si deseas retirarlos recuperablemente."
		service.discloseSkills(&result, skillInstalled)
		return result, fmt.Errorf("%w: integration", ErrVerification)
	}
	handshake, err := service.prober.Probe(ctx, options.Workspace)
	result.Handshake = handshake
	if err != nil || !handshake.OK {
		result.Recovery = "El launcher y la integración quedaron instalados, pero OpenCode no respondió saludablemente. Corrige OpenCode y ejecuta `vgxness setup opencode --status`."
		service.discloseSkills(&result, skillInstalled)
		return result, fmt.Errorf("%w: handshake", ErrVerification)
	}
	result.SelfInstall = selfStatus
	result.Integration = integrationStatus
	if service.skills != nil {
		skillStatus, err := service.skills.Status(ctx, options.Skills)
		if err != nil || skillStatus.State != skills.StateInstalled {
			return result, fmt.Errorf("%w: skills", ErrVerification)
		}
		result.Plan.Skills = skillStatus
	}
	result.Changed = installed.Changed || integrated.Changed || skillInstalled.Changed
	return result, nil
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
	message := "La skill heredada del proveedor puede ya estar retirada y la publicación global no está confirmada; inspecciona con `vgxness skills status` y reintenta con `vgxness skills install`. El paquete global de skills puede haber quedado parcial o modificado; inspecciónalo con `vgxness skills status` antes de reintentar."
	if result.Recovery != "" {
		message = result.Recovery + " " + message
	}
	result.Recovery = message
}

func (service *Service) discloseUnconfirmedGlobalSkills(result *Result) {
	message := "La skill heredada del proveedor puede ya estar retirada y la publicación global no está confirmada; inspecciona con `vgxness skills status` y reintenta con `vgxness skills install`."
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
	result.Recovery = "La actualización del binario se revirtió a la versión administrada anterior."
}

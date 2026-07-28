package setup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/selfinstall"
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
	Workspace   string
}

type Plan struct {
	Provider    string
	Steps       []Step
	SelfInstall selfinstall.Result
	Integration integration.Result
	Bridge      bridge.Response
	Ready       bool
	Blocker     string
}

type Result struct {
	Plan        Plan
	SelfInstall selfinstall.Result
	Integration integration.Result
	Bridge      bridge.Response
	Recovery    string
	Changed     bool
}

type Runtime interface {
	Plan(context.Context, Options) (Plan, error)
	Apply(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Plan, error)
}

type IntegrationFactory func(string) (integration.Runtime, error)

type Service struct {
	installer    selfinstall.Runtime
	preview      integration.Runtime
	integrations IntegrationFactory
	controlPlane bridge.Runtime
}

func New(installer selfinstall.Runtime, preview integration.Runtime, integrations IntegrationFactory, controlPlane bridge.Runtime) *Service {
	return &Service{installer: installer, preview: preview, integrations: integrations, controlPlane: controlPlane}
}

func OpenCodeSteps() []Step {
	return []Step{
		{Number: 1, Title: "Revisar requisitos y estado actual", Explanation: "Comprobaré el binario candidato, los destinos, el workspace y que OpenCode esté disponible y sea compatible. Esta revisión no escribe archivos ni exige un modelo secundario."},
		{Number: 2, Title: "Instalar el launcher estable", Explanation: "Guardaré la versión exacta por SHA-256 y activaré el launcher permanente. No editaré PATH ni descargaré software.", Mutates: true},
		{Number: 3, Title: "Instalar el manager y revisores nativos", Explanation: "Crearé un único vgxness-manager native-first y cinco revisores ocultos de solo lectura para Risk, Readability, Reliability, Resilience y refutación. Trabajan con herramientas, delegación, skills y CodeGraph nativos de OpenCode; no se instalan agentes gobernados.", Mutates: true},
		{Number: 4, Title: "Instalar memoria VGXNESS acotada", Explanation: "Instalaré un plugin mínimo que expone exclusivamente búsqueda, lectura, guardado y olvido explícito sobre el MemoryStore propio de VGXNESS. No contiene orquestación, tickets, edición, CodeGraph, ejecución de agentes ni integración con Engram.", Mutates: true},
		{Number: 5, Title: "Verificar archivos y conexión", Explanation: "Leeré nuevamente hashes y manifiestos, y comprobaré el handshake real con OpenCode desde el workspace seleccionado. La salud depende de OpenCode, los seis perfiles y el plugin exacto de memoria, no del antiguo bridge de ejecución."},
		{Number: 6, Title: "Explicar recuperación", Explanation: "Si una actualización falla antes de integrar OpenCode, intentaré volver a la versión anterior. Una primera instalación o una integración ya escrita se conserva para evitar borrados automáticos y se reporta cómo repararla."},
	}
}

func (service *Service) Plan(ctx context.Context, options Options) (Plan, error) {
	plan := Plan{Provider: "opencode", Steps: OpenCodeSteps()}
	if service == nil || service.installer == nil || service.preview == nil || service.integrations == nil || service.controlPlane == nil || options.Workspace == "" {
		return plan, ErrInvalid
	}
	selfResult, err := service.installer.Preview(ctx, options.SelfInstall)
	if err != nil {
		return plan, err
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
	health, healthErr := service.controlPlane.Status(ctx, options.Workspace)
	plan.Bridge = health
	if healthErr != nil {
		plan.Blocker = "OpenCode no está disponible o el workspace no es válido. Instala una versión compatible y vuelve a ejecutar el wizard."
		return plan, nil
	}
	if !health.OK {
		plan.Blocker = "OpenCode respondió, pero el adaptador no está saludable o la versión es incompatible. Corrige el requisito antes de continuar."
		return plan, nil
	}
	if selfResult.State == selfinstall.StateDrifted || integrationResult.State == integration.StateDrifted {
		plan.Blocker = "Hay contenido administrado modificado o un destino en conflicto. El wizard no sobrescribirá esos archivos."
		return plan, nil
	}
	plan.Ready = true
	return plan, nil
}

func (service *Service) Status(ctx context.Context, options Options) (Plan, error) {
	plan := Plan{Provider: "opencode", Steps: OpenCodeSteps()}
	if service == nil || service.installer == nil || service.preview == nil || service.integrations == nil || service.controlPlane == nil || options.Workspace == "" {
		return plan, ErrInvalid
	}
	selfResult, err := service.installer.Status(ctx, options.SelfInstall)
	if err != nil {
		return plan, err
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
	health, healthErr := service.controlPlane.Status(ctx, options.Workspace)
	plan.Bridge = health
	if healthErr != nil || !health.OK {
		plan.Blocker = "OpenCode no está disponible, es incompatible o el workspace no es válido."
		return plan, nil
	}
	plan.Ready = selfResult.State == selfinstall.StateInstalled && integrationResult.State == integration.StateInstalled
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
		service.recoverBinary(ctx, options, plan, installed, &result)
		return result, err
	}
	selfStatus, err := service.installer.Status(ctx, options.SelfInstall)
	if err != nil || selfStatus.State != selfinstall.StateInstalled || selfStatus.ActiveSHA256 != installed.ActiveSHA256 {
		result.Recovery = "La instalación se conserva para no borrar archivos sin una identidad comprobada. Ejecuta `vgxness self status` y repara el drift antes de reintentar."
		return result, fmt.Errorf("%w: self-install", ErrVerification)
	}
	integrationStatus, err := managed.Status(ctx, options.Integration)
	if err != nil || integrationStatus.State != integration.StateInstalled {
		result.Recovery = "Los archivos instalados se conservan. Ejecuta `vgxness integrate opencode status` para inspeccionar y `uninstall` sólo si deseas retirarlos recuperablemente."
		return result, fmt.Errorf("%w: integration", ErrVerification)
	}
	health, err := service.controlPlane.Status(ctx, options.Workspace)
	result.Bridge = health
	if err != nil || !health.OK {
		result.Recovery = "El launcher y la integración quedaron instalados, pero OpenCode no respondió saludablemente. Corrige OpenCode y ejecuta `vgxness setup opencode --status`."
		return result, fmt.Errorf("%w: bridge", ErrVerification)
	}
	result.SelfInstall = selfStatus
	result.Integration = integrationStatus
	result.Changed = installed.Changed || integrated.Changed
	return result, nil
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

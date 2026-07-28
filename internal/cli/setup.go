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

	setupflow "github.com/vgxness/vgxness/internal/setup"
)

func runSetup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime setupflow.Runtime) int {
	if len(args) == 0 || args[0] != "opencode" {
		fmt.Fprintln(stderr, "usage: vgxness setup opencode [--preview|--status] [--yes] [--workspace PATH] [--bin-dir PATH] [--data-dir PATH] [--config-dir PATH]")
		return 2
	}
	flags := flag.NewFlagSet("setup opencode", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var (
		preview, status, yes bool
		workspace            string
		options              setupflow.Options
	)
	flags.BoolVar(&preview, "preview", false, "explain the complete plan without writing")
	flags.BoolVar(&status, "status", false, "inspect the complete setup without writing")
	flags.BoolVar(&yes, "yes", false, "approve the explained plan non-interactively")
	flags.StringVar(&workspace, "workspace", "", "workspace used for the live OpenCode handshake")
	flags.StringVar(&options.Integration.Model, "model", "", "deprecated compatibility flag; the native integration does not use a child model")
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
	fmt.Fprintf(stdout, "Pasos 3–4: manager, revisores, skills y CodeGraph nativos verificados desde %s\n", terminalSafe(result.Integration.Path))
	fmt.Fprintf(stdout, "Paso 5: handshake OpenCode=%s workspace=%s\n", terminalSafe(result.Bridge.Status), terminalSafe(options.Workspace))
	fmt.Fprintln(stdout, "Paso 6: no fue necesaria recuperación.")
	fmt.Fprintf(stdout, "\nResultado: configuración completa; changed=%t. Abre OpenCode y selecciona vgxness-manager.\n", result.Changed)
	return 0
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
	fmt.Fprintf(writer, "  Launcher: %s (estado=%s)\n", terminalSafe(plan.SelfInstall.LauncherPath), plan.SelfInstall.State)
	fmt.Fprintf(writer, "  Versiones: %s\n", terminalSafe(plan.SelfInstall.DataDir))
	fmt.Fprintf(writer, "  Manager: %s (estado=%s)\n", terminalSafe(plan.Integration.Path), plan.Integration.State)
	fmt.Fprintln(writer, "  Proyección: manager + cinco revisores nativos (sin plugin ni modelo secundario)")
	fmt.Fprintf(writer, "  Workspace de verificación: %s\n", terminalSafe(workspace))
	fmt.Fprintln(writer, "\nLímites: no se editará PATH, no se descargará software, no se sobrescribirá contenido ajeno y no se habilitará shell arbitrario.")
	fmt.Fprintln(writer, "Recuperación: una actualización binaria puede volver a la versión anterior; una primera instalación o integración escrita se conserva y se reporta para evitar borrados silenciosos.")
}

func renderSetupStatus(writer io.Writer, plan setupflow.Plan, workspace string) {
	fmt.Fprintln(writer, "VGXNESS · Estado completo del setup OpenCode")
	fmt.Fprintf(writer, "Launcher: state=%s path=%s active_sha256=%s\n", plan.SelfInstall.State, terminalSafe(plan.SelfInstall.LauncherPath), plan.SelfInstall.ActiveSHA256)
	fmt.Fprintf(writer, "Integración: state=%s projection=native manager=%s\n", plan.Integration.State, terminalSafe(plan.Integration.Path))
	fmt.Fprintf(writer, "Handshake: ok=%t status=%s workspace=%s\n", plan.Bridge.OK, terminalSafe(plan.Bridge.Status), terminalSafe(workspace))
	if plan.Ready {
		fmt.Fprintln(writer, "Resultado: configuración completa y saludable.")
	} else {
		fmt.Fprintf(writer, "Resultado: requiere atención. %s\n", terminalSafe(plan.Blocker))
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

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/vgxness/vgxness/internal/bridge"
)

func runOrchestration(ctx context.Context, args []string, stdout, stderr io.Writer, runtime bridge.Runtime) int {
	if len(args) == 0 || args[0] != "status" && args[0] != "resume" && args[0] != "cancel" && args[0] != "explain" {
		fmt.Fprintln(stderr, "usage: vgxness orchestrate <status|resume|cancel|explain> --workspace PATH --id ID [--owner OWNER]")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("orchestrate "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace, orchestrationID, ownerID string
	flags.StringVar(&workspace, "workspace", "", "absolute workspace")
	flags.StringVar(&orchestrationID, "id", "", "orchestration identity")
	flags.StringVar(&ownerID, "owner", "", "current owner identity")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || workspace == "" || orchestrationID == "" || (command == "resume" || command == "cancel") && ownerID == "" {
		fmt.Fprintln(stderr, "invalid orchestration arguments")
		return 2
	}
	orchestration, ok := runtime.(bridge.OrchestrationRuntime)
	if !ok {
		fmt.Fprintln(stderr, "unavailable: orchestration runtime is not configured")
		return 1
	}
	request := bridge.OrchestrateReferenceRequest{ProtocolVersion: bridge.ProtocolVersion, OrchestrationID: orchestrationID, OwnerID: ownerID}
	var response bridge.Response
	var err error
	switch command {
	case "resume":
		response, err = orchestration.ResumeOrchestration(ctx, workspace, request)
	case "cancel":
		response, err = orchestration.CancelOrchestration(ctx, workspace, request)
	default:
		request.OwnerID = ""
		response, err = orchestration.StatusOrchestration(ctx, workspace, request)
	}
	if err != nil {
		failure := bridge.ErrorResponse(err)
		fmt.Fprintf(stderr, "%s: %s\n", failure.Error.Code, failure.Error.Message)
		return 1
	}
	if command != "explain" {
		if err := bridge.Encode(stdout, response); err != nil {
			fmt.Fprintln(stderr, "operational: orchestration output failed")
			return 1
		}
		return 0
	}
	view := response.Orchestration
	if view == nil {
		fmt.Fprintln(stderr, "operational: orchestration status is incomplete")
		return 1
	}
	fmt.Fprintf(stdout, "orchestration_id=%s\nstatus=%s\ndecision=%s\ncurrent_wave=%d\ntasks=%d\nwaves=%d\nrationale=%s\n",
		terminalSafe(view.OrchestrationID), terminalSafe(view.Status), terminalSafe(view.Plan.Decision), view.CurrentWave,
		len(view.Plan.Tasks), len(view.Plan.Waves), terminalSafe(view.Plan.Rationale))
	return 0
}

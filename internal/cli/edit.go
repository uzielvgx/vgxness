package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/vgxness/vgxness/internal/bridge"
)

func runEditLifecycle(ctx context.Context, args []string, stdout, stderr io.Writer, runtime bridge.Runtime) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: vgxness edit <inspect|approve|integrate|retire|discard> --ticket ID [--manifest SHA256 --actor NAME] [--workspace PATH]")
		return 2
	}
	action := args[0]
	switch action {
	case "inspect", "approve", "integrate", "retire", "discard":
	default:
		fmt.Fprintln(stderr, "usage: vgxness edit <inspect|approve|integrate|retire|discard> --ticket ID [--manifest SHA256 --actor NAME] [--workspace PATH]")
		return 2
	}
	flags := flag.NewFlagSet("edit "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace, ticketID, manifest, actor string
	flags.StringVar(&workspace, "workspace", "", "absolute workspace")
	flags.StringVar(&ticketID, "ticket", "", "native edit ticket")
	flags.StringVar(&manifest, "manifest", "", "approved native edit manifest SHA-256")
	flags.StringVar(&actor, "actor", "", "explicit local approval actor")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid edit arguments")
		return 2
	}
	edits, ok := runtime.(bridge.EditLifecycleRuntime)
	if !ok {
		fmt.Fprintln(stderr, "unavailable: native edit lifecycle runtime is not configured")
		return 1
	}
	workspace, err := operationalWorkspace(workspace)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: workspace is unavailable")
		return 2
	}
	var result bridge.NativeEditLifecycleResult
	if action == "inspect" {
		if manifest != "" || actor != "" {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		request := bridge.NativeEditInspectRequest{TicketID: ticketID}
		if err := bridge.ValidateNativeEditInspect(request); err != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		result, err = edits.InspectNativeEdit(ctx, workspace, request)
	} else {
		request := bridge.NativeEditActionRequest{TicketID: ticketID, ManifestSHA: manifest, Actor: actor}
		if err := bridge.ValidateNativeEditAction(request); err != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		switch action {
		case "approve":
			result, err = edits.ApproveNativeEdit(ctx, workspace, request)
		case "integrate":
			result, err = edits.IntegrateNativeEdit(ctx, workspace, request)
		case "retire":
			result, err = edits.RetireNativeEdit(ctx, workspace, request)
		case "discard":
			result, err = edits.DiscardNativeEdit(ctx, workspace, request)
		}
	}
	if err != nil {
		failure := bridge.ErrorResponse(err)
		fmt.Fprintf(stderr, "%s: %s\n", failure.Error.Code, failure.Error.Message)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "operational: edit lifecycle output failed")
		return 1
	}
	return 0
}

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
		fmt.Fprintln(stderr, "usage: vgxness edit <inspect|review|approve|integrate|retire|discard> --ticket ID [--workspace PATH]")
		return 2
	}
	action := args[0]
	switch action {
	case "inspect", "review", "approve", "integrate", "retire", "discard":
	default:
		fmt.Fprintln(stderr, "usage: vgxness edit <inspect|review|approve|integrate|retire|discard> --ticket ID [--workspace PATH]")
		return 2
	}
	flags := flag.NewFlagSet("edit "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace, ticketID, manifest, reviewManifest, receiptID, actor string
	flags.StringVar(&workspace, "workspace", "", "absolute workspace")
	flags.StringVar(&ticketID, "ticket", "", "native edit ticket")
	flags.StringVar(&manifest, "manifest", "", "approved native edit manifest SHA-256")
	flags.StringVar(&reviewManifest, "review-manifest", "", "Delivery Authority review manifest JSON")
	flags.StringVar(&receiptID, "receipt", "", "exact Delivery Authority review receipt ID")
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
	var result any
	if action == "inspect" {
		if manifest != "" || reviewManifest != "" || receiptID != "" || actor != "" {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		request := bridge.NativeEditInspectRequest{TicketID: ticketID}
		if err := bridge.ValidateNativeEditInspect(request); err != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		result, err = edits.InspectNativeEdit(ctx, workspace, request)
	} else if action == "review" {
		if manifest != "" || receiptID != "" || actor != "" {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		manifestValue, readErr := readDeliveryManifest(reviewManifest)
		if readErr != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		encoded, encodeErr := json.Marshal(manifestValue)
		if encodeErr != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		request := bridge.NativeEditReviewRequest{TicketID: ticketID, Manifest: encoded}
		if err := bridge.ValidateNativeEditReview(request); err != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		result, err = edits.IssueNativeEditReview(ctx, workspace, request)
	} else if action == "approve" {
		if reviewManifest != "" {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		request := bridge.NativeEditApprovalRequest{
			TicketID: ticketID, ManifestSHA: manifest, ReviewReceiptID: receiptID, Actor: actor,
		}
		if err := bridge.ValidateNativeEditApproval(request); err != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		result, err = edits.ApproveNativeEdit(ctx, workspace, request)
	} else {
		if reviewManifest != "" || receiptID != "" {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		request := bridge.NativeEditActionRequest{TicketID: ticketID, ManifestSHA: manifest, Actor: actor}
		if err := bridge.ValidateNativeEditAction(request); err != nil {
			fmt.Fprintln(stderr, "invalid edit arguments")
			return 2
		}
		switch action {
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

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/inspection"
)

func printDeepDoctor(ctx context.Context, stdout, stderr io.Writer, runtime bridge.Runtime, options config.Options, result inspection.Result) int {
	operations, ok := runtime.(bridge.OperationsRuntime)
	if !ok {
		fmt.Fprintln(stderr, "unavailable: operational runtime is not configured")
		return 1
	}
	workspace, err := operationalWorkspace(options.ProjectDir)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: workspace is unavailable")
		return 2
	}
	inventory, err := operations.OperationalInventory(ctx, workspace, bridge.OperationalInventoryRequest{
		StorageRoot: options.StorageRoot, ProjectLocal: options.ProjectLocal,
	})
	if err != nil {
		failure := bridge.ErrorResponse(err)
		fmt.Fprintf(stderr, "%s: %s\n", failure.Error.Code, failure.Error.Message)
		return 1
	}
	chronicle := "absent"
	if result.ChroniclePresent {
		chronicle = "present run=" + terminalSafe(result.RunID)
	}
	orchestrationCounts := make(map[string]int)
	for _, item := range inventory.Orchestrations {
		orchestrationCounts[item.Status]++
	}
	ticketCounts := make(map[string]int)
	for _, item := range inventory.NativeTickets {
		ticketCounts[item.State]++
	}
	fmt.Fprintf(stdout, "storage_root=%s\ndatabase=%s\nmigration=%d\nchronicle=%s\n",
		terminalSafe(result.Root), terminalSafe(result.Database), result.Migration, chronicle)
	fmt.Fprintf(stdout, "orchestrations=%d pending=%d running=%d completed=%d failed=%d cancelled=%d\n",
		len(inventory.Orchestrations), orchestrationCounts["pending"], orchestrationCounts["running"],
		orchestrationCounts["completed"], orchestrationCounts["failed"], orchestrationCounts["cancelled"])
	fmt.Fprintf(stdout, "native_tickets=%d preparing=%d prepared=%d completed=%d failed=%d\n",
		len(inventory.NativeTickets), ticketCounts["preparing"], ticketCounts["prepared"],
		ticketCounts["completed"], ticketCounts["failed"])
	for _, finding := range inventory.Findings {
		fmt.Fprintf(stdout, "finding=%s/%s subject=%s message=%s\n",
			terminalSafe(finding.Severity), terminalSafe(finding.Code), terminalSafe(finding.Subject), terminalSafe(finding.Message))
	}
	fmt.Fprintf(stdout, "doctor=%s\n", terminalSafe(inventory.Health))
	if inventory.Health != "healthy" {
		return 1
	}
	return 0
}

func runMaintenance(ctx context.Context, args []string, stdout, stderr io.Writer, runtime bridge.Runtime) int {
	if len(args) == 0 || args[0] != "prune" {
		fmt.Fprintln(stderr, "usage: vgxness maintenance prune [--workspace PATH] [--storage-root PATH|--project-local] [--older-than DURATION] [--apply]")
		return 2
	}
	flags := flag.NewFlagSet("maintenance prune", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace string
	var retention time.Duration
	var apply bool
	var storageRoot string
	var projectLocal bool
	flags.StringVar(&workspace, "workspace", "", "absolute workspace")
	flags.DurationVar(&retention, "older-than", 30*24*time.Hour, "terminal-state retention")
	flags.BoolVar(&apply, "apply", false, "remove verified candidates")
	flags.StringVar(&storageRoot, "storage-root", "", "storage root")
	flags.BoolVar(&projectLocal, "project-local", false, "use project-local storage")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || retention%time.Second != 0 {
		fmt.Fprintln(stderr, "invalid maintenance arguments")
		return 2
	}
	operations, ok := runtime.(bridge.OperationsRuntime)
	if !ok {
		fmt.Fprintln(stderr, "unavailable: operational runtime is not configured")
		return 1
	}
	workspace, err := operationalWorkspace(workspace)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: workspace is unavailable")
		return 2
	}
	result, err := operations.PruneOperations(ctx, workspace, bridge.OperationalPruneRequest{
		OlderThanSeconds: int64(retention / time.Second), Apply: apply,
		StorageRoot: storageRoot, ProjectLocal: projectLocal,
	})
	if err != nil {
		if len(result.Removed) > 0 {
			_ = json.NewEncoder(stdout).Encode(result)
		}
		failure := bridge.ErrorResponse(err)
		fmt.Fprintf(stderr, "%s: %s\n", failure.Error.Code, failure.Error.Message)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "operational: maintenance output failed")
		return 1
	}
	return 0
}

func operationalWorkspace(workspace string) (string, error) {
	if workspace != "" {
		return workspace, nil
	}
	return os.Getwd()
}

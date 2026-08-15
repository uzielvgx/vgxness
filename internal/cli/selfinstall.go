package cli

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vgxness/vgxness/internal/selfinstall"
)

func runSelfInstall(ctx context.Context, args []string, stdout, stderr io.Writer, runtime selfinstall.Runtime) int {
	if len(args) > 0 && args[0] == "gc" {
		return runSelfInstallGC(ctx, args[1:], stdout, stderr, runtime)
	}
	if len(args) == 0 || !selfInstallAction(args[0]) {
		fmt.Fprintln(stderr, "usage: vgxness self <preview|install|status|rollback> [--bin-dir PATH] [--data-dir PATH]")
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet("self "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options selfinstall.Options
	flags.StringVar(&options.BinDir, "bin-dir", "", "stable launcher directory")
	flags.StringVar(&options.DataDir, "data-dir", "", "version data directory")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid self-install arguments")
		return 2
	}
	if runtime == nil {
		fmt.Fprintln(stderr, "operational: self-install runtime is unavailable")
		return 1
	}
	var (
		result selfinstall.Result
		err    error
	)
	switch action {
	case "preview":
		result, err = runtime.Preview(ctx, options)
	case "install":
		result, err = runtime.Install(ctx, options)
	case "status":
		result, err = runtime.Status(ctx, options)
	case "rollback":
		result, err = runtime.Rollback(ctx, options)
	}
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	var output strings.Builder
	fmt.Fprintf(&output, "state=%s\nlauncher=%s\nmanifest=%s\ndata_dir=%s\nsource_sha256=%s\nactive_sha256=%s\nprevious_sha256=%s\nupdate_available=%t\nrollback_available=%t\nchanged=%t\n",
		result.State, terminalSafe(result.LauncherPath), terminalSafe(result.ManifestPath), terminalSafe(result.DataDir),
		result.SourceSHA256, result.ActiveSHA256, result.PreviousSHA256, result.UpdateAvailable, result.RollbackAvailable, result.Changed,
	)
	_, _ = io.WriteString(stdout, output.String())
	return 0
}

func selfInstallAction(value string) bool {
	return value == "preview" || value == "install" || value == "status" || value == "rollback"
}

func runSelfInstallGC(ctx context.Context, args []string, stdout, stderr io.Writer, runtime selfinstall.Runtime) int {
	action, options, plan, ok := parseGCArguments(args)
	if !ok {
		fmt.Fprintln(stderr, "invalid self-install arguments")
		return 2
	}
	if runtime == nil {
		fmt.Fprintln(stderr, "operational: self-install runtime is unavailable")
		return 1
	}
	var result selfinstall.GCResult
	var err error
	switch action {
	case "preview":
		result, err = runtime.GCPreview(ctx, options)
	case "apply":
		result, err = runtime.GCApply(ctx, options, plan)
	case "recover":
		result, err = runtime.GCRecover(ctx, options)
	}
	if err != nil {
		if action == "apply" && validGCErrorApplyResult(result) {
			writeGCResult(stdout, action, result)
		}
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	if !validGCResult(action, result) {
		fmt.Fprintln(stderr, "operational: self-install garbage collection result is invalid")
		return 1
	}
	writeGCResult(stdout, action, result)
	return 0
}

func parseGCArguments(args []string) (string, selfinstall.Options, string, bool) {
	if len(args) == 0 || (args[0] != "preview" && args[0] != "apply" && args[0] != "recover") {
		return "", selfinstall.Options{}, "", false
	}
	action, options, plan := args[0], selfinstall.Options{}, ""
	seen := map[string]bool{}
	for index := 1; index < len(args); index++ {
		value := args[index]
		name, assigned, hasAssigned := strings.Cut(value, "=")
		if name != "--bin-dir" && name != "--data-dir" && name != "--plan-sha256" || seen[name] {
			return "", selfinstall.Options{}, "", false
		}
		seen[name] = true
		if !hasAssigned {
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return "", selfinstall.Options{}, "", false
			}
			assigned = args[index]
		}
		if assigned == "" {
			return "", selfinstall.Options{}, "", false
		}
		switch name {
		case "--bin-dir":
			options.BinDir = assigned
		case "--data-dir":
			options.DataDir = assigned
		case "--plan-sha256":
			plan = assigned
		}
	}
	if action != "apply" && plan != "" || action == "apply" && !validGCPlan(plan) {
		return "", selfinstall.Options{}, "", false
	}
	return action, options, plan, true
}

func validGCPlan(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validGCResult(action string, result selfinstall.GCResult) bool {
	if result.State != selfinstall.StateInstalled || action != "recover" && !validGCPlan(result.PlanSHA256) {
		return false
	}
	for _, values := range [][]string{result.Candidates, result.Retained, result.Deleted, result.Recovered} {
		if !validGCDigestList(values) {
			return false
		}
	}
	if !disjointGCDigests(result.Candidates, result.Retained) {
		return false
	}
	switch action {
	case "preview":
		return !result.Changed && len(result.Deleted) == 0 && len(result.Recovered) == 0
	case "apply":
		return len(result.Recovered) == 0 && equalGCDigests(result.Deleted, result.Candidates) && result.Changed == (len(result.Candidates) != 0)
	case "recover":
		return result.PlanSHA256 == "" && len(result.Candidates) == 0 && len(result.Retained) == 0 && len(result.Deleted) == 0 && result.Changed == (len(result.Recovered) != 0)
	default:
		return false
	}
}

func validGCErrorApplyResult(result selfinstall.GCResult) bool {
	if result.State != selfinstall.StateInstalled || !validGCPlan(result.PlanSHA256) || len(result.Recovered) != 0 || !result.Changed {
		return false
	}
	for _, values := range [][]string{result.Candidates, result.Retained, result.Deleted} {
		if !validGCDigestList(values) {
			return false
		}
	}
	if !disjointGCDigests(result.Candidates, result.Retained) || len(result.Deleted) == 0 || len(result.Deleted) > len(result.Candidates) {
		return false
	}
	return equalGCDigests(result.Deleted, result.Candidates[:len(result.Deleted)])
}

func validGCDigestList(values []string) bool {
	for index, value := range values {
		if !validGCPlan(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func disjointGCDigests(left, right []string) bool {
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		switch {
		case left[leftIndex] < right[rightIndex]:
			leftIndex++
		case left[leftIndex] > right[rightIndex]:
			rightIndex++
		default:
			return false
		}
	}
	return true
}

func equalGCDigests(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writeGCDigests(output *strings.Builder, prefix string, values []string) {
	values = append([]string(nil), values...)
	sort.Strings(values)
	fmt.Fprintf(output, "%s_count=%d\n", prefix, len(values))
	for _, value := range values {
		fmt.Fprintf(output, "%s_sha256=%s\n", prefix, value)
	}
}

func writeGCResult(stdout io.Writer, action string, result selfinstall.GCResult) {
	var output strings.Builder
	fmt.Fprintf(&output, "state=%s\n", result.State)
	if action != "recover" {
		fmt.Fprintf(&output, "gc_plan_sha256=%s\n", result.PlanSHA256)
	}
	writeGCDigests(&output, "gc_candidate", result.Candidates)
	writeGCDigests(&output, "gc_retained", result.Retained)
	if action == "apply" {
		writeGCDigests(&output, "gc_deleted", result.Deleted)
	}
	if action == "recover" {
		writeGCDigests(&output, "gc_recovered", result.Recovered)
	}
	fmt.Fprintf(&output, "changed=%t\n", result.Changed)
	_, _ = io.WriteString(stdout, output.String())
}

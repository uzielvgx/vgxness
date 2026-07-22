package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/vgxness/vgxness/internal/bridge"
)

func runBridge(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime bridge.Runtime) int {
	validCommands := map[string]bool{
		"status": true, "prepare": true, "complete": true, "fail": true, "read": true,
		"orchestrate-plan": true, "orchestrate-wave": true, "orchestrate-terminal": true,
		"orchestrate-join": true, "orchestrate-status": true, "orchestrate-resume": true, "orchestrate-cancel": true,
	}
	if len(args) == 0 || !validCommands[args[0]] {
		fmt.Fprintln(stderr, "usage: vgxness bridge <status|prepare|complete|fail|read|orchestrate-plan|orchestrate-wave|orchestrate-terminal|orchestrate-join|orchestrate-status|orchestrate-resume|orchestrate-cancel> --workspace PATH [--stdin]")
		fmt.Fprintln(stderr, "note: native sessions create a child, then use prepare, read, complete, and fail")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet("bridge "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace string
	var fromStdin bool
	flags.StringVar(&workspace, "workspace", "", "absolute OpenCode workspace")
	if command != "status" {
		flags.BoolVar(&fromStdin, "stdin", false, "read one bounded JSON request from stdin")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || workspace == "" || command != "status" && !fromStdin {
		response := bridge.ErrorResponse(bridge.ErrInvalid)
		_ = bridge.Encode(stdout, response)
		return 2
	}
	if runtime == nil {
		response := bridge.ErrorResponse(bridge.ErrUnavailable)
		_ = bridge.Encode(stdout, response)
		return 1
	}
	var (
		response bridge.Response
		err      error
	)
	if command == "status" {
		response, err = runtime.Status(ctx, workspace)
	} else {
		native, ok := runtime.(bridge.NativeRuntime)
		if !ok {
			response = bridge.ErrorResponse(bridge.ErrUnavailable)
			_ = bridge.Encode(stdout, response)
			return 1
		}
		switch command {
		case "prepare":
			request, decodeErr := bridge.DecodeDispatch(stdin)
			if decodeErr != nil || bridge.ValidateNativePrepare(request) != nil {
				response = bridge.ErrorResponse(decodeErr)
				_ = bridge.Encode(stdout, response)
				return 2
			}
			response, err = native.Prepare(ctx, workspace, request)
		case "complete":
			request, decodeErr := bridge.DecodeNativeCompletion(stdin)
			if decodeErr != nil {
				response = bridge.ErrorResponse(decodeErr)
				_ = bridge.Encode(stdout, response)
				return 2
			}
			response, err = native.Complete(ctx, workspace, request)
		case "fail":
			request, decodeErr := bridge.DecodeNativeFailure(stdin)
			if decodeErr != nil {
				response = bridge.ErrorResponse(decodeErr)
				_ = bridge.Encode(stdout, response)
				return 2
			}
			response, err = native.Fail(ctx, workspace, request)
		case "read":
			request, decodeErr := bridge.DecodeNativeRead(stdin)
			if decodeErr != nil {
				response = bridge.ErrorResponse(decodeErr)
				_ = bridge.Encode(stdout, response)
				return 2
			}
			response, err = native.ReadNative(ctx, workspace, request)
		case "orchestrate-plan", "orchestrate-wave", "orchestrate-terminal", "orchestrate-join", "orchestrate-status", "orchestrate-resume", "orchestrate-cancel":
			orchestration, ok := runtime.(bridge.OrchestrationRuntime)
			if !ok {
				response = bridge.ErrorResponse(bridge.ErrUnavailable)
				_ = bridge.Encode(stdout, response)
				return 1
			}
			switch command {
			case "orchestrate-plan":
				request, decodeErr := bridge.DecodeOrchestratePlan(stdin)
				if decodeErr != nil {
					response = bridge.ErrorResponse(decodeErr)
					_ = bridge.Encode(stdout, response)
					return 2
				}
				response, err = orchestration.PlanOrchestration(ctx, workspace, request)
			case "orchestrate-wave":
				request, decodeErr := bridge.DecodeOrchestrateWave(stdin)
				if decodeErr != nil {
					response = bridge.ErrorResponse(decodeErr)
					_ = bridge.Encode(stdout, response)
					return 2
				}
				response, err = orchestration.PrepareOrchestrationWave(ctx, workspace, request)
			case "orchestrate-terminal":
				request, decodeErr := bridge.DecodeOrchestrateTerminal(stdin)
				if decodeErr != nil {
					response = bridge.ErrorResponse(decodeErr)
					_ = bridge.Encode(stdout, response)
					return 2
				}
				response, err = orchestration.RecordOrchestrationTerminal(ctx, workspace, request)
			case "orchestrate-join", "orchestrate-status", "orchestrate-resume", "orchestrate-cancel":
				request, decodeErr := bridge.DecodeOrchestrateReference(stdin)
				if decodeErr != nil {
					response = bridge.ErrorResponse(decodeErr)
					_ = bridge.Encode(stdout, response)
					return 2
				}
				switch command {
				case "orchestrate-join":
					response, err = orchestration.JoinOrchestration(ctx, workspace, request)
				case "orchestrate-status":
					response, err = orchestration.StatusOrchestration(ctx, workspace, request)
				case "orchestrate-resume":
					response, err = orchestration.ResumeOrchestration(ctx, workspace, request)
				case "orchestrate-cancel":
					response, err = orchestration.CancelOrchestration(ctx, workspace, request)
				}
			}
		}
	}
	if err != nil {
		response = bridge.ErrorResponse(err)
	}
	if encodeErr := bridge.Encode(stdout, response); encodeErr != nil {
		fmt.Fprintln(stderr, "operational: bridge output failed")
		return 1
	}
	if !response.OK {
		return 1
	}
	return 0
}

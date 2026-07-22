package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/vgxness/vgxness/internal/bridge"
)

func runBridge(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime bridge.Runtime) int {
	if len(args) == 0 || (args[0] != "status" && args[0] != "prepare" && args[0] != "complete" && args[0] != "fail" && args[0] != "read") {
		fmt.Fprintln(stderr, "usage: vgxness bridge <status|prepare|complete|fail|read> --workspace PATH [--stdin]")
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

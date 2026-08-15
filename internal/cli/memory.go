package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

type MemoryRuntime interface {
	Remember(context.Context, config.Options, memory.Remember) (memory.Entry, error)
	Recall(context.Context, config.Options, memory.Recall) ([]memory.Entry, error)
	Recent(context.Context, config.Options, memory.Recent) ([]memory.Entry, error)
	Get(context.Context, config.Options, memory.Lookup) (memory.Entry, error)
	Forget(context.Context, config.Options, memory.Forget) (memory.Entry, error)
	ResolveProject(context.Context, config.Options, string) (string, error)
	Sync(context.Context, config.Options) (memory.SyncResult, error)
	ConfigureSync(context.Context, config.Options, string, string, string) (memory.SyncConfigurationStatus, error)
	SyncStatus(context.Context, config.Options) (memory.SyncConfigurationStatus, error)
	BackfillSyncProject(context.Context, config.Options, string, int) (memory.SyncBackfillResult, error)
}

type memoryInput struct {
	SchemaVersion int          `json:"schemaVersion"`
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Content       string       `json:"content"`
	Query         string       `json:"query"`
	Project       string       `json:"project"`
	Scope         memory.Scope `json:"scope"`
	Type          string       `json:"type"`
	TopicKey      string       `json:"topic"`
	Session       string       `json:"session"`
	Limit         int          `json:"limit"`
	MatchAny      bool         `json:"matchAny"`
}

func runMemory(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime MemoryRuntime) int {
	if len(args) == 0 || (args[0] != "save" && args[0] != "search" && args[0] != "recent" && args[0] != "get" && args[0] != "forget" && args[0] != "sync") || runtime == nil {
		fmt.Fprintln(stderr, "invalid: unsupported memory operation")
		return 2
	}
	verb := args[0]
	if verb == "sync" {
		if len(args) > 1 {
			switch args[1] {
			case "configure":
				return runMemorySyncConfigure(ctx, args[2:], stdin, stdout, stderr, runtime)
			case "status":
				return runMemorySyncStatus(ctx, args[2:], stdout, stderr, runtime)
			case "backfill":
				return runMemorySyncBackfill(ctx, args[2:], stdout, stderr, runtime)
			default:
				if !strings.HasPrefix(args[1], "-") {
					return memoryFailure(stderr, memory.ErrInvalid)
				}
			}
		}
		flags := flag.NewFlagSet(verb, flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var opts config.Options
		var jsonOutput bool
		flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
		flags.StringVar(&opts.CredentialFile, "credential-file", "", "absolute credential file")
		flags.BoolVar(&opts.ProjectLocal, "project-local", false, "project-local storage")
		flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return memoryFailure(stderr, memory.ErrInvalid)
		}
		result, err := runtime.Sync(ctx, opts)
		if err != nil {
			return memoryFailure(stderr, err)
		}
		if jsonOutput {
			_ = json.NewEncoder(stdout).Encode(struct {
				SchemaVersion int               `json:"schemaVersion"`
				Result        memory.SyncResult `json:"result"`
			}{SchemaVersion: 1, Result: result})
		} else {
			fmt.Fprintf(stdout, "status=%s\npushed=%d\npreviously_accepted=%d\nrejected=%d\nretried=%d\nconflicts=%d\nbatches=%d\n", result.Status, result.Pushed, result.PreviouslyAccepted, result.Rejected, result.Retried, result.Conflicts, result.Batches)
		}
		return 0
	}
	flags := flag.NewFlagSet(verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputPath, project, scope, workspace string
	var stdinSource, jsonOutput bool
	var limit int
	flags.StringVar(&inputPath, "input", "", "JSON input file")
	flags.BoolVar(&stdinSource, "stdin", false, "read JSON from stdin")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	flags.StringVar(&project, "project", "", "project")
	flags.StringVar(&scope, "scope", "", "scope")
	flags.StringVar(&workspace, "workspace", "", "canonical workspace used to resolve the project")
	flags.IntVar(&limit, "limit", 0, "search limit")
	var opts config.Options
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.StringVar(&opts.CredentialFile, "credential-file", "", "absolute credential file")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "project-local storage")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || (inputPath != "") == stdinSource {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	data, err := memoryInputBytes(inputPath, stdinSource, stdin)
	if err != nil {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	input, payloadFields, err := decodeMemoryInput(data)
	if err != nil || input.SchemaVersion != 1 {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["project"] && payloadFields["project"] || set["scope"] && payloadFields["scope"] || set["limit"] && payloadFields["limit"] || workspace != "" && (set["project"] || payloadFields["project"]) {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	if set["project"] {
		input.Project = project
	}
	if set["scope"] {
		input.Scope = memory.Scope(scope)
	}
	if set["limit"] {
		input.Limit = limit
	}
	if workspace != "" {
		absolute, absErr := filepath.Abs(workspace)
		if absErr != nil {
			return memoryFailure(stderr, memory.ErrInvalid)
		}
		optionsWorkspace := filepath.Clean(absolute)
		opts.ProjectDir = optionsWorkspace
		input.Project, err = runtime.ResolveProject(ctx, opts, optionsWorkspace)
		if err != nil {
			return memoryFailure(stderr, err)
		}
		if input.Scope == "" {
			input.Scope = memory.ScopeProject
		}
	}

	var result any
	switch verb {
	case "save":
		if input.ID != "" || input.Query != "" || input.Limit != 0 || payloadFields["matchAny"] {
			err = memory.ErrInvalid
			break
		}
		result, err = runtime.Remember(ctx, opts, memory.Remember{Title: input.Title, Content: input.Content, Project: input.Project, Scope: input.Scope, Type: input.Type, TopicKey: input.TopicKey, Session: input.Session})
	case "search":
		if input.ID != "" || input.Title != "" || input.Content != "" || input.Session != "" {
			err = memory.ErrInvalid
			break
		}
		result, err = runtime.Recall(ctx, opts, memory.Recall{Query: input.Query, Project: input.Project, Scope: input.Scope, Type: input.Type, TopicKey: input.TopicKey, Limit: input.Limit, MatchAny: input.MatchAny})
	case "recent":
		if input.ID != "" || input.Title != "" || input.Content != "" || input.Query != "" || input.Type != "" || input.TopicKey != "" || input.Session != "" || payloadFields["matchAny"] {
			err = memory.ErrInvalid
			break
		}
		result, err = runtime.Recent(ctx, opts, memory.Recent{Project: input.Project, Scope: input.Scope, Limit: input.Limit})
	case "get", "forget":
		if input.Title != "" || input.Content != "" || input.Query != "" || input.Type != "" || input.TopicKey != "" || input.Session != "" || input.Limit != 0 || payloadFields["matchAny"] {
			err = memory.ErrInvalid
			break
		}
		lookup := memory.Lookup{ID: input.ID, Project: input.Project, Scope: input.Scope}
		if verb == "get" {
			result, err = runtime.Get(ctx, opts, lookup)
		} else {
			result, err = runtime.Forget(ctx, opts, memory.Forget(lookup))
		}
	}
	if err != nil {
		return memoryFailure(stderr, err)
	}
	var output bytes.Buffer
	if jsonOutput {
		_ = json.NewEncoder(&output).Encode(struct {
			SchemaVersion int `json:"schemaVersion"`
			Result        any `json:"result"`
		}{1, result})
	} else {
		switch value := result.(type) {
		case memory.Entry:
			fmt.Fprintf(&output, "id=%s\ntitle=%s\ncontent=%s\n", terminalSafe(value.ID), terminalSafe(value.Title), terminalSafe(value.Content))
		case []memory.Entry:
			for _, item := range value {
				fmt.Fprintf(&output, "id=%s title=%s preview=%s\n", terminalSafe(item.ID), terminalSafe(item.Title), terminalSafe(item.Preview))
			}
		}
	}
	_, _ = io.Copy(stdout, &output)
	return 0
}

const maxSyncBearerBytes = 512

func runMemorySyncConfigure(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime MemoryRuntime) int {
	flags := flag.NewFlagSet("sync configure", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts config.Options
	var endpoint, deviceID string
	flags.StringVar(&endpoint, "endpoint", "", "sync endpoint")
	flags.StringVar(&deviceID, "device-id", "", "sync device ID")
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.StringVar(&opts.CredentialFile, "credential-file", "", "absolute credential file")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "project-local storage")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || opts.ProjectLocal {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	seen := map[string]bool{}
	flags.Visit(func(value *flag.Flag) { seen[value.Name] = true })
	if !seen["endpoint"] || !seen["device-id"] {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	if _, err := memory.ValidateSyncProfile(memory.SyncProfile{Enabled: true, Endpoint: endpoint, DeviceID: deviceID, CredentialRef: "secret://keychain/sync/pending"}); err != nil {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	bearer := ""
	if opts.CredentialFile == "" {
		var err error
		bearer, err = syncBearer(stdin)
		if err != nil {
			return memoryFailure(stderr, memory.ErrInvalid)
		}
	}
	status, err := runtime.ConfigureSync(ctx, opts, endpoint, deviceID, bearer)
	if err != nil {
		return memoryFailure(stderr, err)
	}
	fmt.Fprintf(stdout, "configured=%t\nenabled=%t\ncredential=%s\n", status.Configured, status.Enabled, status.Credential)
	return 0
}

func syncBearer(stdin io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(stdin, maxSyncBearerBytes+3))
	if err != nil || len(data) == 0 || len(data) > maxSyncBearerBytes+2 {
		return "", memory.ErrInvalid
	}
	value := string(data)
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else {
		value = strings.TrimSuffix(value, "\n")
	}
	if len(value) == 0 || len(value) > maxSyncBearerBytes || strings.ContainsAny(value, "\r\n") {
		return "", memory.ErrInvalid
	}
	return value, nil
}

func runMemorySyncStatus(ctx context.Context, args []string, stdout, stderr io.Writer, runtime MemoryRuntime) int {
	flags := flag.NewFlagSet("sync status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts config.Options
	var jsonOutput bool
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.StringVar(&opts.CredentialFile, "credential-file", "", "absolute credential file")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "project-local storage")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	status, err := runtime.SyncStatus(ctx, opts)
	if err != nil {
		return memoryFailure(stderr, err)
	}
	if jsonOutput {
		_ = json.NewEncoder(stdout).Encode(struct {
			SchemaVersion int `json:"schemaVersion"`
			memory.SyncConfigurationStatus
		}{SchemaVersion: 1, SyncConfigurationStatus: status})
	} else {
		fmt.Fprintf(stdout, "configured=%t\nenabled=%t\ncredential=%s\n", status.Configured, status.Enabled, status.Credential)
	}
	return 0
}

func runMemorySyncBackfill(ctx context.Context, args []string, stdout, stderr io.Writer, runtime MemoryRuntime) int {
	flags := flag.NewFlagSet("sync backfill", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts config.Options
	var workspace string
	var jsonOutput bool
	var limit int
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.StringVar(&workspace, "workspace", "", "workspace")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	flags.IntVar(&limit, "limit", 100, "maximum records")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || workspace == "" || limit < 1 || limit > 1000 {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return memoryFailure(stderr, memory.ErrInvalid)
	}
	result, err := runtime.BackfillSyncProject(ctx, opts, filepath.Clean(absolute), limit)
	if err != nil {
		return memoryFailure(stderr, err)
	}
	if jsonOutput {
		_ = json.NewEncoder(stdout).Encode(result)
	} else {
		fmt.Fprintf(stdout, "schema_version=%d\nlimit=%d\nremaining=%t\nprojects=%d\nsessions=%d\nobservations=%d\nqueued=%d\n", result.SchemaVersion, result.Limit, result.Remaining, result.Projects, result.Sessions, result.Observations, result.Queued)
	}
	return 0
}

func decodeMemoryInput(data []byte) (memoryInput, map[string]bool, error) {
	var input memoryInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return input, nil, memory.ErrInvalid
	}
	fields := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] {
			return input, nil, memory.ErrInvalid
		}
		fields[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return input, nil, memory.ErrInvalid
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(&struct{}{}) != io.EOF {
		return input, nil, memory.ErrInvalid
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return input, nil, memory.ErrInvalid
	}
	return input, fields, nil
}

func memoryInputBytes(path string, fromStdin bool, stdin io.Reader) ([]byte, error) {
	reader := stdin
	if !fromStdin {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil || resolved != absolute {
			return nil, errors.New("invalid input path")
		}
		file, err := os.Open(absolute)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, 65537))
	if err != nil || len(data) > 65536 {
		return nil, errors.New("invalid input")
	}
	return data, nil
}

func memoryFailure(stderr io.Writer, err error) int {
	code, message := failure(err)
	fmt.Fprintln(stderr, message)
	return code
}

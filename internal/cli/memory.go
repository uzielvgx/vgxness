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

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
)

type MemoryRuntime interface {
	Save(context.Context, config.Options, memory.SaveRequest) (memory.MemoryResult, error)
	Search(context.Context, config.Options, memory.SearchRequest) ([]memory.MemoryResult, error)
	Get(context.Context, config.Options, memory.GetRequest) (memory.MemoryResult, error)
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
}

func runMemory(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime MemoryRuntime) int {
	if len(args) == 0 || (args[0] != "save" && args[0] != "search" && args[0] != "get") || runtime == nil {
		fmt.Fprintln(stderr, "invalid: unsupported memory operation")
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet(verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var inputPath, project, scope string
	var stdinSource, jsonOutput bool
	var limit int
	flags.StringVar(&inputPath, "input", "", "JSON input file")
	flags.BoolVar(&stdinSource, "stdin", false, "read JSON from stdin")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	flags.StringVar(&project, "project", "", "project")
	flags.StringVar(&scope, "scope", "", "scope")
	flags.IntVar(&limit, "limit", 0, "search limit")
	var opts config.Options
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
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
	if set["project"] && payloadFields["project"] || set["scope"] && payloadFields["scope"] || set["limit"] && payloadFields["limit"] {
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

	var result any
	switch verb {
	case "save":
		if input.ID != "" || input.Query != "" || input.Limit != 0 {
			err = memory.ErrInvalid
			break
		}
		result, err = runtime.Save(ctx, opts, memory.SaveRequest{Title: input.Title, Content: input.Content, Project: input.Project, Scope: input.Scope, Type: input.Type, TopicKey: input.TopicKey, Session: input.Session})
	case "search":
		if input.ID != "" || input.Title != "" || input.Content != "" || input.Session != "" {
			err = memory.ErrInvalid
			break
		}
		result, err = runtime.Search(ctx, opts, memory.SearchRequest{Query: input.Query, Project: input.Project, Scope: input.Scope, Type: input.Type, TopicKey: input.TopicKey, Limit: input.Limit})
	case "get":
		if input.Title != "" || input.Content != "" || input.Query != "" || input.Type != "" || input.TopicKey != "" || input.Session != "" || input.Limit != 0 {
			err = memory.ErrInvalid
			break
		}
		result, err = runtime.Get(ctx, opts, memory.GetRequest{ID: input.ID, Project: input.Project, Scope: input.Scope})
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
		case memory.MemoryResult:
			fmt.Fprintf(&output, "id=%s\ntitle=%s\ncontent=%s\n", terminalSafe(value.ID), terminalSafe(value.Title), terminalSafe(value.Content))
		case []memory.MemoryResult:
			for _, item := range value {
				fmt.Fprintf(&output, "id=%s title=%s preview=%s\n", terminalSafe(item.ID), terminalSafe(item.Title), terminalSafe(item.Preview))
			}
		}
	}
	_, _ = io.Copy(stdout, &output)
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

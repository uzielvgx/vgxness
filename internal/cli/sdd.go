package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/sdd"
)

type SDDRuntime interface {
	ResolveSDDProject(context.Context, config.Options, string) (string, error)
	CreateChange(context.Context, config.Options, sdd.CreateChangeRequest) (sdd.Change, error)
	ListChanges(context.Context, config.Options, sdd.ListChangesRequest) ([]sdd.Change, error)
	GetChange(context.Context, config.Options, sdd.GetChangeRequest) (sdd.Change, error)
	UpdateInteractionMode(context.Context, config.Options, sdd.UpdateInteractionModeRequest) (sdd.Change, error)
	SaveRevision(context.Context, config.Options, sdd.SaveRevisionRequest) (sdd.Revision, error)
	GetRevision(context.Context, config.Options, sdd.GetRevisionRequest) (sdd.Revision, error)
	ListRevisions(context.Context, config.Options, sdd.ListRevisionsRequest) ([]sdd.Revision, error)
	AcceptRevision(context.Context, config.Options, sdd.AcceptRevisionRequest) (sdd.Revision, error)
	TransitionChange(context.Context, config.Options, sdd.TransitionChangeRequest) (sdd.Change, error)
	ProjectionStatus(context.Context, config.Options, sdd.ProjectionStatusRequest) (sdd.Projection, error)
	RecordProjection(context.Context, config.Options, sdd.RecordProjectionRequest) (sdd.Projection, error)
	RenderProjection(context.Context, config.Options, sdd.RenderProjectionRequest) (sdd.ProjectionDocument, error)
	CompareProjection(context.Context, config.Options, sdd.CompareProjectionRequest) (sdd.ProjectionComparison, error)
}

type sddInput struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	Project              string                `json:"project"`
	ID                   string                `json:"id"`
	IdempotencyKey       string                `json:"idempotencyKey"`
	Title                string                `json:"title"`
	Backend              sdd.Backend           `json:"backend"`
	InteractionMode      sdd.InteractionMode   `json:"interactionMode"`
	Plan                 sdd.Plan              `json:"plan"`
	Status               sdd.ProjectionStatus  `json:"status"`
	ChangeStatus         sdd.ChangeStatus      `json:"changeStatus"`
	ChangeID             string                `json:"changeId"`
	Artifact             sdd.Phase             `json:"artifact"`
	ArtifactID           string                `json:"artifactId"`
	RevisionID           string                `json:"revisionId"`
	Content              string                `json:"content"`
	ExternalLocation     string                `json:"externalLocation"`
	Digest               sdd.Digest            `json:"digest"`
	Inputs               []sdd.RevisionBinding `json:"inputs"`
	InputDigest          sdd.Digest            `json:"inputDigest"`
	ExpectedStateVersion int64                 `json:"expectedStateVersion"`
	TargetPhase          sdd.Phase             `json:"targetPhase"`
	Cancel               bool                  `json:"cancel"`
	Location             string                `json:"location"`
	Limit                int                   `json:"limit"`
	RelativePath         string                `json:"relativePath"`
	ProjectionContent    string                `json:"projectionContent"`
	Missing              bool                  `json:"missing"`
	Symlink              bool                  `json:"symlink"`
}

func runSDD(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime SDDRuntime) int {
	operations := map[string]bool{"create": true, "list": true, "get": true, "set-interaction-mode": true, "save-revision": true, "get-revision": true, "list-revisions": true, "accept-revision": true, "transition": true, "projection-status": true, "record-projection": true, "render-projection": true, "compare-projection": true}
	if len(args) == 0 || !operations[args[0]] || runtime == nil {
		fmt.Fprintln(stderr, "invalid: unsupported SDD operation")
		return 2
	}
	operation := args[0]
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var workspace string
	var stdinSource, jsonOutput bool
	var opts config.Options
	flags.BoolVar(&stdinSource, "stdin", false, "read JSON from stdin")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	flags.StringVar(&workspace, "workspace", "", "canonical workspace used to resolve the project")
	flags.StringVar(&opts.StorageRoot, "storage-root", "", "storage root")
	flags.BoolVar(&opts.ProjectLocal, "project-local", false, "project-local storage")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return sddFailure(stderr, sdd.ErrInvalid)
	}
	if !stdinSource {
		fmt.Fprintln(stderr, "invalid: use --stdin to provide SDD JSON input")
		return 2
	}
	if workspace == "" {
		fmt.Fprintln(stderr, "invalid: provide --workspace for the trusted project")
		return 2
	}
	data, err := memoryInputBytes("", true, stdin)
	if err != nil {
		return sddFailure(stderr, sdd.ErrInvalid)
	}
	input, fields, err := decodeSDDInput(data)
	if err != nil {
		fmt.Fprintln(stderr, "invalid: provide one SDD JSON object with schemaVersion 1")
		return 2
	}
	if input.SchemaVersion != 1 || fields["project"] {
		fmt.Fprintln(stderr, "invalid: schemaVersion must be 1 and project is resolved from --workspace")
		return 2
	}
	if field := unexpectedSDDField(operation, fields); field != "" {
		fmt.Fprintf(stderr, "invalid: field %s is not accepted by SDD %s\n", field, operation)
		return 2
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return sddFailure(stderr, sdd.ErrInvalid)
	}
	opts.ProjectDir = filepath.Clean(absolute)
	project, err := runtime.ResolveSDDProject(ctx, opts, opts.ProjectDir)
	if err != nil {
		return sddFailure(stderr, err)
	}

	var result any
	switch operation {
	case "create":
		request := sdd.CreateChangeRequest{Project: project, IdempotencyKey: input.IdempotencyKey, Title: input.Title, Backend: input.Backend, InteractionMode: input.InteractionMode, Plan: input.Plan}
		if onlySDDFields(fields, "schemaVersion", "idempotencyKey", "title", "backend", "interactionMode", "plan") && request.Validate() == nil {
			result, err = runtime.CreateChange(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "list":
		request := sdd.ListChangesRequest{Project: project, Status: input.ChangeStatus, Limit: input.Limit}
		if onlySDDFields(fields, "schemaVersion", "changeStatus", "limit") && request.Validate() == nil {
			result, err = runtime.ListChanges(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "get":
		request := sdd.GetChangeRequest{Project: project, ID: input.ID}
		if onlySDDFields(fields, "schemaVersion", "id") && request.Validate() == nil {
			result, err = runtime.GetChange(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "set-interaction-mode":
		request := sdd.UpdateInteractionModeRequest{Project: project, ChangeID: input.ChangeID, InteractionMode: input.InteractionMode, ExpectedStateVersion: input.ExpectedStateVersion}
		if onlySDDFields(fields, "schemaVersion", "changeId", "interactionMode", "expectedStateVersion") && request.Validate() == nil {
			result, err = runtime.UpdateInteractionMode(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "save-revision":
		request := sdd.SaveRevisionRequest{Project: project, ChangeID: input.ChangeID, Artifact: input.Artifact, Content: []byte(input.Content), ExternalLocation: input.ExternalLocation, Digest: input.Digest, Inputs: input.Inputs, InputDigest: input.InputDigest, ExpectedStateVersion: input.ExpectedStateVersion}
		if onlySDDFields(fields, "schemaVersion", "changeId", "artifact", "content", "externalLocation", "digest", "inputs", "inputDigest", "expectedStateVersion") && request.Validate() == nil {
			result, err = runtime.SaveRevision(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "get-revision":
		request := sdd.GetRevisionRequest{Project: project, ChangeID: input.ChangeID, RevisionID: input.RevisionID}
		if onlySDDFields(fields, "schemaVersion", "changeId", "revisionId") && request.Validate() == nil {
			result, err = runtime.GetRevision(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "list-revisions":
		request := sdd.ListRevisionsRequest{Project: project, ChangeID: input.ChangeID, Artifact: input.Artifact, Limit: input.Limit}
		if onlySDDFields(fields, "schemaVersion", "changeId", "artifact", "limit") && request.Validate() == nil {
			result, err = runtime.ListRevisions(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "accept-revision":
		request := sdd.AcceptRevisionRequest{Project: project, ChangeID: input.ChangeID, RevisionID: input.RevisionID, ExpectedStateVersion: input.ExpectedStateVersion}
		if onlySDDFields(fields, "schemaVersion", "changeId", "revisionId", "expectedStateVersion") && request.Validate() == nil {
			result, err = runtime.AcceptRevision(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "transition":
		request := sdd.TransitionChangeRequest{Project: project, ChangeID: input.ChangeID, TargetPhase: input.TargetPhase, Cancel: input.Cancel, ExpectedStateVersion: input.ExpectedStateVersion}
		if onlySDDFields(fields, "schemaVersion", "changeId", "targetPhase", "cancel", "expectedStateVersion") && request.Validate() == nil {
			result, err = runtime.TransitionChange(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "projection-status":
		request := sdd.ProjectionStatusRequest{Project: project, ChangeID: input.ChangeID, ArtifactID: input.ArtifactID}
		if onlySDDFields(fields, "schemaVersion", "changeId", "artifactId") && request.Validate() == nil {
			result, err = runtime.ProjectionStatus(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "record-projection":
		request := sdd.RecordProjectionRequest{Project: project, ChangeID: input.ChangeID, ArtifactID: input.ArtifactID, RevisionID: input.RevisionID, Status: input.Status, Digest: input.Digest, Location: input.Location, ExpectedStateVersion: input.ExpectedStateVersion}
		if onlySDDFields(fields, "schemaVersion", "changeId", "artifactId", "revisionId", "status", "digest", "location", "expectedStateVersion") && request.Validate() == nil {
			result, err = runtime.RecordProjection(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "render-projection":
		request := sdd.RenderProjectionRequest{Project: project, ChangeID: input.ChangeID, RevisionID: input.RevisionID}
		if onlySDDFields(fields, "schemaVersion", "changeId", "revisionId") && request.Validate() == nil {
			result, err = runtime.RenderProjection(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	case "compare-projection":
		request := sdd.CompareProjectionRequest{Project: project, ChangeID: input.ChangeID, RevisionID: input.RevisionID, Input: sdd.ProjectionInput{RelativePath: input.RelativePath, Content: []byte(input.ProjectionContent), Missing: input.Missing, Symlink: input.Symlink}}
		if onlySDDFields(fields, "schemaVersion", "changeId", "revisionId", "relativePath", "projectionContent", "missing", "symlink") && request.Validate() == nil {
			result, err = runtime.CompareProjection(ctx, opts, request)
		} else {
			err = sdd.ErrInvalid
		}
	}
	if err != nil {
		return sddFailure(stderr, err)
	}
	var output bytes.Buffer
	if jsonOutput {
		_ = json.NewEncoder(&output).Encode(struct {
			SchemaVersion int `json:"schemaVersion"`
			Result        any `json:"result"`
		}{SchemaVersion: 1, Result: result})
	} else {
		fmt.Fprintf(&output, "%s\n", terminalSafe(fmt.Sprint(result)))
	}
	_, _ = io.Copy(stdout, &output)
	return 0
}

func decodeSDDInput(data []byte) (sddInput, map[string]bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return sddInput{}, nil, sdd.ErrInvalid
	}
	fields := make(map[string]bool)
	for decoder.More() {
		token, err = decoder.Token()
		field, ok := token.(string)
		if err != nil || !ok || fields[field] {
			return sddInput{}, nil, sdd.ErrInvalid
		}
		fields[field] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return sddInput{}, nil, sdd.ErrInvalid
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(&struct{}{}) != io.EOF {
		return sddInput{}, nil, sdd.ErrInvalid
	}
	var input sddInput
	decoder = json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&input); err != nil {
		return sddInput{}, nil, sdd.ErrInvalid
	}
	return input, fields, nil
}

func unexpectedSDDField(operation string, fields map[string]bool) string {
	allowed := map[string][]string{
		"create": {"schemaVersion", "idempotencyKey", "title", "backend", "interactionMode", "plan"}, "list": {"schemaVersion", "changeStatus", "limit"}, "get": {"schemaVersion", "id"},
		"set-interaction-mode": {"schemaVersion", "changeId", "interactionMode", "expectedStateVersion"}, "save-revision": {"schemaVersion", "changeId", "artifact", "content", "externalLocation", "digest", "inputs", "inputDigest", "expectedStateVersion"}, "get-revision": {"schemaVersion", "changeId", "revisionId"}, "list-revisions": {"schemaVersion", "changeId", "artifact", "limit"}, "accept-revision": {"schemaVersion", "changeId", "revisionId", "expectedStateVersion"}, "transition": {"schemaVersion", "changeId", "targetPhase", "cancel", "expectedStateVersion"}, "projection-status": {"schemaVersion", "changeId", "artifactId"}, "record-projection": {"schemaVersion", "changeId", "artifactId", "revisionId", "status", "digest", "location", "expectedStateVersion"}, "render-projection": {"schemaVersion", "changeId", "revisionId"}, "compare-projection": {"schemaVersion", "changeId", "revisionId", "relativePath", "projectionContent", "missing", "symlink"},
	}
	set := make(map[string]bool, len(allowed[operation]))
	for _, field := range allowed[operation] {
		set[field] = true
	}
	var unexpected string
	for field := range fields {
		if !set[field] {
			if unexpected == "" || field < unexpected {
				unexpected = field
			}
		}
	}
	return unexpected
}

func onlySDDFields(fields map[string]bool, allowed ...string) bool {
	set := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		set[field] = true
	}
	for field := range fields {
		if !set[field] {
			return false
		}
	}
	return true
}

func sddFailure(stderr io.Writer, err error) int {
	code, message := 1, "operational: SDD storage failed"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, sdd.ErrChangeCancelled):
		code, message = 130, "cancelled: operation cancelled"
	case errors.Is(err, sdd.ErrInvalid), errors.Is(err, sdd.ErrIllegalTransition), errors.Is(err, sdd.ErrDigestMismatch):
		code, message = 2, "invalid: SDD request is invalid"
	case errors.Is(err, sdd.ErrNotFound):
		message = "not_found: SDD record was not found"
	case errors.Is(err, sdd.ErrStaleState):
		message = "stale: SDD state version changed"
	case errors.Is(err, sdd.ErrInputsChanged), errors.Is(err, sdd.ErrImmutable), errors.Is(err, sdd.ErrConflict):
		message = "conflict: SDD state changed"
	}
	fmt.Fprintln(stderr, message)
	return code
}

package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ProtocolVersion          = "1"
	MaxRequestBytes          = 64 << 10
	MaxNativeResultBytes     = 2 << 20
	MaxNativeCompletionBytes = MaxNativeResultBytes + MaxRequestBytes
	MaxNativeReadBytes       = 256 << 10
	MaxBridgeOutputBytes     = 3*MaxNativeResultBytes + MaxRequestBytes
	maxModelBytes            = 512
)

var (
	ErrInvalid      = errors.New("invalid bridge request")
	ErrUnavailable  = errors.New("bridge unavailable")
	ErrIncompatible = errors.New("bridge incompatible")
	ErrDenied       = errors.New("bridge request denied")
	ErrExecution    = errors.New("bridge execution failed")
)

type Operation string
type ContinuityMode string

const (
	ReadFiles     Operation = "read-files"
	WriteFiles    Operation = "write-files"
	ReviewChanges Operation = "review-changes"

	ContinuitySingle   ContinuityMode = ""
	ContinuityStart    ContinuityMode = "start"
	ContinuityContinue ContinuityMode = "continue"
	ContinuityFinish   ContinuityMode = "finish"
)

var validRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type DispatchRequest struct {
	ProtocolVersion    string         `json:"protocolVersion"`
	TicketID           string         `json:"ticketId,omitempty"`
	Model              string         `json:"model"`
	Operation          Operation      `json:"operation"`
	Goal               string         `json:"goal"`
	AcceptanceCriteria []string       `json:"acceptanceCriteria,omitempty"`
	Continuity         ContinuityMode `json:"continuity,omitempty"`
	RunID              string         `json:"runId,omitempty"`
	ParentSessionID    string         `json:"parentSessionId,omitempty"`
	ParentMessageID    string         `json:"parentMessageId,omitempty"`
	ChildSessionID     string         `json:"childSessionId,omitempty"`
}

type NativeCompletionRequest struct {
	ProtocolVersion string          `json:"protocolVersion"`
	TicketID        string          `json:"ticketId"`
	ParentSessionID string          `json:"parentSessionId"`
	ChildSessionID  string          `json:"childSessionId"`
	MessageID       string          `json:"messageId"`
	Result          json.RawMessage `json:"result"`
}

type NativeFailureRequest struct {
	ProtocolVersion string `json:"protocolVersion"`
	TicketID        string `json:"ticketId"`
	ParentSessionID string `json:"parentSessionId"`
	ChildSessionID  string `json:"childSessionId,omitempty"`
	Category        string `json:"category"`
}

type NativeReadRequest struct {
	ProtocolVersion string `json:"protocolVersion"`
	TicketID        string `json:"ticketId"`
	ChildSessionID  string `json:"childSessionId"`
	Path            string `json:"path"`
	Offset          int64  `json:"offset,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

type NativeReadResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	NextOffset int64  `json:"nextOffset,omitempty"`
	Truncated  bool   `json:"truncated"`
}

type PreparedDispatch struct {
	TicketID     string        `json:"ticketId"`
	ExecutionID  string        `json:"executionId"`
	Agent        string        `json:"agent"`
	Model        string        `json:"model"`
	Prompt       string        `json:"prompt"`
	PromptSHA256 string        `json:"promptSha256"`
	Deadline     string        `json:"deadline"`
	PromptRef    PromptReceipt `json:"promptRef"`
}

type Error struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

type PromptReceipt struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Receipt struct {
	ExecutionID       string        `json:"executionId"`
	Decision          string        `json:"decision"`
	DecisionCondition string        `json:"decisionCondition"`
	Provider          string        `json:"provider"`
	ProviderID        string        `json:"providerId"`
	ProviderVersion   string        `json:"providerVersion"`
	Prompt            PromptReceipt `json:"prompt"`
	StartedAt         string        `json:"startedAt"`
	FinishedAt        string        `json:"finishedAt"`
	EventCount        int           `json:"eventCount"`
}

type Response struct {
	ProtocolVersion string            `json:"protocolVersion"`
	OK              bool              `json:"ok"`
	Bridge          string            `json:"bridge"`
	Provider        string            `json:"provider"`
	Workspace       string            `json:"workspace,omitempty"`
	RunID           string            `json:"runId,omitempty"`
	TaskID          string            `json:"taskId,omitempty"`
	CapsuleID       string            `json:"capsuleId,omitempty"`
	StateVersion    int               `json:"stateVersion,omitempty"`
	MemoryRefs      []string          `json:"memoryRefs,omitempty"`
	Status          string            `json:"status"`
	Result          json.RawMessage   `json:"result,omitempty"`
	Receipt         *Receipt          `json:"receipt,omitempty"`
	Prepared        *PreparedDispatch `json:"prepared,omitempty"`
	Read            *NativeReadResult `json:"read,omitempty"`
	Error           *Error            `json:"error,omitempty"`
}

type Runtime interface {
	Status(context.Context, string) (Response, error)
	Dispatch(context.Context, string, DispatchRequest) (Response, error)
}

type NativeRuntime interface {
	Runtime
	Prepare(context.Context, string, DispatchRequest) (Response, error)
	Complete(context.Context, string, NativeCompletionRequest) (Response, error)
	Fail(context.Context, string, NativeFailureRequest) (Response, error)
	ReadNative(context.Context, string, NativeReadRequest) (Response, error)
}

func DecodeDispatch(reader io.Reader) (DispatchRequest, error) {
	if reader == nil {
		return DispatchRequest{}, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxRequestBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxRequestBytes {
		return DispatchRequest{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request DispatchRequest
	if err := decoder.Decode(&request); err != nil {
		return DispatchRequest{}, ErrInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return DispatchRequest{}, ErrInvalid
	}
	if err := ValidateDispatch(request); err != nil {
		return DispatchRequest{}, err
	}
	return request, nil
}

func DecodeNativeCompletion(reader io.Reader) (NativeCompletionRequest, error) {
	var request NativeCompletionRequest
	if err := decodeExact(reader, MaxNativeCompletionBytes, &request); err != nil || ValidateNativeCompletion(request) != nil {
		return NativeCompletionRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeNativeFailure(reader io.Reader) (NativeFailureRequest, error) {
	var request NativeFailureRequest
	if err := decodeExact(reader, MaxRequestBytes, &request); err != nil || ValidateNativeFailure(request) != nil {
		return NativeFailureRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeNativeRead(reader io.Reader) (NativeReadRequest, error) {
	var request NativeReadRequest
	if err := decodeExact(reader, MaxRequestBytes, &request); err != nil || ValidateNativeRead(request) != nil {
		return NativeReadRequest{}, ErrInvalid
	}
	return request, nil
}

func ValidateNativeCompletion(request NativeCompletionRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ParentSessionID) || !validNativeIdentity(request.ChildSessionID) || !validNativeIdentity(request.MessageID) || len(request.Result) == 0 || len(request.Result) > MaxNativeResultBytes || !json.Valid(request.Result) {
		return ErrInvalid
	}
	return nil
}

func ValidateNativeFailure(request NativeFailureRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ParentSessionID) || request.ChildSessionID != "" && !validNativeIdentity(request.ChildSessionID) {
		return ErrInvalid
	}
	switch request.Category {
	case "native-subagent-failed", "native-subagent-cancelled", "native-subagent-deadline":
		return nil
	default:
		return ErrInvalid
	}
}

func ValidateNativeRead(request NativeReadRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ChildSessionID) || request.Path == "" || len(request.Path) > 4096 || strings.ContainsRune(request.Path, '\x00') || filepath.IsAbs(request.Path) || !filepath.IsLocal(request.Path) || filepath.Clean(request.Path) != request.Path || request.Path == "." || strings.HasSuffix(request.Path, "/") || strings.HasSuffix(request.Path, `\`) || request.Offset < 0 || request.Limit < 0 || request.Limit > MaxNativeReadBytes {
		return ErrInvalid
	}
	return nil
}

func decodeExact(reader io.Reader, limit int64, target any) error {
	if reader == nil || limit <= 0 {
		return ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func validNativeIdentity(value string) bool {
	return len(value) > 0 && len(value) <= 240 && validRunID.MatchString(value)
}

func ValidateDispatch(request DispatchRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validModel(request.Model) || !validOperation(request.Operation) {
		return ErrInvalid
	}
	goal := strings.TrimSpace(request.Goal)
	if goal == "" || utf8.RuneCountInString(request.Goal) > 8192 || len(request.AcceptanceCriteria) > 32 {
		return ErrInvalid
	}
	for index, criterion := range request.AcceptanceCriteria {
		criterion = strings.TrimSpace(criterion)
		if criterion == "" || utf8.RuneCountInString(request.AcceptanceCriteria[index]) > 2048 {
			return ErrInvalid
		}
	}
	if !validContinuity(request.Continuity, request.RunID) {
		return ErrInvalid
	}
	if request.TicketID != "" && !validNativeIdentity(request.TicketID) || request.ParentSessionID != "" && !validNativeIdentity(request.ParentSessionID) || request.ParentMessageID != "" && !validNativeIdentity(request.ParentMessageID) || request.ChildSessionID != "" && !validNativeIdentity(request.ChildSessionID) {
		return ErrInvalid
	}
	return nil
}

func ValidateNativePrepare(request DispatchRequest) error {
	if ValidateDispatch(request) != nil || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ParentSessionID) || !validNativeIdentity(request.ParentMessageID) || !validNativeIdentity(request.ChildSessionID) {
		return ErrInvalid
	}
	return nil
}

func validContinuity(mode ContinuityMode, runID string) bool {
	switch mode {
	case ContinuitySingle, ContinuityStart:
		return runID == ""
	case ContinuityContinue, ContinuityFinish:
		return len(runID) <= 240 && validRunID.MatchString(runID)
	default:
		return false
	}
}

func validModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > maxModelBytes || strings.HasPrefix(model, "-") || strings.ContainsAny(model, " \t\r\n\x00") {
		return false
	}
	provider, modelID, found := strings.Cut(model, "/")
	return found && provider != "" && modelID != "" && !strings.Contains(modelID, "/")
}

func Failure(err error) Error {
	switch {
	case errors.Is(err, context.Canceled):
		return Error{Code: "cancelled", Message: "bridge operation was cancelled", Recoverable: true}
	case errors.Is(err, context.DeadlineExceeded):
		return Error{Code: "deadline_exceeded", Message: "bridge operation exceeded its deadline", Recoverable: true}
	case errors.Is(err, ErrInvalid):
		return Error{Code: "invalid_request", Message: "bridge request is invalid"}
	case errors.Is(err, ErrIncompatible):
		return Error{Code: "incompatible", Message: "OpenCode is incompatible with this bridge"}
	case errors.Is(err, ErrDenied):
		return Error{Code: "denied", Message: "bridge request was denied by policy"}
	case errors.Is(err, ErrUnavailable):
		return Error{Code: "unavailable", Message: "OpenCode bridge is unavailable", Recoverable: true}
	default:
		return Error{Code: "execution_failed", Message: "bounded bridge execution failed", Recoverable: true}
	}
}

func ErrorResponse(err error) Response {
	failure := Failure(err)
	return Response{
		ProtocolVersion: ProtocolVersion,
		OK:              false,
		Bridge:          "unavailable",
		Provider:        "opencode",
		Status:          "error",
		Error:           &failure,
	}
}

func Encode(writer io.Writer, response Response) error {
	if writer == nil {
		return fmt.Errorf("%w: output writer", ErrExecution)
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}

func validOperation(operation Operation) bool {
	return operation == ReadFiles || operation == WriteFiles || operation == ReviewChanges
}

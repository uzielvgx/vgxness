package bridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/navigator"
)

const (
	ProtocolVersion             = "1"
	MaxRequestBytes             = 64 << 10
	MaxNativeResultBytes        = 2 << 20
	MaxOrchestrationResultBytes = 64 << 10
	MaxNativeCompletionBytes    = MaxNativeResultBytes + MaxRequestBytes
	MaxNativeReadBytes          = 256 << 10
	MaxNativeEditBytes          = 256 << 10
	MaxNativeEditRequestBytes   = 6*MaxNativeEditBytes + MaxRequestBytes
	MaxNativeCodeGraphBytes     = 512 << 10
	MaxBridgeOutputBytes        = 3*MaxNativeResultBytes + MaxRequestBytes
	maxModelBytes               = 512
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
type CodeGraphOperation string
type NativeValidationOperation string

const (
	ReadFiles        Operation = "read-files"
	AnalyzeStructure Operation = "analyze-structure"
	WriteFiles       Operation = "write-files"
	ReviewChanges    Operation = "review-changes"

	CodeGraphStatus   CodeGraphOperation = "status"
	CodeGraphExplore  CodeGraphOperation = "explore"
	CodeGraphImpact   CodeGraphOperation = "impact"
	CodeGraphAffected CodeGraphOperation = "affected"

	NativeValidationFormat NativeValidationOperation = "format"
	NativeValidationTest   NativeValidationOperation = "test"
	NativeValidationVet    NativeValidationOperation = "vet"

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

// OrchestrateInput is the complete untrusted public boundary. It intentionally
// carries no model, session identity, agent choice, task graph, or parallelism
// hint: those values come from trusted adapters or Navigator.
type OrchestrateInput struct {
	Goal               string   `json:"goal"`
	AcceptanceCriteria []string `json:"acceptanceCriteria,omitempty"`
}

type OrchestrateContext struct {
	Model           string
	ParentSessionID string
	ParentMessageID string
}

type OrchestrateRequest struct {
	ProtocolVersion string
	Model           string
	Input           OrchestrateInput
	ParentSessionID string
	ParentMessageID string
}

type OrchestratePlanRequest struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Model           string           `json:"model"`
	Input           OrchestrateInput `json:"input"`
	ParentSessionID string           `json:"parentSessionId"`
	ParentMessageID string           `json:"parentMessageId"`
	CandidateTasks  []navigator.Task `json:"candidateTasks"`
}

type OrchestrateWaveRequest struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	OrchestrationID string                 `json:"orchestrationId"`
	OwnerID         string                 `json:"ownerId"`
	Bindings        []OrchestrationBinding `json:"bindings"`
}

type OrchestrationBinding struct {
	TaskID         string `json:"taskId"`
	ChildSessionID string `json:"childSessionId"`
	TicketID       string `json:"ticketId"`
	ClaimToken     string `json:"claimToken"`
}

type OrchestrateTerminalRequest struct {
	ProtocolVersion string          `json:"protocolVersion"`
	OrchestrationID string          `json:"orchestrationId"`
	OwnerID         string          `json:"ownerId"`
	TaskID          string          `json:"taskId"`
	TicketID        string          `json:"ticketId"`
	ChildSessionID  string          `json:"childSessionId"`
	Status          string          `json:"status"`
	MessageID       string          `json:"messageId,omitempty"`
	ResultID        string          `json:"resultId,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Failure         string          `json:"failure,omitempty"`
}

type OrchestrateReferenceRequest struct {
	ProtocolVersion string `json:"protocolVersion"`
	OrchestrationID string `json:"orchestrationId"`
	OwnerID         string `json:"ownerId,omitempty"`
	TaskID          string `json:"taskId,omitempty"`
	ChildSessionID  string `json:"childSessionId,omitempty"`
	ClaimToken      string `json:"claimToken,omitempty"`
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
	Exists     bool   `json:"exists"`
	Content    string `json:"content"`
	SHA256     string `json:"sha256,omitempty"`
	NextOffset int64  `json:"nextOffset,omitempty"`
	Truncated  bool   `json:"truncated"`
}

type NativeEditRequest struct {
	ProtocolVersion string `json:"protocolVersion"`
	TicketID        string `json:"ticketId"`
	ChildSessionID  string `json:"childSessionId"`
	Path            string `json:"path"`
	Content         string `json:"content"`
	ExpectedSHA256  string `json:"expectedSha256,omitempty"`
	Create          bool   `json:"create,omitempty"`
}

type NativeEditResult struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	PreviousSHA256 string `json:"previousSha256,omitempty"`
	Bytes          int    `json:"bytes"`
	Created        bool   `json:"created"`
}

type NativeEditArtifact struct {
	Worktree     string             `json:"worktree"`
	BaseRevision string             `json:"baseRevision"`
	Changes      []NativeEditResult `json:"changes"`
	ManifestSHA  string             `json:"manifestSha256"`
}

type NativeCodeGraphRequest struct {
	ProtocolVersion string             `json:"protocolVersion"`
	TicketID        string             `json:"ticketId"`
	ChildSessionID  string             `json:"childSessionId"`
	Operation       CodeGraphOperation `json:"operation"`
	Query           string             `json:"query,omitempty"`
	Symbol          string             `json:"symbol,omitempty"`
	Files           []string           `json:"files,omitempty"`
	Depth           int                `json:"depth,omitempty"`
	MaxFiles        int                `json:"maxFiles,omitempty"`
}

type NativeCodeGraphResult struct {
	Operation    CodeGraphOperation `json:"operation"`
	Format       string             `json:"format"`
	Content      string             `json:"content"`
	OutputSHA256 string             `json:"outputSha256"`
	StartedAt    string             `json:"startedAt"`
	FinishedAt   string             `json:"finishedAt"`
}

type NativeCodeGraphReceipt struct {
	Operation    CodeGraphOperation `json:"operation"`
	InputSHA256  string             `json:"inputSha256"`
	OutputSHA256 string             `json:"outputSha256"`
	StartedAt    string             `json:"startedAt"`
	FinishedAt   string             `json:"finishedAt"`
}

type NativeValidationRequest struct {
	ProtocolVersion string                    `json:"protocolVersion"`
	TicketID        string                    `json:"ticketId"`
	ChildSessionID  string                    `json:"childSessionId"`
	Operation       NativeValidationOperation `json:"operation"`
	Packages        []string                  `json:"packages,omitempty"`
}

type NativeValidationResult struct {
	Operation    NativeValidationOperation `json:"operation"`
	Packages     []string                  `json:"packages,omitempty"`
	Success      bool                      `json:"success"`
	ExitCode     int                       `json:"exitCode"`
	Output       string                    `json:"output,omitempty"`
	OutputSHA256 string                    `json:"outputSha256"`
	Changes      []NativeEditResult        `json:"changes,omitempty"`
	StartedAt    string                    `json:"startedAt"`
	FinishedAt   string                    `json:"finishedAt"`
}

type NativeValidationReceipt struct {
	Operation    NativeValidationOperation `json:"operation"`
	Packages     []string                  `json:"packages,omitempty"`
	InputSHA256  string                    `json:"inputSha256"`
	OutputSHA256 string                    `json:"outputSha256"`
	Success      bool                      `json:"success"`
	ExitCode     int                       `json:"exitCode"`
	StartedAt    string                    `json:"startedAt"`
	FinishedAt   string                    `json:"finishedAt"`
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
	ProtocolVersion string                  `json:"protocolVersion"`
	OK              bool                    `json:"ok"`
	Bridge          string                  `json:"bridge"`
	Provider        string                  `json:"provider"`
	Workspace       string                  `json:"workspace,omitempty"`
	RunID           string                  `json:"runId,omitempty"`
	TaskID          string                  `json:"taskId,omitempty"`
	CapsuleID       string                  `json:"capsuleId,omitempty"`
	StateVersion    int                     `json:"stateVersion,omitempty"`
	MemoryRefs      []string                `json:"memoryRefs,omitempty"`
	Status          string                  `json:"status"`
	Result          json.RawMessage         `json:"result,omitempty"`
	Receipt         *Receipt                `json:"receipt,omitempty"`
	Prepared        *PreparedDispatch       `json:"prepared,omitempty"`
	Read            *NativeReadResult       `json:"read,omitempty"`
	Edit            *NativeEditResult       `json:"edit,omitempty"`
	EditArtifact    *NativeEditArtifact     `json:"editArtifact,omitempty"`
	CodeGraph       *NativeCodeGraphResult  `json:"codegraph,omitempty"`
	Validation      *NativeValidationResult `json:"validation,omitempty"`
	Orchestration   *OrchestrationView      `json:"orchestration,omitempty"`
	Error           *Error                  `json:"error,omitempty"`
}

type OrchestrationPreparedTask struct {
	TaskID         string           `json:"taskId"`
	ChildSessionID string           `json:"childSessionId"`
	Prepared       PreparedDispatch `json:"prepared"`
}

type OrchestrationView struct {
	OrchestrationID string                        `json:"orchestrationId"`
	ScheduleID      string                        `json:"scheduleId"`
	OwnerID         string                        `json:"ownerId"`
	ParentSessionID string                        `json:"parentSessionId"`
	Status          string                        `json:"status"`
	CurrentWave     int                           `json:"currentWave"`
	NextWave        int                           `json:"nextWave"`
	Plan            navigator.Plan                `json:"plan"`
	Prepared        []OrchestrationPreparedTask   `json:"prepared,omitempty"`
	EditArtifacts   map[string]NativeEditArtifact `json:"editArtifacts,omitempty"`
	Join            json.RawMessage               `json:"join,omitempty"`
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
	EditNative(context.Context, string, NativeEditRequest) (Response, error)
	QueryNativeCodeGraph(context.Context, string, NativeCodeGraphRequest) (Response, error)
	ValidateNative(context.Context, string, NativeValidationRequest) (Response, error)
}

type OrchestrationRuntime interface {
	NativeRuntime
	PlanOrchestration(context.Context, string, OrchestratePlanRequest) (Response, error)
	PrepareOrchestrationWave(context.Context, string, OrchestrateWaveRequest) (Response, error)
	RecordOrchestrationTerminal(context.Context, string, OrchestrateTerminalRequest) (Response, error)
	JoinOrchestration(context.Context, string, OrchestrateReferenceRequest) (Response, error)
	StatusOrchestration(context.Context, string, OrchestrateReferenceRequest) (Response, error)
	ResumeOrchestration(context.Context, string, OrchestrateReferenceRequest) (Response, error)
	CancelOrchestration(context.Context, string, OrchestrateReferenceRequest) (Response, error)
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

func DecodeOrchestrateInput(reader io.Reader) (OrchestrateInput, error) {
	var input OrchestrateInput
	if err := decodeExact(reader, MaxRequestBytes, &input); err != nil || ValidateOrchestrateInput(input) != nil {
		return OrchestrateInput{}, ErrInvalid
	}
	return input, nil
}

func DecodeOrchestratePlan(reader io.Reader) (OrchestratePlanRequest, error) {
	var request OrchestratePlanRequest
	if err := decodeExact(reader, MaxNativeCompletionBytes, &request); err != nil || ValidateOrchestratePlan(request) != nil {
		return OrchestratePlanRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeOrchestrateWave(reader io.Reader) (OrchestrateWaveRequest, error) {
	var request OrchestrateWaveRequest
	if err := decodeExact(reader, MaxRequestBytes, &request); err != nil || ValidateOrchestrateWave(request) != nil {
		return OrchestrateWaveRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeOrchestrateTerminal(reader io.Reader) (OrchestrateTerminalRequest, error) {
	var request OrchestrateTerminalRequest
	if err := decodeExact(reader, MaxNativeCompletionBytes, &request); err != nil || ValidateOrchestrateTerminal(request) != nil {
		return OrchestrateTerminalRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeOrchestrateReference(reader io.Reader) (OrchestrateReferenceRequest, error) {
	var request OrchestrateReferenceRequest
	if err := decodeExact(reader, MaxRequestBytes, &request); err != nil || ValidateOrchestrateReference(request) != nil {
		return OrchestrateReferenceRequest{}, ErrInvalid
	}
	return request, nil
}

func NewOrchestrateRequest(input OrchestrateInput, trusted OrchestrateContext) (OrchestrateRequest, error) {
	request := OrchestrateRequest{
		ProtocolVersion: ProtocolVersion, Model: trusted.Model, Input: input,
		ParentSessionID: trusted.ParentSessionID, ParentMessageID: trusted.ParentMessageID,
	}
	if err := ValidateOrchestrateRequest(request); err != nil {
		return OrchestrateRequest{}, err
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

func DecodeNativeEdit(reader io.Reader) (NativeEditRequest, error) {
	var request NativeEditRequest
	if err := decodeExact(reader, MaxNativeEditRequestBytes, &request); err != nil || ValidateNativeEdit(request) != nil {
		return NativeEditRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeNativeCodeGraph(reader io.Reader) (NativeCodeGraphRequest, error) {
	var request NativeCodeGraphRequest
	if err := decodeExact(reader, MaxRequestBytes, &request); err != nil || ValidateNativeCodeGraph(request) != nil {
		return NativeCodeGraphRequest{}, ErrInvalid
	}
	return request, nil
}

func DecodeNativeValidation(reader io.Reader) (NativeValidationRequest, error) {
	var request NativeValidationRequest
	if err := decodeExact(reader, MaxRequestBytes, &request); err != nil || ValidateNativeValidation(request) != nil {
		return NativeValidationRequest{}, ErrInvalid
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

func ValidateNativeEdit(request NativeEditRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ChildSessionID) ||
		request.Path == "" || len(request.Path) > 4096 || strings.ContainsRune(request.Path, '\x00') || filepath.IsAbs(request.Path) ||
		!filepath.IsLocal(request.Path) || filepath.Clean(request.Path) != request.Path || request.Path == "." ||
		strings.HasSuffix(request.Path, "/") || strings.HasSuffix(request.Path, `\`) || !utf8.ValidString(request.Content) ||
		strings.ContainsRune(request.Content, '\x00') || len(request.Content) > MaxNativeEditBytes {
		return ErrInvalid
	}
	if request.Create {
		if request.ExpectedSHA256 != "" {
			return ErrInvalid
		}
		return nil
	}
	if !validSHA256(request.ExpectedSHA256) {
		return ErrInvalid
	}
	return nil
}

func ValidateNativeCodeGraph(request NativeCodeGraphRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ChildSessionID) || request.Depth < 0 || request.Depth > 5 || request.MaxFiles < 0 || request.MaxFiles > 12 {
		return ErrInvalid
	}
	switch request.Operation {
	case CodeGraphStatus:
		if strings.TrimSpace(request.Query) != "" || strings.TrimSpace(request.Symbol) != "" || len(request.Files) != 0 {
			return ErrInvalid
		}
	case CodeGraphExplore:
		if strings.TrimSpace(request.Query) == "" || len(request.Query) > 4096 || strings.ContainsRune(request.Query, '\x00') || strings.TrimSpace(request.Symbol) != "" || len(request.Files) != 0 {
			return ErrInvalid
		}
	case CodeGraphImpact:
		if strings.TrimSpace(request.Symbol) == "" || len(request.Symbol) > 512 || strings.ContainsRune(request.Symbol, '\x00') || strings.ContainsAny(request.Symbol, "\r\n") || strings.TrimSpace(request.Query) != "" || len(request.Files) != 0 {
			return ErrInvalid
		}
	case CodeGraphAffected:
		if strings.TrimSpace(request.Query) != "" || strings.TrimSpace(request.Symbol) != "" || len(request.Files) == 0 || len(request.Files) > 32 {
			return ErrInvalid
		}
		for _, path := range request.Files {
			if path == "" || len(path) > 4096 || strings.ContainsRune(path, '\x00') || filepath.IsAbs(path) || !filepath.IsLocal(path) || filepath.Clean(path) != path || path == "." {
				return ErrInvalid
			}
		}
	default:
		return ErrInvalid
	}
	return nil
}

func ValidateNativeValidation(request NativeValidationRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.TicketID) ||
		!validNativeIdentity(request.ChildSessionID) || len(request.Packages) > 16 {
		return ErrInvalid
	}
	switch request.Operation {
	case NativeValidationFormat:
		if len(request.Packages) != 0 {
			return ErrInvalid
		}
	case NativeValidationTest, NativeValidationVet:
		for _, selector := range request.Packages {
			if !validGoPackageSelector(selector) {
				return ErrInvalid
			}
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validGoPackageSelector(value string) bool {
	if value == "." || value == "./..." {
		return true
	}
	if len(value) < 3 || len(value) > 256 || !strings.HasPrefix(value, "./") ||
		strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "./"), "/")
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		if part == "..." {
			return index == len(parts)-1
		}
		for _, character := range part {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.' {
				continue
			}
			return false
		}
	}
	return true
}

func ValidateOrchestrateRequest(request OrchestrateRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validModel(request.Model) || !validNativeIdentity(request.ParentSessionID) || !validNativeIdentity(request.ParentMessageID) || ValidateOrchestrateInput(request.Input) != nil {
		return ErrInvalid
	}
	return nil
}

func ValidateOrchestrateInput(input OrchestrateInput) error {
	return validateBoundedIntent(input.Goal, input.AcceptanceCriteria)
}

func ValidateOrchestratePlan(request OrchestratePlanRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validModel(request.Model) || !validNativeIdentity(request.ParentSessionID) || !validNativeIdentity(request.ParentMessageID) || ValidateOrchestrateInput(request.Input) != nil || len(request.CandidateTasks) == 0 || len(request.CandidateTasks) > navigator.MaxTasks {
		return ErrInvalid
	}
	return nil
}

func ValidateOrchestrateWave(request OrchestrateWaveRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.OrchestrationID) || !validNativeIdentity(request.OwnerID) || len(request.Bindings) == 0 || len(request.Bindings) > navigator.DefaultMaxParallel {
		return ErrInvalid
	}
	seenTasks, seenChildren, seenTickets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, binding := range request.Bindings {
		if !validNativeIdentity(binding.TaskID) || !validNativeIdentity(binding.ChildSessionID) || !validNativeIdentity(binding.TicketID) || !validNativeIdentity(binding.ClaimToken) || seenTasks[binding.TaskID] || seenChildren[binding.ChildSessionID] || seenTickets[binding.TicketID] {
			return ErrInvalid
		}
		seenTasks[binding.TaskID], seenChildren[binding.ChildSessionID], seenTickets[binding.TicketID] = true, true, true
	}
	return nil
}

func ValidateOrchestrateTerminal(request OrchestrateTerminalRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.OrchestrationID) || !validNativeIdentity(request.OwnerID) || !validNativeIdentity(request.TaskID) || !validNativeIdentity(request.TicketID) || !validNativeIdentity(request.ChildSessionID) {
		return ErrInvalid
	}
	switch request.Status {
	case "completed":
		if !validNativeIdentity(request.MessageID) || !validNativeIdentity(request.ResultID) || len(request.Result) == 0 || len(request.Result) > MaxOrchestrationResultBytes || !json.Valid(request.Result) || request.Failure != "" {
			return ErrInvalid
		}
	case "failed", "cancelled":
		if strings.TrimSpace(request.Failure) == "" || utf8.RuneCountInString(request.Failure) > 2048 || request.MessageID != "" || request.ResultID != "" || len(request.Result) != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func ValidateOrchestrateReference(request OrchestrateReferenceRequest) error {
	if request.ProtocolVersion != ProtocolVersion || !validNativeIdentity(request.OrchestrationID) || request.OwnerID != "" && !validNativeIdentity(request.OwnerID) {
		return ErrInvalid
	}
	recoveryFields := 0
	for _, value := range []string{request.TaskID, request.ChildSessionID, request.ClaimToken} {
		if value != "" {
			recoveryFields++
			if !validNativeIdentity(value) {
				return ErrInvalid
			}
		}
	}
	if recoveryFields != 0 && recoveryFields != 3 {
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
	if validateBoundedIntent(request.Goal, request.AcceptanceCriteria) != nil {
		return ErrInvalid
	}
	if !validContinuity(request.Continuity, request.RunID) {
		return ErrInvalid
	}
	if request.TicketID != "" && !validNativeIdentity(request.TicketID) || request.ParentSessionID != "" && !validNativeIdentity(request.ParentSessionID) || request.ParentMessageID != "" && !validNativeIdentity(request.ParentMessageID) || request.ChildSessionID != "" && !validNativeIdentity(request.ChildSessionID) {
		return ErrInvalid
	}
	return nil
}

func validateBoundedIntent(goal string, criteria []string) error {
	if strings.TrimSpace(goal) == "" || utf8.RuneCountInString(goal) > 8192 || len(criteria) > 32 {
		return ErrInvalid
	}
	for _, criterion := range criteria {
		if strings.TrimSpace(criterion) == "" || utf8.RuneCountInString(criterion) > 2048 {
			return ErrInvalid
		}
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
	return operation == ReadFiles || operation == AnalyzeStructure || operation == WriteFiles || operation == ReviewChanges
}

func validSHA256(value string) bool {
	if !strings.HasPrefix(value, "sha256-") || len(value) != len("sha256-")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256-"))
	return err == nil
}

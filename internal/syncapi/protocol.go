package syncapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	ProtocolVersion      = 1
	MediaType            = "application/vnd.vgxness.sync+json;version=1"
	MaxBodyBytes         = 1 << 20
	MaxPullResponseBytes = syncservice.MaxPullResponseBytes
	MaxPushItems         = 16
	DefaultPullLimit     = 10
	MaxPullLimit         = 25
	MaxJSONDepth         = 32
)

var (
	ErrInvalidRequest     = errors.New("invalid sync request")
	ErrLimitExceeded      = errors.New("sync limit exceeded")
	ErrUnsupportedVersion = errors.New("unsupported sync protocol version")
)

type PushRequest struct {
	ProtocolVersion int                    `json:"protocol_version"`
	Items           []syncservice.Mutation `json:"items"`
}

type PushResponse struct {
	ProtocolVersion int                  `json:"protocol_version"`
	Results         []syncservice.Result `json:"results"`
}

type PullRequest struct {
	ProtocolVersion int                `json:"protocol_version"`
	Cursor          syncservice.Cursor `json:"cursor"`
	ProjectID       string             `json:"project_id,omitempty"`
	Limit           int                `json:"limit,omitempty"`
}

type PullChange = syncservice.Change

type PullResponse struct {
	ProtocolVersion int          `json:"protocol_version"`
	HistoryID       string       `json:"history_id"`
	ProjectID       string       `json:"project_id,omitempty"`
	Position        int64        `json:"position"`
	Watermark       int64        `json:"watermark,omitempty"`
	HasMore         bool         `json:"has_more"`
	Changes         []PullChange `json:"changes,omitempty"`
}

// DiscoveryResponse is the v1 authenticated bootstrap discovery response.
type DiscoveryResponse = syncservice.Discovery

type ErrorCode string

const (
	ErrorInvalidInput        ErrorCode = "invalid_input"
	ErrorLimitExceeded       ErrorCode = "limit_exceeded"
	ErrorUnsupportedVersion  ErrorCode = "unsupported_version"
	ErrorUnsupportedSemantic ErrorCode = "unsupported_semantic"
	ErrorUnavailable         ErrorCode = "unavailable"
	ErrorUnauthorized        ErrorCode = "unauthorized"
	ErrorRevoked             ErrorCode = "revoked"
	ErrorConflict            ErrorCode = "conflict"
	ErrorHistory             ErrorCode = "history_error"
	ErrorCursor              ErrorCode = "cursor_error"
)

func (c ErrorCode) String() string { return string(c) }

func DecodePushRequest(body []byte) (PushRequest, error) {
	var request PushRequest
	if err := decodeStrict(body, &request); err != nil {
		return PushRequest{}, err
	}
	if err := ValidatePushRequest(request); err != nil {
		return PushRequest{}, err
	}
	return request, nil
}

func DecodePullRequest(body []byte) (PullRequest, error) {
	var request PullRequest
	if err := decodeStrict(body, &request); err != nil {
		return PullRequest{}, err
	}
	if err := ValidatePullRequest(&request); err != nil {
		return PullRequest{}, err
	}
	return request, nil
}

// DecodeDiscoveryResponse decodes a strict, bounded discovery response.
func DecodeDiscoveryResponse(body []byte) (DiscoveryResponse, error) {
	var response DiscoveryResponse
	if err := decodeStrict(body, &response); err != nil {
		return DiscoveryResponse{}, err
	}
	if err := syncservice.ValidateDiscovery(response); err != nil {
		return DiscoveryResponse{}, ErrInvalidRequest
	}
	return response, nil
}

// DecodePushResponse decodes a strict, bounded push response.
func DecodePushResponse(body []byte) (PushResponse, error) {
	var response PushResponse
	if err := decodeStrict(body, &response); err != nil {
		return PushResponse{}, err
	}
	return response, nil
}

// DecodeCapabilitiesResponse decodes a strict, bounded capabilities response.
func DecodeCapabilitiesResponse(body []byte) (CapabilitiesResponse, error) {
	var response CapabilitiesResponse
	if err := decodeStrict(body, &response); err != nil {
		return CapabilitiesResponse{}, err
	}
	return response, nil
}

func ValidatePushRequest(request PushRequest) error {
	if request.ProtocolVersion != ProtocolVersion {
		return ErrUnsupportedVersion
	}
	if len(request.Items) > MaxPushItems {
		return ErrLimitExceeded
	}
	if len(request.Items) == 0 {
		return ErrInvalidRequest
	}
	for _, item := range request.Items {
		if err := syncservice.ValidateMutation(item); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePushResponse validates ordered terminal results against their request.
func ValidatePushResponse(request PushRequest, response PushResponse) error {
	if response.ProtocolVersion != ProtocolVersion || len(response.Results) != len(request.Items) || len(response.Results) == 0 || len(response.Results) > MaxPushItems {
		return ErrInvalidRequest
	}
	sequenceMutationIDs := make(map[int64]string, len(response.Results))
	for index, result := range response.Results {
		if result.MutationID == "" || result.MutationID != request.Items[index].MutationID {
			return ErrInvalidRequest
		}
		switch result.Disposition {
		case syncservice.DispositionAccepted, syncservice.DispositionPreviouslyAccepted:
			if result.Sequence == nil || *result.Sequence < 1 || result.Code != "" || result.Retryable {
				return ErrInvalidRequest
			}
			if request.Items[index].BaseVersion == math.MaxInt64 || result.Version < 1 || result.Version != request.Items[index].BaseVersion+1 {
				return ErrInvalidRequest
			}
			if mutationID, ok := sequenceMutationIDs[*result.Sequence]; ok && mutationID != result.MutationID {
				return ErrInvalidRequest
			}
			sequenceMutationIDs[*result.Sequence] = result.MutationID
		case syncservice.DispositionConflict:
			if result.Sequence == nil || *result.Sequence < 1 || result.Version < 1 || result.Code != "" || result.Retryable {
				return ErrInvalidRequest
			}
			if mutationID, ok := sequenceMutationIDs[*result.Sequence]; ok && mutationID != result.MutationID {
				return ErrInvalidRequest
			}
			sequenceMutationIDs[*result.Sequence] = result.MutationID
		case syncservice.DispositionRejected:
			if result.Sequence != nil || result.Version != 0 || result.Retryable || !safeResultCode(result.Code) {
				return ErrInvalidRequest
			}
		default:
			return ErrInvalidRequest
		}
	}
	return nil
}

func safeResultCode(code string) bool {
	switch code {
	case "invalid_device", "revoked", "invalid_input", "unsupported_semantic", "mutation_id_hash_mismatch", "invalid_replay", "invalid_base", "stale_base", "invalid_prerequisite", "topic_collision":
		return true
	default:
		return false
	}
}

func ValidatePullRequest(request *PullRequest) error {
	if request == nil {
		return ErrInvalidRequest
	}
	if request.ProtocolVersion != ProtocolVersion {
		return ErrUnsupportedVersion
	}
	if request.Limit == 0 {
		request.Limit = DefaultPullLimit
	}
	if request.Limit > MaxPullLimit {
		return ErrLimitExceeded
	}
	if request.Limit < 1 {
		return ErrInvalidRequest
	}
	if err := syncservice.ValidateCursor(request.Cursor); err != nil {
		return err
	}
	if request.ProjectID != "" && !validProjectID(request.ProjectID) {
		return ErrInvalidRequest
	}
	if request.Cursor.Watermark < 0 || request.Cursor.Watermark > 0 && request.Cursor.Watermark < request.Cursor.Position {
		return syncservice.ErrInvalidCursor
	}
	return nil
}

func DecodePullResponse(body []byte) (PullResponse, error) {
	var envelope struct {
		ProtocolVersion int               `json:"protocol_version"`
		HistoryID       string            `json:"history_id"`
		ProjectID       string            `json:"project_id,omitempty"`
		Position        int64             `json:"position"`
		Watermark       int64             `json:"watermark,omitempty"`
		HasMore         bool              `json:"has_more"`
		Changes         []json.RawMessage `json:"changes,omitempty"`
	}
	if len(body) > MaxPullResponseBytes {
		return PullResponse{}, ErrLimitExceeded
	}
	if len(body) == 0 || !utf8.Valid(body) || jsonDepth(body) > MaxJSONDepth || json.Unmarshal(body, &envelope) != nil {
		return PullResponse{}, ErrInvalidRequest
	}
	response := PullResponse{ProtocolVersion: envelope.ProtocolVersion, HistoryID: envelope.HistoryID, ProjectID: envelope.ProjectID, Position: envelope.Position, Watermark: envelope.Watermark, HasMore: envelope.HasMore, Changes: make([]syncservice.Change, len(envelope.Changes))}
	for index, raw := range envelope.Changes {
		if err := decodePullChange(raw, &response.Changes[index]); err != nil {
			return PullResponse{}, ErrInvalidRequest
		}
	}
	if response.ProtocolVersion != ProtocolVersion {
		return PullResponse{}, ErrUnsupportedVersion
	}
	if syncservice.ValidateCursor(syncservice.Cursor{HistoryID: response.HistoryID, Position: response.Position}) != nil {
		return PullResponse{}, ErrInvalidRequest
	}
	if response.Watermark < 0 || response.Watermark > 0 && response.Position > response.Watermark {
		return PullResponse{}, ErrInvalidRequest
	}
	if response.Watermark > 0 && response.HasMore != (response.Position < response.Watermark) {
		return PullResponse{}, ErrInvalidRequest
	}
	if len(response.Changes) > MaxPullLimit {
		return PullResponse{}, ErrLimitExceeded
	}
	var previous int64
	for _, change := range response.Changes {
		if change.Sequence <= previous || response.ProjectID == "" && previous > 0 && change.Sequence != previous+1 || change.CanonicalVersion < 1 || response.Watermark > 0 && change.Sequence > response.Watermark || syncservice.ValidateMutation(change.Mutation) != nil || syncservice.ValidateChangeEnvelope(change) != nil || syncservice.VerifyChangeHash(change) != nil || response.ProjectID != "" && ValidateProjectPullChange(change, response.ProjectID) != nil {
			return PullResponse{}, ErrInvalidRequest
		}
		previous = change.Sequence
	}
	if response.ProjectID == "" && previous > 0 && previous != response.Position {
		return PullResponse{}, ErrInvalidRequest
	}
	if response.ProjectID != "" && !validProjectID(response.ProjectID) {
		return PullResponse{}, ErrInvalidRequest
	}
	if response.ProjectID != "" && (response.HasMore && (previous == 0 || response.Position != previous) || !response.HasMore && response.Position != response.Watermark) {
		return PullResponse{}, ErrInvalidRequest
	}
	if response.ProjectID != "" && (len(response.Changes) != 0 || response.Position > 0 || response.HasMore) && response.Watermark <= 0 {
		return PullResponse{}, ErrInvalidRequest
	}
	return response, nil
}

// DecodeStrictPullResponse rejects duplicate and unknown fields for untrusted clients.
func DecodeStrictPullResponse(body []byte) (PullResponse, error) {
	var envelope struct {
		ProtocolVersion *int              `json:"protocol_version"`
		HistoryID       *string           `json:"history_id"`
		ProjectID       *string           `json:"project_id,omitempty"`
		Position        *int64            `json:"position"`
		Watermark       *int64            `json:"watermark,omitempty"`
		HasMore         *bool             `json:"has_more"`
		Changes         []json.RawMessage `json:"changes,omitempty"`
	}
	if err := decodeStrictLimit(body, MaxPullResponseBytes, &envelope); err != nil {
		return PullResponse{}, err
	}
	if envelope.ProtocolVersion == nil || envelope.HistoryID == nil || envelope.Position == nil || envelope.HasMore == nil {
		return PullResponse{}, ErrInvalidRequest
	}
	return DecodePullResponse(body)
}

func validProjectID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value && id.Variant() == uuid.RFC4122 && id.Version() >= 1 && id.Version() <= 5
}

// ValidateProjectPullChange binds payload-bearing changes to a project pull.
// Tombstones carry no project payload; repository history establishes that link.
func ValidateProjectPullChange(change syncservice.Change, projectID string) error {
	if !validProjectID(projectID) {
		return ErrInvalidRequest
	}
	m := change.Mutation
	switch {
	case m.Project != nil:
		if m.Project.ID == projectID {
			return nil
		}
	case m.Session != nil:
		if m.Session.ProjectID == projectID {
			return nil
		}
	case m.Observation != nil:
		if m.Observation.ProjectID == projectID {
			return nil
		}
	case m.Resolution != nil && m.Resolution.Observation != nil:
		if m.Resolution.Observation.ProjectID == projectID {
			return nil
		}
	case m.Tombstone != nil:
		return nil
	}
	return ErrInvalidRequest
}

func decodePullChange(body []byte, change *syncservice.Change) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(change); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return nil
}

func decodeStrict(body []byte, value any) error {
	return decodeStrictLimit(body, MaxBodyBytes, value)
}

func decodeStrictLimit(body []byte, limit int, value any) error {
	if len(body) > limit || jsonDepth(body) > MaxJSONDepth {
		return ErrLimitExceeded
	}
	if len(body) == 0 || !utf8.Valid(body) {
		return ErrInvalidRequest
	}
	if hasDuplicateFields(body) {
		return ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return nil
}

func hasDuplicateFields(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var walk func() bool
	walk = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return true
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return false
		}
		switch delimiter {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return true
				}
				name, ok := key.(string)
				if !ok {
					return true
				}
				if _, exists := seen[name]; exists {
					return true
				}
				seen[name] = struct{}{}
				if walk() {
					return true
				}
			}
		case '[':
			for decoder.More() {
				if walk() {
					return true
				}
			}
		}
		_, err = decoder.Token()
		return err != nil
	}
	return walk()
}

func CodeFor(err error) ErrorCode {
	switch {
	case errors.Is(err, ErrLimitExceeded), errors.Is(err, syncservice.ErrLimitExceeded):
		return ErrorLimitExceeded
	case errors.Is(err, ErrUnsupportedVersion):
		return ErrorUnsupportedVersion
	case errors.Is(err, syncservice.ErrUnsupportedSemantic):
		return ErrorUnsupportedSemantic
	case errors.Is(err, syncservice.ErrInvalidCursor):
		return ErrorCursor
	default:
		return ErrorInvalidInput
	}
}

func jsonDepth(body []byte) int {
	depth, maxDepth := 0, 0
	inString, escaped := false, false
	for _, b := range body {
		if inString {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		if b == '"' {
			inString = true
		} else if b == '{' || b == '[' {
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		} else if b == '}' || b == ']' {
			depth--
		}
	}
	return maxDepth
}

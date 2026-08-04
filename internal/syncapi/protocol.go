package syncapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/syncservice"
)

const (
	ProtocolVersion      = 1
	MediaType            = "application/vnd.vgxness.sync+json;version=1"
	MaxBodyBytes         = 1 << 20
	MaxPullResponseBytes = 2 << 20
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
	Limit           int                `json:"limit,omitempty"`
}

type PullChange struct {
	Sequence int64                `json:"sequence"`
	Mutation syncservice.Mutation `json:"mutation"`
}

type PullResponse struct {
	ProtocolVersion int          `json:"protocol_version"`
	HistoryID       string       `json:"history_id"`
	Position        int64        `json:"position"`
	HasMore         bool         `json:"has_more"`
	Changes         []PullChange `json:"changes,omitempty"`
}

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
	return nil
}

func DecodePullResponse(body []byte) (PullResponse, error) {
	var response PullResponse
	if len(body) > MaxPullResponseBytes {
		return PullResponse{}, ErrLimitExceeded
	}
	if len(body) == 0 || !utf8.Valid(body) || jsonDepth(body) > MaxJSONDepth || json.Unmarshal(body, &response) != nil {
		return PullResponse{}, ErrInvalidRequest
	}
	if response.ProtocolVersion != ProtocolVersion {
		return PullResponse{}, ErrUnsupportedVersion
	}
	if syncservice.ValidateCursor(syncservice.Cursor{HistoryID: response.HistoryID, Position: response.Position}) != nil {
		return PullResponse{}, ErrInvalidRequest
	}
	if len(response.Changes) > MaxPullLimit {
		return PullResponse{}, ErrLimitExceeded
	}
	var previous int64
	for _, change := range response.Changes {
		if change.Sequence <= previous || syncservice.ValidateMutation(change.Mutation) != nil {
			return PullResponse{}, ErrInvalidRequest
		}
		previous = change.Sequence
	}
	if previous > 0 && previous != response.Position {
		return PullResponse{}, ErrInvalidRequest
	}
	return response, nil
}

func decodeStrict(body []byte, value any) error {
	if len(body) > MaxBodyBytes || jsonDepth(body) > MaxJSONDepth {
		return ErrLimitExceeded
	}
	if len(body) == 0 || !utf8.Valid(body) {
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

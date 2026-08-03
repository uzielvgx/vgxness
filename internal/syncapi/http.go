package syncapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

var ErrUnauthenticated = errors.New("syncapi unauthenticated")

// Identity is the non-secret identity authenticated for a sync request.
type Identity struct {
	OwnerID  uuid.UUID
	DeviceID uuid.UUID
}

// Authenticator authenticates a syntactically valid bearer and returns its
// non-secret identity.
type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
}

// CapabilitiesResponse is the v1 capabilities representation.
type CapabilitiesResponse struct {
	ProtocolVersion int      `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
}

type identityContextKey struct{}

// IdentityFromContext returns the identity authenticated by Handler.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

type capabilitiesFunc func(context.Context) CapabilitiesResponse

type handler struct {
	authenticator Authenticator
	capabilities  capabilitiesFunc
}

// NewHandler returns the HTTP handler for the implemented sync v1 endpoints.
func NewHandler(authenticator Authenticator) http.Handler {
	return newHandler(authenticator, nil)
}

func newHandler(authenticator Authenticator, capabilities capabilitiesFunc) http.Handler {
	if capabilities == nil {
		capabilities = func(context.Context) CapabilitiesResponse {
			return CapabilitiesResponse{ProtocolVersion: ProtocolVersion, Capabilities: []string{"capabilities"}}
		}
	}
	return &handler{authenticator: authenticator, capabilities: capabilities}
}

func (handler *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/v1/sync/capabilities" {
		writeError(writer, http.StatusNotFound, ErrorInvalidInput, false)
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeError(writer, http.StatusMethodNotAllowed, ErrorInvalidInput, false)
		return
	}

	identity, status := handler.authenticate(request)
	if status != 0 {
		code := ErrorUnavailable
		if status == http.StatusUnauthorized {
			code = ErrorUnauthorized
		}
		writeError(writer, status, code, status == http.StatusUnauthorized)
		return
	}
	if len(request.Header.Values("Accept")) != 1 || request.Header.Get("Accept") != MediaType {
		writeError(writer, http.StatusNotAcceptable, ErrorUnsupportedVersion, false)
		return
	}
	if request.Body != nil && requestHasBody(request.Body) {
		writeError(writer, http.StatusBadRequest, ErrorInvalidInput, false)
		return
	}

	response := handler.capabilities(request.WithContext(context.WithValue(request.Context(), identityContextKey{}, identity)).Context())
	writeJSON(writer, http.StatusOK, response, false)
}

func (handler *handler) authenticate(request *http.Request) (Identity, int) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return Identity{}, http.StatusUnauthorized
	}
	bearer := strings.TrimPrefix(values[0], "Bearer ")
	if !validBearer(bearer) {
		return Identity{}, http.StatusUnauthorized
	}
	if handler.authenticator == nil {
		return Identity{}, http.StatusServiceUnavailable
	}
	identity, err := handler.authenticator.Authenticate(request.Context(), bearer)
	if errors.Is(err, ErrUnauthenticated) {
		return Identity{}, http.StatusUnauthorized
	}
	if err != nil {
		return Identity{}, http.StatusServiceUnavailable
	}
	if identity.OwnerID == uuid.Nil || identity.DeviceID == uuid.Nil {
		return Identity{}, http.StatusUnauthorized
	}
	return identity, 0
}

func validBearer(bearer string) bool {
	if len(bearer) != 85 {
		return false
	}
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 || parts[0] != "vgx1" {
		return false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil || id.String() != parts[1] {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	defer clearBytes(raw)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == parts[2]
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func requestHasBody(body io.ReadCloser) bool {
	defer body.Close()
	var value [1]byte
	for attempts := 0; attempts < 2; attempts++ {
		count, err := body.Read(value[:])
		if count > 0 || err != nil && !errors.Is(err, io.EOF) {
			return true
		}
		if errors.Is(err, io.EOF) {
			return false
		}
	}
	return true
}

func writeError(writer http.ResponseWriter, status int, code ErrorCode, authenticate bool) {
	writeJSON(writer, status, struct {
		ProtocolVersion int       `json:"protocol_version"`
		Error           ErrorCode `json:"error"`
	}{ProtocolVersion: ProtocolVersion, Error: code}, authenticate)
}

func writeJSON(writer http.ResponseWriter, status int, value any, authenticate bool) {
	writer.Header().Set("Content-Type", MediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if authenticate {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	}
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

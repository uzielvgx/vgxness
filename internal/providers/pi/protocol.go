// Package pi implements the private vgxness-pi/v1 sidecar boundary.
package pi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"unicode/utf8"
)

const Protocol = "vgxness-pi/v1"
const MaxRecordBytes = 1 << 20

type Mode string

const (
	ReadOnly Mode = "read-only"
	Full     Mode = "full"
)

type Request struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	Workspace string          `json:"workspace"`
	Mode      Mode            `json:"mode"`
	Role      string          `json:"role"`
	Payload   json.RawMessage `json:"payload"`
}
type Cancel struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func DecodeCancel(input []byte) (Cancel, error) {
	if len(input) == 0 || len(input) > MaxRecordBytes || !utf8.Valid(input) || input[len(input)-1] != '\n' || bytes.Contains(input, []byte{'\r'}) {
		return Cancel{}, errors.New("invalid cancel")
	}
	input = input[:len(input)-1]
	if len(input) == 0 || bytes.Contains(input, []byte{'\n'}) || hasDuplicateKeys(input) {
		return Cancel{}, errors.New("invalid cancel")
	}
	var cancel Cancel
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cancel); err != nil || decoder.Decode(&struct{}{}) != io.EOF || cancel.Type != "cancel" || !validCorrelationID(cancel.ID) {
		return Cancel{}, errors.New("invalid cancel")
	}
	return cancel, nil
}

// DecodeRecord accepts exactly one LF-terminated, closed-schema request record.
func DecodeRecord(input []byte) (Request, error) {
	if len(input) == 0 || len(input) > MaxRecordBytes || !utf8.Valid(input) || input[len(input)-1] != '\n' || bytes.Contains(input, []byte{'\r'}) {
		return Request{}, errors.New("invalid record framing")
	}
	input = input[:len(input)-1]
	if len(input) == 0 || bytes.Contains(input, []byte{'\n'}) || hasDuplicateKeys(input) {
		return Request{}, errors.New("invalid record")
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil || decoder.More() || decoder.Decode(&struct{}{}) != io.EOF || request.Type != "request" || request.ID == "" || request.Operation == "" || request.Workspace == "" || request.Role == "" || len(request.Payload) == 0 || bytes.Equal(request.Payload, []byte("null")) || (request.Mode != ReadOnly && request.Mode != Full) {
		return Request{}, errors.New("invalid request")
	}
	return request, nil
}

func hasDuplicateKeys(input []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(input))
	return duplicateValue(decoder)
}

func duplicateValue(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return true
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return false
	}
	switch delim {
	case '{':
		keys := map[string]struct{}{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return true
			}
			name, ok := key.(string)
			if !ok {
				return true
			}
			if _, exists := keys[name]; exists {
				return true
			}
			keys[name] = struct{}{}
			if duplicateValue(decoder) {
				return true
			}
		}
		_, err := decoder.Token()
		return err != nil
	case '[':
		for decoder.More() {
			if duplicateValue(decoder) {
				return true
			}
		}
		_, err := decoder.Token()
		return err != nil
	default:
		return true
	}
}

type Binding struct {
	Workspace string
	Mode      Mode
	Role      string
}

// AuthorityError is safe to expose over the private protocol without leaking
// host paths or policy details.
type AuthorityError struct{ Code string }

func (e *AuthorityError) Error() string { return e.Code }

func (binding Binding) Authorize(request Request) error {
	if request.Workspace != binding.Workspace {
		return &AuthorityError{Code: "workspace_mismatch"}
	}
	if request.Mode != binding.Mode {
		return &AuthorityError{Code: "mode_denied"}
	}
	if request.Role != binding.Role {
		return &AuthorityError{Code: "authorization_denied"}
	}
	operation, exists := operations[request.Operation]
	if !exists || !operation.modes[binding.Mode] || !operation.roles[binding.Role] {
		return &AuthorityError{Code: "authorization_denied"}
	}
	return nil
}

type operationPolicy struct {
	modes   map[Mode]bool
	roles   map[string]bool
	mutates bool
}

func operationMutates(name string) bool { return operations[name].mutates }

var operations = map[string]operationPolicy{
	"memory.recall":              {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"memory.get":                 {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"memory.recent":              {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"memory.remember":            {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.forget":              {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.project.resolve":     {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"memory.project.initialize":  {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.sync.configure":      {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.sync.status":         {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"memory.sync":                {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.sync.backfill":       {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.sync.repair_project": {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.sync.reseed":         {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.sync.rejoin":         {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.session.start":       {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.session.checkpoint":  {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.session.renew":       {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.session.end":         {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"memory.session.context":     {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"memory.session.draft_save":  {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.get":                    {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"sdd.get_revision":           {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"sdd.list":                   {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"sdd.list_revisions":         {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"sdd.create":                 {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.set_interaction_mode":   {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.save_revision":          {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.accept_revision":        {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.transition":             {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.cancel":                 {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.projection_status":      {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"sdd.record_projection":      {modes: map[Mode]bool{Full: true}, roles: managerOnly, mutates: true},
	"sdd.render_projection":      {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"sdd.compare_projection":     {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
	"model.resolve":              {modes: map[Mode]bool{ReadOnly: true, Full: true}, roles: allRoles},
}

var allRoles = map[string]bool{
	"manager": true, "general": true, "verifier": true, "care-reviewer": true,
	"care-specialist": true, "care-challenger": true,
	"explore": true, "sdd-research": true, "sdd-proposal": true, "sdd-spec": true, "sdd-design": true, "sdd-tasks": true, "sdd-apply": true,
}
var managerOnly = map[string]bool{"manager": true}

type Replay struct {
	mu    sync.Mutex
	limit int
	seen  map[string]struct{}
}

func NewReplay(limit int) *Replay { return &Replay{limit: limit, seen: make(map[string]struct{})} }

func (replay *Replay) Accept(id string) bool {
	if replay == nil {
		return false
	}
	replay.mu.Lock()
	defer replay.mu.Unlock()
	if !validCorrelationID(id) || replay.limit <= len(replay.seen) {
		return false
	}
	if _, exists := replay.seen[id]; exists {
		return false
	}
	replay.seen[id] = struct{}{}
	return true
}

func validCorrelationID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range []byte(id) {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func (binding Binding) String() string {
	return fmt.Sprintf("%s:%s:%s", binding.Workspace, binding.Mode, binding.Role)
}

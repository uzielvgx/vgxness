package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const previewLimit, previewBudget = 256, 4096

type MemoryStore interface {
	Save(context.Context, Observation) (Observation, error)
	Search(context.Context, Search) ([]Observation, error)
	Get(context.Context, string, string, Scope) (Observation, error)
}

type SourceVerifier func(context.Context, string, string) error

type MemoryService struct {
	store        MemoryStore
	producer     string
	verifySource SourceVerifier
}

func NewMemoryService(store MemoryStore, producer string, verifySource SourceVerifier) MemoryService {
	return MemoryService{store: store, producer: producer, verifySource: verifySource}
}

type SaveRequest struct {
	Title, Content, Project, Type, TopicKey, Session, SourceProvider, SourceID string
	Scope                                                                      Scope
	State                                                                      State
	References                                                                 []string
}
type SearchRequest struct {
	Query, Project, Type, TopicKey string
	Scope                          Scope
	States                         []State
	Limit                          int
	MatchAny                       bool
}
type GetRequest struct {
	ID, Project string
	Scope       Scope
}
type MemoryResult struct {
	ID, Title, Project, Type, TopicKey, Session, Producer, SourceProvider, SourceID string
	Scope                                                                           Scope
	State                                                                           State
	CreatedAt, UpdatedAt                                                            time.Time
	Preview, Content                                                                string
	References                                                                      []string
}

func (s MemoryService) Save(ctx context.Context, request SaveRequest) (MemoryResult, error) {
	if err := ctx.Err(); err != nil {
		return MemoryResult{}, err
	}
	if request.Project == "" {
		request.Project = "default"
	}
	if request.Scope == "" {
		request.Scope = ScopeProject
	}
	if request.Type == "" {
		request.Type = "learning"
	}
	if request.State == "" {
		request.State = StateActive
	}
	pairedSource := request.SourceProvider != "" && request.SourceID != ""
	if !validText(request.Content, 4096, false) || !validText(request.Title, 256, true) || !validMetadata(request.Project, request.Type, request.TopicKey, request.Session, s.producer, request.SourceProvider, request.SourceID) || !validReferences(request.References) || request.Scope != ScopeProject && request.Scope != ScopePersonal || request.State != StateActive && request.State != StateNeedsReview || (request.SourceProvider == "") != (request.SourceID == "") || pairedSource && (s.verifySource == nil || s.verifySource(ctx, request.SourceProvider, request.SourceID) != nil) {
		return MemoryResult{}, fmt.Errorf("%w: invalid save request", ErrInvalid)
	}
	item, err := s.store.Save(ctx, Observation{Title: request.Title, Content: request.Content, Project: request.Project, Scope: request.Scope, Type: request.Type, TopicKey: request.TopicKey, Session: request.Session, Provenance: Provenance{Producer: s.producer, SourceProvider: request.SourceProvider, SourceID: request.SourceID}, State: request.State, References: append([]string(nil), request.References...)})
	if err != nil {
		return MemoryResult{}, err
	}
	return shape(item, true), nil
}

func (s MemoryService) Search(ctx context.Context, request SearchRequest) ([]MemoryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query, ok := safeQuery(request.Query, request.MatchAny)
	if !ok || request.Project == "" || request.Scope != ScopeProject && request.Scope != ScopePersonal || request.Limit < 0 || request.Limit > 50 || !validMetadata(request.Project, request.Type, request.TopicKey) {
		return nil, fmt.Errorf("%w: invalid search request", ErrInvalid)
	}
	if request.Limit == 0 {
		request.Limit = 20
	}
	states := append([]State(nil), request.States...)
	if len(states) == 0 {
		states = []State{StateActive}
	}
	for _, state := range states {
		if state != StateActive && state != StateNeedsReview && state != StateArchived {
			return nil, fmt.Errorf("%w: invalid search request", ErrInvalid)
		}
	}
	filter := Search{Query: query, Project: request.Project, Scope: request.Scope, TopicKey: request.TopicKey, Limit: request.Limit, States: states}
	if request.Type != "" {
		filter.Types = []string{request.Type}
	}
	items, err := s.store.Search(ctx, filter)
	if err != nil {
		return nil, err
	}
	results, remaining := make([]MemoryResult, len(items)), previewBudget
	for i, item := range items {
		results[i] = shape(item, false)
		preview := []rune(item.Content)
		if len(preview) > previewLimit {
			preview = preview[:previewLimit]
		}
		if len(preview) > remaining {
			preview = preview[:remaining]
		}
		results[i].Preview = string(preview)
		remaining -= len(preview)
	}
	return results, nil
}

func (s MemoryService) Get(ctx context.Context, request GetRequest) (MemoryResult, error) {
	if err := ctx.Err(); err != nil {
		return MemoryResult{}, err
	}
	if request.ID == "" || request.Project == "" || request.Scope != ScopeProject && request.Scope != ScopePersonal || !validMetadata(request.ID, request.Project) {
		return MemoryResult{}, fmt.Errorf("%w: invalid get request", ErrInvalid)
	}
	item, err := s.store.Get(ctx, request.ID, request.Project, request.Scope)
	if err != nil {
		return MemoryResult{}, err
	}
	return shape(item, true), nil
}

func safeQuery(value string, matchAny bool) (string, bool) {
	terms := strings.Fields(value)
	if len(terms) == 0 {
		return "", false
	}
	for i, term := range terms {
		upper := strings.ToUpper(term)
		if upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
			return "", false
		}
		for _, r := range term {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' {
				return "", false
			}
		}
		terms[i] = `"` + term + `"`
	}
	separator := " "
	if matchAny {
		separator = " OR "
	}
	return strings.Join(terms, separator), true
}

func validReferences(references []string) bool {
	if len(references) > 50 {
		return false
	}
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if !validText(reference, 256, false) {
			return false
		}
		if _, exists := seen[reference]; exists {
			return false
		}
		seen[reference] = struct{}{}
	}
	return true
}

func validMetadata(values ...string) bool {
	for _, value := range values {
		if !validText(value, 256, true) {
			return false
		}
	}
	return true
}
func validText(value string, limit int, optional bool) bool {
	if !optional && strings.TrimSpace(value) == "" || len([]rune(value)) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func shape(item Observation, content bool) MemoryResult {
	result := MemoryResult{ID: item.ID, Title: item.Title, Project: item.Project, Scope: item.Scope, Type: item.Type, TopicKey: item.TopicKey, Session: item.Session, Producer: item.Provenance.Producer, SourceProvider: item.Provenance.SourceProvider, SourceID: item.Provenance.SourceID, State: item.State, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, References: append([]string(nil), item.References...)}
	if content {
		result.Content = item.Content
	}
	return result
}

package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	defaultPiStoreMaxBytes = 1 << 20
	maxPiStoreProviders    = 256
	maxPiStoreModels       = 4096
)

// piStoreEfforts is the canonical effort order Pi accepts for a worker or
// Manager selection. Larger native thinking levels exist in Pi's model store
// but its selection contract rejects them, so they are not offered.
var piStoreEfforts = [...]string{"minimal", "low", "medium", "high", "xhigh"}

// PiStore discovers the models Pi already maintains locally in its model
// store. It performs no network access; Refresh re-reads the same local file.
type PiStore struct {
	path     string
	maxBytes int64
}

// NewPiStore reads the model store at modelsStorePath, normally
// <agent-dir>/models-store.json.
func NewPiStore(modelsStorePath string) *PiStore {
	return &PiStore{path: modelsStorePath, maxBytes: defaultPiStoreMaxBytes}
}

func (store *PiStore) Discover(ctx context.Context) (Snapshot, error) {
	return store.read(ctx)
}

func (store *PiStore) Refresh(ctx context.Context) (Snapshot, error) {
	return store.read(ctx)
}

func (store *PiStore) read(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrDiscovery, err)
	}
	if store.path == "" {
		return Snapshot{}, ErrDiscovery
	}
	info, err := os.Lstat(store.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > store.maxBytes {
		return Snapshot{}, ErrDiscovery
	}
	file, err := os.Open(store.path)
	if err != nil {
		return Snapshot{}, ErrDiscovery
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !opened.Mode().IsRegular() || opened.Size() <= 0 || opened.Size() > store.maxBytes {
		return Snapshot{}, ErrDiscovery
	}
	data, err := io.ReadAll(io.LimitReader(file, store.maxBytes+1))
	if err != nil || int64(len(data)) > store.maxBytes {
		return Snapshot{}, ErrDiscovery
	}
	return parsePiStore(data)
}

// parsePiStore accepts only the bounded provider/model shape Pi persists. A
// provider whose entry lacks a model list, a model without a valid identifier,
// or a duplicate reference invalidates the whole snapshot so the TUI never
// presents a partially understood catalog as complete.
func parsePiStore(data []byte) (Snapshot, error) {
	document := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &document); err != nil || document == nil || len(document) == 0 || len(document) > maxPiStoreProviders {
		return Snapshot{}, ErrInvalidOutput
	}
	models := make(map[string]struct{})
	providers := make(map[string]struct{})
	variants := make(map[string][]string)
	for provider, raw := range document {
		if !validSegment(provider) {
			return Snapshot{}, ErrInvalidOutput
		}
		entry := struct {
			Models []json.RawMessage `json:"models"`
		}{}
		if err := json.Unmarshal(raw, &entry); err != nil || entry.Models == nil {
			return Snapshot{}, ErrInvalidOutput
		}
		for _, rawModel := range entry.Models {
			model := struct {
				ID               string         `json:"id"`
				Reasoning        bool           `json:"reasoning"`
				ThinkingLevelMap map[string]any `json:"thinkingLevelMap"`
			}{}
			if err := json.Unmarshal(rawModel, &model); err != nil {
				return Snapshot{}, ErrInvalidOutput
			}
			reference := provider + "/" + model.ID
			if _, ok := ValidReference(reference); !ok {
				return Snapshot{}, ErrInvalidOutput
			}
			if _, duplicate := models[reference]; duplicate {
				return Snapshot{}, ErrInvalidOutput
			}
			if len(models) >= maxPiStoreModels {
				return Snapshot{}, ErrInvalidOutput
			}
			efforts, err := piStoreSupportedEfforts(model.Reasoning, model.ThinkingLevelMap)
			if err != nil {
				return Snapshot{}, ErrInvalidOutput
			}
			models[reference] = struct{}{}
			providers[provider] = struct{}{}
			variants[reference] = efforts
		}
	}
	if len(models) == 0 {
		return Snapshot{}, ErrInvalidOutput
	}
	return Snapshot{
		Source:    SourceLocal,
		Providers: sortedKeys(providers),
		Models:    sortedKeys(models),
		Variants:  variants,
	}, nil
}

// piStoreSupportedEfforts mirrors the Pi selection contract: non-off efforts
// require reasoning and a non-null thinking level; null disables an effort.
func piStoreSupportedEfforts(reasoning bool, levels map[string]any) ([]string, error) {
	efforts := make([]string, 0, len(piStoreEfforts))
	if !reasoning {
		return efforts, nil
	}
	for _, effort := range piStoreEfforts {
		value, exists := levels[effort]
		if !exists || value == nil {
			continue
		}
		if _, ok := value.(string); !ok {
			return nil, ErrInvalidOutput
		}
		efforts = append(efforts, effort)
	}
	return efforts, nil
}

package modelcatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writePiStore(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models-store.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPiStoreDiscoverReadsModelsAndSupportedEfforts(t *testing.T) {
	path := writePiStore(t, `{
		"openai-codex": {
			"models": [
				{"id": "gpt-5.6-luna", "reasoning": true, "thinkingLevelMap": {"xhigh": "xhigh", "max": "max", "minimal": "low"}},
				{"id": "gpt-5.5", "reasoning": true, "thinkingLevelMap": {"xhigh": "xhigh", "medium": null}},
				{"id": "plain", "reasoning": false, "thinkingLevelMap": {"high": "high"}}
			]
		},
		"anthropic": {"models": [{"id": "claude", "reasoning": true, "thinkingLevelMap": {"low": "low", "medium": "medium", "high": "high"}}]}
	}`)
	store := NewPiStore(path)

	snapshot, err := store.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := Snapshot{
		Source:    SourceLocal,
		Providers: []string{"anthropic", "openai-codex"},
		Models: []string{
			"anthropic/claude",
			"openai-codex/gpt-5.5",
			"openai-codex/gpt-5.6-luna",
			"openai-codex/plain",
		},
		Variants: map[string][]string{
			"anthropic/claude":          {"low", "medium", "high"},
			"openai-codex/gpt-5.5":      {"xhigh"},
			"openai-codex/gpt-5.6-luna": {"minimal", "xhigh"},
			"openai-codex/plain":        {},
		},
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot = %#v, want %#v", snapshot, want)
	}

	refreshed, err := store.Refresh(context.Background())
	if err != nil || !reflect.DeepEqual(refreshed, want) {
		t.Fatalf("Refresh() = (%#v, %v), want same local snapshot", refreshed, err)
	}
}

func TestPiStoreFailsClosedWithoutDisclosingContent(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "not json", data: "secret-token"},
		{name: "trailing json", data: `{"provider": {"models": [{"id": "model"}]}} {}`},
		{name: "empty object", data: `{}`},
		{name: "no models", data: `{"provider": {"models": []}}`},
		{name: "missing models", data: `{"provider": {}}`},
		{name: "models not array", data: `{"provider": {"models": {}}}`},
		{name: "provider not object", data: `{"provider": "secret-token"}`},
		{name: "unsafe provider", data: `{"provider name": {"models": [{"id": "model"}]}}`},
		{name: "missing id", data: `{"provider": {"models": [{"name": "model"}]}}`},
		{name: "unsafe id", data: `{"provider": {"models": [{"id": "model name"}]}}`},
		{name: "oversized id", data: `{"provider": {"models": [{"id": "` + strings.Repeat("m", maxSegmentBytes+1) + `"}]}}`},
		{name: "duplicate reference", data: `{"provider": {"models": [{"id": "model"}, {"id": "model"}]}}`},
		{name: "invalid level", data: `{"provider": {"models": [{"id": "model", "reasoning": true, "thinkingLevelMap": {"high": 3}}]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewPiStore(writePiStore(t, test.data))
			snapshot, err := store.Discover(context.Background())
			if !errors.Is(err, ErrInvalidOutput) || !reflect.DeepEqual(snapshot, Snapshot{}) {
				t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrInvalidOutput", snapshot, err)
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("error disclosed model store content: %q", err)
			}
		})
	}
}

func TestPiStoreRejectsUnsafeFilesAndHonorsCancellation(t *testing.T) {
	if snapshot, err := NewPiStore("").Discover(context.Background()); !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
	}
	missing := filepath.Join(t.TempDir(), "models-store.json")
	if snapshot, err := NewPiStore(missing).Discover(context.Background()); !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover() = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
	}

	target := writePiStore(t, `{"provider": {"models": [{"id": "model"}]}}`)
	empty := filepath.Join(t.TempDir(), "models-store.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := NewPiStore(empty).Discover(context.Background()); !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover(empty) = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
	}
	link := filepath.Join(t.TempDir(), "models-store.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := NewPiStore(link).Discover(context.Background()); !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover(symlink) = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
	}

	oversized := writePiStore(t, `{"provider": {"models": [{"id": "`+strings.Repeat("m", 64)+`"}]}}`)
	store := &PiStore{path: oversized, maxBytes: 8}
	if snapshot, err := store.Discover(context.Background()); !errors.Is(err, ErrDiscovery) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover(oversized) = (%#v, %v), want zero snapshot and ErrDiscovery", snapshot, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if snapshot, err := NewPiStore(target).Discover(ctx); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("Discover(cancelled) = (%#v, %v), want zero snapshot and context.Canceled", snapshot, err)
	}
}

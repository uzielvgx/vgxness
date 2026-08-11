package modelcatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const (
	maxReferenceBytes    = 512
	maxSegmentBytes      = 256
	maxVariantBytes      = 64
	maxVariantValueBytes = 64 << 10
)

var ErrInvalidOutput = errors.New("invalid model catalog output")

type Source string

const (
	SourceLocal     Source = "local"
	SourceRefreshed Source = "refreshed"
)

type Snapshot struct {
	Source    Source
	Providers []string
	Models    []string
	Variants  map[string][]string
}

func parseSnapshot(output []byte, source Source) (Snapshot, error) {
	providers := make(map[string]struct{})
	models := make(map[string]struct{})
	variants := make(map[string][]string)
	for len(output) > 0 {
		line, rest, found := bytes.Cut(output, []byte{'\n'})
		if !found {
			return Snapshot{}, ErrInvalidOutput
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})
		provider, ok := ValidReference(string(line))
		if !ok {
			return Snapshot{}, ErrInvalidOutput
		}
		output = rest
		value, remaining, err := parseRecordJSON(output)
		if err != nil {
			return Snapshot{}, ErrInvalidOutput
		}
		modelVariants, err := parseVariants(value)
		if err != nil {
			return Snapshot{}, ErrInvalidOutput
		}
		reference := string(line)
		if _, duplicate := models[reference]; duplicate {
			return Snapshot{}, ErrInvalidOutput
		}
		providers[provider] = struct{}{}
		models[reference] = struct{}{}
		variants[reference] = modelVariants
		output = remaining
	}
	if len(models) == 0 {
		return Snapshot{}, ErrInvalidOutput
	}

	snapshot := Snapshot{
		Source:    source,
		Providers: sortedKeys(providers),
		Models:    sortedKeys(models),
		Variants:  variants,
	}
	return snapshot, nil
}

func parseRecordJSON(output []byte) (json.RawMessage, []byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil || len(value) == 0 || value[0] != '{' {
		return nil, nil, ErrInvalidOutput
	}
	offset := decoder.InputOffset()
	if offset < 1 || offset > int64(len(output)) {
		return nil, nil, ErrInvalidOutput
	}
	remaining := output[offset:]
	if len(remaining) == 0 {
		return value, nil, nil
	}
	if remaining[0] == '\r' {
		if len(remaining) == 1 || remaining[1] != '\n' {
			return nil, nil, ErrInvalidOutput
		}
		return value, remaining[2:], nil
	}
	if remaining[0] != '\n' {
		return nil, nil, ErrInvalidOutput
	}
	return value, remaining[1:], nil
}

func parseVariants(value json.RawMessage) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidOutput
	}
	variants := make([]string, 0)
	foundVariants := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidOutput
		}
		name, ok := key.(string)
		if !ok {
			return nil, ErrInvalidOutput
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil || len(raw) > maxVariantValueBytes {
			return nil, ErrInvalidOutput
		}
		if name == "variants" {
			if foundVariants {
				return nil, ErrInvalidOutput
			}
			foundVariants = true
			variants, err = orderedVariants(raw)
			if err != nil {
				return nil, err
			}
		}
	}
	if _, err := decoder.Token(); err != nil || !foundVariants {
		return nil, ErrInvalidOutput
	}
	if decoder.More() {
		return nil, ErrInvalidOutput
	}
	return variants, nil
}

func orderedVariants(raw json.RawMessage) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidOutput
	}
	seen := make(map[string]struct{})
	variants := make([]string, 0)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidOutput
		}
		variant, ok := key.(string)
		if !ok || !validVariant(variant) {
			return nil, ErrInvalidOutput
		}
		if _, duplicate := seen[variant]; duplicate {
			return nil, ErrInvalidOutput
		}
		seen[variant] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || len(value) > maxVariantValueBytes {
			return nil, ErrInvalidOutput
		}
		variants = append(variants, variant)
	}
	if _, err := decoder.Token(); err != nil || decoder.More() {
		return nil, ErrInvalidOutput
	}
	return variants, nil
}

func validVariant(value string) bool {
	if value == "" || len(value) > maxVariantBytes {
		return false
	}
	for _, value := range []byte(value) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-' || value == '_' {
			continue
		}
		return false
	}
	return true
}

// ValidReference validates the bounded model identifier grammar used at every
// local discovery and setup boundary and returns its provider segment.
func ValidReference(reference string) (string, bool) {
	if reference == "" || len(reference) > maxReferenceBytes || strings.HasPrefix(reference, "@") {
		return "", false
	}
	segments := strings.Split(reference, "/")
	if len(segments) < 2 {
		return "", false
	}
	for _, segment := range segments {
		if !validSegment(segment) {
			return "", false
		}
	}
	return segments[0], true
}

func validSegment(segment string) bool {
	if segment == "" || len(segment) > maxSegmentBytes {
		return false
	}
	for _, value := range []byte(segment) {
		switch {
		case value >= 'a' && value <= 'z':
		case value >= 'A' && value <= 'Z':
		case value >= '0' && value <= '9':
		case strings.ContainsRune("-_.:@+", rune(value)):
		default:
			return false
		}
	}
	return true
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

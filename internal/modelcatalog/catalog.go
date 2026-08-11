package modelcatalog

import (
	"errors"
	"sort"
	"strings"
)

const (
	maxReferenceBytes = 512
	maxSegmentBytes   = 256
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
}

func parseSnapshot(output []byte, source Source) (Snapshot, error) {
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return Snapshot{}, ErrInvalidOutput
	}

	providers := make(map[string]struct{})
	models := make(map[string]struct{})
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		provider, ok := validReference(line)
		if !ok {
			return Snapshot{}, ErrInvalidOutput
		}
		providers[provider] = struct{}{}
		models[line] = struct{}{}
	}

	snapshot := Snapshot{
		Source:    source,
		Providers: sortedKeys(providers),
		Models:    sortedKeys(models),
	}
	return snapshot, nil
}

func validReference(reference string) (string, bool) {
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

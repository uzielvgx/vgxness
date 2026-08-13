// Package orchestration defines provider-neutral manager policy.
package orchestration

import (
	"errors"
	"regexp"
	"strings"
)

// ContractIdentity binds provider projections to this shared policy.
const ContractIdentity = "vgxness-orchestration/v1"

// ContractPolicy is embedded verbatim in each installed manager artifact.
const ContractPolicy = "Canonical routing: accepted SDD, authorized implementation, direct bounded information, otherwise Explore. Structural Evidence Capsule: identity, query, source, revision, digest, paths, symbols, call path, stale, contradicted; reuse only a matching valid capsule, otherwise direct inspection. Review depth: zero passive docs/images, one ordinary, four hot paths."

type Route string

const (
	RouteDirect  Route = "direct"
	RouteSDD     Route = "sdd"
	RouteGeneral Route = "general"
	RouteExplore Route = "explore"
)

// Request contains only the predicates needed for canonical route selection.
type Request struct {
	Repository     bool
	ExactLocalRead bool
	SDDAccepted    bool
	Implementation bool
}

// RouteFor evaluates predicates in their canonical order.
func RouteFor(request Request) Route {
	switch {
	case request.SDDAccepted:
		return RouteSDD
	case request.Implementation:
		return RouteGeneral
	case !request.Repository || request.ExactLocalRead:
		return RouteDirect
	default:
		return RouteExplore
	}
}

const (
	maxEvidenceText  = 512
	maxEvidenceItems = 16
	maxEvidenceBytes = 2048
)

var ErrInvalidEvidence = errors.New("invalid structural evidence capsule")
var evidenceDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// StructuralEvidence is a bounded reusable structural-evidence capsule.
type StructuralEvidence struct {
	Contract     string
	Query        string
	Revision     string
	Digest       string
	Source       string
	Paths        []string
	Symbols      []string
	CallPath     []string
	Stale        bool
	Contradicted bool
}

func (e StructuralEvidence) Validate() error {
	if e.Contract != ContractIdentity || !validEvidenceText(e.Query) || !validEvidenceText(e.Revision) || !evidenceDigest.MatchString(e.Digest) || !validEvidenceText(e.Source) || !validEvidenceItems(e.Paths) || !validEvidenceItems(e.Symbols) || !validEvidenceItems(e.CallPath) || evidenceSize(e) > maxEvidenceBytes {
		return ErrInvalidEvidence
	}
	return nil
}

func (e StructuralEvidence) ReusableFor(query, revision, digest string) bool {
	return e.Validate() == nil && e.Query == query && e.Revision == revision && e.Digest == digest && !e.Stale && !e.Contradicted
}

func (e StructuralEvidence) FallbackRequired(query, revision, digest string) bool {
	return !e.ReusableFor(query, revision, digest)
}

type ReviewDepth int

const (
	ReviewNone ReviewDepth = iota
	ReviewOne
	ReviewFour
)

// ReviewDepthFor is deterministic from changed paths. Passive docs and images
// need no lenses; concrete hot-path names require all four lenses.
func ReviewDepthFor(paths []string) ReviewDepth {
	ordinary := false
	for _, path := range paths {
		lower := strings.ToLower(path)
		if passivePath(lower) {
			continue
		}
		if hotPath(lower) {
			return ReviewFour
		}
		ordinary = true
	}
	if ordinary {
		return ReviewOne
	}
	return ReviewNone
}

func validEvidenceText(value string) bool { return value != "" && len(value) <= maxEvidenceText }

func validEvidenceItems(values []string) bool {
	if len(values) == 0 || len(values) > maxEvidenceItems {
		return false
	}
	for _, value := range values {
		if !validEvidenceText(value) {
			return false
		}
	}
	return true
}

func evidenceSize(e StructuralEvidence) int {
	total := len(e.Contract) + len(e.Query) + len(e.Revision) + len(e.Digest) + len(e.Source)
	for _, values := range [][]string{e.Paths, e.Symbols, e.CallPath} {
		for _, value := range values {
			total += len(value)
		}
	}
	return total
}

func hotPath(path string) bool {
	base := strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".go")
	for _, category := range []string{"auth", "security", "secret", "payment", "installer", "install", "permission", "data-loss", "dataloss", "durability", "shell", "process", "exec", "command", "integration"} {
		if base == category || base == category+"s" || strings.HasPrefix(base, category+"_") || strings.HasSuffix(path, "/"+category) {
			return true
		}
	}
	return false
}

func passivePath(path string) bool {
	if strings.HasPrefix(path, "docs/") || strings.HasSuffix(path, ".md") {
		return true
	}
	for _, extension := range []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif", ".bmp", ".tif", ".tiff", ".ico"} {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return false
}

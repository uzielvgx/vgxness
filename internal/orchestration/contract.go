// Package orchestration defines provider-neutral manager policy.
package orchestration

import (
	"errors"
	"regexp"
	"strings"
)

// ContractIdentity binds provider projections to this shared policy.
const ContractIdentity = "vgxness-orchestration/v1"

const (
	// ContractBudgetPolicy is the canonical tools/delegations projection.
	ContractBudgetPolicy = "Canonical budgets: direct 0 tools/0 delegations; assisted simple 3 tools/0 delegations and complex 3 tools/1 delegation; action 6 tools/0 delegations; engineering 30 tools/5 delegations; assured 40 tools/5 delegations."

	// PreviousContractPolicyV59 preserves the policy embedded in Manager v59
	// and its Codex-parity predecessor artifacts.
	PreviousContractPolicyV59 = "Adaptive contract: silently classify domain, operation, side effect, complexity, and risk without tools or delegation. Direct: conversation, writing, translation, summarization, brainstorming, and no-effect planning use zero execution tools, skills, todos, delegation, or review. Assisted: bounded simple exact reads use at most three total tool attempts and no delegation or todo; complex evidence research may use at most one read-only delegation. Action, engineering, and assured routes retain authorization and review guarantees. " + ContractBudgetPolicy + " Parallelize at most five independent read-only agents concurrently and never overlap workspace writers. All execution tool and delegation attempts, including failures and retries, count against budget; halt and report before the next attempt would exceed it, with no silent escalation. For engineering and assured routes exhaustion is a checkpoint: explicit user continuation opens a fresh same-route window while preserving scope, authorization, lineage, todos, candidate, and child context. These are prompt-level instructions, not runtime enforcement. Load a skill only when its specialized workflow materially improves quality, safety, or verification. Use a todo only when execution state or user-visible tracking benefits. Memory: intent-triggered recall rules remain unchanged; save durable, evidence-backed, safely assessed facts with at most one memory tool; never save transient state, logs, secrets, or personal data or automatically cloud-sync."

	// ContractPolicy is embedded verbatim in each current installed manager artifact.
	ContractPolicy = PreviousContractPolicyV59 + " After significant work and immediately before reporting IMPLEMENTED, VERIFIED, DELIVERED, MERGED, or INSTALLED, assess whether durable, evidence-backed, safely assessed knowledge exists and save it before the final response; a successful save never replaces that response. Partial or interrupted work with durable handoff value is eligible. Never save transient state, raw logs, secrets, personal data, or transcripts; no automatic cloud sync; at most one autonomous save."

	// PreviousContractPolicy reconstructs the exact v48/v8 provider artifacts.
	PreviousContractPolicy = "Canonical routing: accepted SDD, authorized implementation, direct bounded information, otherwise Explore. Structural Evidence Capsule: identity, query, source, revision, digest, paths, symbols, call path, stale, contradicted; reuse only a matching valid capsule, otherwise direct inspection. Review depth: zero passive docs/images, one ordinary, four hot paths."

	// PreviousContractPolicyV51 reconstructs the exact contract embedded in the
	// immediately preceding OpenCode v51 and Codex v11 packages.
	PreviousContractPolicyV51 = "Adaptive contract: silently classify domain, operation, side effect, complexity, and risk without tools or delegation, choose the least-cost route. Direct: conversation, writing, translation, summarization, brainstorming, and no-effect planning use zero execution tools, skills, todos, delegation, or review. Assisted: bounded simple exact reads use at most three total tool attempts and no delegation or todo; complex evidence research may use at most one read-only delegation. Action, engineering, and assured routes retain existing authorization, readback, General, Explore, TDD, freeze, verifier, review, and delivery guarantees. Canonical budgets: direct 0 tools/0 delegations; assisted simple 3 tools/0 delegations and complex 3 tools/1 delegation; action 6 tools/0 delegations; engineering 12 tools/2 delegations; assured 16 tools/2 delegations. All execution tool and delegation attempts, including failures and retries, count against budget; halt and report before the next attempt would exceed it, with no silent escalation. These are prompt-level instructions, not runtime enforcement. Load a skill only when its specialized workflow materially improves quality, safety, or verification. Use a todo only when execution state or user-visible tracking benefits, not merely because an answer has several steps. Memory is orthogonal: intent-triggered recall rules remain unchanged; after any route, autonomously save only durable, evidence-backed, safely assessed project decisions, preferences, constraints, or learnings using at most one memory tool; never save transient state, logs, secrets, or personal data, require engineering ceremony, or automatically cloud-sync."

	// ReadinessManagerContract is evidence-only preparation, never authority.
	ReadinessManagerContract = "Readiness contract: readiness-envelope/v1 is evidence only. Manager alone assembles and immediately revalidates the envelope; invalidate it when mission identity, scope, acceptance criteria, skills, permitted validation, candidate, provider artifact, target hash, or dependency changes. It is never approval, authorization, validation, review, lifecycle authority, or host enforcement. Direct and exempt routes create no readiness envelope or ceremony. Verifier and reviewers never approve readiness."

	// ReadinessWriterContract binds non-exempt writer behavior to one envelope.
	ReadinessWriterContract = "Readiness writer contract: readiness-envelope/v1; reject missing, stale, malformed, mismatched, BLOCKED, or INCONCLUSIVE readiness envelopes before writing; recheck the accepted binding and echo the accepted envelopeDigest in the return. Readiness is never approval or host enforcement."
)

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

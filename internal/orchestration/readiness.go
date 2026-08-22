package orchestration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
)

const ReadinessSchemaVersion = "readiness-envelope/v1"
const maxReadinessItems = 128
const maxReadinessString = 4096

type ActivationClass string

const (
	ActivationExempt ActivationClass = "exempt"
	ActivationLight  ActivationClass = "light"
	ActivationFull   ActivationClass = "full"
)

type ReadinessStatus string

const (
	ReadinessReady        ReadinessStatus = "READY"
	ReadinessInconclusive ReadinessStatus = "INCONCLUSIVE"
	ReadinessBlocked      ReadinessStatus = "BLOCKED"
)

type BindingKind string

const (
	BindingGeneral BindingKind = "general"
	BindingSDD     BindingKind = "sdd"
)

type EvidenceAvailability string

const (
	EvidenceAvailable     EvidenceAvailability = "available"
	EvidenceUnavailable   EvidenceAvailability = "unavailable"
	EvidenceNotApplicable EvidenceAvailability = "not-applicable"
)

type ReasonCode string
type RiskCategory string

type ActivationFacts struct{ WriteIntent, DirectRoute, SimpleExactRead, SDDAccepted, Delivery, FrozenCandidate, CrossPlatform, LifecycleRecovery, AuthorizationSecurity, Secrets, Payments, Installer, DataLossExposure, ShellProcess, Durability, IdentityDigest, ProviderTemplate, ConcreteHotPath, UnknownRisk bool }

func ClassifyActivation(f ActivationFacts) ActivationClass {
	if !f.WriteIntent || f.DirectRoute || f.SimpleExactRead {
		return ActivationExempt
	}
	if f.SDDAccepted || f.Delivery || f.FrozenCandidate || f.CrossPlatform || f.LifecycleRecovery || f.AuthorizationSecurity || f.Secrets || f.Payments || f.Installer || f.DataLossExposure || f.ShellProcess || f.Durability || f.IdentityDigest || f.ProviderTemplate || f.ConcreteHotPath || f.UnknownRisk {
		return ActivationFull
	}
	return ActivationLight
}

type AcceptedInput struct{ ArtifactID, RevisionID, Digest string }
type ChangedPath struct {
	Path, SHA256 string
	NoSymlink    bool
}
type ReviewBinding struct {
	CandidateDigest               string
	ChangedPaths                  []ChangedPath
	DiffScope, AcceptanceCriteria []string
}
type ExpectedBinding struct {
	Kind                                                                                        BindingKind
	ChangeID, MissionID, ReplayNonce, ContextDigest, TaskArtifactID, TaskRevisionID, TaskDigest string
	ExpectedStateVersion                                                                        int
	Inputs                                                                                      []AcceptedInput
	CandidateDigest, BaseIdentity                                                               string
	ChangedPaths                                                                                []ChangedPath
	ReviewBinding                                                                               ReviewBinding
}
type Scope struct {
	Authorized, WriteIntent              bool
	Paths, Criteria, PermittedValidation []string
	AuthorizationReference               string
	Targets                              []TargetIdentity
}
type TargetIdentity struct {
	Path, SHA256       string
	Missing, NoSymlink bool
}
type EvidenceKind string

const (
	EvidenceKindAuthorization EvidenceKind = "authorization"
	EvidenceKindContract      EvidenceKind = "contract"
	EvidenceKindCommand       EvidenceKind = "command"
	EvidenceKindTarget        EvidenceKind = "target"
	EvidenceKindProvider      EvidenceKind = "provider"
	EvidenceKindSelfAttested  EvidenceKind = "self-attested"
)

type EvidenceReceipt struct {
	Kind                                                      EvidenceKind
	Locator, Digest, ObservedResult, CandidateDigest, Excerpt string
	Availability                                              EvidenceAvailability
	Current, Contradictory                                    bool
	RiskCategory                                              RiskCategory
	Rationale, NotApplicableRationale                         string
}
type Unknown struct {
	Question, Requirement string
	Consequential         bool
}
type Dependency struct {
	Kind, Identity, Digest string
	StateVersion           int
	NoSymlink              bool
}
type ReadinessEnvelope struct {
	SchemaVersion         string            `json:"schemaVersion"`
	Activation            ActivationClass   `json:"activationClass"`
	Status                ReadinessStatus   `json:"status"`
	MissionEvidence       json.RawMessage   `json:"missionEvidence"`
	MissionEvidenceDigest string            `json:"missionEvidenceDigest"`
	Binding               ExpectedBinding   `json:"binding"`
	Scope                 Scope             `json:"scope"`
	Evidence              []EvidenceReceipt `json:"evidence"`
	Unknowns              []Unknown         `json:"unknowns"`
	Dependencies          []Dependency      `json:"dependencies"`
	RiskCategories        []RiskCategory    `json:"riskCategories"`
	Blockers              []ReasonCode      `json:"blockers"`
	EnvelopeDigest        string            `json:"-"`
}
type ValidationResult struct {
	Status  ReadinessStatus
	Reasons []ReasonCode
}
type BuildInput struct {
	MissionEvidence json.RawMessage
	Activation      ActivationFacts
	Binding         ExpectedBinding
	Scope           Scope
	Evidence        []EvidenceReceipt
	Unknowns        []Unknown
	Dependencies    []Dependency
	RiskCategories  []RiskCategory
}

// ReadinessExpectation is the complete current-state comparator.  Unlike a digest,
// it independently retains every value whose freshness affects a write.
type ReadinessExpectation struct {
	MissionEvidenceDigest string
	Binding               ExpectedBinding
	Scope                 Scope
	Evidence              []EvidenceReceipt
	Unknowns              []Unknown
	Dependencies          []Dependency
	RiskCategories        []RiskCategory
	Activation            ActivationClass
}

func ReadinessExpectationFromBuildInput(in BuildInput) ReadinessExpectation {
	in = in.clone()
	return ReadinessExpectation{MissionEvidenceDigest: MissionEvidenceDigest(in.MissionEvidence), Binding: in.Binding, Scope: in.Scope, Evidence: in.Evidence, Unknowns: in.Unknowns, Dependencies: in.Dependencies, RiskCategories: in.RiskCategories, Activation: ClassifyActivation(in.Activation)}
}

func BuildReadiness(in BuildInput) (ReadinessEnvelope, ValidationResult) {
	in = in.clone()
	if ClassifyActivation(in.Activation) == ActivationExempt {
		return ReadinessEnvelope{}, ValidationResult{}
	}
	e := ReadinessEnvelope{SchemaVersion: ReadinessSchemaVersion, Activation: ClassifyActivation(in.Activation), MissionEvidence: append(json.RawMessage(nil), in.MissionEvidence...), Binding: in.Binding, Scope: in.Scope, Evidence: in.Evidence, Unknowns: in.Unknowns, Dependencies: in.Dependencies, RiskCategories: in.RiskCategories}
	e.MissionEvidenceDigest = MissionEvidenceDigest(e.MissionEvidence)
	r := validateShape(e)
	e.Status, e.Blockers = r.Status, r.Reasons
	e.EnvelopeDigest = EnvelopeDigest(e)
	// Validate the assembled, digest-bearing envelope through the same complete
	// comparator used by consumers.  Build never trusts a caller supplied status.
	want := ReadinessExpectation{MissionEvidenceDigest: e.MissionEvidenceDigest, Binding: e.Binding, Scope: e.Scope, Evidence: e.Evidence, Unknowns: e.Unknowns, Dependencies: e.Dependencies, RiskCategories: e.RiskCategories, Activation: e.Activation}
	r = ValidateReadiness(e, want, nil)
	e.Status, e.Blockers = r.Status, r.Reasons
	e.EnvelopeDigest = EnvelopeDigest(e)
	return e, r
}

// ValidateReadiness retains the legacy ExpectedBinding form while accepting a full expectation.
func ValidateReadiness(e ReadinessEnvelope, expected any, _ any) ValidationResult {
	r := validateShape(e)
	if e.MissionEvidenceDigest != MissionEvidenceDigest(e.MissionEvidence) || e.EnvelopeDigest != EnvelopeDigest(e) {
		r = blocked(r, "envelope_digest_mismatch")
	}
	derived := normalize(r)
	if e.Status != "" && e.Status != derived.Status {
		r = blocked(r, "status_forged")
	}
	if !validReasons(e.Blockers) || (e.Status != "" && !reflect.DeepEqual(derived.Reasons, e.Blockers)) {
		r = blocked(r, "required_field_missing")
	}
	switch want := expected.(type) {
	case ExpectedBinding:
		if !reflect.DeepEqual(e.Binding, want) {
			r = blocked(r, "binding_mismatch")
		}
	case *ExpectedBinding:
		if want == nil || !reflect.DeepEqual(e.Binding, *want) {
			r = blocked(r, "binding_mismatch")
		}
	case ReadinessExpectation:
		if !matchesExpectation(e, want) {
			r = blocked(r, "binding_mismatch")
		}
	case *ReadinessExpectation:
		if want == nil || !matchesExpectation(e, *want) {
			r = blocked(r, "binding_mismatch")
		}
	default:
		r = blocked(r, "binding_mismatch")
	}
	return normalize(r)
}
func matchesExpectation(e ReadinessEnvelope, w ReadinessExpectation) bool {
	return e.MissionEvidenceDigest == w.MissionEvidenceDigest && e.Activation == w.Activation && reflect.DeepEqual(e.Binding, w.Binding) && reflect.DeepEqual(e.Scope, w.Scope) && reflect.DeepEqual(e.Evidence, w.Evidence) && reflect.DeepEqual(e.Unknowns, w.Unknowns) && reflect.DeepEqual(e.Dependencies, w.Dependencies) && reflect.DeepEqual(e.RiskCategories, w.RiskCategories)
}
func InvalidationReasons(e ReadinessEnvelope, want any) []ReasonCode {
	return ValidateReadiness(e, want, nil).Reasons
}

func (in BuildInput) clone() BuildInput {
	out := in
	out.MissionEvidence = append(json.RawMessage(nil), in.MissionEvidence...)
	out.Binding = cloneBinding(in.Binding)
	out.Scope = cloneScope(in.Scope)
	out.Evidence = append([]EvidenceReceipt(nil), in.Evidence...)
	out.Unknowns = append([]Unknown(nil), in.Unknowns...)
	out.Dependencies = append([]Dependency(nil), in.Dependencies...)
	out.RiskCategories = append([]RiskCategory(nil), in.RiskCategories...)
	return out
}
func cloneBinding(b ExpectedBinding) ExpectedBinding {
	b.Inputs = append([]AcceptedInput(nil), b.Inputs...)
	b.ChangedPaths = append([]ChangedPath(nil), b.ChangedPaths...)
	b.ReviewBinding.ChangedPaths = append([]ChangedPath(nil), b.ReviewBinding.ChangedPaths...)
	b.ReviewBinding.DiffScope = append([]string(nil), b.ReviewBinding.DiffScope...)
	b.ReviewBinding.AcceptanceCriteria = append([]string(nil), b.ReviewBinding.AcceptanceCriteria...)
	return b
}
func cloneScope(s Scope) Scope {
	s.Paths = append([]string(nil), s.Paths...)
	s.Criteria = append([]string(nil), s.Criteria...)
	s.PermittedValidation = append([]string(nil), s.PermittedValidation...)
	s.Targets = append([]TargetIdentity(nil), s.Targets...)
	return s
}

type WriterReturnEcho struct {
	MissionID, ReplayNonce, EnvelopeDigest, CandidateDigest, BaseIdentity string
	ChangedPaths                                                          []ChangedPath
	ReviewBinding                                                         ReviewBinding
}

func ValidateWriterReturn(got WriterReturnEcho, e ReadinessEnvelope, want ExpectedBinding) error {
	if r := ValidateReadiness(e, want, nil); r.Status != ReadinessReady || got.MissionID != want.MissionID || got.ReplayNonce != want.ReplayNonce || got.EnvelopeDigest != e.EnvelopeDigest || got.CandidateDigest != want.CandidateDigest || got.BaseIdentity != want.BaseIdentity || !reflect.DeepEqual(got.ChangedPaths, want.ChangedPaths) || !reflect.DeepEqual(got.ReviewBinding, want.ReviewBinding) {
		return errors.New("readiness writer echo mismatch")
	}
	return nil
}

func validateShape(e ReadinessEnvelope) ValidationResult {
	r := ValidationResult{Status: ReadinessReady}
	if e.SchemaVersion != ReadinessSchemaVersion || !validActivation(e.Activation) || (e.Status != "" && !validStatus(e.Status)) || len(e.MissionEvidence) == 0 || len(e.MissionEvidence) > maxCanonicalBytes || !e.Scope.Authorized || !e.Scope.WriteIntent || !validStrings(e.Scope.Paths, true) || !validStrings(e.Scope.Criteria, false) || !validStrings(e.Scope.PermittedValidation, false) || !validText(e.Scope.AuthorizationReference) || !validText(e.Binding.MissionID) || !validText(e.Binding.ReplayNonce) || !validDigest(e.MissionEvidenceDigest) {
		r = blocked(r, "required_field_missing")
	}
	if e.Activation == ActivationExempt || (e.Binding.Kind != BindingGeneral && e.Binding.Kind != BindingSDD) {
		r = blocked(r, "binding_mismatch")
	}
	if e.Binding.Kind == BindingGeneral && !validDigest(e.Binding.ContextDigest) {
		r = blocked(r, "binding_mismatch")
	}
	if e.Binding.Kind == BindingSDD && (!validText(e.Binding.ChangeID) || !validText(e.Binding.TaskArtifactID) || !validText(e.Binding.TaskRevisionID) || !validDigest(e.Binding.TaskDigest) || e.Binding.ExpectedStateVersion <= 0 || !validInputs(e.Binding.Inputs)) {
		r = blocked(r, "binding_mismatch")
	}
	if !validCollection(e.Dependencies) || !validRisks(e.RiskCategories) || !validTargets(e.Scope.Targets) {
		r = blocked(r, "required_field_missing")
	}
	for _, t := range e.Scope.Targets {
		if !validPath(t.Path) || !t.NoSymlink || (t.Missing && t.SHA256 != "") || (!t.Missing && !validDigest(t.SHA256)) || (e.Binding.Kind == BindingSDD && !hasTargetDependency(e.Dependencies, t)) {
			r = blocked(r, "target_hash_mismatch")
		}
	}
	if !validCandidate(e.Binding, hasRisk(e.RiskCategories, "frozen")) {
		r = blocked(r, "candidate_mismatch")
	}
	if len(e.Evidence) == 0 {
		r = inconclusive(r, "evidence_unavailable")
	}
	if len(e.Evidence) > maxReadinessItems {
		r = blocked(r, "required_field_missing")
	} else {
		seenEvidence := map[string]bool{}
		for _, ev := range e.Evidence {
			key := string(ev.Kind) + "\x00" + ev.Locator + "\x00" + string(ev.RiskCategory) + "\x00" + ev.Digest + "\x00" + ev.CandidateDigest
			if seenEvidence[key] {
				r = blocked(r, "required_field_missing")
			}
			seenEvidence[key] = true
			if !validEvidenceStructure(ev) {
				r = blocked(r, "required_field_missing")
				continue
			}
			if !validEvidenceBinding(ev, e) {
				r = blocked(r, "candidate_mismatch")
			} else if !evidenceSatisfies(ev) {
				r = inconclusive(r, "evidence_unavailable")
			}
		}
	}
	for _, risk := range e.RiskCategories {
		if !validRisk(risk) {
			r = blocked(r, "required_field_missing")
		} else if !riskEvidenceCurrent(e, risk) {
			r = inconclusive(r, "risk_evidence_gap")
		}
	}
	seenDependencies := map[string]bool{}
	for _, d := range e.Dependencies {
		key := d.Kind + "\x00" + d.Identity
		if !validText(d.Kind) || !validText(d.Identity) || (d.Kind != "target" && !validDigest(d.Digest)) || (d.Kind == "target" && d.Digest != "" && !validDigest(d.Digest)) || seenDependencies[key] {
			r = blocked(r, "binding_mismatch")
		}
		seenDependencies[key] = true
	}
	if e.Binding.Kind == BindingSDD && !hasDependency(e.Dependencies, "task", e.Binding.TaskArtifactID, e.Binding.TaskDigest) {
		r = blocked(r, "binding_mismatch")
	}
	if len(e.Unknowns) > maxReadinessItems {
		r = blocked(r, "required_field_missing")
	} else {
		seenUnknowns := map[struct{ question, requirement string }]bool{}
		for _, u := range e.Unknowns {
			key := struct{ question, requirement string }{u.Question, u.Requirement}
			if !validText(u.Question) || !validText(u.Requirement) || seenUnknowns[key] {
				r = blocked(r, "required_field_missing")
			}
			seenUnknowns[key] = true
			if u.Consequential {
				r = inconclusive(r, "consequential_unknown")
			}
		}
	}
	return normalize(r)
}
func validCandidate(b ExpectedBinding, required bool) bool {
	present := b.CandidateDigest != "" || b.BaseIdentity != "" || len(b.ChangedPaths) != 0 || b.ReviewBinding.CandidateDigest != "" || len(b.ReviewBinding.ChangedPaths) != 0 || len(b.ReviewBinding.DiffScope) != 0 || len(b.ReviewBinding.AcceptanceCriteria) != 0
	if !required && !present {
		return true
	}
	return validDigest(b.CandidateDigest) && validText(b.BaseIdentity) && validChangedPaths(b.ChangedPaths) && b.ReviewBinding.CandidateDigest == b.CandidateDigest && reflect.DeepEqual(b.ReviewBinding.ChangedPaths, b.ChangedPaths) && validStrings(b.ReviewBinding.DiffScope, false) && validStrings(b.ReviewBinding.AcceptanceCriteria, false)
}
func validChangedPaths(v []ChangedPath) bool {
	if len(v) == 0 || len(v) > maxReadinessItems {
		return false
	}
	seen := map[string]bool{}
	for _, p := range v {
		if !validPath(p.Path) || !validDigest(p.SHA256) || !p.NoSymlink || seen[p.Path] {
			return false
		}
		seen[p.Path] = true
	}
	return true
}
func riskEvidenceCurrent(e ReadinessEnvelope, risk RiskCategory) bool {
	for _, v := range e.Evidence {
		if v.RiskCategory == risk && evidenceSatisfies(v) && v.Kind != EvidenceKindSelfAttested {
			return true
		}
	}
	return false
}
func validEvidenceStructure(v EvidenceReceipt) bool {
	return validEvidenceKind(v.Kind) && validText(v.Locator) && validRisk(v.RiskCategory) && validText(v.ObservedResult) &&
		(v.Excerpt == "" || validText(v.Excerpt)) && (v.Rationale == "" || validText(v.Rationale)) &&
		(v.NotApplicableRationale == "" || validText(v.NotApplicableRationale)) && (v.Digest == "" || validDigest(v.Digest)) &&
		(v.CandidateDigest == "" || validDigest(v.CandidateDigest)) && validAvailability(v.Availability)
}
func validEvidenceBinding(v EvidenceReceipt, e ReadinessEnvelope) bool {
	if v.CandidateDigest != "" && (hasRisk(e.RiskCategories, "frozen") || e.Binding.CandidateDigest != "") && v.CandidateDigest != e.Binding.CandidateDigest {
		return false
	}
	if v.Kind != EvidenceKindTarget {
		return true
	}
	for _, t := range e.Scope.Targets {
		if v.Locator == t.Path && v.Digest == t.SHA256 && (t.Missing || v.Digest != "") {
			return true
		}
	}
	return false
}
func evidenceSatisfies(v EvidenceReceipt) bool {
	if v.Kind == EvidenceKindSelfAttested {
		return false
	}
	switch v.Availability {
	case EvidenceAvailable:
		return v.Current && !v.Contradictory
	case EvidenceNotApplicable:
		return (validText(v.NotApplicableRationale) || validText(v.Rationale)) && !v.Contradictory
	}
	return false
}
func blocked(r ValidationResult, code ReasonCode) ValidationResult {
	r.Status = ReadinessBlocked
	r.Reasons = append(r.Reasons, code)
	return r
}
func inconclusive(r ValidationResult, code ReasonCode) ValidationResult {
	if r.Status != ReadinessBlocked {
		r.Status = ReadinessInconclusive
	}
	r.Reasons = append(r.Reasons, code)
	return r
}
func validActivation(v ActivationClass) bool { return v == ActivationLight || v == ActivationFull }
func validStatus(v ReadinessStatus) bool {
	return v == ReadinessReady || v == ReadinessInconclusive || v == ReadinessBlocked
}
func validPath(v string) bool {
	if !validText(v) || strings.HasPrefix(v, "/") || strings.Contains(v, "\\") {
		return false
	}
	for _, p := range strings.Split(v, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func validText(v string) bool { return v != "" && len(v) <= maxReadinessString }
func validStrings(v []string, paths bool) bool {
	if len(v) == 0 || len(v) > maxReadinessItems {
		return false
	}
	seen := map[string]bool{}
	for _, x := range v {
		if (paths && !validPath(x)) || (!paths && !validText(x)) || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}
func validInputs(v []AcceptedInput) bool {
	if len(v) == 0 || len(v) > maxReadinessItems {
		return false
	}
	seen := map[string]bool{}
	for _, x := range v {
		k := x.ArtifactID + "\x00" + x.RevisionID
		if !validText(x.ArtifactID) || !validText(x.RevisionID) || !validDigest(x.Digest) || seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}
func validCollection[T any](v []T) bool { return len(v) > 0 && len(v) <= maxReadinessItems }
func validRisks(v []RiskCategory) bool {
	if !validCollection(v) {
		return false
	}
	seen := map[RiskCategory]bool{}
	for _, x := range v {
		if !validRisk(x) || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}
func validTargets(v []TargetIdentity) bool {
	if !validCollection(v) {
		return false
	}
	seen := map[string]bool{}
	for _, t := range v {
		if seen[t.Path] {
			return false
		}
		seen[t.Path] = true
	}
	return true
}
func validEvidenceKind(v EvidenceKind) bool {
	switch v {
	case EvidenceKindAuthorization, EvidenceKindContract, EvidenceKindCommand, EvidenceKindTarget, EvidenceKindProvider, EvidenceKindSelfAttested:
		return true
	}
	return false
}
func validAvailability(v EvidenceAvailability) bool {
	return v == EvidenceAvailable || v == EvidenceUnavailable || v == EvidenceNotApplicable
}
func validReasons(v []ReasonCode) bool {
	if len(v) > maxReadinessItems {
		return false
	}
	for _, x := range v {
		if !validReason(x) {
			return false
		}
	}
	return true
}
func hasDependency(d []Dependency, kind, identity, digest string) bool {
	for _, x := range d {
		if x.Kind == kind && x.Identity == identity && x.Digest == digest {
			return true
		}
	}
	return false
}
func hasTargetDependency(d []Dependency, t TargetIdentity) bool {
	for _, x := range d {
		if x.Kind == "target" && x.Identity == t.Path && x.NoSymlink && x.Digest == t.SHA256 {
			return true
		}
	}
	return false
}
func validRisk(v RiskCategory) bool {
	switch v {
	case "sdd", "delivery", "frozen", "cross-platform", "lifecycle-recovery", "authorization-security", "secrets", "payments", "installer", "data-loss-exposure", "shell-process", "durability", "identity-digest", "provider-template", "unknown-risk":
		return true
	}
	return false
}
func hasRisk(v []RiskCategory, x RiskCategory) bool {
	for _, r := range v {
		if r == x {
			return true
		}
	}
	return false
}
func normalize(r ValidationResult) ValidationResult {
	sort.Slice(r.Reasons, func(i, j int) bool { return r.Reasons[i] < r.Reasons[j] })
	r.Reasons = dedupe(r.Reasons)
	return r
}
func dedupe(in []ReasonCode) []ReasonCode {
	out := in[:0]
	for _, x := range in {
		if len(out) == 0 || out[len(out)-1] != x {
			out = append(out, x)
		}
	}
	return out
}
func MissionEvidenceDigest(raw json.RawMessage) string {
	b, err := CanonicalJSON(raw)
	if err != nil {
		return ""
	}
	return sha256hex(b)
}
func EnvelopeDigest(e ReadinessEnvelope) string {
	e.EnvelopeDigest = ""
	b, err := CanonicalValue(e)
	if err != nil {
		return ""
	}
	return sha256hex(b)
}
func sha256hex(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && s == strings.ToLower(s)
}

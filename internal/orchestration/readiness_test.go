package orchestration

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReadinessActivationPrecedenceAndZeroCeremony(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts ActivationFacts
		want  ActivationClass
	}{
		{"direct read is exempt", ActivationFacts{DirectRoute: true}, ActivationExempt},
		{"simple exact read is exempt", ActivationFacts{SimpleExactRead: true}, ActivationExempt},
		{"exempt does not escalate from unrelated full facts", ActivationFacts{DirectRoute: true, IdentityDigest: true}, ActivationExempt},
		{"ordinary authorized write is light", ActivationFacts{WriteIntent: true}, ActivationLight},
		{"sdd is full", ActivationFacts{WriteIntent: true, SDDAccepted: true}, ActivationFull},
		{"frozen candidate is full", ActivationFacts{WriteIntent: true, FrozenCandidate: true}, ActivationFull},
		{"provider template is full", ActivationFacts{WriteIntent: true, ProviderTemplate: true}, ActivationFull},
		{"unknown risk is full", ActivationFacts{WriteIntent: true, UnknownRisk: true}, ActivationFull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyActivation(tc.facts); got != tc.want {
				t.Fatalf("ClassifyActivation(%+v) = %q, want %q", tc.facts, got, tc.want)
			}
		})
	}
	// A no-write request must remain ceremony-free even when readiness inputs are absent.
	if got := ClassifyActivation(ActivationFacts{}); got != ActivationExempt {
		t.Fatalf("no-write request = %q, want exempt", got)
	}
}

func TestReadinessLightAndFullHappyPaths(t *testing.T) {
	light := completeBuildInput()
	light.Activation = ActivationFacts{WriteIntent: true}
	light.Binding = ExpectedBinding{Kind: BindingGeneral, MissionID: "light", ReplayNonce: "nonce", ContextDigest: digest64('c')}
	light.RiskCategories = []RiskCategory{"identity-digest"}
	light.Dependencies = []Dependency{{Kind: "source", Identity: "a.go", Digest: digest64('d')}}
	if e, got := BuildReadiness(light); got.Status != ReadinessReady || e.Activation != ActivationLight {
		t.Fatalf("complete light envelope = %+v, %+v", e, got)
	}

	full := completeBuildInput()
	e, got := BuildReadiness(full)
	if got.Status != ReadinessReady || e.Activation != ActivationFull {
		t.Fatalf("complete SDD envelope = %+v, %+v", e, got)
	}
	if e.Binding.Kind != BindingSDD || e.Binding.TaskArtifactID == "" || len(e.Binding.Inputs) == 0 {
		t.Fatalf("SDD binding is incomplete: %+v", e.Binding)
	}
}

func TestReadinessRejectsEveryFreshnessMutation(t *testing.T) {
	base := completeBuildInput()
	envelope, result := BuildReadiness(base)
	if result.Status != ReadinessReady {
		t.Fatalf("base = %+v", result)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ReadinessEnvelope, *ExpectedBinding)
	}{
		{"mission", func(e *ReadinessEnvelope, _ *ExpectedBinding) {
			e.MissionEvidence = json.RawMessage(`{"mission":"changed"}`)
		}},
		{"mission digest", func(e *ReadinessEnvelope, _ *ExpectedBinding) { e.MissionEvidenceDigest = digest64('z') }},
		{"change", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.ChangeID = "other" }},
		{"task artifact", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.TaskArtifactID = "other" }},
		{"task revision", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.TaskRevisionID = "other" }},
		{"task digest", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.TaskDigest = digest64('z') }},
		{"input identity", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.Inputs[0].ArtifactID = "other" }},
		{"input revision", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.Inputs[0].RevisionID = "other" }},
		{"input digest", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.Inputs[0].Digest = digest64('z') }},
		{"input order", func(_ *ReadinessEnvelope, w *ExpectedBinding) {
			w.Inputs = append(w.Inputs, w.Inputs[0])
			w.Inputs[0], w.Inputs[1] = w.Inputs[1], w.Inputs[0]
		}},
		{"state", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.ExpectedStateVersion++ }},
		{"mission identity", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.MissionID = "other" }},
		{"replay", func(_ *ReadinessEnvelope, w *ExpectedBinding) { w.ReplayNonce = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := envelope
			e.Binding.Inputs = append([]AcceptedInput(nil), envelope.Binding.Inputs...)
			want := base.Binding
			want.Inputs = append([]AcceptedInput(nil), base.Binding.Inputs...)
			tc.mutate(&e, &want)
			if got := ValidateReadiness(e, want, nil); got.Status != ReadinessBlocked {
				t.Fatalf("%s = %+v, want BLOCKED", tc.name, got)
			}
		})
	}
}

func TestReadinessRejectsScopeEvidenceAndTargetFreshness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BuildInput)
		want   ReadinessStatus
	}{
		{"unauthorized", func(in *BuildInput) { in.Scope.Authorized = false }, ReadinessBlocked},
		{"paths omitted", func(in *BuildInput) { in.Scope.Paths = nil }, ReadinessBlocked},
		{"criteria omitted", func(in *BuildInput) { in.Scope.Criteria = nil }, ReadinessBlocked},
		{"validation omitted", func(in *BuildInput) { in.Scope.PermittedValidation = nil }, ReadinessBlocked},
		{"authorization reference omitted", func(in *BuildInput) { in.Scope.AuthorizationReference = "" }, ReadinessBlocked},
		{"target hash", func(in *BuildInput) { in.Scope.Targets[0].SHA256 = digest64('z') }, ReadinessBlocked},
		{"target missing", func(in *BuildInput) { in.Scope.Targets[0].Missing = true }, ReadinessBlocked},
		{"target symlink", func(in *BuildInput) { in.Scope.Targets[0].NoSymlink = false }, ReadinessBlocked},
		{"evidence omitted", func(in *BuildInput) { in.Evidence = nil }, ReadinessInconclusive},
		{"evidence stale", func(in *BuildInput) { in.Evidence[0].Current = false }, ReadinessInconclusive},
		{"self attestation", func(in *BuildInput) { in.Evidence[0].Kind = EvidenceKindSelfAttested }, ReadinessInconclusive},
		{"consequential unknown", func(in *BuildInput) { in.Unknowns = []Unknown{{Question: "q", Requirement: "C", Consequential: true}} }, ReadinessInconclusive},
		{"dependency omitted", func(in *BuildInput) { in.Dependencies = nil }, ReadinessBlocked},
		{"provider dependency drift", func(in *BuildInput) { in.Dependencies[0].Digest = digest64('z') }, ReadinessBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := completeBuildInput()
			tc.mutate(&in)
			_, got := BuildReadiness(in)
			if got.Status != tc.want {
				t.Fatalf("%s = %+v, want %s", tc.name, got, tc.want)
			}
		})
	}
}

func TestReadinessStatusSemanticsAndWriterEcho(t *testing.T) {
	in := completeBuildInput()
	e, _ := BuildReadiness(in)
	if err := ValidateWriterReturn(writerEcho(in.Binding, e), e, in.Binding); err != nil {
		t.Fatalf("valid full echo: %v", err)
	}
	for _, got := range []WriterReturnEcho{{}, {MissionID: "wrong", ReplayNonce: in.Binding.ReplayNonce, EnvelopeDigest: e.EnvelopeDigest}, {MissionID: in.Binding.MissionID, ReplayNonce: "wrong", EnvelopeDigest: e.EnvelopeDigest}, {MissionID: in.Binding.MissionID, ReplayNonce: in.Binding.ReplayNonce, EnvelopeDigest: digest64('z')}} {
		if ValidateWriterReturn(got, e, in.Binding) == nil {
			t.Fatalf("accepted bad echo: %+v", got)
		}
	}
	e.Status, e.EnvelopeDigest = ReadinessReady, EnvelopeDigest(e)
	e.Scope.Paths = []string{"../escape"}
	if got := ValidateReadiness(e, in.Binding, nil); got.Status != ReadinessBlocked {
		t.Fatalf("forged READY = %+v", got)
	}
}

func completeBuildInput() BuildInput {
	paths := []ChangedPath{{Path: "a.go", SHA256: digest64('c'), NoSymlink: true}}
	b := ExpectedBinding{Kind: BindingSDD, ChangeID: "change", MissionID: "mission", ReplayNonce: "nonce", TaskArtifactID: "task", TaskRevisionID: "revision", TaskDigest: digest64('a'), ExpectedStateVersion: 18, Inputs: []AcceptedInput{{ArtifactID: "input", RevisionID: "revision", Digest: digest64('b')}}, CandidateDigest: digest64('d'), BaseIdentity: "base", ChangedPaths: paths, ReviewBinding: ReviewBinding{CandidateDigest: digest64('d'), ChangedPaths: paths, DiffScope: []string{"a.go"}, AcceptanceCriteria: []string{"C1"}}}
	return BuildInput{MissionEvidence: json.RawMessage(`{"mission":"m"}`), Activation: ActivationFacts{WriteIntent: true, SDDAccepted: true, IdentityDigest: true, AuthorizationSecurity: true, ShellProcess: true, ProviderTemplate: true, FrozenCandidate: true}, Binding: b, Scope: Scope{Authorized: true, WriteIntent: true, Paths: []string{"a.go"}, Criteria: []string{"C1"}, AuthorizationReference: "auth", PermittedValidation: []string{"go test"}, Targets: []TargetIdentity{{Path: "a.go", SHA256: digest64('c'), NoSymlink: true}}}, RiskCategories: []RiskCategory{"sdd", "authorization-security", "shell-process", "identity-digest", "provider-template", "frozen"}, Evidence: []EvidenceReceipt{{Kind: EvidenceKindContract, Locator: "task", Digest: digest64('a'), Availability: EvidenceAvailable, Current: true, ObservedResult: "accepted", RiskCategory: "sdd"}, {Kind: EvidenceKindAuthorization, Locator: "authorization", Availability: EvidenceAvailable, Current: true, ObservedResult: "granted", RiskCategory: "authorization-security"}, {Kind: EvidenceKindCommand, Locator: "go test", Availability: EvidenceAvailable, Current: true, ObservedResult: "passed", RiskCategory: "shell-process"}, {Kind: EvidenceKindTarget, Locator: "a.go", Digest: digest64('c'), Availability: EvidenceAvailable, Current: true, ObservedResult: "hashed", RiskCategory: "identity-digest"}, {Kind: EvidenceKindProvider, Locator: "provider", Availability: EvidenceAvailable, Current: true, ObservedResult: "resolved", RiskCategory: "provider-template"}, {Kind: EvidenceKindContract, Locator: "candidate", CandidateDigest: digest64('d'), Availability: EvidenceAvailable, Current: true, ObservedResult: "reviewed", RiskCategory: "frozen"}}, Dependencies: []Dependency{{Kind: "task", Identity: "task", Digest: digest64('a')}, {Kind: "target", Identity: "a.go", Digest: digest64('c'), NoSymlink: true}}}
}

func writerEcho(b ExpectedBinding, e ReadinessEnvelope) WriterReturnEcho {
	return WriterReturnEcho{MissionID: b.MissionID, ReplayNonce: b.ReplayNonce, EnvelopeDigest: e.EnvelopeDigest, CandidateDigest: b.CandidateDigest, BaseIdentity: b.BaseIdentity, ChangedPaths: b.ChangedPaths, ReviewBinding: b.ReviewBinding}
}

func digest64(b byte) string {
	return string([]byte{b}) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func TestReadinessRejectsRehashedForgedScopeAndDependency(t *testing.T) {
	in := completeBuildInput()
	e, _ := BuildReadiness(in)
	e.Scope.Paths[0] = "forged.go"
	e.Evidence[0].Locator = "forged-evidence"
	e.Dependencies[0].Identity = "forged-task"
	e.EnvelopeDigest = EnvelopeDigest(e)
	if got := ValidateReadiness(e, in.Binding, nil); got.Status != ReadinessBlocked {
		t.Fatalf("rehashed forged envelope = %+v, want BLOCKED", got)
	}
}

func TestReadinessRequiresCandidateAndReviewBindings(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(ExpectedBinding{}), reflect.TypeOf(WriterReturnEcho{})} {
		for _, field := range []string{"CandidateDigest", "BaseIdentity", "ChangedPaths", "ReviewBinding"} {
			if _, ok := typ.FieldByName(field); !ok {
				t.Fatalf("%s must bind %s", typ.Name(), field)
			}
		}
	}
}

func TestReadinessAllowsAuthorizedExplicitlyMissingTarget(t *testing.T) {
	in := completeBuildInput()
	in.Scope.Targets[0] = TargetIdentity{Path: "new.go", Missing: true, NoSymlink: true}
	in.Dependencies[1] = Dependency{Kind: "target", Identity: "new.go", NoSymlink: true}
	in.Evidence[3].Locator, in.Evidence[3].Digest = "new.go", ""
	if _, got := BuildReadiness(in); got.Status != ReadinessReady {
		t.Fatalf("authorized missing target = %+v, want READY", got)
	}
	in.Scope.Targets[0].SHA256 = digest64('x')
	if _, got := BuildReadiness(in); got.Status != ReadinessBlocked {
		t.Fatalf("missing target with hash = %+v, want BLOCKED", got)
	}
}

func TestBuildReadinessExemptEmitsNoEnvelope(t *testing.T) {
	e, got := BuildReadiness(BuildInput{Activation: ActivationFacts{DirectRoute: true}})
	if e.SchemaVersion != "" || e.EnvelopeDigest != "" || got.Status != "" {
		t.Fatalf("exempt result = %+v, %+v; want no envelope", e, got)
	}
}

func TestReadinessRejectsDuplicateCollections(t *testing.T) {
	in := completeBuildInput()
	in.Scope.Paths = []string{"a.go", "a.go"}
	in.RiskCategories = []RiskCategory{"sdd", "sdd"}
	if _, got := BuildReadiness(in); got.Status != ReadinessBlocked {
		t.Fatalf("duplicate collections = %+v, want BLOCKED", got)
	}
}

func TestReadinessFullExpectationRejectsEveryRehashedFreshnessDrift(t *testing.T) {
	in := completeBuildInput()
	e, got := BuildReadiness(in)
	if got.Status != ReadinessReady {
		t.Fatalf("valid full fixture = %+v", got)
	}
	want := ReadinessExpectationFromBuildInput(in)
	for _, tc := range []struct {
		name   string
		mutate func(*ReadinessEnvelope)
	}{
		{"scope criteria", func(v *ReadinessEnvelope) { v.Scope.Criteria[0] = "other" }},
		{"evidence result", func(v *ReadinessEnvelope) { v.Evidence[0].ObservedResult = "other" }},
		{"non-consequential unknown", func(v *ReadinessEnvelope) { v.Unknowns = []Unknown{{Question: "q", Requirement: "r"}} }},
		{"risk order", func(v *ReadinessEnvelope) {
			v.RiskCategories[0], v.RiskCategories[1] = v.RiskCategories[1], v.RiskCategories[0]
		}},
		{"dependency order", func(v *ReadinessEnvelope) {
			v.Dependencies[0], v.Dependencies[1] = v.Dependencies[1], v.Dependencies[0]
		}},
		{"candidate binding", func(v *ReadinessEnvelope) { v.Binding.CandidateDigest = digest64('z') }},
		{"review binding", func(v *ReadinessEnvelope) { v.Binding.ReviewBinding.DiffScope[0] = "other.go" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := cloneReadinessEnvelope(e)
			tc.mutate(&v)
			v.EnvelopeDigest = EnvelopeDigest(v)
			if got := ValidateReadiness(v, want, nil); got.Status != ReadinessBlocked {
				t.Fatalf("rehashed %s = %+v, want BLOCKED", tc.name, got)
			}
		})
	}
}

func TestInvalidationReasonsUsesFullReadinessExpectation(t *testing.T) {
	in := completeBuildInput()
	e, _ := BuildReadiness(in)
	e.Scope.Criteria[0] = "drift"
	e.EnvelopeDigest = EnvelopeDigest(e)
	if reasons := InvalidationReasons(e, ReadinessExpectationFromBuildInput(in)); !reflect.DeepEqual(reasons, []ReasonCode{"binding_mismatch"}) {
		t.Fatalf("scope drift reasons = %v, want controlled mismatch", reasons)
	}
}

func TestReadinessEvidenceSemanticsAreRiskLocal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BuildInput)
	}{
		{"missing", func(v *BuildInput) { v.Evidence = v.Evidence[1:] }},
		{"stale", func(v *BuildInput) { v.Evidence[0].Current = false }},
		{"unavailable", func(v *BuildInput) { v.Evidence[0].Availability, v.Evidence[0].Current = EvidenceUnavailable, false }},
		{"contradictory", func(v *BuildInput) { v.Evidence[0].Contradictory = true }},
		{"self-attested", func(v *BuildInput) { v.Evidence[0].Kind = EvidenceKindSelfAttested }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := completeBuildInput()
			tc.mutate(&in)
			if _, got := BuildReadiness(in); got.Status != ReadinessInconclusive {
				t.Fatalf("%s evidence = %+v, want INCONCLUSIVE", tc.name, got)
			}
		})
	}
	in := completeBuildInput()
	in.Evidence[0].Availability, in.Evidence[0].NotApplicableRationale = EvidenceNotApplicable, "bounded"
	if _, got := BuildReadiness(in); got.Status != ReadinessReady {
		t.Fatalf("bounded not-applicable evidence = %+v, want READY", got)
	}
	in.Evidence[0].NotApplicableRationale = ""
	if _, got := BuildReadiness(in); got.Status != ReadinessInconclusive {
		t.Fatalf("unexplained not-applicable evidence = %+v, want INCONCLUSIVE", got)
	}
}

func TestReadinessRejectsIsolatedDuplicateAndOversizeValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BuildInput)
	}{
		{"risk", func(v *BuildInput) { v.RiskCategories = append(v.RiskCategories, v.RiskCategories[0]) }},
		{"target", func(v *BuildInput) { v.Scope.Targets = append(v.Scope.Targets, v.Scope.Targets[0]) }},
		{"dependency", func(v *BuildInput) { v.Dependencies = append(v.Dependencies, v.Dependencies[0]) }},
		{"evidence", func(v *BuildInput) { v.Evidence = append(v.Evidence, v.Evidence[0]) }},
		{"candidate path", func(v *BuildInput) {
			v.Binding.ChangedPaths = append(v.Binding.ChangedPaths, v.Binding.ChangedPaths[0])
			v.Binding.ReviewBinding.ChangedPaths = append(v.Binding.ReviewBinding.ChangedPaths, v.Binding.ReviewBinding.ChangedPaths[0])
		}},
		{"review path", func(v *BuildInput) {
			v.Binding.ReviewBinding.ChangedPaths = append(v.Binding.ReviewBinding.ChangedPaths, v.Binding.ReviewBinding.ChangedPaths[0])
		}},
		{"input", func(v *BuildInput) { v.Binding.Inputs = append(v.Binding.Inputs, v.Binding.Inputs[0]) }},
		{"oversize collection", func(v *BuildInput) {
			for i := 0; i <= maxReadinessItems; i++ {
				v.Unknowns = append(v.Unknowns, Unknown{Question: "q" + string(rune(i)), Requirement: "r"})
			}
		}},
		{"oversize string", func(v *BuildInput) { v.Scope.Criteria[0] = strings.Repeat("x", maxReadinessString+1) }},
		{"oversize raw evidence", func(v *BuildInput) {
			v.MissionEvidence = json.RawMessage(`"` + strings.Repeat("x", maxCanonicalBytes+1) + `"`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := completeBuildInput()
			tc.mutate(&in)
			if _, got := BuildReadiness(in); got.Status != ReadinessBlocked {
				t.Fatalf("%s = %+v, want deterministic BLOCKED", tc.name, got)
			}
		})
	}
}

func cloneReadinessEnvelope(e ReadinessEnvelope) ReadinessEnvelope {
	v := e
	v.Binding = cloneBinding(e.Binding)
	v.Scope = cloneScope(e.Scope)
	v.Evidence = append([]EvidenceReceipt(nil), e.Evidence...)
	v.Unknowns = append([]Unknown(nil), e.Unknowns...)
	v.Dependencies = append([]Dependency(nil), e.Dependencies...)
	v.RiskCategories = append([]RiskCategory(nil), e.RiskCategories...)
	return v
}

func TestReadinessOversizedEvidenceIsMalformed(t *testing.T) {
	in := completeBuildInput()
	evidence := append([]EvidenceReceipt(nil), in.Evidence...)
	for i := len(evidence); i <= maxReadinessItems; i++ {
		evidence = append(evidence, EvidenceReceipt{
			Kind:           EvidenceKindContract,
			Locator:        "supplemental-evidence-" + string(rune(i)),
			Availability:   EvidenceAvailable,
			Current:        true,
			ObservedResult: "accepted",
			RiskCategory:   "sdd",
		})
	}
	in.Evidence = evidence
	if _, got := BuildReadiness(in); got.Status != ReadinessBlocked {
		t.Fatalf("oversized structurally valid evidence = %+v, want BLOCKED", got)
	}
}

func TestReadinessDuplicateNonConsequentialUnknownIsBlocked(t *testing.T) {
	unknown := Unknown{Question: "clarify", Requirement: "C1"}

	single := completeBuildInput()
	single.Unknowns = []Unknown{unknown}
	if _, got := BuildReadiness(single); got.Status != ReadinessReady {
		t.Fatalf("single non-consequential unknown = %+v, want READY", got)
	}

	duplicate := completeBuildInput()
	duplicate.Unknowns = []Unknown{unknown, unknown}
	if _, got := BuildReadiness(duplicate); got.Status != ReadinessBlocked {
		t.Fatalf("duplicate non-consequential unknown = %+v, want BLOCKED", got)
	}
}

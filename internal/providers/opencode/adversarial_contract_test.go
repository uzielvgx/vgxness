package opencode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/sdd"
)

// These are deterministic contract-shape checks over generated artifacts. They
// do not run OpenCode or claim runtime/atomic host enforcement.
type contractEvidence struct{ ID, CandidateDigest string }
type contractFinding struct {
	ID        string
	ProofRefs []string
}
type contractBinding struct {
	CandidateDigest, DiffScope string
	ChangedPaths, Acceptance   []string
}
type contractReview struct {
	CandidateDigest, Verdict, Mode, CorrectionDelta, FrozenLedger string
	Binding                                                       contractBinding
	Evidence                                                      []contractEvidence
	Findings                                                      []contractFinding
	RefuterIDs, SuppliedSevereIDs                                 []string
}

func sha256Text(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validContractReview(value contractReview) bool {
	if !sha256Text(value.CandidateDigest) || value.Binding.CandidateDigest != value.CandidateDigest || value.Binding.DiffScope == "" || len(value.Binding.ChangedPaths) == 0 || len(value.Binding.Acceptance) == 0 || (value.Mode != "initial" && value.Mode != "scoped-validation") || (value.Mode == "initial" && (value.CorrectionDelta != "" || value.FrozenLedger != "")) || (value.Mode == "scoped-validation" && (value.CorrectionDelta == "" || value.FrozenLedger != value.CandidateDigest)) {
		return false
	}
	if (len(value.Findings) == 0 && value.Verdict != "clean") || (len(value.Findings) > 0 && value.Verdict != "findings") {
		return false
	}
	evidence := map[string]int{}
	for _, receipt := range value.Evidence {
		if receipt.ID == "" || receipt.CandidateDigest != value.CandidateDigest {
			return false
		}
		evidence[receipt.ID]++
		if evidence[receipt.ID] != 1 {
			return false
		}
	}
	if len(evidence) == 0 {
		return false
	}
	findingIDs := map[string]bool{}
	for _, finding := range value.Findings {
		if finding.ID == "" || len(finding.ProofRefs) == 0 || findingIDs[finding.ID] {
			return false
		}
		findingIDs[finding.ID] = true
		for _, reference := range finding.ProofRefs {
			if evidence[reference] != 1 {
				return false
			}
		}
	}
	supplied, refuted := map[string]bool{}, map[string]bool{}
	for _, id := range value.SuppliedSevereIDs {
		if id == "" || supplied[id] {
			return false
		}
		supplied[id] = true
	}
	for _, id := range value.RefuterIDs {
		if id == "" || refuted[id] || !supplied[id] {
			return false
		}
		refuted[id] = true
	}
	return reflect.DeepEqual(supplied, refuted)
}

type contractSDDHandoff struct {
	ExpectedStateVersion, ObservedStateVersion int64
	MissionNonce, PreviouslySeenNonce          string
	TaskDigest, ExpectedTaskDigest             string
	InputDigest, ExpectedInputDigest           string
	RelativePath, ExpectedPath                 string
	NoSymlink                                  bool
	ExpectedReadback, ObservedReadback         string
}

func validContractSDDHandoff(value contractSDDHandoff) bool {
	return value.ExpectedStateVersion > 0 && value.ExpectedStateVersion == value.ObservedStateVersion && value.MissionNonce != "" && value.MissionNonce != value.PreviouslySeenNonce && sha256Text(value.TaskDigest) && value.TaskDigest == value.ExpectedTaskDigest && sha256Text(value.InputDigest) && value.InputDigest == value.ExpectedInputDigest && value.RelativePath == value.ExpectedPath && safeContractPath(value.RelativePath) && value.NoSymlink && sha256Text(value.ExpectedReadback) && value.ExpectedReadback == value.ObservedReadback
}

func safeContractPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, `\\`+"\x00") && !strings.HasPrefix(value, "/") && path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "../")
}

func TestGeneratedReviewAndSDDContractsHaveRoleSpecificClauses(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, clauses := range map[string][]string{
		reviewRiskName:        {"You are the Risk lens", "Use stable finding IDs prefixed RISK-", "Evidence Receipt needs a stable evidenceId"},
		reviewReadabilityName: {"You are the Readability lens", "Use stable finding IDs prefixed READ-", "Evidence Receipt needs a stable evidenceId"},
		reviewReliabilityName: {"You are the Reliability lens", "Use stable finding IDs prefixed REL-", "Evidence Receipt needs a stable evidenceId"},
		reviewResilienceName:  {"You are the Resilience lens", "Use stable finding IDs prefixed RES-", "Evidence Receipt needs a stable evidenceId"},
		reviewRefuterName:     {"You are the severe-finding refuter", "only supplied severe inferential finding IDs", "results only for supplied severe inferential IDs"},
		sddResearchName:       {"You are the read-only SDD research agent", "Return bounded evidence and candidate artifact content"},
		sddProposalName:       {"You are the read-only SDD proposal agent", "Return bounded evidence and candidate artifact content"},
		sddSpecName:           {"You are the read-only SDD spec agent", "Return bounded evidence and candidate artifact content"},
		sddDesignName:         {"You are the read-only SDD design agent", "Return bounded evidence and candidate artifact content"},
		sddTasksName:          {"You are the read-only SDD tasks agent", "Return bounded evidence and candidate artifact content"},
		sddApplyName:          {"read-only implementation and patch composer", "expectedStateVersion", "no-symlink constraints", "SHA-256 readback"},
	} {
		content := string(bundle.agents[name])
		for _, clause := range clauses {
			if !strings.Contains(content, clause) {
				t.Errorf("%s missing role contract clause %q", name, clause)
			}
		}
	}
}

func TestGeneratedPromptExamplesHaveStructuralContracts(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{reviewRiskName, reviewReadabilityName, reviewReliabilityName, reviewResilienceName} {
		example := promptJSONExample(t, bundle.agents[name], `{"schemaVersion"`)
		requireJSONFields(t, example, "schemaVersion:number", "mode:string", "reviewBinding:object", "candidate:object", "summary:string", "evidence:array", "findings:array", "verdict:string")
		requireJSONFields(t, objectField(t, example, "reviewBinding"), "candidateDigest:string", "changedPaths:array", "diffScope:string", "acceptanceCriteria:array")
		requireJSONFields(t, objectField(t, example, "candidate"), "digest:string", "changedPaths:array")
		requireJSONArrayObjectFields(t, example, "evidence", "evidenceId:string", "candidateDigest:string", "kind:string", "locator:string")
		requireJSONArrayObjectFields(t, example, "findings", "id:string", "proofRefs:array", "severity:string")
	}
	refuter := promptJSONExample(t, bundle.agents[reviewRefuterName], `{"schemaVersion"`)
	requireJSONFields(t, refuter, "role:string", "reviewBinding:object", "candidate:object", "evidence:array", "results:array")
	if refuter["role"] != "refuter" {
		t.Fatalf("refuter example role=%v", refuter["role"])
	}
	requireJSONArrayObjectFields(t, refuter, "results", "findingId:string", "outcome:string", "proofRefs:array")
	apply := promptJSONExample(t, bundle.agents[sddApplyName], `{"status"`)
	requireJSONFields(t, apply, "status:string", "missionIdentity:string", "replayNonce:string", "taskRevision:object", "acceptedInputs:array", "expectedStateVersion:number", "proposedChanges:array", "validationPlan:array", "tddEvidence:object")
	requireJSONFields(t, objectField(t, apply, "taskRevision"), "id:string", "digest:string")
	requireJSONArrayObjectFields(t, apply, "acceptedInputs", "artifactId:string", "revisionId:string", "digest:string")
	requireJSONArrayObjectFields(t, apply, "proposedChanges", "path:string", "expectedSHA256:string", "noSymlink:bool", "patch:string")
	for _, name := range []string{sddResearchName, sddProposalName, sddSpecName, sddDesignName, sddTasksName} {
		example := promptJSONExample(t, bundle.agents[name], `{"status"`)
		requireJSONFields(t, example, "status:string", "candidateContent:string", "evidence:array", "openQuestions:array", "blockers:array")
	}
}

func TestGeneratedManagerGeneralVerifierContractClauses(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, clauses := range map[string][]string{
		managerAgentName:  {"sole engineering, orchestration, SDD lifecycle, Git, and GitHub authority", "managed general as the delegated implementation worker", "one exact Review Binding: candidateDigest, exact changedPaths, diffScope, and acceptanceCriteria"},
		generalAgentName:  {"immediately before each write recheck", "exact readback SHA-256", "do not eliminate TOCTOU risk", "Do not accept revisions, transition phases, or record projections"},
		verifierAgentName: {"verification remains non-mutating", "Never edit, fix, format, delegate", "A validation command that unexpectedly changes the candidate makes the result INCONCLUSIVE"},
	} {
		for _, clause := range clauses {
			if !strings.Contains(string(bundle.agents[name]), clause) {
				t.Errorf("%s missing contract clause %q", name, clause)
			}
		}
	}
}

func TestGeneratedRepositoryChildrenValidateAndEchoContextCapsule(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{exploreAgentName, generalAgentName, verifierAgentName, reviewRiskName, reviewReadabilityName, reviewReliabilityName, reviewResilienceName, reviewRefuterName} {
		content := string(bundle.agents[name])
		for _, clause := range []string{"Context Capsule v1", "goal, criteria, nonGoals, decisions, authorization, constraints, evidenceRefs, lineage, and contextDigest", "Manager-attested digest", "capsule contextDigest and mission's external contextDigest", "parentContextDigest", "Echo the accepted contextDigest unchanged", "digest-bound synthesis", "Do not independently recompute", "not a security boundary"} {
			if !strings.Contains(content, clause) {
				t.Errorf("%s missing child context clause %q", name, clause)
			}
		}
		for _, forbidden := range []string{"Recompute lowercase SHA-256", "object keys sorted lexicographically", "no insignificant whitespace", "array order preserved"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s exceeds child capability with %q", name, forbidden)
			}
		}
	}
	manager := string(bundle.agents[managerAgentName])
	for _, clause := range []string{"sole digest-computation owner", "non-SDD repository delegation", "object keys sorted lexicographically", "no insignificant whitespace", "array order preserved", "contextDigest field omitted", "compute lowercase SHA-256 with an available read-only local hashing capability before task launch", "compare the computed digest with both", "altered capsule content even when", "stale repeated digest", "Count this computation within the selected route budget", "If the capability is unavailable, do not delegate", "SDD missions retain their stronger accepted artifact, revision, digest, and stateVersion bindings without duplicating this capsule"} {
		if !strings.Contains(manager, clause) {
			t.Errorf("manager missing deterministic capsule clause %q", clause)
		}
	}
}

func TestActiveProfilesRenderOneCanonicalChildContextContract(t *testing.T) {
	for name, base := range map[string]string{
		generalAgentName:  canonicalGeneralPrompt,
		verifierAgentName: canonicalVerifierPrompt,
		exploreAgentName:  explorePrompt,
	} {
		if got := strings.Count(base, activeChildContextPlaceholder); got != 1 {
			t.Errorf("%s placeholder count=%d, want 1", name, got)
		}
		if strings.Contains(base, "Require a Context Capsule v1") {
			t.Errorf("%s duplicates the canonical child context contract", name)
		}
		active, err := activeProfilePrompt(base)
		if err != nil {
			t.Fatalf("%s active profile: %v", name, err)
		}
		if got := strings.Count(active, "Require a Context Capsule v1"); got != 1 {
			t.Errorf("%s rendered context contract count=%d, want 1", name, got)
		}
	}
}

func TestActiveProfilesUseSmallerContextContractsWithoutLosingInvariants(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name := range map[string]string{generalAgentName: canonicalGeneralPrompt, verifierAgentName: canonicalVerifierPrompt, exploreAgentName: explorePrompt} {
		legacy := strings.Replace(string(bundle.agents[name]), activeChildContextContract, nativeChildContextContract, 1)
		if got := len(bundle.agents[name]); got >= len(legacy) {
			t.Errorf("%s generated bytes=%d, want less than legacy %d", name, got, len(legacy))
		}
		for _, required := range []string{"Context Capsule v1", "Manager-attested digest", "parentContextDigest", "Echo the accepted contextDigest unchanged", "Do not independently recompute", "not a security boundary"} {
			if !strings.Contains(string(bundle.agents[name]), required) {
				t.Errorf("%s missing invariant %q", name, required)
			}
		}
	}
}

func TestAlteredContextCapsuleWithRepeatedSuppliedDigestIsRejectedByContract(t *testing.T) {
	canonicalDigest := func(capsule map[string]any) string {
		copy := make(map[string]any, len(capsule))
		for key, value := range capsule {
			if key != "contextDigest" {
				copy[key] = value
			}
		}
		canonical, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(canonical)
		return hex.EncodeToString(digest[:])
	}
	original := map[string]any{"goal": "bounded repair", "criteria": []any{"A", "B"}, "lineage": map[string]any{"origin": "request", "hops": []any{"manager", "general"}}}
	supplied := canonicalDigest(original)
	original["contextDigest"] = supplied
	altered := map[string]any{"lineage": original["lineage"], "criteria": original["criteria"], "goal": "broadened repair", "contextDigest": supplied}
	if recomputed := canonicalDigest(altered); recomputed == supplied {
		t.Fatal("altered capsule retained the original canonical digest")
	}
	if capsuleDigest, externalDigest := altered["contextDigest"], supplied; capsuleDigest != externalDigest {
		t.Fatal("adversarial setup must repeat one stale supplied digest")
	}
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bundle.agents[managerAgentName]), "Reject altered capsule content even when the capsule and mission repeat the same supplied digest") {
		t.Error("manager does not reject the repeated stale-digest adversary")
	}
}

func TestSDDProfilesRemainContextCapsuleFreeWithExactPredecessors(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, role := range map[string]sdd.Role{sddResearchName: sdd.RoleResearch, sddProposalName: sdd.RoleProposal, sddSpecName: sdd.RoleSpec, sddDesignName: sdd.RoleDesign, sddTasksName: sdd.RoleTasks, sddApplyName: sdd.RoleApply} {
		current := bundle.agents[name]
		if bytes.Contains(current, []byte("Context Capsule v1")) {
			t.Errorf("%s current identity gained Context Capsule bytes", name)
		}
		predecessor := previousSDDAgentPredecessor(role, current)
		if len(predecessor) == 0 || bytes.Contains(predecessor, []byte("Context Capsule v1")) {
			t.Errorf("%s exact predecessor identity changed", name)
		}
	}
}

func promptJSONExample(t *testing.T, content []byte, prefix string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("decode prompt example: %v", err)
		}
		return value
	}
	t.Fatalf("missing JSON example with prefix %q", prefix)
	return nil
}

func objectField(t *testing.T, value map[string]any, field string) map[string]any {
	t.Helper()
	result, ok := value[field].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object", field)
	}
	return result
}

func requireJSONFields(t *testing.T, value map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		name, kind, _ := strings.Cut(field, ":")
		value, ok := value[name]
		if !ok || !jsonKind(value, kind) {
			t.Errorf("field %s is not %s: %#v", name, kind, value)
		}
	}
}

func requireJSONArrayObjectFields(t *testing.T, value map[string]any, field string, fields ...string) {
	t.Helper()
	values, ok := value[field].([]any)
	if !ok || len(values) == 0 {
		t.Fatalf("%s is not a non-empty array", field)
	}
	object, ok := values[0].(map[string]any)
	if !ok {
		t.Fatalf("%s element is not an object", field)
	}
	requireJSONFields(t, object, fields...)
}

func jsonKind(value any, kind string) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "bool":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	}
	return false
}

func TestAdversarialReviewContractValidator(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := contractReview{CandidateDigest: digest, Verdict: "findings", Mode: "initial", Binding: contractBinding{CandidateDigest: digest, DiffScope: "exact", ChangedPaths: []string{"a.go"}, Acceptance: []string{"works"}}, Evidence: []contractEvidence{{ID: "proof", CandidateDigest: digest}}, Findings: []contractFinding{{ID: "finding", ProofRefs: []string{"proof"}}}, SuppliedSevereIDs: []string{"finding"}, RefuterIDs: []string{"finding"}}
	if !validContractReview(valid) {
		t.Fatal("valid review contract rejected")
	}
	for name, mutate := range map[string]func(*contractReview){
		"duplicate evidence ID": func(v *contractReview) { v.Evidence = append(v.Evidence, v.Evidence[0]) }, "empty evidence ID": func(v *contractReview) { v.Evidence[0].ID = "" }, "missing evidence": func(v *contractReview) { v.Evidence = nil }, "dangling proofRef": func(v *contractReview) { v.Findings[0].ProofRefs = []string{"missing"} }, "candidateDigest mismatch": func(v *contractReview) { v.Evidence[0].CandidateDigest = strings.Repeat("b", 64) }, "invalid candidateDigest": func(v *contractReview) { v.CandidateDigest = "bad" }, "Review Binding mismatch": func(v *contractReview) { v.Binding.CandidateDigest = strings.Repeat("b", 64) }, "stale Review Binding": func(v *contractReview) { v.Binding.DiffScope = "" }, "initial correctionDelta": func(v *contractReview) { v.CorrectionDelta = "delta" }, "missing frozenLedger": func(v *contractReview) { v.Mode, v.CorrectionDelta = "scoped-validation", "delta" }, "ledger candidate mismatch": func(v *contractReview) {
			v.Mode, v.CorrectionDelta, v.FrozenLedger = "scoped-validation", "delta", strings.Repeat("b", 64)
		}, "unknown refuter ID": func(v *contractReview) { v.RefuterIDs = []string{"unknown"} }, "incomplete refuter IDs": func(v *contractReview) { v.RefuterIDs = nil },
		"duplicate finding ID": func(v *contractReview) { v.Findings = append(v.Findings, v.Findings[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Evidence = append([]contractEvidence(nil), valid.Evidence...)
			candidate.Findings = append([]contractFinding(nil), valid.Findings...)
			mutate(&candidate)
			if validContractReview(candidate) {
				t.Fatal("invalid review contract accepted")
			}
		})
	}
	clean := valid
	clean.Findings, clean.SuppliedSevereIDs, clean.RefuterIDs, clean.Verdict = nil, nil, nil, "clean"
	if !validContractReview(clean) {
		t.Fatal("clean zero-finding verdict rejected")
	}
	clean.Evidence = append(clean.Evidence, clean.Evidence[0])
	if validContractReview(clean) {
		t.Fatal("clean verdict accepted duplicate evidence")
	}
}

func TestAdversarialSDDHandoffContractValidator(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := contractSDDHandoff{ExpectedStateVersion: 2, ObservedStateVersion: 2, MissionNonce: "new", PreviouslySeenNonce: "old", TaskDigest: digest, ExpectedTaskDigest: digest, InputDigest: digest, ExpectedInputDigest: digest, RelativePath: "openspec/changes/change.md", ExpectedPath: "openspec/changes/change.md", NoSymlink: true, ExpectedReadback: digest, ObservedReadback: digest}
	if !validContractSDDHandoff(valid) {
		t.Fatal("valid SDD handoff contract rejected")
	}
	for name, mutate := range map[string]func(*contractSDDHandoff){
		"stale stateVersion": func(v *contractSDDHandoff) { v.ObservedStateVersion++ }, "replay nonce": func(v *contractSDDHandoff) { v.PreviouslySeenNonce = v.MissionNonce }, "task digest mismatch": func(v *contractSDDHandoff) { v.TaskDigest = strings.Repeat("b", 64) }, "input digest mismatch": func(v *contractSDDHandoff) { v.InputDigest = strings.Repeat("b", 64) }, "absolute path": func(v *contractSDDHandoff) { v.RelativePath = "/tmp/x" }, "parent escape": func(v *contractSDDHandoff) { v.RelativePath = "../x" }, "cleaning required": func(v *contractSDDHandoff) { v.RelativePath = "a/../x" }, "backslash": func(v *contractSDDHandoff) { v.RelativePath = `a\x` }, "missing readback": func(v *contractSDDHandoff) { v.ObservedReadback = "" }, "mismatched readback": func(v *contractSDDHandoff) { v.ObservedReadback = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if validContractSDDHandoff(candidate) {
				t.Fatal("invalid SDD handoff contract accepted")
			}
		})
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if !hasContractSymlink(root, "linked/file.md") {
		t.Fatal("intermediate symlink was not detected")
	}
	// This inspection is evidence-only; it cannot provide atomic host enforcement.
}

func hasContractSymlink(root, relative string) bool {
	if !safeContractPath(relative) {
		return true
	}
	current := root
	for _, part := range strings.Split(relative, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		if err != nil && !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func TestGeneratedPermissionMapsAndHoldoutMetadataLeakage(t *testing.T) {
	bundle, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	general := map[string]string{"*": "allow", "vgxness_memory_save": "deny", "vgxness_memory_forget": "deny", "vgxness_sdd_create": "deny", "vgxness_sdd_set_interaction_mode": "deny", "vgxness_sdd_save_revision": "deny", "vgxness_sdd_accept_revision": "deny", "vgxness_sdd_transition": "deny", "vgxness_sdd_record_projection": "deny"}
	reviewer := map[string]string{"*": "deny", "read": "allow", "grep": "allow", "glob": "allow", "list": "allow", "skill": "allow", "codegraph_explore": "allow", "vgxness_memory_search": "allow", "vgxness_memory_get": "allow", "task": "deny"}
	for name, want := range map[string]map[string]string{managerAgentName: {"*": "allow"}, generalAgentName: general, verifierAgentName: general, reviewRiskName: reviewer, reviewReadabilityName: reviewer, reviewReliabilityName: reviewer, reviewResilienceName: reviewer, reviewRefuterName: reviewer} {
		got, err := parseContractPermissions(bundle.agents[name])
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s permissions=%v err=%v", name, got, err)
		}
	}
	data, err := os.ReadFile("testdata/manager-context-v43-baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(data, &metadata) != nil || metadata["partitions"] == nil || metadata["aggregate"] == nil {
		t.Fatal("baseline metadata malformed")
	}
	var baseline struct {
		SchemaVersion int `json:"schema_version"`
		Target        struct {
			ManagerVersion int    `json:"manager_version"`
			Commit         string `json:"commit"`
			OpenCode       string `json:"opencode"`
		} `json:"target"`
		Partitions struct {
			Development struct {
				Count  int    `json:"count"`
				Digest string `json:"digest"`
			} `json:"development"`
			ProtectedHoldout struct {
				Count  int    `json:"count"`
				Digest string `json:"digest"`
			} `json:"protected_holdout"`
		} `json:"partitions"`
	}
	if err := json.Unmarshal(data, &baseline); err != nil || baseline.SchemaVersion != contextEvalSchemaVersion || baseline.Target.ManagerVersion < 1 || baseline.Target.Commit == "" || baseline.Target.OpenCode == "" || baseline.Partitions.Development.Count < 0 || baseline.Partitions.ProtectedHoldout.Count < 0 || !sha256Text(baseline.Partitions.Development.Digest) || !sha256Text(baseline.Partitions.ProtectedHoldout.Digest) {
		t.Fatalf("invalid available holdout metadata: %+v err=%v", baseline, err)
	}
	for _, leaked := range []string{"prompt", "answer", "label", "outcome", "raw_trace", "protected_holdout_cases"} {
		if strings.Contains(string(data), leaked) {
			t.Errorf("holdout metadata leaked %q", leaked)
		}
	}
	// This is metadata-leakage validation only; it cannot prove external holdout
	// custody, freeze timing, or dataset disjointness.
}

func parseContractPermissions(content []byte) (map[string]string, error) {
	parts := strings.SplitN(string(content), "---", 3)
	if len(parts) != 3 {
		return nil, os.ErrInvalid
	}
	result := map[string]string{}
	active := false
	for _, line := range strings.Split(parts[1], "\n") {
		if line == "permission:" {
			active = true
			continue
		}
		if active && !strings.HasPrefix(line, "  ") {
			break
		}
		if !active {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		key = strings.Trim(key, `"`)
		if !ok || key == "" || (value != "allow" && value != "deny") || result[key] != "" {
			return nil, os.ErrInvalid
		}
		result[key] = value
	}
	return result, nil
}

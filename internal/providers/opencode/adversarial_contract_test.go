package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
		if !ok || key == "" || (value != "allow" && value != "ask" && value != "deny") || result[key] != "" {
			return nil, os.ErrInvalid
		}
		result[key] = value
	}
	return result, nil
}

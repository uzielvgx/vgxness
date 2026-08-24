package e2e_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/orchestration"
	codex "github.com/vgxness/vgxness/internal/providers/codex"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestCAREParityCurrentProviderInventories(t *testing.T) {
	want := map[sdd.Role]string{
		sdd.RoleCAREReviewer:   "care-reviewer",
		sdd.RoleCARESpecialist: "care-specialist",
		sdd.RoleCAREChallenger: "care-challenger",
	}
	openCode := map[sdd.Role]string{}
	for _, identity := range opencode.ModelAgentInventoryV3() {
		if identity.Class == sdd.ManagedAgentClassReview {
			openCode[identity.Role] = identity.ArtifactKey
		}
	}
	if len(openCode) != len(want) {
		t.Fatalf("OpenCode current review inventory = %v, want exactly %d CARE roles", openCode, len(want))
	}
	for role, name := range want {
		if !strings.Contains(openCode[role], name) {
			t.Errorf("OpenCode current inventory lacks %s at a CARE identity: %v", role, openCode)
		}
	}
	for _, legacy := range []string{"risk", "readability", "reliability", "resilience", "refuter"} {
		for _, path := range openCode {
			if strings.Contains(path, legacy) {
				t.Errorf("OpenCode current inventory retains fixed-lens alias %q in %q", legacy, path)
			}
		}
	}

	pkg, err := codex.Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	codexProfiles := map[string]string{}
	for _, artifact := range pkg.Artifacts {
		codexProfiles[artifact.Path] = string(artifact.Bytes)
	}
	if len(pkg.Artifacts) != 13 {
		t.Fatalf("Codex current artifact count = %d, want 13", len(pkg.Artifacts))
	}
	for _, name := range want {
		path := "agents/" + name + ".toml"
		if content, ok := codexProfiles[path]; !ok || !strings.Contains(content, `sandbox_mode = "read-only"`) {
			t.Errorf("Codex current profile %q is absent or not read-only", path)
		}
	}
	for _, legacy := range []string{"risk", "readability", "reliability", "resilience", "refuter"} {
		if _, ok := codexProfiles["agents/"+legacy+".toml"]; ok {
			t.Errorf("Codex current inventory retains fixed-lens alias %q", legacy)
		}
	}
	if got, want := opencode.OrchestrationContractIdentity(), codex.OrchestrationContractIdentity(); got != want {
		t.Fatalf("provider orchestration identities differ: OpenCode=%q Codex=%q", got, want)
	}
}

func TestCAREParityPublicPolicyAndContractSurface(t *testing.T) {
	mission := reflect.TypeFor[orchestration.CAREMission]()
	for _, field := range []string{"SchemaVersion", "Role", "MissionID", "ReplayNonce", "ReviewBinding", "CandidateIdentity", "CatalogRef", "ChangeProfileDigest", "AssurancePlanDigest", "Assignment", "Skills", "EvidenceScope", "CorrectionDelta"} {
		if _, ok := mission.FieldByName(field); !ok {
			t.Errorf("CARE mission lacks %s", field)
		}
	}
	for _, kind := range []orchestration.ChallengeTargetKind{orchestration.ChallengeClaim, orchestration.ChallengeFinding, orchestration.ChallengeEvidence, orchestration.ChallengeScope} {
		if kind == "" {
			t.Error("CARE challenge target kind is empty")
		}
	}
	for _, outcome := range []orchestration.Outcome{orchestration.OutcomePass, orchestration.OutcomeFail, orchestration.OutcomeInconclusive} {
		if outcome == "" {
			t.Error("CARE outcome is empty")
		}
	}
	for _, test := range []struct {
		level    orchestration.ActivationLevel
		roles    int
		attempts int
	}{
		{orchestration.ActivationStandard, 1, 3},
		{orchestration.ActivationElevated, 2, 4},
		{orchestration.ActivationCritical, 3, 5},
	} {
		allocation, err := orchestration.AllocationFor(test.level, true)
		if err != nil || len(allocation.Roles) != test.roles || allocation.RequiredAttempts != test.attempts || !allocation.IncludeVerifier {
			t.Errorf("allocation for %s = %+v, %v", test.level, allocation, err)
		}
	}
	if _, err := orchestration.AllocationFor("invalid", false); !errors.Is(err, orchestration.ErrCAREBlocked) {
		t.Fatalf("invalid allocation error = %v, want ErrCAREBlocked", err)
	}
}

func TestCAREParityCatalogDigestAndMalformedInputFailClosed(t *testing.T) {
	catalog, err := orchestration.GovernedRiskCatalog()
	if err != nil || catalog.CatalogID != orchestration.GovernedRiskCatalogID || len(catalog.Risks) == 0 {
		t.Fatalf("governed catalog = %+v, %v", catalog, err)
	}
	digest, err := orchestration.CAREDigest(catalog)
	if err != nil || len(digest) != 64 {
		t.Fatalf("catalog digest = %q, %v", digest, err)
	}
	canonical, err := orchestration.CanonicalCARE(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := orchestration.RequireCanonicalCARE(canonical, catalog, len(canonical)); err != nil {
		t.Fatalf("canonical catalog rejected: %v", err)
	}
	if err := orchestration.RequireCanonicalCARE(append([]byte(" "), canonical...), catalog, len(canonical)+1); !errors.Is(err, orchestration.ErrCAREBlocked) {
		t.Fatalf("non-canonical catalog error = %v, want ErrCAREBlocked", err)
	}
	if err := orchestration.RequireCanonicalCARE(nil, catalog, len(canonical)); !errors.Is(err, orchestration.ErrCAREBlocked) {
		t.Fatalf("empty CARE input error = %v, want ErrCAREBlocked", err)
	}
}

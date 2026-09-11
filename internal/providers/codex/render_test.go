package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

func TestPluginManifestNormalizesAndBindsSemanticVersion(t *testing.T) {
	pkg, err := Render("v1.2.3-alpha.1")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(artifact(t, pkg, "plugins/vgxness/.codex-plugin/plugin.json").Bytes, &manifest); err != nil || manifest.Version != "1.2.3-alpha.1" {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	for _, version := range []string{"v1.2.3\"x", "v1.2.3\n", "v01.2.3"} {
		if _, err := Render(version); err == nil {
			t.Fatalf("Render(%q) accepted invalid version", version)
		}
	}
	tampered := clonePackage(pkg)
	tampered.version = "bad"
	if tampered.Validate() == nil {
		t.Fatal("Validate accepted invalid package version")
	}
}

func TestCAREDelegationRendersOnlyCurrentProfiles(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, item := range pkg.Artifacts {
		paths[item.Path] = true
	}
	for _, path := range []string{"agents/care-reviewer.toml", "agents/care-specialist.toml", "agents/care-challenger.toml"} {
		if !paths[path] {
			t.Errorf("missing CARE profile %s", path)
		}
	}
	if len(pkg.Artifacts) != 10 {
		t.Errorf("Codex package artifact count = %d, want 10 including lifecycle artifacts and receipt", len(pkg.Artifacts))
	}
	for _, legacy := range []string{"risk", "readability", "reliability", "resilience", "refuter"} {
		if paths["agents/"+legacy+".toml"] {
			t.Errorf("legacy profile %s is current", legacy)
		}
	}
}

func TestCurrentPackageValidationRejectsMissingOrRetiredProfiles(t *testing.T) {
	pkg, err := Render("v1.2.3")
	require(t, err == nil)
	missing := clonePackage(pkg)
	missing.Artifacts = missing.Artifacts[:len(missing.Artifacts)-1]
	missing.SHA256 = aggregateSHA256(missing.Artifacts)
	require(t, missing.Validate() != nil)
	retired := clonePackage(pkg)
	retired.profiles[0] = profile{path: "agents/risk.toml", name: "risk", instructions: "retired test role"}
	retired.Artifacts[1] = Artifact{Path: retired.profiles[0].path, Bytes: []byte(renderProfile(retired.profiles[0]))}
	retired.SHA256 = aggregateSHA256(retired.Artifacts)
	require(t, retired.Validate() != nil)
}

func TestRenderPlanUsesSharedModelMatrix(t *testing.T) {
	roles := map[string]modelplan.Role{
		"agents/explore.toml":         modelplan.RoleResearch,
		"agents/general.toml":         modelplan.RoleImplementation,
		"agents/verifier.toml":        modelplan.RoleVerification,
		"agents/care-reviewer.toml":   modelplan.RoleCAREReviewer,
		"agents/care-specialist.toml": modelplan.RoleCARESpecialist,
		"agents/care-challenger.toml": modelplan.RoleCAREChallenger,
	}
	for _, plan := range []modelplan.Plan{modelplan.PlanLow, modelplan.PlanMedium, modelplan.PlanHigh, modelplan.PlanUltra} {
		pkg, err := RenderPlan("v1.2.3", plan)
		if err != nil {
			t.Fatalf("RenderPlan(%s): %v", plan, err)
		}
		config := modelplan.DefaultModelPlanConfig()
		config.ActivePlan = plan
		resolved, err := modelplan.ResolveOpenCodePlan(config)
		if err != nil {
			t.Fatal(err)
		}
		for path, role := range roles {
			assignment := resolved.Roles[role]
			content := string(artifact(t, pkg, path).Bytes)
			model := strings.TrimPrefix(assignment.Model, "openai/")
			if !strings.Contains(content, `model = "`+model+`"`) || !strings.Contains(content, `model_reasoning_effort = "`+string(assignment.Variant)+`"`) {
				t.Fatalf("%s %s does not match %+v: %s", plan, path, assignment, content)
			}
		}
	}
}

func TestRenderProducesNativeCodexProjection(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{
		"AGENTS.md",
		"agents/care-challenger.toml",
		"agents/care-reviewer.toml",
		"agents/care-specialist.toml",
		"agents/explore.toml",
		"agents/general.toml",
		"agents/verifier.toml",
		".agents/plugins/marketplace.json",
		"plugins/vgxness/.codex-plugin/plugin.json",
		receiptPath,
	}
	if got := artifactPaths(pkg.Artifacts); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("paths = %v, want %v", got, wantPaths)
	}
	if strings.Contains(string(artifact(t, pkg, "AGENTS.md").Bytes), "OpenCode") {
		t.Fatal("manager instructions name an unavailable OpenCode tool")
	}
	for _, item := range pkg.Artifacts {
		if item.Path == ".mcp.json" {
			t.Fatalf("unexpected plugin artifact %q", item.Path)
		}
	}
}

func TestManagerUsesSharedOrchestrationContract(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	content := string(artifact(t, pkg, "AGENTS.md").Bytes)
	contract, loadErr := orchestration.LoadManagerContract()
	if loadErr != nil || OrchestrationContractIdentity() != orchestration.ContractIdentity || !strings.Contains(content, contract.RenderManagerSections()) {
		t.Errorf("Codex manager lacks shared contract %q", orchestration.ContractIdentity)
	}
}

func TestPackageValidateRejectsCallerMutationsAndStaleDigests(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := pkg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	pkg.Artifacts[0].Bytes[0] = 'X'
	if err := pkg.Validate(); err == nil {
		t.Fatal("Validate accepted caller mutation with a stale digest")
	}
	pkg.SHA256 = aggregate(pkg.Artifacts)
	if err := pkg.Validate(); err == nil {
		t.Fatal("Validate accepted caller mutation after digest recomputation")
	}
}

func TestPackageValidateRequiresExactArtifactPaths(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Package){
		"unknown replaces expected": func(pkg *Package) {
			pkg.Artifacts[len(pkg.Artifacts)-1] = Artifact{Path: "agents/unknown.toml"}
		},
		"duplicate path": func(pkg *Package) {
			pkg.Artifacts[len(pkg.Artifacts)-1] = pkg.Artifacts[len(pkg.Artifacts)-2]
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := clonePackage(pkg)
			mutate(&candidate)
			candidate.SHA256 = aggregate(candidate.Artifacts)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate accepted an incomplete or duplicate package")
			}
		})
	}
}

func TestRenderIsDeterministicAndCopiesArtifacts(t *testing.T) {
	first, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("renders differ: %#v != %#v", first, second)
	}
	if got, want := first.SHA256, aggregate(first.Artifacts); got != want {
		t.Fatalf("aggregate SHA-256 = %q, want %q", got, want)
	}
	first.Artifacts[0].Bytes[0] = 'X'
	third, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Artifacts[0].Bytes, third.Artifacts[0].Bytes) {
		t.Fatal("mutating returned artifact changed a later render")
	}
}

func TestRenderRejectsInvalidVersions(t *testing.T) {
	for _, version := range []string{"", "dev", "1.2.3", "v1.2", "v1.2.3/../../x", "v01.2.3", "v1.2.3-", "v1.2.3-alpha..1", "v1.2.3-01", "v1.2.3+build.1"} {
		t.Run(version, func(t *testing.T) {
			if _, err := Render(version); err == nil {
				t.Fatalf("Render(%q) succeeded", version)
			}
		})
	}
}

func TestValidateRelativePathRejectsTraversal(t *testing.T) {
	for _, path := range []string{"", "/absolute", "../escape", "nested/../../escape", `nested\\escape`, "."} {
		t.Run(path, func(t *testing.T) {
			if err := validateRelativePath(path); err == nil {
				t.Fatalf("validateRelativePath(%q) succeeded", path)
			}
		})
	}
}

func artifactPaths(artifacts []Artifact) []string {
	paths := make([]string, len(artifacts))
	for i, artifact := range artifacts {
		paths[i] = artifact.Path
	}
	return paths
}

func artifact(t *testing.T, pkg Package, path string) Artifact {
	t.Helper()
	for _, item := range pkg.Artifacts {
		if item.Path == path {
			return item
		}
	}
	t.Fatalf("artifact %q not found", path)
	return Artifact{}
}

func aggregate(artifacts []Artifact) string {
	hash := sha256.New()
	for _, artifact := range artifacts {
		hash.Write([]byte(artifact.Path))
		hash.Write([]byte{0})
		hash.Write(artifact.Bytes)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

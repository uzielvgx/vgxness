// Package codex renders a deterministic native Codex agent projection.
package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))(?:\.(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)))*)?$`)

const (
	historicalCodexHooksPath = "plugins/vgxness/hooks.json"
	historicalCodexHooksJSON = `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}],"PreCompact":[{"hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}],"PostCompact":[{"hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}],"PostToolUse":[{"matcher":"vgxness_memory_session_summary","hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}],"Stop":[{"hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}],"SessionEnd":[{"hooks":[{"type":"command","command":"vgxness memory codex-hook --stdin"}]}]}}` + "\n"
)

// Artifact is one projection file. Bytes belong exclusively to the returned Package.
type Artifact struct {
	Path  string
	Bytes []byte
}

// Package is an in-memory, filesystem-free Codex projection. Its artifacts and
// bytes are caller-owned mutable copies; call Validate before publication.
type Package struct {
	Artifacts   []Artifact
	SHA256      string
	version     string
	profiles    []profile
	plan        modelplan.Plan
	legacy      bool
	current     bool
	fromReceipt bool
}

type profile struct {
	path         string
	name         string
	description  string
	model        string
	reasoning    string
	sandbox      string
	mcpTools     []string
	instructions string
}

// Render returns the native Codex projection for a strict v-prefixed SemVer
// release, optionally with a SemVer prerelease. It performs no host interaction.
func Render(version string) (Package, error) {
	return RenderPlan(version, modelplan.PlanMedium)
}

// RenderPlan returns the native Codex projection for one shared model plan.
// The primary manager remains host-selected; the plan binds delegated profiles.
func RenderPlan(version string, plan modelplan.Plan) (Package, error) {
	selected, err := sharedProfilesForPlan(plan)
	if err != nil {
		return Package{}, err
	}
	pkg, err := renderPackage(version, selected, plan, false)
	if err != nil {
		return Package{}, err
	}
	pkg.Artifacts = append(pkg.Artifacts, lifecycleArtifacts(pkg.version)...)
	if err := appendReceipt(&pkg); err != nil {
		return Package{}, err
	}
	pkg.SHA256 = aggregateSHA256(pkg.Artifacts)
	pkg.current = true
	if err := pkg.Validate(); err != nil {
		return Package{}, err
	}
	return clonePackage(pkg), nil
}

func lifecycleArtifacts(version string) []Artifact {
	manifest, err := json.Marshal(struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
		Author      struct {
			Name string `json:"name"`
		} `json:"author"`
		License string `json:"license"`
	}{Name: "vgxness", Version: version, Description: "VGXNESS memory lifecycle", Author: struct {
		Name string `json:"name"`
	}{Name: "VGXNESS"}, License: "Apache-2.0"})
	if err != nil {
		panic(err)
	}
	return []Artifact{
		{Path: ".agents/plugins/marketplace.json", Bytes: []byte(`{"name":"vgxness","interface":{"displayName":"VGXNESS"},"plugins":[{"name":"vgxness","source":{"source":"local","path":"./plugins/vgxness"},"policy":{"installation":"AVAILABLE","authentication":"ON_USE"},"category":"Developer Tools"}]}` + "\n")},
		{Path: "plugins/vgxness/.codex-plugin/plugin.json", Bytes: append(manifest, '\n')},
	}
}

func historicalCodexHooksArtifact() Artifact {
	return Artifact{Path: historicalCodexHooksPath, Bytes: []byte(historicalCodexHooksJSON)}
}

func renderPackage(version string, selected []profile, plan modelplan.Plan, legacy bool) (Package, error) {
	if !releaseVersion.MatchString(version) {
		return Package{}, errors.New("version must be a strict v-prefixed SemVer release")
	}
	manager := activeManagerInstructions()
	if manager == "" {
		return Package{}, integration.ErrInvalid
	}
	artifacts := []Artifact{{Path: "AGENTS.md", Bytes: []byte(manager)}}
	for _, item := range selected {
		artifacts = append(artifacts, Artifact{Path: item.path, Bytes: []byte(renderProfile(item))})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	pkg := Package{Artifacts: artifacts, version: strings.TrimPrefix(version, "v"), profiles: append([]profile(nil), selected...), plan: plan, legacy: legacy}
	pkg.SHA256 = aggregateSHA256(pkg.Artifacts)
	return clonePackage(pkg), nil
}

// OrchestrationContractIdentity identifies the provider-neutral policy used by
// this provider without changing Codex's native prompt or MCP semantics.
func OrchestrationContractIdentity() string { return orchestration.ContractIdentity }

var profileRoles = map[string]modelplan.Role{
	"agents/explore.toml":         modelplan.RoleResearch,
	"agents/general.toml":         modelplan.RoleImplementation,
	"agents/verifier.toml":        modelplan.RoleVerification,
	"agents/care-reviewer.toml":   modelplan.RoleCAREReviewer,
	"agents/care-specialist.toml": modelplan.RoleCARESpecialist,
	"agents/care-challenger.toml": modelplan.RoleCAREChallenger,
}

func profilesForPlan(plan modelplan.Plan) ([]profile, error) {
	config := modelplan.DefaultModelPlanConfig()
	config.ActivePlan = plan
	resolved, err := modelplan.ResolveOpenCodePlan(config)
	if err != nil {
		return nil, fmt.Errorf("invalid Codex model plan: %w", err)
	}
	selected := append([]profile(nil), currentNativeProfiles...)
	for index := range selected {
		role, ok := profileRoles[selected[index].path]
		if !ok {
			return nil, fmt.Errorf("Codex profile %q has no model-plan role", selected[index].path)
		}
		assignment, ok := resolved.Roles[role]
		if !ok || !strings.HasPrefix(assignment.Model, "openai/") {
			return nil, fmt.Errorf("Codex role %q has an invalid model assignment", role)
		}
		selected[index].model = strings.TrimPrefix(assignment.Model, "openai/")
		selected[index].reasoning = string(assignment.Variant)
	}
	return selected, nil
}

func renderProfile(item profile) string {
	return fmt.Sprintf("name = %q\ndescription = %q\ndeveloper_instructions = %q\nmodel = %q\nmodel_reasoning_effort = %q\nsandbox_mode = %q\n\n[mcp_servers.vgxness]\ncommand = \"vgxness\"\nargs = [\"mcp\", \"--full\"]\nenabled_tools = %s\n", item.name, item.description, item.instructions, item.model, item.reasoning, item.sandbox, tomlStrings(item.mcpTools))
}

func tomlStrings(values []string) string {
	encoded := make([]string, len(values))
	for index, value := range values {
		encoded[index] = fmt.Sprintf("%q", value)
	}
	return "[" + strings.Join(encoded, ", ") + "]"
}

func (pkg Package) Validate() error {
	if !releaseVersion.MatchString("v"+pkg.version) || !pkg.current || pkg.legacy || len(pkg.profiles) != 6 || len(pkg.Artifacts) != 10 {
		return errors.New("invalid current Codex package")
	}
	seen := map[string]bool{}
	for _, a := range pkg.Artifacts {
		if validateRelativePath(a.Path) != nil || seen[a.Path] {
			return errors.New("invalid artifact paths")
		}
		seen[a.Path] = true
	}
	if !packageReceiptMatches(pkg) || aggregateSHA256(pkg.Artifacts) != pkg.SHA256 {
		return errors.New("invalid package receipt or digest")
	}
	if pkg.fromReceipt {
		return nil
	}
	current, e := sharedProfilesForPlan(pkg.plan)
	if e != nil {
		return e
	}
	copy := pkg
	copy.Artifacts = pkg.Artifacts[:len(pkg.Artifacts)-1]
	if !packageMatchesWithLifecycle(copy, current, activeManagerInstructions()) {
		return errors.New("invalid current native projection")
	}
	return nil
}

func packageMatchesWithLifecycle(pkg Package, profiles []profile, manager string) bool {
	n := len(lifecycleArtifacts(pkg.version))
	if len(pkg.Artifacts) != len(profiles)+1+n {
		return false
	}
	for i, want := range lifecycleArtifacts(pkg.version) {
		got := pkg.Artifacts[len(pkg.Artifacts)-n+i]
		if got.Path != want.Path || !bytes.Equal(got.Bytes, want.Bytes) {
			return false
		}
	}
	copy := pkg
	copy.Artifacts = pkg.Artifacts[:len(pkg.Artifacts)-n]
	return packageMatches(copy, profiles, manager)
}

func packageMatches(pkg Package, profiles []profile, manager string) bool {
	if len(pkg.Artifacts) != len(profiles)+1 || string(pkg.Artifacts[0].Bytes) != manager {
		return false
	}
	expected := make(map[string]string, len(profiles))
	for _, profile := range profiles {
		expected[profile.path] = renderProfile(profile)
	}
	for _, artifact := range pkg.Artifacts[1:] {
		content, ok := expected[artifact.Path]
		if !ok || content != string(artifact.Bytes) {
			return false
		}
		delete(expected, artifact.Path)
	}
	return len(expected) == 0
}

func validateRelativePath(value string) error {
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") || path.Clean(value) != value || value == "." {
		return fmt.Errorf("invalid relative artifact path %q", value)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return fmt.Errorf("artifact path traversal %q", value)
		}
	}
	return nil
}

// aggregateSHA256 hashes lexical artifacts as path, NUL, bytes, NUL for each
// artifact. NUL delimiters make the path-and-bytes input unambiguous.
func aggregateSHA256(artifacts []Artifact) string {
	hash := sha256.New()
	for _, artifact := range artifacts {
		_, _ = hash.Write([]byte(artifact.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(artifact.Bytes)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func clonePackage(source Package) Package {
	result := Package{Artifacts: make([]Artifact, len(source.Artifacts)), SHA256: source.SHA256, version: source.version, profiles: append([]profile(nil), source.profiles...), plan: source.plan, legacy: source.legacy, current: source.current, fromReceipt: source.fromReceipt}
	for index, artifact := range source.Artifacts {
		result.Artifacts[index] = Artifact{Path: artifact.Path, Bytes: append([]byte(nil), artifact.Bytes...)}
	}
	return result
}

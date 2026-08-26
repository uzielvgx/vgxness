//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/sdd"
)

func TestCleanCheckoutSetupAndNativeSDD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows runtime smoke is tracked separately")
	}
	repository := repositoryRoot(t)
	root := t.TempDir()
	buildDirectory := filepath.Join(root, "build")
	fakeBinDirectory := filepath.Join(root, "fake-bin")
	homeDirectory := filepath.Join(root, "home")
	temporaryDirectory := filepath.Join(root, "tmp")
	workspace := filepath.Join(root, "workspace")
	launcherDirectory := filepath.Join(root, "installed", "bin")
	dataDirectory := filepath.Join(root, "installed", "data")
	configDirectory := filepath.Join(root, "opencode")
	for _, directory := range []string{buildDirectory, fakeBinDirectory, homeDirectory, temporaryDirectory, workspace} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Hermetic workspace\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	sourceExecutable := filepath.Join(buildDirectory, executableName("vgxness"))
	fakeOpenCode := filepath.Join(fakeBinDirectory, executableName("opencode"))
	build(t, goExecutable, repository, sourceExecutable, "./cmd/vgxness")
	build(t, goExecutable, repository, fakeOpenCode, "./internal/e2e/testdata/fakeopencode")

	environment := isolatedEnvironment(homeDirectory, temporaryDirectory, fakeBinDirectory)
	setupOutput := run(t, environment, workspace, sourceExecutable,
		"setup", "opencode", "--yes", "--workspace", workspace,
		"--bin-dir", launcherDirectory, "--data-dir", dataDirectory, "--config-dir", configDirectory,
		"--model-efficient", "openai/gpt-5.6-luna", "--model-balanced", "anthropic/claude-sonnet", "--model-frontier", "acme/frontier",
		"--model-efficient-effort", "low", "--model-balanced-effort", "high", "--model-frontier-effort", "ultra",
	)
	for _, expected := range []string{"Paso 1 de 7", "Paso 7 de 7", "configuración completa", "handshake OpenCode=healthy", "Reinicia OpenCode para cargar vgxness-manager"} {
		if !strings.Contains(setupOutput, expected) {
			t.Fatalf("setup output is missing %q:\n%s", expected, setupOutput)
		}
	}

	launcher := filepath.Join(launcherDirectory, executableName("vgxness"))
	manager := filepath.Join(configDirectory, "agents", "vgxness-manager.md")
	general := filepath.Join(configDirectory, "agents", "general.md")
	explore := filepath.Join(configDirectory, "agents", "explore.md")
	verifier := filepath.Join(configDirectory, "agents", "vgxness-verifier.md")
	memoryPlugin := filepath.Join(configDirectory, "plugins", "vgxness.ts")
	defaultAgentConfig := filepath.Join(configDirectory, "opencode.json")
	reviewers := []string{
		"vgxness-care-reviewer.md",
		"vgxness-care-specialist.md",
		"vgxness-care-challenger.md",
	}
	sddProfiles := []string{
		"vgxness-sdd-research.md",
		"vgxness-sdd-proposal.md",
		"vgxness-sdd-spec.md",
		"vgxness-sdd-design.md",
		"vgxness-sdd-tasks.md",
		"vgxness-sdd-apply.md",
	}
	managedProfiles := append(reviewerPaths(configDirectory, reviewers), reviewerPaths(configDirectory, sddProfiles)...)
	for _, path := range append([]string{launcher, manager, general, explore, verifier, defaultAgentConfig}, managedProfiles...) {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("expected installed regular file %s: %v", path, statErr)
		}
	}
	if _, err := os.Stat(memoryPlugin); !os.IsNotExist(err) {
		t.Fatalf("setup retained retired OpenCode plugin: %v", err)
	}
	managerData, err := os.ReadFile(manager)
	if err != nil {
		t.Fatalf("read installed manager: %v", err)
	}
	for _, required := range []struct {
		name  string
		value string
	}{
		{"active v58 marker", "artifact: opencode-agent/vgxness-manager; version: 58"},
		{"model and variant", "model: acme/frontier\nvariant: xhigh"},
		{"proportional ceremony", "Apply ceremony proportionally: small authorized repository changes remain delegated and do not imply SDD or delivery."},
		{"context capsule", "Carry a Context Capsule v1 alongside the smallest applicable mission shape."},
		{"context digest ownership", "The Manager is the sole digest-computation owner for every non-SDD repository delegation."},
		{"stacked-pr", "automatically load `stacked-pr`"},
		{"pre-write gate", "Before delegating any workspace write"},
		{"global permission", "permission:\n  \"*\": allow"},
		{"sdd-lifecycle", "Load `sdd-lifecycle` before creating an accepted SDD change"},
		{"managed catalog", "managed global portable catalog"},
		{"same-name collision", "same-name/project-local skill collides"},
		{"verifier and CARE order", "The verifier runs first; each applicable CARE role then reviews that same candidate."},
		{"candidate-bound outcomes", "Require PASS, FAIL, or INCONCLUSIVE with candidate-bound evidence;"},
		{"CARE risk tiers", "CARE risk tiers: passive documentation or images are exempt; standard uses reviewer; elevated uses reviewer then specialist; critical uses reviewer, specialist, then challenger."},
		{"correction invalidation", "Permit at most one correction and one scoped revalidation; a correction creates a new candidate and invalidates prior evidence."},
		{"Manager authority", "The Manager alone decides completion."},
	} {
		if !bytes.Contains(managerData, []byte(required.value)) {
			t.Errorf("installed manager is missing %s clause %q", required.name, required.value)
		}
	}
	if got := bytes.Count(managerData, []byte("artifact: opencode-agent/vgxness-manager; version: 58")); got != 1 {
		t.Fatalf("installed current manager v58 marker count=%d, want 1", got)
	}
	if got := bytes.Count(managerData, []byte("artifact: opencode-agent/vgxness-manager; version: 57")); got != 0 {
		t.Fatalf("installed current manager retains v57 marker count=%d, want 0", got)
	}
	if bytes.Contains(managerData, []byte("automatically load `vgxness-autonomous-stacked-pr`")) {
		t.Fatal("installed manager retains retired vgxness-autonomous-stacked-pr loading")
	}
	manifestData, err := os.ReadFile(filepath.Join(configDirectory, "vgxness", "model-plan.json"))
	for _, expected := range []string{`"schemaVersion": 3`, `"configV3"`, `"provider": "mixed"`, `"agents/vgxness-care-reviewer.md"`, `"agents/vgxness-care-specialist.md"`, `"agents/vgxness-care-challenger.md"`} {
		if err != nil || !bytes.Contains(manifestData, []byte(expected)) {
			t.Fatalf("setup did not install the expected v3 manifest content %q: %v\n%s", expected, err, manifestData)
		}
	}
	type manifest struct {
		SchemaVersion int                    `json:"schemaVersion"`
		ManagedBy     string                 `json:"managedBy"`
		Config        *sdd.ModelPlanConfig   `json:"config,omitempty"`
		Resolved      *sdd.OpenCodePlan      `json:"resolved,omitempty"`
		ConfigV2      *sdd.ModelPlanConfigV2 `json:"configV2,omitempty"`
		ResolvedV2    *sdd.OpenCodePlanV2    `json:"resolvedV2,omitempty"`
		ConfigV3      *sdd.ModelPlanConfigV3 `json:"configV3,omitempty"`
		ResolvedV3    *sdd.OpenCodePlanV3    `json:"resolvedV3,omitempty"`
		Artifacts     map[string]string      `json:"artifacts"`
	}
	var predecessorManifest manifest
	if err := json.Unmarshal(manifestData, &predecessorManifest); err != nil {
		t.Fatalf("decode current manifest: %v", err)
	}
	for _, name := range reviewers {
		current, readErr := os.ReadFile(filepath.Join(configDirectory, "agents", name))
		if readErr != nil {
			t.Fatalf("read current CARE role %s: %v", name, readErr)
		}
		predecessor := exactCAREV1Agent(t, repository, name, current)
		digest := sha256.Sum256(predecessor)
		predecessorManifest.Artifacts["agents/"+name] = hex.EncodeToString(digest[:])
		if err := os.WriteFile(filepath.Join(configDirectory, "agents", name), predecessor, 0o600); err != nil {
			t.Fatalf("seed exact CARE-v1 role %s: %v", name, err)
		}
	}
	predecessorManifestData, err := json.MarshalIndent(predecessorManifest, "", "  ")
	if err != nil {
		t.Fatalf("encode exact v57 manifest: %v", err)
	}
	predecessorManifestData = append(predecessorManifestData, '\n')
	manifestPath := filepath.Join(configDirectory, "vgxness", "model-plan.json")
	if err := os.WriteFile(manifestPath, predecessorManifestData, 0o600); err != nil {
		t.Fatalf("seed exact v57 manifest: %v", err)
	}
	run(t, environment, workspace, sourceExecutable,
		"setup", "opencode", "--yes", "--workspace", workspace,
		"--bin-dir", launcherDirectory, "--data-dir", dataDirectory, "--config-dir", configDirectory,
	)
	upgradedManager, err := os.ReadFile(manager)
	if err != nil || !bytes.Equal(upgradedManager, managerData) {
		t.Fatalf("exact v57 manager did not upgrade to captured v58 bytes: %v", err)
	}
	upgradedManifest, err := os.ReadFile(manifestPath)
	if err != nil || !bytes.Equal(upgradedManifest, manifestData) {
		t.Fatalf("exact v57 manifest did not upgrade to captured v58 bytes: %v", err)
	}
	generalData, generalErr := os.ReadFile(general)
	exploreData, exploreErr := os.ReadFile(explore)
	verifierData, verifierErr := os.ReadFile(verifier)
	for _, required := range []struct {
		name string
		data []byte
		err  error
		want []string
	}{
		{"general", generalData, generalErr, []string{"artifact: opencode-agent/general; version: 10", "permission:\n  \"*\": allow", "delegated non-SDD implementation worker", "Require a Context Capsule v1 for every non-SDD repository mission:", "Echo the accepted contextDigest unchanged in the return."}},
		{"explore", exploreData, exploreErr, []string{"artifact: opencode-agent/explore; version: 4", "permission:\n  \"*\": deny", "codegraph_codegraph_explore: allow", "Require a Context Capsule v1 for every non-SDD repository mission:", "Echo the accepted contextDigest unchanged in the return."}},
		{"verifier", verifierData, verifierErr, []string{"artifact: opencode-agent/vgxness-verifier; version: 7", "permission:\n  \"*\": allow", "one exact Review Binding", "Require a Context Capsule v1 for every non-SDD repository mission:", "Echo the accepted contextDigest unchanged in the return.", "PASS|FAIL|INCONCLUSIVE"}},
	} {
		if required.err != nil {
			t.Fatalf("read installed %s contract: %v", required.name, required.err)
		}
		for _, want := range required.want {
			if !bytes.Contains(required.data, []byte(want)) {
				t.Errorf("installed %s contract is missing %q", required.name, want)
			}
		}
	}
	defaultAgentData, defaultAgentErr := os.ReadFile(defaultAgentConfig)
	if defaultAgentErr != nil || !bytes.Contains(defaultAgentData, []byte(`"default_agent": "vgxness-manager"`)) || !bytes.Contains(defaultAgentData, []byte(`"--full"`)) {
		t.Fatalf("setup did not select vgxness-manager by default: %v", defaultAgentErr)
	}
	if err := os.Rename(sourceExecutable, sourceExecutable+".offline"); err != nil {
		t.Fatalf("retire source executable: %v", err)
	}

	statusOutput := run(t, environment, workspace, launcher,
		"setup", "opencode", "--status", "--workspace", workspace,
		"--bin-dir", launcherDirectory, "--data-dir", dataDirectory, "--config-dir", configDirectory,
	)
	if !strings.Contains(statusOutput, "Launcher: state=installed") || !strings.Contains(statusOutput, "Handshake: ok=true status=healthy") || !strings.Contains(statusOutput, "Plan de modelos:  provider=mixed manifest=") {
		t.Fatalf("installed setup is not healthy:\n%s", statusOutput)
	}

	var createdEnvelope struct {
		SchemaVersion int        `json:"schemaVersion"`
		Result        sdd.Change `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, `{"schemaVersion":1,"idempotencyKey":"hermetic-lifecycle-1","title":"Hermetic lifecycle","backend":"memory","interactionMode":"automatic","plan":"low"}`, launcher, "sdd", "create", "--stdin", "--json", "--workspace", workspace), &createdEnvelope)
	change := createdEnvelope.Result
	if createdEnvelope.SchemaVersion != 1 || change.ID == "" || change.StateVersion != 1 || change.Phase != sdd.PhaseExplore {
		t.Fatalf("unexpected SDD change: %+v", createdEnvelope)
	}
	var modeEnvelope struct {
		Result sdd.Change `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"interactionMode":"interactive","expectedStateVersion":1}`, change.ID), launcher, "sdd", "set-interaction-mode", "--stdin", "--json", "--workspace", workspace), &modeEnvelope)
	if modeEnvelope.Result.InteractionMode != sdd.InteractionInteractive || modeEnvelope.Result.StateVersion != 2 {
		t.Fatalf("unexpected SDD mode update: %+v", modeEnvelope.Result)
	}
	var savedEnvelope struct {
		Result sdd.Revision `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"artifact":"explore","content":"bounded research","expectedStateVersion":2}`, change.ID), launcher, "sdd", "save-revision", "--stdin", "--json", "--workspace", workspace), &savedEnvelope)
	var acceptedEnvelope struct {
		Result sdd.Revision `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"revisionId":%q,"expectedStateVersion":%d}`, change.ID, savedEnvelope.Result.ID, savedEnvelope.Result.StateVersion), launcher, "sdd", "accept-revision", "--stdin", "--json", "--workspace", workspace), &acceptedEnvelope)
	var transitionEnvelope struct {
		Result sdd.Change `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"targetPhase":"proposal","expectedStateVersion":%d}`, change.ID, acceptedEnvelope.Result.StateVersion), launcher, "sdd", "transition", "--stdin", "--json", "--workspace", workspace), &transitionEnvelope)
	if transitionEnvelope.Result.Phase != sdd.PhaseProposal {
		t.Fatalf("SDD lifecycle did not advance: %+v", transitionEnvelope.Result)
	}

	if info, err := os.Stat(filepath.Join(homeDirectory, ".vgxness", "memory.db")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("global project-isolated memory store is missing: info=%v err=%v", info, err)
	}
}

func exactV57Manager(t *testing.T, repository string) string {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join(repository, "internal", "e2e", "testdata", "opencode-manager.v57.acme-frontier-xhigh.md"))
	if err != nil {
		t.Fatalf("read exact v57 manager fixture: %v", err)
	}
	if got := bytes.Count(fixture, []byte("artifact: opencode-agent/vgxness-manager; version: 57")); got != 1 {
		t.Fatalf("exact v57 fixture marker count=%d, want 1", got)
	}
	if bytes.Contains(fixture, []byte("artifact: opencode-agent/vgxness-manager; version: 58")) {
		t.Fatal("exact v57 fixture retains v58 marker")
	}
	return string(fixture)
}

func exactCAREV1Agent(t *testing.T, repository, name string, current []byte) []byte {
	t.Helper()
	base, err := os.ReadFile(filepath.Join(repository, "internal", "providers", "opencode", "templates", strings.TrimSuffix(name, ".md")+".v1.md"))
	if err != nil {
		t.Fatalf("read CARE-v1 snapshot %s: %v", name, err)
	}
	start := bytes.Index(current, []byte("model: "))
	if start < 0 {
		t.Fatalf("current CARE role %s lacks model binding", name)
	}
	end := bytes.Index(current[start:], []byte("permission:\n"))
	if end < 0 {
		t.Fatalf("current CARE role %s lacks model binding", name)
	}
	binding := current[start : start+end]
	return bytes.Replace(base, []byte("hidden: true\n"), append([]byte("hidden: true\n"), binding...), 1)
}

func reviewerPaths(configDirectory string, names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(configDirectory, "agents", name))
	}
	return paths
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	return canonicalPath(t, filepath.Join(filepath.Dir(file), "..", ".."))
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(resolved)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func build(t *testing.T, goExecutable, repository, output, packagePath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goExecutable, "build", "-trimpath", "-o", output, packagePath)
	command.Dir = repository
	command.Env = replaceEnvironment(os.Environ(), map[string]string{"GOPROXY": "off", "GOTOOLCHAIN": "local"}, "GOPROXY", "GOTOOLCHAIN")
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, combined)
	}
}

func isolatedEnvironment(home, temporary, fakeBin string) []string {
	path := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	return replaceEnvironment(os.Environ(), map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "TMPDIR": temporary,
		"TMP": temporary, "TEMP": temporary, "PATH": path,
	}, "HOME", "XDG_CONFIG_HOME", "TMPDIR", "TMP", "TEMP", "PATH", "OPENCODE_", "VGXNESS_")
}

func replaceEnvironment(current []string, additions map[string]string, denied ...string) []string {
	filtered := make([]string, 0, len(current)+len(additions))
	for _, entry := range current {
		name, _, found := strings.Cut(entry, "=")
		if !found || deniedEnvironment(name, denied) {
			continue
		}
		filtered = append(filtered, entry)
	}
	for name, value := range additions {
		filtered = append(filtered, name+"="+value)
	}
	return filtered
}

func deniedEnvironment(name string, denied []string) bool {
	for _, item := range denied {
		if strings.HasSuffix(item, "_") && strings.HasPrefix(name, item) || name == item {
			return true
		}
	}
	return false
}

func run(t *testing.T, environment []string, directory, executable string, args ...string) string {
	t.Helper()
	return runWithInput(t, environment, directory, "", executable, args...)
}

func runWithInput(t *testing.T, environment []string, directory, input, executable string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = environment
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run %s %q: %v\nstdout:\n%s\nstderr:\n%s", executable, args, err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr from %s %q:\n%s", executable, args, stderr.String())
	}
	return stdout.String()
}

func decodeJSON(t *testing.T, data string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v\n%s", err, data)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("invalid trailing JSON in response: %v\n%s", err, data)
	}
}

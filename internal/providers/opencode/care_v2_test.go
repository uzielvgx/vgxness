package opencode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestCAREV2RolesKeepExactV1SnapshotsAndBindingContract(t *testing.T) {
	roles := []struct{ name, snapshot, digest string }{
		{"vgxness-care-reviewer.md", "vgxness-care-reviewer.v1.md", "fafc153e5cfbb6b575a615f0ac81c5eb855a74bd43f3c313d6eecf2f26a96b74"},
		{"vgxness-care-specialist.md", "vgxness-care-specialist.v1.md", "a6b70b3218b58e7391953b1f8254b00d0d9a5dd1571d6dddf38611093f3cb39c"},
		{"vgxness-care-challenger.md", "vgxness-care-challenger.v1.md", "6d0a7895f02c339df53b6a041537999e94b1c6fe7c9f4e2a4f3091fc18759cde"},
	}
	for _, role := range roles {
		t.Run(role.name, func(t *testing.T) {
			snapshot, err := os.ReadFile(filepath.Join("templates", role.snapshot))
			if err != nil || artifactSHA256(snapshot) != role.digest {
				t.Fatalf("v1 snapshot err=%v digest=%s", err, artifactSHA256(snapshot))
			}
			current, err := os.ReadFile(filepath.Join("templates", role.name))
			if err != nil || !strings.Contains(string(current), "version: 2") {
				t.Fatalf("v2 prompt err=%v", err)
			}
			for _, required := range []string{"candidateDigest", "exact changedPaths", "diffScope", "acceptanceCriteria", "matching candidate identity", "missing, stale, malformed, or mismatched", "INCONCLUSIVE", "Echo the complete Review Binding unchanged", "PASS|FAIL|INCONCLUSIVE", "evidence", "findings", "claim recommendations", "uncertainty", "blockers", "authorization", "implementation", "lifecycle", "Git", "persistence", "network", "shell", "package", "delegation"} {
				if !strings.Contains(string(current), required) {
					t.Errorf("missing %q", required)
				}
			}
		})
	}
}

func TestCAREV1PackagesUpgradeThroughInstallAndReinstall(t *testing.T) {
	builders := map[string]func(t *testing.T) modelPlanBundle{
		"schema-v1": func(t *testing.T) modelPlanBundle {
			b, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
			if err != nil {
				t.Fatal(err)
			}
			return b
		},
		"schema-v2": func(t *testing.T) modelPlanBundle { return mustBuildModelPlanV2(t, sdd.DefaultModelPlanConfigV2()) },
		"schema-v3": func(t *testing.T) modelPlanBundle {
			b, err := buildModelPlanBundleV3(projectModelPlanToV3(sdd.DefaultModelPlanConfig()))
			if err != nil {
				t.Fatal(err)
			}
			return b
		},
	}
	cases := []struct {
		name         string
		build        func(t *testing.T) modelPlanBundle
		manifestless bool
	}{
		{name: "schema-v1", build: builders["schema-v1"]},
		{name: "schema-v2", build: builders["schema-v2"]},
		{name: "schema-v3", build: builders["schema-v3"]},
		{name: "canonical-schema-v3-manifestless", build: builders["schema-v3"], manifestless: true},
	}
	operations := []struct {
		name string
		call func(*Integration, context.Context, integration.Options) (integration.Result, error)
	}{
		{name: "Install", call: (*Integration).Install},
		{name: "Reinstall", call: (*Integration).Reinstall},
	}
	for _, tc := range cases {
		for _, operation := range operations {
			t.Run(tc.name+"/"+operation.name, func(t *testing.T) {
				current := tc.build(t)
				predecessor, err := immediatePredecessor(current)
				if err != nil {
					t.Fatal(err)
				}
				root := t.TempDir()
				writeModelPlanBundleFixture(t, root, predecessor)
				if tc.manifestless {
					testRemoveModelPlanManifest(t, root)
				}
				result, err := operation.call(NewIntegration(), context.Background(), integration.Options{ConfigDir: root})
				if err != nil || !result.Changed || result.State != integration.StateInstalled {
					t.Fatalf("%s=%+v err=%v", operation.name, result, err)
				}
				assertCurrentBundleReadback(t, root, current)
			})
		}
	}
}

func testRemoveModelPlanManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, "vgxness", modelPlanManifestName)); err != nil {
		t.Fatal(err)
	}
}

func assertCurrentBundleReadback(t *testing.T, root string, current modelPlanBundle) {
	t.Helper()
	for name, want := range current.agents {
		got, err := os.ReadFile(filepath.Join(root, "agents", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s readback=%v exact=%t", name, err, bytes.Equal(got, want))
		}
	}
	got, err := os.ReadFile(filepath.Join(root, "vgxness", modelPlanManifestName))
	if err != nil || !bytes.Equal(got, current.manifest) {
		t.Fatalf("manifest readback=%v exact=%t", err, bytes.Equal(got, current.manifest))
	}
}

func TestCAREV1PackageDriftFailsClosedWithoutMutation(t *testing.T) {
	current, err := buildModelPlanBundleV3(projectModelPlanToV3(sdd.DefaultModelPlanConfig()))
	if err != nil {
		t.Fatal(err)
	}
	immediate, err := immediatePredecessor(current)
	if err != nil {
		t.Fatal(err)
	}
	predecessor, err := previousCAREV1ModelPlanBundle(immediate)
	if err != nil {
		t.Fatal(err)
	}
	operations := []struct {
		name string
		call func(*Integration, context.Context, integration.Options) (integration.Result, error)
	}{
		{name: "Install", call: (*Integration).Install},
		{name: "Reinstall", call: (*Integration).Reinstall},
	}
	for _, manifestless := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			for _, roleName := range []string{"vgxness-care-reviewer.md", "vgxness-care-specialist.md", "vgxness-care-challenger.md"} {
				for _, operation := range operations {
					t.Run(map[bool]string{false: "manifest", true: "canonical-manifestless"}[manifestless]+"/"+map[bool]string{false: "one-byte", true: "mixed"}[mixed]+"/"+roleName+"/"+operation.name, func(t *testing.T) {
						root := t.TempDir()
						writeModelPlanBundleFixture(t, root, predecessor)
						role := filepath.Join(root, "agents", roleName)
						if mixed {
							if err := os.WriteFile(role, immediate.agents[roleName], 0o600); err != nil {
								t.Fatal(err)
							}
						} else {
							b, e := os.ReadFile(role)
							if e != nil {
								t.Fatal(e)
							}
							b[len(b)-1] ^= 1
							if e = os.WriteFile(role, b, 0o600); e != nil {
								t.Fatal(e)
							}
						}
						manifest := filepath.Join(root, "vgxness", modelPlanManifestName)
						if manifestless {
							testRemoveModelPlanManifest(t, root)
						}
						before := careSeedSnapshot(t, root, predecessor, !manifestless)
						_, err := operation.call(NewIntegration(), context.Background(), integration.Options{ConfigDir: root})
						if !errors.Is(err, integration.ErrConflict) && !errors.Is(err, integration.ErrDrift) {
							t.Fatalf("%s error=%v", operation.name, err)
						}
						careSeedUnchanged(t, root, before)
						if manifestless {
							if _, statErr := os.Stat(manifest); !errors.Is(statErr, os.ErrNotExist) {
								t.Fatalf("manifest created: %v", statErr)
							}
						}
					})
				}
			}
		}
	}
}

func careSeedSnapshot(t *testing.T, root string, bundle modelPlanBundle, manifest bool) map[string][]byte {
	t.Helper()
	paths := make([]string, 0, len(bundle.agents)+1)
	for name := range bundle.agents {
		paths = append(paths, filepath.Join(root, "agents", name))
	}
	if manifest {
		paths = append(paths, filepath.Join(root, "vgxness", modelPlanManifestName))
	}
	sort.Strings(paths)
	result := make(map[string][]byte, len(paths))
	for _, path := range paths {
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = value
	}
	return result
}

func careSeedUnchanged(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	paths := make([]string, 0, len(before))
	for path := range before {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, before[path]) {
			t.Fatalf("seed changed %s err=%v", path, err)
		}
	}
}

func TestCAREV2LifecyclePreservesOnlyRoleDelta(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	immediate, err := immediatePredecessor(current)
	if err != nil {
		t.Fatal(err)
	}
	careV1, err := previousCAREV1ModelPlanBundle(immediate)
	if err != nil {
		t.Fatal(err)
	}
	next, err := previousV57ModelPlanBundle(careV1)
	if err != nil {
		t.Fatal(err)
	}
	care := map[string]bool{"vgxness-care-reviewer.md": true, "vgxness-care-specialist.md": true, "vgxness-care-challenger.md": true}
	for name, currentBytes := range current.agents {
		if changed := !bytes.Equal(currentBytes, immediate.agents[name]); changed != (name == managerAgentName) {
			t.Errorf("current M59 to immediate M58 change for %s=%t, want manager-only", name, changed)
		}
	}
	for name, immediateBytes := range immediate.agents {
		if changed := !bytes.Equal(immediateBytes, careV1.agents[name]); changed != (care[name] || name == managerAgentName) {
			t.Errorf("CARE-v2 M59 to CARE-v1 M58 change for %s=%t, want manager-and-role-only", name, changed)
		}
	}
	for name, careV1Bytes := range careV1.agents {
		if changed := !bytes.Equal(careV1Bytes, next.agents[name]); changed != (name == managerAgentName) {
			t.Errorf("CARE-v1 M58 to CARE-v1 M57 change for %s=%t, want manager-only", name, changed)
		}
	}
	if !bytes.Contains(current.agents[managerAgentName], []byte(managerCurrentMarker)) || !bytes.Contains(immediate.agents[managerAgentName], []byte(managerV59Marker)) || !bytes.Contains(careV1.agents[managerAgentName], []byte(managerV58Marker)) || !bytes.Contains(next.agents[managerAgentName], []byte(managerV57Marker)) {
		t.Fatal("CARE-v2 M59 -> CARE-v2 M58 -> CARE-v1 M58 -> CARE-v1 M57 manager markers are not retained")
	}
	for name := range care {
		if !bytes.Contains(immediate.agents[name], []byte("; version: 2")) || !bytes.Contains(careV1.agents[name], []byte("; version: 1")) {
			t.Fatalf("CARE role marker for %s does not transition from v2 to v1", name)
		}
	}
}

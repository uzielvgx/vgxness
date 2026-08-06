package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func TestRenderProducesOfficialMCPOnlyPackage(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := artifactPaths(pkg.Artifacts), []string{".codex-plugin/plugin.json", ".mcp.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}

	var plugin map[string]any
	mustUnmarshal(t, artifact(t, pkg, ".codex-plugin/plugin.json").Bytes, &plugin)
	if got, want := plugin, map[string]any{
		"name": "vgxness", "version": "1.2.3", "description": "Read-only project memory for VGXNESS.", "mcpServers": "./.mcp.json",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plugin = %#v, want %#v", got, want)
	}

	var mcp map[string]any
	mustUnmarshal(t, artifact(t, pkg, ".mcp.json").Bytes, &mcp)
	if got, want := mcp, map[string]any{"vgxness": map[string]any{"command": "vgxness", "args": []any{"mcp"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mcp = %#v, want %#v", got, want)
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

func TestRenderPreservesPrereleaseVersionWithoutLeadingV(t *testing.T) {
	pkg, err := Render("v0.1.0-alpha.1")
	if err != nil {
		t.Fatal(err)
	}
	var plugin pluginManifest
	mustUnmarshal(t, artifact(t, pkg, ".codex-plugin/plugin.json").Bytes, &plugin)
	if got, want := plugin.Version, "0.1.0-alpha.1"; got != want {
		t.Fatalf("manifest version = %q, want %q", got, want)
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

func TestValidatePackageRejectsUnexpectedCapabilities(t *testing.T) {
	pkg, err := Render("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	pkg.Artifacts[1].Bytes = []byte(`{"vgxness":{"command":"vgxness","args":["mcp"],"env":{"TOKEN":"x"}}}`)
	pkg.SHA256 = aggregate(pkg.Artifacts)
	if err := validatePackage(pkg); err == nil {
		t.Fatal("validatePackage accepted an unexpected MCP capability")
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

func mustUnmarshal(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
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

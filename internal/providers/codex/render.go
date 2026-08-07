// Package codex renders the MCP-only Codex plugin package.
package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))(?:\.(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)))*)?$`)

// Artifact is one package file. Bytes belong exclusively to the returned Package.
type Artifact struct {
	Path  string
	Bytes []byte
}

// Package is an in-memory, filesystem-free plugin package.
type Package struct {
	Artifacts []Artifact
	SHA256    string
}

type pluginManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	MCPServers  string `json:"mcpServers"`
}

type mcpServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Render returns the complete Codex plugin package for a strict v-prefixed
// SemVer release, optionally with a SemVer prerelease. It performs no
// filesystem or host interaction.
func Render(version string) (Package, error) {
	if !releaseVersion.MatchString(version) {
		return Package{}, errors.New("version must be a strict v-prefixed SemVer release")
	}
	plugin, err := json.Marshal(pluginManifest{
		Name:        "vgxness",
		Version:     strings.TrimPrefix(version, "v"),
		Description: "VGXNESS project memory and SDD lifecycle MCP.",
		MCPServers:  "./.mcp.json",
	})
	if err != nil {
		return Package{}, fmt.Errorf("marshal plugin manifest: %w", err)
	}
	mcp, err := json.Marshal(map[string]mcpServer{
		"vgxness": {Command: "vgxness", Args: []string{"mcp", "--full"}},
	})
	if err != nil {
		return Package{}, fmt.Errorf("marshal MCP configuration: %w", err)
	}
	pkg := Package{Artifacts: []Artifact{
		{Path: ".codex-plugin/plugin.json", Bytes: plugin},
		{Path: ".mcp.json", Bytes: mcp},
	}}
	pkg.SHA256 = aggregateSHA256(pkg.Artifacts)
	if err := validatePackage(pkg); err != nil {
		return Package{}, err
	}
	return clonePackage(pkg), nil
}

func validatePackage(pkg Package) error {
	if len(pkg.Artifacts) != 2 {
		return errors.New("package must contain exactly two artifacts")
	}
	previous := ""
	for _, artifact := range pkg.Artifacts {
		if err := validateRelativePath(artifact.Path); err != nil {
			return err
		}
		if artifact.Path <= previous {
			return errors.New("artifact paths must be unique and lexical")
		}
		if !json.Valid(artifact.Bytes) {
			return fmt.Errorf("artifact %q is not valid JSON", artifact.Path)
		}
		previous = artifact.Path
	}
	if pkg.Artifacts[0].Path != ".codex-plugin/plugin.json" || pkg.Artifacts[1].Path != ".mcp.json" {
		return errors.New("package contains unexpected artifacts")
	}
	if err := validateContents(pkg.Artifacts); err != nil {
		return err
	}
	if pkg.SHA256 != aggregateSHA256(pkg.Artifacts) {
		return errors.New("invalid package aggregate SHA-256")
	}
	return nil
}

func validateContents(artifacts []Artifact) error {
	var plugin pluginManifest
	if err := decodeStrictJSON(artifacts[0].Bytes, &plugin); err != nil {
		return fmt.Errorf("decode plugin manifest: %w", err)
	}
	if plugin.Name != "vgxness" || plugin.Description != "VGXNESS project memory and SDD lifecycle MCP." || plugin.MCPServers != "./.mcp.json" || !releaseVersion.MatchString("v"+plugin.Version) {
		return errors.New("invalid plugin manifest")
	}
	var servers map[string]mcpServer
	if err := decodeStrictJSON(artifacts[1].Bytes, &servers); err != nil {
		return fmt.Errorf("decode MCP configuration: %w", err)
	}
	server, ok := servers["vgxness"]
	if len(servers) != 1 || !ok || server.Command != "vgxness" || len(server.Args) != 2 || server.Args[0] != "mcp" || server.Args[1] != "--full" {
		return errors.New("invalid MCP configuration")
	}
	return nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
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
	result := Package{Artifacts: make([]Artifact, len(source.Artifacts)), SHA256: source.SHA256}
	for index, artifact := range source.Artifacts {
		result.Artifacts[index] = Artifact{Path: artifact.Path, Bytes: append([]byte(nil), artifact.Bytes...)}
	}
	sort.Slice(result.Artifacts, func(i, j int) bool { return result.Artifacts[i].Path < result.Artifacts[j].Path })
	return result
}

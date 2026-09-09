package e2e_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/providers/pi"
	"github.com/vgxness/vgxness/internal/release"
)

// Exercises the portable package on this host; native target claims need their own runners.
func TestPiExtractedNativePackageJourney(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "release")
	if err := release.PackagePi(context.Background(), root, output); err != nil {
		t.Fatal(err)
	}
	workspace, extensionStorage, agent := t.TempDir(), t.TempDir(), t.TempDir()
	installed, err := pi.Install(context.Background(), pi.Options{ReleaseDir: output, AgentDir: agent, InstallRoot: filepath.Join(agent, "vgxness-managed")})
	if err != nil || !installed.Changed {
		t.Fatalf("install=%+v err=%v", installed, err)
	}
	settings, err := os.ReadFile(filepath.Join(agent, "settings.json"))
	var discovery struct {
		Packages []string `json:"packages"`
	}
	if err != nil || json.Unmarshal(settings, &discovery) != nil || len(discovery.Packages) != 1 || discovery.Packages[0] != installed.PackagePath {
		t.Fatalf("settings discovery entry=%q err=%v", settings, err)
	}
	userData := filepath.Join(workspace, "user-data.txt")
	if err := os.WriteFile(userData, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	checkProbe := exec.Command(node, filepath.Join(installed.PackagePath, "src", "probe.ts"))
	checkProbe.Env = []string{"HOME=" + t.TempDir(), "PATH=" + filepath.Dir(node), "TMPDIR=" + t.TempDir(), "SystemRoot=" + os.Getenv("SystemRoot")}
	if data, err := checkProbe.CombinedOutput(); err != nil || !strings.Contains(string(data), `"runtime":"typescript"`) {
		t.Fatalf("isolated portable probe: %v: %s", err, data)
	}
	if _, err := os.Stat(filepath.Join(installed.PackagePath, "node_modules")); !os.IsNotExist(err) {
		t.Fatal("package contains runtime dependency sidecars")
	}
	loader, err := piSDKLoader()
	if err != nil {
		t.Skip(err)
	}
	t.Logf("resolved Pi SDK loader: %s", loader)
	// Load through the real Pi SDK and use only temporary local storage.
	resourceLoader := filepath.Join(filepath.Dir(filepath.Dir(loader)), "resource-loader.js")
	script := `const {pathToFileURL}=await import("node:url"); const {DefaultResourceLoader}=await import(pathToFileURL(process.argv[1]).href); const [workspace,agent]=process.argv.slice(2); const resources=new DefaultResourceLoader({cwd:workspace,agentDir:agent,noSkills:true,noPromptTemplates:true,noThemes:true,noContextFiles:true}); await resources.reload(); const v=resources.getExtensions(); if(v.errors.length||v.extensions.length!==1) throw new Error(JSON.stringify(v)); const handlers=v.extensions[0].handlers; const context={sessionManager:{getSessionId:()=>"installed-session"}}; for(const handler of handlers.get("session_start")??[]) await handler({reason:"startup"},context); const tools=[...v.extensions[0].tools.values()].map(x=>x.definition); const save=tools.find(x=>x.name==="memory_save"), get=tools.find(x=>x.name==="memory_get"); if(!save||!get) throw new Error("settings package was not discovered"); try { const saved=JSON.parse((await save.execute("save",{title:"fixture",content:"installed TypeScript write"})).content[0].text); const id=saved.id??saved.ID; if(typeof id!=="string") throw new Error("TypeScript memory save returned no id"); const loaded=JSON.parse((await get.execute("get",{id})).content[0].text); if((loaded.content??loaded.Content)!=="installed TypeScript write") throw new Error("TypeScript memory readback mismatch"); } finally { for(const handler of handlers.get("session_shutdown")??[]) await handler({reason:"quit"},context); }`
	check := exec.Command(node, "--input-type=module", "-e", script, resourceLoader, workspace, agent)
	check.Dir = workspace
	check.Env = []string{"HOME=" + t.TempDir(), "PATH=" + filepath.Dir(node), "PI_CODING_AGENT_DIR=" + agent, "PI_CODING_AGENT_SESSION_DIR=" + t.TempDir(), "VGXNESS_PI_STORAGE_ROOT=" + extensionStorage}
	if data, err := check.CombinedOutput(); err != nil {
		t.Fatalf("offline SDK load: %v: %s", err, data)
	}
	if err := release.PackagePi(context.Background(), root, output); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("no-overwrite result=%v", err)
	}
	if value, err := os.ReadFile(userData); err != nil || string(value) != "preserve me" {
		t.Fatalf("user data after package removal=%q err=%v", value, err)
	}
}

func TestMain(m *testing.M) {
	if runtime.GOOS == "darwin" {
		if directory, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
			_ = os.Setenv("TMPDIR", directory)
		}
	}
	os.Exit(m.Run())
}

func piRequestLine(t *testing.T, id, operation, workspace, mode, role string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"type": "request", "id": id, "operation": operation, "workspace": workspace, "mode": mode, "role": role, "payload": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func piSDKLoader() (string, error) {
	if root := os.Getenv("PI_SDK_ROOT"); root != "" {
		candidate := filepath.Join(root, "dist", "core", "extensions", "loader.js")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		repository := filepath.Clean(filepath.Join(cwd, "..", ".."))
		candidate := filepath.Join(repository, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "core", "extensions", "loader.js")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	for _, root := range strings.Split(os.Getenv("NODE_PATH"), string(os.PathListSeparator)) {
		if root == "" {
			continue
		}
		candidate := filepath.Join(root, "@earendil-works", "pi-coding-agent", "dist", "core", "extensions", "loader.js")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	candidate := filepath.Join(os.Getenv("HOME"), ".npm-global", "lib", "node_modules", "@earendil-works", "pi-coding-agent", "dist", "core", "extensions", "loader.js")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("installed Pi SDK loader unavailable; set PI_SDK_ROOT")
}

func extractPackage(t *testing.T, archive string) string {
	t.Helper()
	root := t.TempDir()
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(header.Name, "package/") || strings.Contains(header.Name, "..") || header.Typeflag != tar.TypeReg {
			t.Fatalf("unsafe tar entry %q", header.Name)
		}
		target := filepath.Join(root, strings.TrimPrefix(header.Name, "package/"))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, reader); err != nil {
			t.Fatal(err)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

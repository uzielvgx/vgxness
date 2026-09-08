package e2e_test

import (
	"archive/tar"
	"bufio"
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

// This is deliberately an extracted-package journey. It proves the native
// Linux arm64 sidecar and the installed Pi SDK loader without npm installation
// or a provider call. Cross-built targets are checked by release assembly but
// cannot establish their target runtime behavior on this host.
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
	mainRoot := installed.PackagePath
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	backendRoot := filepath.Join(mainRoot, "node_modules", "@vgxness", "pi-backend-"+platform+"-"+arch)
	userData := filepath.Join(workspace, "user-data.txt")
	if err := os.WriteFile(userData, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := "vgxness-pi-backend"
	if runtime.GOOS == "windows" {
		backend += ".exe"
	}
	cmd := exec.Command(filepath.Join(backendRoot, "bin", backend), "--protocol", "vgxness-pi/v1", "--workspace", workspace, "--storage-root", extensionStorage, "--mode", "full", "--role", "manager")
	cmd.Dir = workspace
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "PI_CODING_AGENT_DIR=" + agent, "PI_CODING_AGENT_SESSION_DIR=" + t.TempDir(), "VGXNESS_PI_STORAGE_ROOT=" + extensionStorage}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var hello map[string]any
	if err := json.Unmarshal(line, &hello); err != nil || hello["type"] != "hello" || hello["protocol"] != "vgxness-pi/v1" {
		t.Fatalf("hello=%s err=%v", line, err)
	}
	if _, err := stdin.Write(line); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write(piRequestLine(t, "fixture", "memory.project.initialize", workspace, "full", "manager")); err != nil {
		t.Fatal(err)
	}
	result, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(result, &response); err != nil || response["type"] != "result" {
		t.Fatalf("result=%s err=%v", result, err)
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	loader, err := piSDKLoader()
	if err != nil {
		t.Skip(err)
	}
	// The extracted extension must keep local storage configuration out of the
	// closed backend wire record, execute a real native tool, and close its
	// session/backend in the same SDK lifecycle used by Pi.
	resourceLoader := filepath.Join(filepath.Dir(filepath.Dir(loader)), "resource-loader.js")
	script := `const {DefaultResourceLoader}=await import(process.argv[1]); const [workspace,agent]=process.argv.slice(2); const resources=new DefaultResourceLoader({cwd:workspace,agentDir:agent,noSkills:true,noPromptTemplates:true,noThemes:true,noContextFiles:true}); await resources.reload(); const v=resources.getExtensions(); if(v.errors.length||v.extensions.length!==1) throw new Error(JSON.stringify(v)); const handlers=v.extensions[0].handlers; const context={sessionManager:{getSessionId:()=>"installed-session"}}; const tools=[...v.extensions[0].tools.values()].map(x=>x.definition); const save=tools.find(x=>x.name==="memory_save"), get=tools.find(x=>x.name==="memory_get"); if(!save||!get) throw new Error("settings package was not discovered"); try { const saved=JSON.parse((await save.execute("save",{title:"fixture",content:"installed native write"})).content[0].text); const id=saved.id??saved.ID; if(typeof id!=="string") throw new Error("native memory save returned no id"); const loaded=JSON.parse((await get.execute("get",{id})).content[0].text); if((loaded.content??loaded.Content)!=="installed native write") throw new Error("native memory readback mismatch"); } finally { for(const handler of handlers.get("session_shutdown")??[]) await handler({reason:"quit"},context); }`
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
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

//go:build e2e && codex_e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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

	codexprovider "github.com/vgxness/vgxness/internal/providers/codex"
)

const maxRuntimeOutput = 1 << 20

var errRuntimeOutputTooLarge = errors.New("runtime output exceeds limit")

func TestCodexDelegationRuntime(t *testing.T) {
	if os.Getenv("VGXNESS_CODEX_E2E") != "1" {
		t.Skip("set VGXNESS_CODEX_E2E=1 to run authenticated Codex delegation cases")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Codex runtime cases require a supported local Codex CLI")
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("Codex CLI is unavailable")
	}
	preflight(t, codex)
	verifyAmbientProfiles(t)
	cases := []struct{ name, prompt, marker string }{
		{"explore", "Delegate this inspection to Explore as a fresh specialist task. Do not use a full-history fork. Return exactly EXPLORE: disposable Codex delegation fixture.", "EXPLORE: disposable Codex delegation fixture"},
		{"general", "Delegate this change to General as a fresh specialist task. Do not use a full-history fork. Create only owned.txt containing exactly general-owned, then return exactly GENERAL: general-owned.", "GENERAL: general-owned"},
		{"verifier", "Delegate to Verifier as a fresh specialist task. Do not use a full-history fork. Validate frozen candidate digest test-digest, changedPaths [README.md], diffScope README.md, acceptanceCriteria title present; return exactly VERIFIER-PASS if valid.", "VERIFIER-PASS"},
		{"reviewer", "Delegate to Reliability as a fresh specialist task. Do not use a full-history fork. Review frozen candidate with Review Binding candidateDigest test-digest, exact changedPaths [README.md], diffScope README.md, acceptanceCriteria title present; return exactly REVIEWER-BOUND.", "REVIEWER-BOUND"},
		{"refuter", "Delegate to Refuter as a fresh specialist task. Do not use a full-history fork. Evaluate supplied severe finding F-1 against frozen candidate with Review Binding candidateDigest test-digest, exact changedPaths [README.md], diffScope README.md, acceptanceCriteria title present; return exactly REFUTER-BOUND.", "REFUTER-BOUND"},
	}
	filter := os.Getenv("VGXNESS_CODEX_E2E_CASE")
	if filter != "" {
		known := false
		for _, test := range cases {
			known = known || filter == test.name
		}
		if !known {
			t.Fatalf("unknown VGXNESS_CODEX_E2E_CASE %q", filter)
		}
	}
	for _, test := range cases {
		if filter != "" && filter != test.name {
			continue
		}
		t.Run(test.name, func(t *testing.T) { runDelegationCase(t, codex, test.name, test.prompt, test.marker) })
	}
}

func preflight(t *testing.T, codex string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, codex, "login", "status")
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("Codex authentication preflight failed: %v (%d bytes)", err, len(output))
	}
}

func verifyAmbientProfiles(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := codexprovider.Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyProfiles(filepath.Join(home, ".codex"), pkg); err != nil {
		t.Fatal(err)
	}
}

func verifyProfiles(root string, pkg codexprovider.Package) error {
	for _, artifact := range pkg.Artifacts[1:] {
		path := filepath.Join(root, filepath.FromSlash(artifact.Path))
		data, metadata, err := regularFile(path, len(artifact.Bytes)+1)
		if err != nil || !bytes.Equal(data, artifact.Bytes) {
			return fmt.Errorf("profile mismatch %s (%s): %w", artifact.Path, metadata, err)
		}
	}
	return nil
}

func runDelegationCase(t *testing.T, codex, name, prompt, marker string) {
	t.Helper()
	root := t.TempDir()
	installFixture(t, root)
	before := fixtureFiles(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sandbox := "read-only"
	if name == "general" {
		sandbox = "workspace-write"
	}
	command := exec.CommandContext(ctx, codex, "exec", "--json", "--ephemeral", "-c", `approval_policy="never"`, "--sandbox", sandbox, "--skip-git-repo-check", prompt)
	command.Dir = root
	output := &boundedBuffer{limit: maxRuntimeOutput}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		t.Fatalf("Codex %s delegation failed: %v (%s)", name, err, output.metadata())
	}
	events := codexJSONEvents(t, output.Bytes())
	lower := strings.ToLower(output.String())
	if !containsJSONValue(events, "type", "collab_tool_call") || strings.Contains(lower, "full-history forked agents inherit") || strings.Contains(lower, "omit agent_type") || !strings.Contains(output.String(), marker) {
		t.Fatalf("Codex %s lacks delegation, compatibility, or result evidence (%s)", name, output.metadata())
	}
	after := fixtureFiles(t, root)
	if name == "general" {
		owned, _, err := regularFile(filepath.Join(root, "owned.txt"), 64)
		if err != nil || string(owned) != "general-owned" || len(after) != len(before)+1 {
			t.Fatalf("General did not make exactly its owned write: before=%v after=%v", before, after)
		}
		delete(after, "owned.txt")
	}
	if !sameFiles(before, after) {
		t.Fatalf("read-only %s mutated fixture: before=%v after=%v", name, before, after)
	}
}

func installFixture(t *testing.T, root string) {
	t.Helper()
	pkg, err := codexprovider.Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range pkg.Artifacts[:1] {
		if err := os.WriteFile(filepath.Join(root, artifact.Path), artifact.Bytes, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# disposable Codex delegation fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func codexJSONEvents(t *testing.T, output []byte) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) == nil {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatalf("Codex emitted no JSON events: %s", output)
	}
	return events
}

func containsJSONValue(values []map[string]any, key string, want any) bool {
	for _, value := range values {
		if containsValue(value, key, want) {
			return true
		}
	}
	return false
}
func containsValue(value any, key string, want any) bool {
	switch value := value.(type) {
	case map[string]any:
		for current, nested := range value {
			if current == key && nested == want || containsValue(nested, key, want) {
				return true
			}
		}
	case []any:
		for _, nested := range value {
			if containsValue(nested, key, want) {
				return true
			}
		}
	}
	return false
}
func fixtureFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, _, err := regularFile(path, maxRuntimeOutput)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files[strings.TrimPrefix(path, root+string(filepath.Separator))] = fmt.Sprintf("size=%d sha256=%x", len(data), sum)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func regularFile(path string, limit int) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "unavailable", err
	}
	metadata := fmt.Sprintf("size=%d mode=%s", info.Size(), info.Mode().Type())
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, metadata, fmt.Errorf("unsafe file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, metadata, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, metadata, fmt.Errorf("bounded read failed")
	}
	return data, metadata, nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.overflow || b.Len()+len(data) > b.limit {
		b.overflow = true
		return 0, errRuntimeOutputTooLarge
	}
	return b.Buffer.Write(data)
}
func (b *boundedBuffer) metadata() string {
	sum := sha256.Sum256(b.Bytes())
	return fmt.Sprintf("bytes=%d sha256=%x overflow=%t", b.Len(), sum, b.overflow)
}
func TestRuntimeInventoryRejectsSymlinkAndOutputOverflow(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := regularFile(filepath.Join(root, "link"), 16); err == nil {
		t.Fatal("accepted symlink")
	}
	b := &boundedBuffer{limit: 2}
	if _, err := b.Write([]byte("abc")); !errors.Is(err, errRuntimeOutputTooLarge) || !b.overflow {
		t.Fatal("bounded output did not fail")
	}
}
func TestRuntimeProfilePreflight(t *testing.T) {
	pkg, err := codexprovider.Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := verifyProfiles(root, pkg); err == nil {
		t.Fatal("accepted missing profiles")
	}
	for _, artifact := range pkg.Artifacts[1:] {
		path := filepath.Join(root, filepath.FromSlash(artifact.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, artifact.Bytes, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyProfiles(root, pkg); err != nil {
		t.Fatal(err)
	}
}
func sameFiles(before, after map[string]string) bool {
	if len(before) != len(after) {
		return false
	}
	for path, content := range before {
		if after[path] != content {
			return false
		}
	}
	return true
}

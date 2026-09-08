package e2e_test

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPiProtocolCLI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "vgxness-pi-backend")
	build := exec.Command("go", "build", "-o", bin, "./cmd/vgxness-pi-backend")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	workspace, storage := t.TempDir(), t.TempDir()
	cmd := exec.Command(bin, "--protocol", "vgxness-pi/v1", "--workspace", workspace, "--storage-root", storage, "--mode", "full", "--role", "manager")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(out)
	hello, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var observed map[string]any
	if err := json.Unmarshal(hello, &observed); err != nil || observed["type"] != "hello" {
		t.Fatalf("hello=%s err=%v", hello, err)
	}
	if _, err := in.Write(hello); err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"type":"request","id":"cli","operation":"memory.project.initialize","workspace":"` + workspace + `","mode":"full","role":"manager","payload":{}}` + "\n")
	if _, err := in.Write(request); err != nil {
		t.Fatal(err)
	}
	result, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(result, &record); err != nil || record["type"] != "result" || record["id"] != "cli" {
		t.Fatalf("result=%s err=%v", result, err)
	}
}

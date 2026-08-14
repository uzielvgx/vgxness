package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestSkillRegistryListStatusAndRefresh(t *testing.T) {
	workspace := t.TempDir()
	path := workspace + "/.agents/skills/one/SKILL.md"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: one\n---"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, command := range []string{"list", "status", "refresh"} {
		var out, stderr bytes.Buffer
		if code := RunSkillRegistry(context.Background(), []string{command}, &out, &stderr, workspace); code != 0 || stderr.Len() != 0 || out.Len() == 0 {
			t.Fatalf("%s: code=%d stdout=%q stderr=%q", command, code, out.String(), stderr.String())
		}
	}
	var status, statusErr bytes.Buffer
	if code := RunSkillRegistry(context.Background(), []string{"status"}, &status, &statusErr, workspace); code != 0 || !strings.Contains(status.String(), "status=") || !strings.Contains(status.String(), "candidates=1") || !strings.Contains(status.String(), "from_cache=") || !strings.Contains(status.String(), "root path=") {
		t.Fatalf("status: code=%d stdout=%q stderr=%q", code, status.String(), statusErr.String())
	}
	var out, stderr bytes.Buffer
	if code := RunSkillRegistry(context.Background(), []string{"status", "--host", "invalid"}, &out, &stderr, workspace); code != 2 || !strings.HasPrefix(stderr.String(), "usage:") {
		t.Fatalf("invalid host: code=%d stderr=%q", code, stderr.String())
	}
	if code := RunSkillRegistry(context.Background(), []string{"list", "extra"}, &out, &stderr, workspace); code != 2 {
		t.Fatalf("extra arg code=%d", code)
	}
}

func TestSkillRegistryHostPolicy(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(workspace, ".opencode", "skills", "host", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("host"), 0644); err != nil {
		t.Fatal(err)
	}
	var common, opencode, stderr bytes.Buffer
	if RunSkillRegistry(context.Background(), []string{"list", "--host", "common"}, &common, &stderr, workspace) != 0 || RunSkillRegistry(context.Background(), []string{"list", "--host", "opencode"}, &opencode, &stderr, workspace) != 0 || common.Len() != 0 || !strings.Contains(opencode.String(), "name=host") {
		t.Fatalf("common=%q opencode=%q stderr=%q", common.String(), opencode.String(), stderr.String())
	}
}

func TestSkillRegistryListEscapesC1Controls(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".agents", "skills", "one", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: one\u009bcontrol\n---"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var out, stderr bytes.Buffer
	if code := RunSkillRegistry(context.Background(), []string{"list"}, &out, &stderr, workspace); code != 0 || strings.ContainsRune(out.String(), '\u009b') || !strings.Contains(out.String(), `\u009b`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}

type fakeInspector struct {
	result inspection.Result
	err    error
	calls  int
}

func runBasicCLI(ctx context.Context, args []string, stdout, stderr *bytes.Buffer, inspector Inspector) int {
	return RunProductSDDRuntime(ctx, args, strings.NewReader(""), stdout, stderr, inspector, nil, nil, nil, nil, nil, nil)
}

func (f *fakeInspector) Status(context.Context, config.Options) (inspection.Result, error) {
	f.calls++
	return f.result, f.err
}
func (f *fakeInspector) Doctor(context.Context, config.Options) (inspection.Result, error) {
	f.calls++
	return f.result, f.err
}

func TestCLI_StatusDoctorHealthy(t *testing.T) {
	for _, command := range []string{"status", "doctor"} {
		t.Run(command, func(t *testing.T) {
			f := &fakeInspector{result: inspection.Result{Root: "/tmp/root", Database: "/tmp/root/memory.db", Migration: 1}}
			var out, stderr bytes.Buffer
			code := runBasicCLI(context.Background(), []string{command, "--storage-root", "/tmp/root"}, &out, &stderr, f)
			testutil.Require(t, code == 0, "exit=%d stderr=%s", code, stderr.String())
			testutil.Require(t, strings.Contains(out.String(), "migration=1") && !strings.Contains(out.String(), "chronicle") && stderr.Len() == 0, "unexpected output: %q / %q", out.String(), stderr.String())
		})
	}
}

func TestCLI_RejectsUnsupportedExportDuringWriteWithoutMutation(t *testing.T) {
	f := &fakeInspector{}
	var out, stderr bytes.Buffer
	code := runBasicCLI(context.Background(), []string{"export"}, &out, &stderr, f)
	testutil.Require(t, code == 2 && f.calls == 0 && out.Len() == 0, "exit=%d calls=%d out=%q", code, f.calls, out.String())
}

func TestCLI_RejectsV1NonGoalCommands(t *testing.T) {
	for _, command := range []string{"tui", "agent", "backup", "restore", "sync", "bridge", "delivery", "edit", "maintenance", "orchestrate"} {
		var stderr bytes.Buffer
		code := runBasicCLI(context.Background(), []string{command}, &bytes.Buffer{}, &stderr, &fakeInspector{})
		testutil.Require(t, code == 2 && strings.HasPrefix(stderr.String(), "usage: vgxness "), "%s exit=%d stderr=%q", command, code, stderr.String())
	}
}

func TestCLI_CancellationHasNoPartialOutput(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runBasicCLI(context.Background(), []string{"status"}, &out, &stderr, &fakeInspector{err: context.Canceled})
	testutil.Require(t, code == 130 && out.Len() == 0 && errors.Is(context.Canceled, context.Canceled), "exit=%d out=%q", code, out.String())
}

func TestCLI_EscapesDynamicControlCharacters(t *testing.T) {
	controls := "line\ncarriage\rtab\tescape\x1bdel\x7f"
	for _, command := range []string{"status", "doctor"} {
		t.Run(command, func(t *testing.T) {
			result := inspection.Result{Root: "/root/" + controls, Database: "/db/" + controls, Migration: 1}
			var out, stderr bytes.Buffer
			code := runBasicCLI(context.Background(), []string{command}, &out, &stderr, &fakeInspector{result: result})
			testutil.Require(t, code == 0 && stderr.Len() == 0, "exit=%d stderr=%q", code, stderr.String())
			for _, r := range out.String() {
				if (r < ' ' && r != '\n') || r == 0x7f {
					t.Fatalf("unsafe control %U in %q", r, out.String())
				}
			}
			testutil.Require(t, strings.Contains(out.String(), `\n`) && strings.Contains(out.String(), `\x1b`), "controls were not escaped: %q", out.String())
		})
	}
}

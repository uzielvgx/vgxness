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
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/testutil"
)

type fakeMemoryRuntime struct {
	result memory.MemoryResult
	items  []memory.MemoryResult
	err    error
	calls  int
}

func (f *fakeMemoryRuntime) Save(context.Context, config.Options, memory.SaveRequest) (memory.MemoryResult, error) {
	f.calls++
	return f.result, f.err
}
func (f *fakeMemoryRuntime) Search(context.Context, config.Options, memory.SearchRequest) ([]memory.MemoryResult, error) {
	f.calls++
	return f.items, f.err
}
func (f *fakeMemoryRuntime) Get(context.Context, config.Options, memory.GetRequest) (memory.MemoryResult, error) {
	f.calls++
	return f.result, f.err
}

func runMemoryTest(args []string, input string, runtime MemoryRuntime) (int, string, string) {
	var out, stderr bytes.Buffer
	code := RunIO(context.Background(), args, strings.NewReader(input), &out, &stderr, &fakeInspector{}, runtime)
	return code, out.String(), stderr.String()
}

func TestMemoryCLI_StrictSingleSourceInput(t *testing.T) {
	valid := `{"schemaVersion":1,"content":"body"}`
	file := filepath.Join(t.TempDir(), "request.json")
	testutil.NoError(t, os.WriteFile(file, []byte(valid), 0o600))
	link := filepath.Join(t.TempDir(), "request.json")
	testutil.NoError(t, os.Symlink(file, link))
	cases := []struct {
		name  string
		args  []string
		input string
	}{
		{"valid stdin", []string{"memory", "save", "--stdin"}, valid},
		{"unknown", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","unknown":1}`},
		{"duplicate", []string{"memory", "save", "--stdin", "--project", "p"}, `{"schemaVersion":1,"content":"x","project":"p"}`},
		{"duplicate empty project", []string{"memory", "save", "--stdin", "--project", "p"}, `{"schemaVersion":1,"content":"x","project":""}`},
		{"duplicate zero limit", []string{"memory", "search", "--stdin", "--limit", "10"}, `{"schemaVersion":1,"query":"x","project":"p","scope":"project","limit":0}`},
		{"duplicate JSON key", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","content":"y"}`},
		{"caller producer", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","producer":"agent"}`},
		{"caller source", []string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x","sourceProvider":"chronicle","sourceId":"event-1"}`},
		{"malformed", []string{"memory", "save", "--stdin"}, `{`},
		{"oversized", []string{"memory", "save", "--stdin"}, strings.Repeat("x", 65538)},
		{"symlink", []string{"memory", "save", "--input", link}, ""},
		{"both", []string{"memory", "save", "--stdin", "--input", file}, valid},
		{"neither", []string{"memory", "save"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &fakeMemoryRuntime{}
			code, out, _ := runMemoryTest(tc.args, tc.input, runtime)
			validCase := tc.name == "valid stdin"
			testutil.Require(t, code == map[bool]int{true: 0, false: 2}[validCase] && out == "" == !validCase && runtime.calls == map[bool]int{true: 1}[validCase], "exit=%d calls=%d out=%q", code, runtime.calls, out)
		})
	}
}

func TestMemoryCLI_RenderStableSafeAndAtomicOutput(t *testing.T) {
	runtime := &fakeMemoryRuntime{result: memory.MemoryResult{ID: "id", Title: "line\n\x1b", Content: "body"}}
	code, out, stderr := runMemoryTest([]string{"memory", "get", "--stdin", "--json"}, `{"schemaVersion":1,"id":"id","project":"p","scope":"project"}`, runtime)
	testutil.Require(t, code == 0 && strings.Contains(out, `"schemaVersion":1`) && strings.Contains(out, `"ID":"id"`) && stderr == "", "json=%q stderr=%q", out, stderr)
	code, out, _ = runMemoryTest([]string{"memory", "get", "--stdin"}, `{"schemaVersion":1,"id":"id","project":"p","scope":"project"}`, runtime)
	testutil.Require(t, code == 0 && strings.Contains(out, `line\n\x1b`), "unsafe human output: %q", out)
	runtime.err = errors.New("secret /path SELECT")
	code, out, stderr = runMemoryTest([]string{"memory", "get", "--stdin"}, `{"schemaVersion":1,"id":"id","project":"p","scope":"project"}`, runtime)
	testutil.Require(t, code == 1 && out == "" && !strings.Contains(stderr, "secret"), "unsafe failure: %d %q %q", code, out, stderr)
}

func TestMemoryCLI_ClassifiedErrorsAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{memory.ErrInvalid, 2}, {memory.ErrConflict, 1}, {memory.ErrNotFound, 1}, {memory.ErrCorrupt, 1}, {context.Canceled, 130}, {context.DeadlineExceeded, 130}} {
		runtime := &fakeMemoryRuntime{err: tc.err}
		code, out, _ := runMemoryTest([]string{"memory", "save", "--stdin"}, `{"schemaVersion":1,"content":"x"}`, runtime)
		testutil.Require(t, code == tc.code && out == "", "error=%v exit=%d out=%q", tc.err, code, out)
	}
}

func TestMemoryCLI_UnsupportedOperationsNeverOpenOrMutateStorage(t *testing.T) {
	for _, verb := range []string{"list", "review", "delete", "relations", "sync"} {
		runtime := &fakeMemoryRuntime{}
		code, out, _ := runMemoryTest([]string{"memory", verb, "--stdin"}, `{}`, runtime)
		testutil.Require(t, code == 2 && runtime.calls == 0 && out == "", "%s exit=%d calls=%d", verb, code, runtime.calls)
	}
}

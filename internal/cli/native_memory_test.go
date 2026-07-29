package cli

import (
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/memory"
)

func TestMemoryCLIForgetUsesStrictVersionedBoundary(t *testing.T) {
	runtime := &fakeMemoryRuntime{result: memory.Entry{ID: "entry", State: memory.StateArchived}}
	code, out, stderr := runMemoryTest([]string{"memory", "forget", "--stdin", "--json"}, `{"schemaVersion":1,"id":"entry","project":"p","scope":"project"}`, runtime)
	if code != 0 || runtime.calls != 1 || !strings.Contains(out, `"State":"archived"`) || stderr != "" {
		t.Fatalf("forget: code=%d calls=%d out=%q stderr=%q", code, runtime.calls, out, stderr)
	}
	code, out, _ = runMemoryTest([]string{"memory", "forget", "--stdin"}, `{"schemaVersion":2,"id":"entry","project":"p","scope":"project"}`, runtime)
	if code != 2 || out != "" || runtime.calls != 1 {
		t.Fatalf("unversioned forget reached runtime: code=%d calls=%d out=%q", code, runtime.calls, out)
	}
}

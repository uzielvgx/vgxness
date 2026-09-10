package pi

import (
	"path/filepath"
	"testing"
)

func TestAuthorizeCanonicalWorkspaceAliases(t *testing.T) {
	workspace := t.TempDir()
	canonical, _, err := CanonicalWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{Workspace: canonical, Mode: ReadOnly, Role: "general"}
	alias := workspace + string(filepath.Separator) + "."
	if alias == canonical {
		t.Fatal("canonical alias was cleaned before authorization")
	}
	request := Request{Workspace: alias, Mode: ReadOnly, Role: "general", Operation: "memory.recall"}
	if err := binding.Authorize(request); err != nil {
		t.Fatal(err)
	}
	request.Workspace = t.TempDir()
	if err := binding.Authorize(request); err == nil {
		t.Fatal("other workspace accepted")
	}
	request.Workspace = ""
	if err := binding.Authorize(request); err == nil {
		t.Fatal("empty workspace accepted")
	}
	request.Workspace = "."
	if err := binding.Authorize(request); err == nil {
		t.Fatal("relative workspace accepted")
	}
	request.Workspace = alias
	request.Mode = Full
	if err := binding.Authorize(request); err == nil {
		t.Fatal("mode mismatch accepted")
	}
	request.Mode, request.Role = ReadOnly, "manager"
	if err := binding.Authorize(request); err == nil {
		t.Fatal("role mismatch accepted")
	}
}

func TestDecodeRejectsInvalidRecords(t *testing.T) {
	for _, input := range []string{
		`{"type":"request","id":"x","operation":"read"}` + "\r\n",
		`{"type":"request","id":"x","operation":"read"}`,
		`{"type":"request","id":"x","unknown":true}` + "\n",
		`{"type":"request","id":"x","id":"y"}` + "\n",
	} {
		if _, err := DecodeRecord([]byte(input)); err == nil {
			t.Fatalf("DecodeRecord(%q) accepted invalid input", input)
		}
	}
	if _, err := DecodeRecord([]byte(`{"type":"request","id":"x","operation":"memory.recall","workspace":"/workspace","mode":"read-only","role":"general"} {}` + "\n")); err == nil {
		t.Fatal("DecodeRecord accepted a trailing JSON value")
	}
}

func TestAuthorizeBindsWorkspaceModeAndRole(t *testing.T) {
	binding := Binding{Workspace: "/workspace", Mode: ReadOnly, Role: "general"}
	if err := binding.Authorize(Request{Workspace: "/workspace", Mode: ReadOnly, Role: "general", Operation: "memory.recall"}); err != nil {
		t.Fatal(err)
	}
	if err := binding.Authorize(Request{Workspace: "/workspace", Mode: ReadOnly, Role: "general", Operation: "memory.remember"}); err == nil {
		t.Fatal("read-only mutation was authorized")
	}
	if err := binding.Authorize(Request{Workspace: "/workspace", Mode: ReadOnly, Role: "general", Operation: "memory.recallAndMutate"}); err == nil {
		t.Fatal("unknown operation was authorized")
	}
	if err := binding.Authorize(Request{Workspace: "/other", Mode: ReadOnly, Role: "general", Operation: "memory.recall"}); err == nil {
		t.Fatal("workspace mismatch was authorized")
	}
}

func TestReplayAcceptsCorrelationIDOnce(t *testing.T) {
	r := NewReplay(2)
	if !r.Accept("one") || r.Accept("one") || !r.Accept("two") || r.Accept("three") {
		t.Fatal("replay limit was not enforced")
	}
}

func TestProtocolRejectsMalformedUTF8BeforeAuthorization(t *testing.T) {
	input := []byte(`{"type":"request","id":"x","operation":"memory.recall","workspace":"/workspace","mode":"read-only","role":"general"}` + "\n")
	input[12] = 0xff
	if _, err := DecodeRecord(input); err == nil {
		t.Fatal("malformed UTF-8 was accepted")
	}
}

func TestDecodeRejectsClosedSchemaEscapes(t *testing.T) {
	for _, input := range [][]byte{
		[]byte(`{"type":"request","id":"x","operation":"memory.recall","workspace":"/workspace","mode":"read-only","role":"general","unknown":{}}` + "\n"),
		[]byte(`{"type":"request","id":"x","operation":"memory.recall","workspace":"/workspace","mode":"read-only","role":"general","nested":{"id":"a","id":"b"}}` + "\n"),
	} {
		if _, err := DecodeRecord(input); err == nil {
			t.Fatalf("DecodeRecord accepted closed-schema escape %q", input)
		}
	}
}

func TestDecodeRejectsNullPayloadAndWorkerRolesRemainReadOnly(t *testing.T) {
	if _, err := DecodeRecord([]byte(`{"type":"request","id":"x","operation":"memory.recall","workspace":"/workspace","mode":"read-only","role":"explore","payload":null}` + "\n")); err == nil {
		t.Fatal("null payload accepted")
	}
	for _, role := range []string{"explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger"} {
		binding := Binding{Workspace: "/workspace", Mode: ReadOnly, Role: role}
		if err := binding.Authorize(Request{Workspace: "/workspace", Mode: ReadOnly, Role: role, Operation: "memory.recall"}); err != nil {
			t.Fatalf("read worker %s denied: %v", role, err)
		}
		if err := binding.Authorize(Request{Workspace: "/workspace", Mode: ReadOnly, Role: role, Operation: "memory.remember"}); err == nil {
			t.Fatalf("worker %s mutated", role)
		}
	}
}

func TestAuthorizeRejectsUnknownFullModeOperation(t *testing.T) {
	binding := Binding{Workspace: "/workspace", Mode: Full, Role: "general"}
	if err := binding.Authorize(Request{Workspace: "/workspace", Mode: Full, Role: "general", Operation: "unknown.mutate"}); err == nil {
		t.Fatal("unknown full-mode operation was authorized")
	}
}

func TestAuthorizeAllowsManagerMemoryButDeniesReaderMutations(t *testing.T) {
	for _, operation := range []string{"memory.remember", "memory.session.start"} {
		if err := (Binding{Workspace: "/workspace", Mode: Full, Role: "manager"}).Authorize(Request{Workspace: "/workspace", Mode: Full, Role: "manager", Operation: operation}); err != nil {
			t.Fatalf("manager %s: %v", operation, err)
		}
		if err := (Binding{Workspace: "/workspace", Mode: Full, Role: "general"}).Authorize(Request{Workspace: "/workspace", Mode: Full, Role: "general", Operation: operation}); err == nil {
			t.Fatalf("reader role authorized mutation %s", operation)
		}
	}
}

func TestReplayRejectsInvalidCorrelationIDs(t *testing.T) {
	replay := NewReplay(2)
	if replay.Accept("\x1f") {
		t.Fatal("non-printable correlation ID was accepted")
	}
}

func TestDecodeRejectsUnboundedCorrelationID(t *testing.T) {
	input := `{"type":"request","id":"` + string(make([]byte, 129)) + `","operation":"memory.recall","workspace":"/workspace","mode":"read-only","role":"general","payload":{}}` + "\n"
	if _, err := DecodeRecord([]byte(input)); err == nil {
		t.Fatal("DecodeRecord accepted an oversized correlation ID")
	}
}

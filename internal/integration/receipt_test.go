package integration

import (
	"bytes"
	"strings"
	"testing"
)

func TestReceiptIsCanonicalAndBoundToManagedBytes(t *testing.T) {
	files := map[string][]byte{"AGENTS.md": []byte("current manager"), "agents/general.toml": []byte("current worker")}
	data, err := EncodeReceipt("codex", strings.Repeat("a", 64), "medium", files)
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeReceipt(data, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Matches("AGENTS.md", files["AGENTS.md"]) || r.Matches("AGENTS.md", []byte("modified")) || r.Matches("config.toml", []byte("foreign")) {
		t.Fatal("receipt did not bind bytes and paths")
	}
	for _, bad := range [][]byte{bytes.Replace(data, []byte(`"schema":1`), []byte(`"schema":1,"schema":1`), 1), bytes.Replace(data, []byte(`"schema":1`), []byte(`"schema":2`), 1), append(data, []byte(`{}`)...)} {
		if _, err := DecodeReceipt(bad, "codex"); err == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	if _, err := DecodeReceipt(data, "opencode"); err == nil {
		t.Fatal("cross-provider receipt accepted")
	}
	for _, p := range []string{"../config.toml", "/tmp/foreign", "agents/../config.toml", "agents/foreign.toml", "config.toml", "C:/foreign"} {
		if _, err := EncodeReceipt("codex", strings.Repeat("a", 64), "medium", map[string][]byte{p: []byte("foreign")}); err == nil {
			t.Fatalf("unowned path accepted: %s", p)
		}
	}
}

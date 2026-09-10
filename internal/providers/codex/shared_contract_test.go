package codex

import (
	"encoding/json"

	"github.com/vgxness/vgxness/internal/orchestration"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestSharedCurrentCodexProjection(t *testing.T) {
	c, e := orchestration.LoadManagerContract()
	if e != nil {
		t.Fatal(e)
	}
	p, e := Render("v0.0.0")
	if e != nil {
		t.Fatal(e)
	}
	files := map[string]string{}
	for _, a := range p.Artifacts {
		files[a.Path] = string(a.Bytes)
	}
	if !strings.Contains(files["AGENTS.md"], c.RenderManagerSections()) {
		t.Fatal("current Manager does not use shared policy")
	}
	for _, r := range c.Roles {
		value := files["agents/"+r.ID+".toml"]
		var instructions string
		for _, line := range strings.Split(value, "\n") {
			if strings.HasPrefix(line, "developer_instructions = ") {
				instructions, e = strconv.Unquote(strings.TrimPrefix(line, "developer_instructions = "))
				if e != nil {
					t.Fatal(e)
				}
			}
		}
		if !strings.Contains(instructions, r.Instructions) {
			t.Errorf("role %s not shared", r.ID)
		}
	}
}

func TestNativeSharedDevelopmentScenarios(t *testing.T) {
	raw, e := os.ReadFile("../../orchestration/testdata/manager-scenarios.json")
	if e != nil {
		t.Fatal(e)
	}
	var corpus struct {
		Cases []struct{ ID, Fragment string }
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	p, e := Render("v0.0.0")
	if e != nil {
		t.Fatal(e)
	}
	text := ""
	for _, a := range p.Artifacts {
		if a.Path == "AGENTS.md" {
			text = string(a.Bytes)
		}
	}
	if len(corpus.Cases) != 11 {
		t.Fatal("missing scenarios")
	}
	for _, scenario := range corpus.Cases {
		if !strings.Contains(text, scenario.Fragment) {
			t.Errorf("native projection lacks scenario %s", scenario.ID)
		}
	}
}

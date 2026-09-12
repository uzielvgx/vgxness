package agentmodels

import "testing"

func TestSelectionModes(t *testing.T) {
	c, err := Single("anthropic/claude-sonnet", "off")
	if err != nil || len(c.Assignments) != 7 {
		t.Fatalf("%+v %v", c, err)
	}
	for _, a := range c.Assignments {
		if a.Model != "anthropic/claude-sonnet" {
			t.Fatal(a)
		}
	}
	c.Assignments["explore"] = Assignment{Model: "other/model", Effort: "off"}
	if c.Validate() == nil {
		t.Fatal("single mode accepted mixed assignments")
	}
	c.Mode = "per-agent"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	delete(c.Assignments, "manager")
	if c.Validate() == nil {
		t.Fatal("missing Manager accepted")
	}
}

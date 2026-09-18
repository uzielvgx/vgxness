package agentmodels

import (
	"strings"
	"testing"
)

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

func TestExactVariantTokensAreOptionalAndBounded(t *testing.T) {
	c, err := Single("acme/model", "off")
	if err != nil {
		t.Fatal(err)
	}
	c.Mode = "per-agent"
	manager := Assignment{Model: "acme/model", Effort: "off", Variant: "max"}
	c.Assignments["manager"] = manager
	if err := c.Validate(); err != nil {
		t.Fatalf("bounded variant rejected: %v", err)
	}
	c.Assignments["manager"] = Assignment{Model: "acme/model", Effort: "off", Variant: "max!"}
	if c.Validate() == nil {
		t.Fatal("unsafe variant accepted")
	}
	c.Assignments["manager"] = Assignment{Model: "acme/model", Effort: "off", Variant: strings.Repeat("v", 65)}
	if c.Validate() == nil {
		t.Fatal("oversized variant accepted")
	}
	c.Assignments["manager"] = Assignment{Model: "acme/model", Effort: "off", Variant: "xhigh_2"}
	if err := c.Validate(); err != nil {
		t.Fatalf("underscore variant rejected: %v", err)
	}
}

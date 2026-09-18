// Package agentmodels describes explicit model choices, independently of plans.
package agentmodels

import (
	"fmt"
	"github.com/vgxness/vgxness/internal/modelcatalog"
)

var Roles = [...]string{"manager", "explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger"}

type Assignment struct {
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Variant string `json:"variant,omitempty"`
}
type Config struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Mode          string                `json:"mode"`
	Assignments   map[string]Assignment `json:"assignments"`
}

func Single(model, effort string) (Config, error) {
	c := Config{SchemaVersion: 1, Mode: "single", Assignments: map[string]Assignment{}}
	for _, role := range Roles {
		c.Assignments[role] = Assignment{Model: model, Effort: effort}
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 || (c.Mode != "single" && c.Mode != "per-agent") || len(c.Assignments) != len(Roles) {
		return fmt.Errorf("invalid model selection: choose single or all seven per-agent assignments")
	}
	for _, role := range Roles {
		a, exists := c.Assignments[role]
		_, valid := modelcatalog.ValidReference(a.Model)
		if !exists || !valid || len(a.Model) > 385 {
			return fmt.Errorf("invalid model reference for %s", role)
		}
		switch a.Effort {
		case "off", "minimal", "low", "medium", "high", "xhigh":
		default:
			return fmt.Errorf("invalid model effort for %s", role)
		}
		// Variant carries the exact discovery token for providers that expose
		// one. It is optional so Pi selections stay within its own contract.
		if a.Variant != "" && !validVariant(a.Variant) {
			return fmt.Errorf("invalid model variant for %s", role)
		}
		if c.Mode == "single" && a != c.Assignments["manager"] {
			return fmt.Errorf("single mode requires identical assignments")
		}
	}
	return nil
}

func validVariant(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, value := range []byte(value) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-' || value == '_' {
			continue
		}
		return false
	}
	return true
}

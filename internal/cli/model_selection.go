package cli

import (
	"flag"
	"fmt"
	"github.com/vgxness/vgxness/internal/agentmodels"
	"strings"
)

type agentModelFlags struct {
	mode, model, effort string
	agents              map[string]string
}
type agentModelValue struct{ values map[string]string }

func (v agentModelValue) String() string { return "" }
func (v agentModelValue) Set(s string) error {
	role, ref, ok := strings.Cut(s, "=")
	if !ok || v.values[role] != "" {
		return fmt.Errorf("use one ROLE=PROVIDER/MODEL per agent")
	}
	valid := false
	for _, r := range agentmodels.Roles {
		if r == role {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown agent role")
	}
	v.values[role] = ref
	return nil
}
func (m *agentModelFlags) register(f *flag.FlagSet, prefix string) {
	m.agents = map[string]string{}
	f.StringVar(&m.mode, prefix+"model-mode", "", "single or per-agent (OpenCode/Pi only)")
	f.StringVar(&m.model, prefix+"model", "", "provider/model for all agents")
	f.StringVar(&m.effort, prefix+"model-effort", "off", "off, minimal, low, medium, high or xhigh")
	f.Var(agentModelValue{m.agents}, prefix+"agent-model", "ROLE=PROVIDER/MODEL; repeat for all seven agents")
}
func (m agentModelFlags) config() (*agentmodels.Config, error) {
	if m.mode == "" && m.model == "" && len(m.agents) == 0 && m.effort == "off" {
		return nil, nil
	}
	if m.mode == "single" {
		if len(m.agents) != 0 {
			return nil, fmt.Errorf("single model cannot include per-agent overrides")
		}
		c, err := agentmodels.Single(m.model, m.effort)
		return &c, err
	}
	if m.mode != "per-agent" || m.model != "" {
		return nil, fmt.Errorf("choose --model-mode single or per-agent")
	}
	c := agentmodels.Config{SchemaVersion: 1, Mode: m.mode, Assignments: map[string]agentmodels.Assignment{}}
	for role, ref := range m.agents {
		c.Assignments[role] = agentmodels.Assignment{Model: ref, Effort: m.effort}
	}
	return &c, c.Validate()
}

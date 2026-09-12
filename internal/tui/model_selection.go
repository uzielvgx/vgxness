package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/vgxness/vgxness/internal/agentmodels"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"strings"
)

type modelChoice struct {
	Mode   string
	Rows   [7]agentmodels.Assignment
	Edited bool
}

func cloneModelChoice(c *agentmodels.Config) *agentmodels.Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Assignments = map[string]agentmodels.Assignment{}
	for k, v := range c.Assignments {
		out.Assignments[k] = v
	}
	return &out
}
func (c modelChoice) config() *agentmodels.Config {
	if !c.Edited {
		return nil
	}
	out := &agentmodels.Config{SchemaVersion: 1, Mode: c.Mode, Assignments: map[string]agentmodels.Assignment{}}
	for i, role := range agentmodels.Roles {
		a := c.Rows[i]
		if c.Mode == "single" {
			a = c.Rows[0]
		}
		out.Assignments[role] = a
	}
	return out
}
func (m *Model) seedModelChoices(plan setupflow.MultiPlan) {
	for _, p := range plan.Providers {
		if p.Provider == setupflow.ProviderCodex && !m.codexPlanEdited && p.Integration.ModelPlan != "" {
			m.setupSelected = string(p.Integration.ModelPlan)
		}
		index := 0
		if p.Provider == setupflow.ProviderPi {
			index = 1
		} else if p.Provider != setupflow.ProviderOpenCode {
			continue
		}
		if m.modelChoices[index].Edited {
			continue
		}
		c := modelChoice{Mode: "single"}
		for i := range c.Rows {
			c.Rows[i].Effort = "off"
		}
		if p.Models != nil {
			c.Mode = p.Models.Mode
			for i, r := range agentmodels.Roles {
				c.Rows[i] = p.Models.Assignments[r]
			}
		} else if p.Integration.ModelAssignments != nil {
			for i, id := range setupAgentRows {
				for _, a := range p.Integration.ModelAssignments {
					if id.ArtifactKey == a.ArtifactKey {
						effort := string(a.Variant)
						if effort == "" {
							effort = "off"
						}
						c.Rows[i] = agentmodels.Assignment{Model: a.Model, Effort: effort}
					}
				}
			}
			for _, row := range c.Rows {
				if row != c.Rows[0] {
					c.Mode = "per-agent"
				}
			}
		}
		m.modelChoices[index] = c
	}
}
func (m Model) choiceProvider() setupflow.Provider {
	if len(m.setupProviders) == 0 {
		return setupflow.ProviderOpenCode
	}
	return m.setupProviders[m.modelChoiceProvider%len(m.setupProviders)]
}
func (m *Model) invalidateModelChoice() {
	m.setupPreviewed = false
	m.setupConfirm = false
	m.setupPlan.Digest = ""
	m.modelChoiceError = ""
}
func (m *Model) updateModelChoices(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	provider := m.choiceProvider()
	index := 0
	if provider == setupflow.ProviderPi {
		index = 1
	}
	c := &m.modelChoices[index]
	if c.Mode == "" {
		c.Mode = "single"
		for i := range c.Rows {
			c.Rows[i].Effort = "off"
		}
	}
	if m.modelChoiceEditing {
		switch msg.String() {
		case "esc":
			m.modelChoiceEditing = false
		case "enter":
			row := m.modelChoiceRow
			if c.Mode == "single" {
				row = 0
			}
			c.Rows[row].Model = strings.TrimSpace(m.modelChoiceInput)
			c.Edited = true
			m.modelChoiceEditing = false
			m.invalidateModelChoice()
		case "backspace":
			r := []rune(m.modelChoiceInput)
			if len(r) > 0 {
				m.modelChoiceInput = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" && len(m.modelChoiceInput)+len(msg.Text) <= 385 {
				m.modelChoiceInput += msg.Text
			}
		}
		return true, nil
	}
	switch msg.String() {
	case "tab":
		m.modelChoiceProvider++
		m.modelChoiceRow = 0
		return true, nil
	case "esc":
		m.setupView = setupViewProviders
		return true, nil
	case "r":
		return true, m.loadSetupPlan()
	case "enter":
		for i, c := range m.modelChoices {
			p := setupflow.ProviderOpenCode
			if i == 1 {
				p = setupflow.ProviderPi
			}
			if !m.hasSetupProvider(p) {
				continue
			}
			if config := c.config(); config != nil {
				if err := config.Validate(); err != nil {
					m.modelChoiceError = err.Error()
					return true, nil
				}
			}
		}
		if m.setupPreviewed {
			m.setupView = setupViewReview
			return true, nil
		}
		return true, m.loadSetupPlan()
	}
	if provider == setupflow.ProviderCodex {
		switch msg.String() {
		case "up", "k", "left":
			return true, m.selectSetupPlan(-1)
		case "down", "j", "right":
			return true, m.selectSetupPlan(1)
		}
		return true, nil
	}
	switch msg.String() {
	case "1":
		c.Mode = "single"
		m.modelChoiceRow = 0
		c.Edited = true
		m.invalidateModelChoice()
	case "2":
		if c.Mode == "single" {
			for i := 1; i < len(c.Rows); i++ {
				if c.Rows[i].Model == "" {
					c.Rows[i] = c.Rows[0]
				}
			}
		}
		c.Mode = "per-agent"
		c.Edited = true
		m.invalidateModelChoice()
	case "up", "k":
		if c.Mode == "per-agent" {
			m.modelChoiceRow = (m.modelChoiceRow + 6) % 7
		}
	case "down", "j":
		if c.Mode == "per-agent" {
			m.modelChoiceRow = (m.modelChoiceRow + 1) % 7
		}
	case "m":
		m.modelChoiceEditing = true
		m.modelChoiceInput = c.Rows[m.modelChoiceRow].Model
	case "e":
		efforts := []string{"off", "minimal", "low", "medium", "high", "xhigh"}
		a := &c.Rows[m.modelChoiceRow]
		next := "off"
		for i, e := range efforts {
			if a.Effort == e {
				next = efforts[(i+1)%len(efforts)]
				break
			}
		}
		a.Effort = next
		c.Edited = true
		m.invalidateModelChoice()
	}
	return true, nil
}
func (m Model) multiSetupPlanLines() []string {
	provider := m.choiceProvider()
	lines := []string{"INSTALL · 2 OF 3 · MODELS", fmt.Sprintf("Provider: %s  [Tab] next provider", provider), ""}
	if provider == setupflow.ProviderCodex {
		lines = append(lines, "Codex plan: "+m.setupSelected, "[↑↓] low · medium · high · ultra")
	} else {
		index := 0
		if provider == setupflow.ProviderPi {
			index = 1
		}
		c := m.modelChoices[index]
		mode := c.Mode
		if mode == "" {
			mode = "single"
		}
		lines = append(lines, "[1] One model for all agents   [2] One model per agent", "Selected: "+mode)
		if !c.Edited {
			lines = append(lines, "Existing selections are preserved until you edit them.")
		}
		for i, r := range agentmodels.Roles {
			if mode == "single" && i > 0 {
				break
			}
			if mode == "single" {
				r = "all agents"
			}
			marker := " "
			if i == m.modelChoiceRow {
				marker = "▸"
			}
			lines = append(lines, fmt.Sprintf("%s %s: %s  effort=%s", marker, r, setupValue(c.Rows[i].Model), setupValue(c.Rows[i].Effort)))
		}
		lines = append(lines, "[m] Enter provider/model   [e] Change effort", "Availability and credentials are checked by the host when used.")
	}
	if m.modelChoiceEditing {
		lines = append(lines, "Model: "+sanitizeTerminal(m.modelChoiceInput), "[Enter] save  [Esc] cancel")
	}
	if m.modelChoiceError != "" {
		lines = append(lines, "Error: "+sanitizeTerminal(m.modelChoiceError))
	}
	if m.setupPlanErr != nil {
		lines = append(lines, "Preview failed: "+setupActionableError(m.setupPlanErr))
	}
	if m.setupPlanLoading {
		lines = append(lines, "Loading preview...")
	} else if m.setupPreviewed {
		lines = append(lines, "[Enter] review installation")
	} else {
		lines = append(lines, "[Enter] preview selections")
	}
	return lines
}

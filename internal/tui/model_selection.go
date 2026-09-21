package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/vgxness/vgxness/internal/agentmodels"
	"github.com/vgxness/vgxness/internal/modelplan"
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
		// Mode stays empty when the provider has no installed selection, so the
		// Models screen opens with the one-model/per-agent decision first.
		c := modelChoice{}
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
						c.Rows[i] = seededModelChoiceAssignment(a)
					}
				}
			}
			if c.Rows[0].Model != "" {
				c.Mode = "single"
				for _, row := range c.Rows {
					if row != c.Rows[0] {
						c.Mode = "per-agent"
						break
					}
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

func modelChoiceIndex(provider setupflow.Provider) int {
	if provider == setupflow.ProviderPi {
		return 1
	}
	return 0
}

func (m Model) modelChoiceTarget(provider setupflow.Provider) int {
	index := modelChoiceIndex(provider)
	row := m.modelChoiceRow
	if m.modelChoices[index].Mode == "single" {
		row = 0
	}
	return row
}

// modelChoiceCatalogRows returns the scanned catalog for the provider tab.
func (m Model) modelChoiceCatalogRows(provider setupflow.Provider) []SetupCatalogModel {
	if provider == setupflow.ProviderPi {
		return m.piCatalog
	}
	return m.setupCatalog
}

func (m Model) modelChoiceCatalogLoading(provider setupflow.Provider) bool {
	if provider == setupflow.ProviderPi {
		return m.piCatalogLoading
	}
	return m.setupCatalogLoading
}

func (m Model) modelChoiceCatalogErr(provider setupflow.Provider) error {
	if provider == setupflow.ProviderPi {
		return m.piCatalogErr
	}
	return m.setupCatalogErr
}

// modelChoiceVariants returns the scanned efforts for a discovered model.
func (m Model) modelChoiceVariants(provider setupflow.Provider, reference string) []string {
	for _, row := range m.modelChoiceCatalogRows(provider) {
		if row.Reference == reference {
			return append([]string(nil), row.Variants...)
		}
	}
	return nil
}

func (m Model) modelChoiceKnown(provider setupflow.Provider, reference string) bool {
	for _, row := range m.modelChoiceCatalogRows(provider) {
		if row.Reference == reference {
			return true
		}
	}
	return false
}

// modelChoiceEffortOptions offers the scanned efforts first. A discovered
// model with no efforts keeps the provider default only; an undiscovered
// manual reference keeps the legacy effort vocabulary.
func (m Model) modelChoiceEffortOptions(provider setupflow.Provider, reference string) []string {
	if !m.modelChoiceKnown(provider, reference) {
		return []string{"off", "minimal", "low", "medium", "high", "xhigh"}
	}
	options := []string{"off"}
	for _, variant := range m.modelChoiceVariants(provider, reference) {
		if variant == "off" || variant == "" {
			continue
		}
		duplicate := false
		for _, option := range options {
			if option == variant {
				duplicate = true
				break
			}
		}
		if !duplicate {
			options = append(options, variant)
		}
	}
	return options
}

func nextModelChoiceOption(options []string, current string) string {
	for index, option := range options {
		if option == current {
			return options[(index+1)%len(options)]
		}
	}
	return options[0]
}

// openCodeVariantEffort maps a discovered OpenCode variant token onto the
// provider-neutral effort vocabulary for requested-effort reporting.
func openCodeVariantEffort(variant string) string {
	switch variant {
	case "minimal", "low", "medium", "high", "xhigh":
		return variant
	case "max":
		return "xhigh"
	default:
		return "off"
	}
}

// applyModelChoiceOption stores the exact OpenCode discovery token while the
// effort keeps its provider-neutral meaning. Pi only carries efforts.
func applyModelChoiceOption(provider setupflow.Provider, assignment *agentmodels.Assignment, option string) {
	assignment.Variant, assignment.Effort = "", option
	if provider != setupflow.ProviderOpenCode || option == "off" {
		return
	}
	switch option {
	case "minimal", "low", "medium", "high", "xhigh":
		return
	default:
		assignment.Variant = option
		assignment.Effort = openCodeVariantEffort(option)
	}
}

// seededModelChoiceAssignment rebuilds an installed OpenCode assignment so the
// Models screen shows and edits the exact variant that is installed.
func seededModelChoiceAssignment(a modelplan.OpenCodeAgentAssignmentV3) agentmodels.Assignment {
	variant := string(a.Variant)
	effort := "off"
	if variant != "" {
		effort = openCodeVariantEffort(variant)
	}
	return agentmodels.Assignment{Model: a.Model, Effort: effort, Variant: variant}
}

func (m *Model) invalidateModelChoice() {
	m.setupPreviewed = false
	m.setupConfirm = false
	m.setupPlan.Digest = ""
	m.modelChoiceError = ""
}

func (m *Model) beginModelChoiceManual(provider setupflow.Provider) {
	index := modelChoiceIndex(provider)
	row := m.modelChoiceTarget(provider)
	m.modelChoiceEditing = true
	m.manualInput = newPickerInput("", "provider/model")
	m.manualInput.SetWidth(max(12, m.width-24))
	m.manualInput.SetValue(m.modelChoices[index].Rows[row].Model)
	m.manualInput.CursorEnd()
	m.manualInput.Focus()
}

func (m *Model) endModelChoiceManual() {
	m.modelChoiceEditing = false
	m.manualInput.Blur()
}

// updateModelChoiceModeGate handles the first decision for a provider that has
// no installed selection: one model for all agents, or one model per agent.
func (m *Model) updateModelChoiceModeGate(msg tea.KeyPressMsg, c *modelChoice) (bool, tea.Cmd) {
	switch msg.String() {
	case "q":
		return false, nil
	case "esc":
		m.setupView = setupViewProviders
		return true, nil
	case "r":
		return true, tea.Batch(m.loadSetupPlan(), m.refreshProviderCatalog(m.choiceProvider()))
	case "tab":
		m.modelChoiceProvider++
		m.modelChoiceRow = 0
		return true, m.ensureProviderCatalog(m.choiceProvider())
	case "up", "k", "left", "down", "j", "right":
		m.modelChoiceModeChoice = (m.modelChoiceModeChoice + 1) % 2
		return true, nil
	case "1":
		m.modelChoiceModeChoice = 0
		m.commitModelChoiceMode(c)
		return true, nil
	case "2":
		m.modelChoiceModeChoice = 1
		m.commitModelChoiceMode(c)
		return true, nil
	case "enter":
		m.commitModelChoiceMode(c)
		return true, nil
	}
	return true, nil
}

func (m *Model) commitModelChoiceMode(c *modelChoice) {
	if m.modelChoiceModeChoice == 0 {
		c.Mode = "single"
	} else {
		c.Mode = "per-agent"
	}
	for i := range c.Rows {
		if c.Rows[i].Effort == "" {
			c.Rows[i].Effort = "off"
		}
	}
	m.modelChoiceRow = 0
}

func (m Model) modelChoiceModeLines(provider setupflow.Provider, c modelChoice) []string {
	lines := []string{"", fmt.Sprintf("%s · How should models be assigned?", provider)}
	for index, option := range []string{"One model for all agents", "One model per agent"} {
		marker := " "
		if index == m.modelChoiceModeChoice {
			marker = "▸"
		}
		lines = append(lines, fmt.Sprintf("%s [%d] %s", marker, index+1, option))
	}
	if !c.Edited {
		lines = append(lines, "", "Existing selections are preserved until you edit them.")
	}
	return lines
}

func (m *Model) refreshProviderCatalog(provider setupflow.Provider) tea.Cmd {
	switch provider {
	case setupflow.ProviderOpenCode:
		return m.loadSetupCatalog(true)
	case setupflow.ProviderPi:
		return m.loadPiCatalog(true)
	}
	return nil
}

func (m *Model) updateModelChoices(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	provider := m.choiceProvider()
	index := modelChoiceIndex(provider)
	c := &m.modelChoices[index]
	if m.pickerOpen {
		return m.updateModelPicker(msg)
	}
	if m.modelChoiceEditing {
		switch msg.String() {
		case "esc":
			m.endModelChoiceManual()
		case "enter":
			row := 0
			if c.Mode == "per-agent" {
				row = m.modelChoiceRow
			}
			entered := strings.TrimSpace(m.manualInput.Value())
			if entered != c.Rows[row].Model {
				c.Rows[row].Variant = ""
				c.Rows[row].Effort = "off"
			}
			c.Rows[row].Model = entered
			c.Edited = true
			m.endModelChoiceManual()
			m.invalidateModelChoice()
		default:
			var cmd tea.Cmd
			m.manualInput, cmd = m.manualInput.Update(msg)
			return true, cmd
		}
		return true, nil
	}
	if provider != setupflow.ProviderCodex && c.Mode == "" {
		return m.updateModelChoiceModeGate(msg, c)
	}
	switch msg.String() {
	case "q":
		// Let the global quit handler close the TUI, as on every other screen.
		return false, nil
	case "tab":
		m.modelChoiceProvider++
		m.modelChoiceRow = 0
		return true, m.ensureProviderCatalog(m.choiceProvider())
	case "esc":
		m.setupView = setupViewProviders
		return true, nil
	case "r":
		if provider == setupflow.ProviderCodex {
			return true, m.loadSetupPlan()
		}
		return true, tea.Batch(m.loadSetupPlan(), m.refreshProviderCatalog(provider))
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
	case "m", "/":
		return true, m.openModelPicker()
	case "i":
		if provider == setupflow.ProviderCodex {
			return true, nil
		}
		m.beginModelChoiceManual(provider)
		return true, nil
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
	case "e":
		row := m.modelChoiceTarget(provider)
		options := m.modelChoiceEffortOptions(provider, c.Rows[row].Model)
		current := c.Rows[row].Variant
		if current == "" {
			current = c.Rows[row].Effort
		}
		next := nextModelChoiceOption(options, current)
		applyModelChoiceOption(provider, &c.Rows[row], next)
		c.Edited = true
		m.invalidateModelChoice()
	}
	return true, nil
}
func (m Model) multiSetupPlanLines() []string {
	provider := m.choiceProvider()
	gate := false
	lines := []string{"INSTALL · 2 OF 3 · MODELS", "Provider: " + string(provider), ""}
	if provider == setupflow.ProviderCodex {
		lines = append(lines, "Codex plan: "+m.setupSelected, "[↑↓] low · medium · high · ultra")
	} else {
		c := m.modelChoices[modelChoiceIndex(provider)]
		if c.Mode == "" {
			gate = true
			lines = append(lines, m.modelChoiceModeLines(provider, c)...)
		} else {
			mode := c.Mode
			lines = append(lines, fmt.Sprintf("Mode: %s   [1] all agents · [2] per agent", mode))
			if !c.Edited {
				lines = append(lines, "Existing selections preserved until edited.")
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
				value := "effort=" + setupValue(c.Rows[i].Effort)
				if c.Rows[i].Variant != "" {
					value = "variant=" + setupValue(c.Rows[i].Variant)
				}
				lines = append(lines, fmt.Sprintf("%s %s: %s  %s", marker, r, setupValue(c.Rows[i].Model), value))
			}
			lines = append(lines, m.modelChoiceCatalogStatus(provider)...)
			row := m.modelChoiceTarget(provider)
			if variants := m.modelChoiceVariants(provider, c.Rows[row].Model); len(variants) > 0 {
				lines = appendSetupWrapped(lines, "variants  ", strings.Join(variants, " · "), m.setupViewport.Width())
			}
		}
	}
	if m.modelChoiceEditing {
		lines = append(lines, "Model: "+m.manualInput.View())
	}
	if m.modelChoiceError != "" {
		lines = append(lines, "Error: "+sanitizeTerminal(m.modelChoiceError))
	}
	if m.setupPlanErr != nil {
		lines = append(lines, "Preview failed: "+setupActionableError(m.setupPlanErr))
	}
	if !gate {
		if m.setupPlanLoading {
			lines = append(lines, "Loading preview...")
		} else if m.setupPreviewed {
			lines = append(lines, "[Enter] review installation")
		} else {
			lines = append(lines, "[Enter] preview selections")
		}
	}
	return lines
}

func (m Model) modelChoiceCatalogStatus(provider setupflow.Provider) []string {
	rows := m.modelChoiceCatalogRows(provider)
	switch {
	case m.modelChoiceCatalogLoading(provider):
		return []string{"... Scanning local models..."}
	case m.modelChoiceCatalogErr(provider) != nil:
		if len(rows) > 0 {
			return []string{fmt.Sprintf("✕ Refresh failed · %d previously scanned models · [r] retry", len(rows))}
		}
		return []string{"✕ Scan unavailable · [r] retry · [i] type reference"}
	case len(rows) == 0:
		return []string{"! No local models · [r] rescan · [i] type reference"}
	default:
		providers := map[string]struct{}{}
		for _, row := range rows {
			providers[row.Provider] = struct{}{}
		}
		modelLabel := fmt.Sprintf("%d local models", len(rows))
		if len(rows) == 1 {
			modelLabel = "1 local model"
		}
		providerLabel := fmt.Sprintf("%d providers", len(providers))
		if len(providers) == 1 {
			providerLabel = "1 provider"
		}
		return []string{"✓ " + modelLabel + " · " + providerLabel}
	}
}

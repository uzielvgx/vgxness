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
						c.Rows[i] = seededModelChoiceAssignment(a)
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

func (m Model) filteredModelChoiceCatalog(provider setupflow.Provider) []SetupCatalogModel {
	rows := m.modelChoiceCatalogRows(provider)
	query := strings.ToLower(strings.TrimSpace(m.modelChoiceQuery))
	if query == "" {
		return rows
	}
	filtered := make([]SetupCatalogModel, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Reference), query) || strings.Contains(strings.ToLower(row.Provider), query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
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
	m.modelChoiceInput = m.modelChoices[index].Rows[row].Model
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

func (m *Model) updateModelChoiceSearch(provider setupflow.Provider, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.modelChoiceSearching, m.modelChoiceQuery, m.modelChoiceResultIndex = false, "", 0
		return true, nil
	case "enter":
		m.selectFilteredModelChoice(provider)
		return true, nil
	case "backspace":
		value := []rune(m.modelChoiceQuery)
		if len(value) > 0 {
			m.modelChoiceQuery = string(value[:len(value)-1])
		}
		m.modelChoiceResultIndex = 0
		return true, nil
	case "down":
		if count := len(m.filteredModelChoiceCatalog(provider)); count > 0 {
			m.modelChoiceResultIndex = (m.modelChoiceResultIndex + 1) % count
		}
		return true, nil
	case "up":
		if count := len(m.filteredModelChoiceCatalog(provider)); count > 0 {
			m.modelChoiceResultIndex = (m.modelChoiceResultIndex + count - 1) % count
		}
		return true, nil
	}
	if msg.Text != "" && len(m.modelChoiceQuery)+len(msg.Text) <= 385 {
		m.modelChoiceQuery += msg.Text
		m.modelChoiceResultIndex = 0
	}
	return true, nil
}

func (m *Model) selectFilteredModelChoice(provider setupflow.Provider) {
	matches := m.filteredModelChoiceCatalog(provider)
	if len(matches) == 0 {
		return
	}
	selected := matches[min(m.modelChoiceResultIndex, len(matches)-1)]
	index := modelChoiceIndex(provider)
	row := m.modelChoiceTarget(provider)
	if m.modelChoices[index].Rows[row].Model != selected.Reference {
		m.modelChoices[index].Rows[row].Variant = ""
		m.modelChoices[index].Rows[row].Effort = "off"
	}
	m.modelChoices[index].Rows[row].Model = selected.Reference
	m.modelChoices[index].Edited = true
	m.modelChoiceSearching, m.modelChoiceQuery, m.modelChoiceResultIndex = false, "", 0
	m.invalidateModelChoice()
}

func (m *Model) updateModelChoices(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	provider := m.choiceProvider()
	index := modelChoiceIndex(provider)
	c := &m.modelChoices[index]
	if c.Mode == "" {
		c.Mode = "single"
		for i := range c.Rows {
			c.Rows[i].Effort = "off"
		}
	}
	if m.modelChoiceSearching {
		return m.updateModelChoiceSearch(provider, msg)
	}
	if m.modelChoiceEditing {
		switch msg.String() {
		case "esc":
			m.modelChoiceEditing = false
		case "enter":
			row := 0
			if c.Mode == "per-agent" {
				row = m.modelChoiceRow
			}
			entered := strings.TrimSpace(m.modelChoiceInput)
			if entered != c.Rows[row].Model {
				c.Rows[row].Variant = ""
				c.Rows[row].Effort = "off"
			}
			c.Rows[row].Model = entered
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
		if provider == setupflow.ProviderCodex {
			return true, nil
		}
		m.modelChoiceSearching, m.modelChoiceQuery, m.modelChoiceResultIndex = true, "", 0
		return true, m.ensureProviderCatalog(provider)
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
	lines := []string{"INSTALL · 2 OF 3 · MODELS", fmt.Sprintf("Provider: %s  [Tab] next provider", provider), ""}
	if provider == setupflow.ProviderCodex {
		lines = append(lines, "Codex plan: "+m.setupSelected, "[↑↓] low · medium · high · ultra")
	} else {
		c := m.modelChoices[modelChoiceIndex(provider)]
		if m.modelChoiceSearching {
			lines = append(lines, m.modelChoiceSearchLines(provider)...)
		} else {
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
				value := "effort=" + setupValue(c.Rows[i].Effort)
				if c.Rows[i].Variant != "" {
					value = "variant=" + setupValue(c.Rows[i].Variant)
				}
				lines = append(lines, fmt.Sprintf("%s %s: %s  %s", marker, r, setupValue(c.Rows[i].Model), value))
			}
			lines = append(lines, "[m] Choose scanned model   [i] Type provider/model   [e] Change effort")
			lines = append(lines, m.modelChoiceCatalogStatus(provider)...)
			row := m.modelChoiceTarget(provider)
			if variants := m.modelChoiceVariants(provider, c.Rows[row].Model); len(variants) > 0 {
				lines = appendSetupWrapped(lines, "allowed variants  ", strings.Join(variants, " · "), m.setupViewport.Width())
			}
			lines = append(lines, "Availability and credentials are checked by the host when used.")
		}
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

func (m Model) modelChoiceCatalogStatus(provider setupflow.Provider) []string {
	rows := m.modelChoiceCatalogRows(provider)
	switch {
	case m.modelChoiceCatalogLoading(provider):
		return []string{"... Scanning local models for " + string(provider) + "..."}
	case m.modelChoiceCatalogErr(provider) != nil:
		if len(rows) > 0 {
			return []string{fmt.Sprintf("✕ Refresh failed; showing %d previously scanned models. [r] Retry explicit refresh, or [i] type a reference.", len(rows))}
		}
		return []string{"✕ Scanned models unavailable. [r] Retry explicit refresh, or [i] type a reference."}
	case len(rows) == 0:
		return []string{"! No locally discovered models. [r] Rescan, or [i] type a reference."}
	default:
		providers := map[string]struct{}{}
		for _, row := range rows {
			providers[row.Provider] = struct{}{}
		}
		return appendSetupWrapped([]string{}, "✓ ", fmt.Sprintf("%d scanned models · %d providers · %s", len(rows), len(providers), setupDiscoveryDisclaimer), m.setupViewport.Width())
	}
}

func (m Model) modelChoiceSearchLines(provider setupflow.Provider) []string {
	lines := []string{"MODEL CATALOG SEARCH · " + string(provider)}
	rows := m.modelChoiceCatalogRows(provider)
	if m.modelChoiceCatalogLoading(provider) {
		lines = append(lines, "... Scanning local models...")
	} else if m.modelChoiceCatalogErr(provider) != nil {
		if len(rows) > 0 {
			lines = append(lines, fmt.Sprintf("✕ Refresh failed; showing %d previously scanned models. [Esc] then [r] to retry.", len(rows)))
		} else {
			lines = append(lines, "✕ Scanned models unavailable. [Esc] then [r] to rescan, or [i] to type a reference.")
		}
	}
	matches := m.filteredModelChoiceCatalog(provider)
	count := fmt.Sprintf("%d matching local models", len(matches))
	if len(matches) == 1 {
		count = "1 matching local model"
	}
	lines = append(lines, "Query  "+setupValue(m.modelChoiceQuery)+"  ·  "+count, "")
	start := max(0, m.modelChoiceResultIndex-3)
	end := min(len(matches), start+7)
	for index := start; index < end; index++ {
		marker := " "
		if index == m.modelChoiceResultIndex {
			marker = "▸"
		}
		row := marker + " " + matches[index].Reference
		if variants := len(matches[index].Variants); variants == 1 {
			row += "  · 1 effort"
		} else if variants > 1 {
			row += fmt.Sprintf("  · %d efforts", variants)
		} else {
			row += "  · provider default"
		}
		if index == m.modelChoiceResultIndex {
			row = studioFocus.Width(max(1, m.setupViewport.Width()-6)).Render(row)
		}
		lines = append(lines, row)
	}
	if len(matches) == 0 && !m.modelChoiceCatalogLoading(provider) && m.modelChoiceCatalogErr(provider) == nil {
		if len(rows) == 0 {
			lines = append(lines, "No locally discovered models. [Esc] then [r] rescan or [i] type a reference.")
		} else {
			lines = append(lines, "No local model matches this query.")
		}
	}
	if len(matches) > 0 && m.modelChoiceResultIndex < len(matches) {
		if variants := matches[m.modelChoiceResultIndex].Variants; len(variants) > 0 {
			lines = appendSetupWrapped(lines, "", "efforts  "+strings.Join(variants, " · "), m.setupViewport.Width())
		}
	}
	lines = append(lines, "[↑↓] result  [Enter] assign  [Esc] cancel")
	return lines
}

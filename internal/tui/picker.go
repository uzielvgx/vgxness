package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

type pickerStep uint8

const (
	pickerProviders pickerStep = iota
	pickerModels
)

const (
	pickerMaxVisibleRows = 9
	pickerMaxWidth       = 76
)

type pickerProvider struct {
	name  string
	count int
}

func newPickerInput(prompt, placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = prompt
	input.Placeholder = placeholder
	input.CharLimit = 256
	input.SetWidth(40)
	return input
}

// openModelPicker opens the provider-then-model modal for the active tab.
func (m *Model) openModelPicker() tea.Cmd {
	provider := m.choiceProvider()
	if provider == setupflow.ProviderCodex {
		return nil
	}
	m.pickerOpen = true
	m.pickerStep = pickerProviders
	m.pickerProvider = ""
	m.pickerIndex = 0
	m.pickerFilter = newPickerInput("> ", "filter providers")
	m.pickerFilter.SetWidth(max(10, m.pickerContentWidth()-2))
	m.pickerFilter.Focus()
	return m.ensureProviderCatalog(provider)
}

func (m *Model) closeModelPicker() {
	m.pickerOpen = false
	m.pickerStep = pickerProviders
	m.pickerProvider = ""
	m.pickerIndex = 0
	m.pickerFilter.Blur()
	m.pickerFilter.SetValue("")
}

func (m Model) pickerCatalog() []SetupCatalogModel {
	return m.modelChoiceCatalogRows(m.choiceProvider())
}

func (m Model) pickerProviders() []pickerProvider {
	counts := map[string]int{}
	order := make([]string, 0)
	for _, row := range m.pickerCatalog() {
		if _, seen := counts[row.Provider]; !seen {
			order = append(order, row.Provider)
		}
		counts[row.Provider]++
	}
	sort.Strings(order)
	query := strings.ToLower(strings.TrimSpace(m.pickerFilter.Value()))
	providers := make([]pickerProvider, 0, len(order))
	for _, name := range order {
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		providers = append(providers, pickerProvider{name: name, count: counts[name]})
	}
	return providers
}

func (m Model) pickerModels() []SetupCatalogModel {
	query := strings.ToLower(strings.TrimSpace(m.pickerFilter.Value()))
	models := make([]SetupCatalogModel, 0)
	for _, row := range m.pickerCatalog() {
		if row.Provider != m.pickerProvider {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(row.Reference), query) {
			continue
		}
		models = append(models, row)
	}
	return models
}

func (m Model) pickerLength() int {
	if m.pickerStep == pickerProviders {
		return len(m.pickerProviders())
	}
	return len(m.pickerModels())
}

// pickerRows bounds the visible list so the modal plus its frame never exceeds
// the terminal height (title, filter, blank, rows, blank, help and two borders).
func (m Model) pickerRows() int {
	rows := m.height - 8
	if rows < 1 {
		rows = 1
	}
	if rows > pickerMaxVisibleRows {
		rows = pickerMaxVisibleRows
	}
	return rows
}

func (m *Model) updateModelPicker(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.pickerStep == pickerModels {
			m.pickerStep = pickerProviders
			m.pickerIndex = 0
			m.pickerFilter.SetValue("")
			m.pickerFilter.Placeholder = "filter providers"
			return true, nil
		}
		m.closeModelPicker()
		return true, nil
	case "enter":
		m.selectModelPicker()
		return true, nil
	case "up":
		m.movePicker(-1)
		return true, nil
	case "down":
		m.movePicker(1)
		return true, nil
	case "pgup":
		m.movePicker(-m.pickerRows())
		return true, nil
	case "pgdown":
		m.movePicker(m.pickerRows())
		return true, nil
	}
	var cmd tea.Cmd
	m.pickerFilter, cmd = m.pickerFilter.Update(msg)
	m.pickerIndex = 0
	return true, cmd
}

func (m *Model) movePicker(offset int) {
	count := m.pickerLength()
	if count == 0 {
		m.pickerIndex = 0
		return
	}
	m.pickerIndex = (m.pickerIndex + offset%count + count) % count
}

func (m *Model) selectModelPicker() {
	if m.pickerStep == pickerProviders {
		providers := m.pickerProviders()
		if len(providers) == 0 {
			return
		}
		m.pickerProvider = providers[min(m.pickerIndex, len(providers)-1)].name
		m.pickerStep = pickerModels
		m.pickerIndex = 0
		m.pickerFilter.SetValue("")
		m.pickerFilter.Placeholder = "filter models"
		return
	}
	models := m.pickerModels()
	if len(models) == 0 {
		return
	}
	m.assignPickedModel(models[min(m.pickerIndex, len(models)-1)].Reference)
	m.closeModelPicker()
}

// assignPickedModel writes the chosen reference to the active agent row.
func (m *Model) assignPickedModel(reference string) {
	provider := m.choiceProvider()
	index := modelChoiceIndex(provider)
	row := m.modelChoiceTarget(provider)
	if m.modelChoices[index].Rows[row].Model != reference {
		m.modelChoices[index].Rows[row].Variant = ""
		m.modelChoices[index].Rows[row].Effort = "off"
	}
	m.modelChoices[index].Rows[row].Model = reference
	m.modelChoices[index].Edited = true
	m.invalidateModelChoice()
}

func (m Model) pickerBoxWidth() int {
	return min(pickerMaxWidth, max(30, m.width-6))
}

// pickerContentWidth is the box width minus border and horizontal padding.
func (m Model) pickerContentWidth() int {
	return m.pickerBoxWidth() - 4
}

func variantSummary(variants []string) string {
	if len(variants) == 0 {
		return "provider default"
	}
	return strings.Join(variants, " · ")
}

func pickerRow(marker string, left, right string, inner int, selected bool) string {
	leftCell := marker + " " + truncateSetupRunes(left, max(1, inner-2))
	line := leftCell
	if right != "" {
		gap := inner - lipgloss.Width(leftCell) - lipgloss.Width(right)
		if gap < 1 {
			right = truncateSetupRunes(right, max(1, inner-lipgloss.Width(leftCell)-1))
			gap = max(1, inner-lipgloss.Width(leftCell)-lipgloss.Width(right))
		}
		line = lipgloss.JoinHorizontal(lipgloss.Left, leftCell, strings.Repeat(" ", gap), right)
	}
	if selected {
		return studioFocus.Width(inner).Render(line)
	}
	return line
}

// modelPickerBox renders the modal content for the provider and model steps.
func (m Model) modelPickerBox() string {
	content := m.pickerContentWidth()
	provider := m.choiceProvider()
	title := "Select provider"
	if m.pickerStep == pickerModels {
		title = m.pickerProvider + " · Select model"
	}
	lines := []string{
		studioAccent.Render(truncateSetupRunes(title, content)),
		m.pickerFilter.View(),
		"",
	}
	switch {
	case m.modelChoiceCatalogLoading(provider):
		lines = append(lines, "  ... scanning local models ...")
	case m.modelChoiceCatalogErr(provider) != nil:
		lines = append(lines, "  scan unavailable · close with Esc, then press r")
	default:
		if m.pickerStep == pickerProviders {
			providers := m.pickerProviders()
			if len(providers) == 0 {
				lines = append(lines, "  no matching providers")
				break
			}
			start, end := pickerWindow(m.pickerIndex, len(providers), m.pickerRows())
			for index := start; index < end; index++ {
				count := fmt.Sprintf("%d models", providers[index].count)
				if providers[index].count == 1 {
					count = "1 model"
				}
				marker := " "
				if index == m.pickerIndex {
					marker = "\u25b8"
				}
				lines = append(lines, pickerRow(marker, providers[index].name, count, content, index == m.pickerIndex))
			}
		} else {
			models := m.pickerModels()
			if len(models) == 0 {
				lines = append(lines, "  no matching models")
				break
			}
			start, end := pickerWindow(m.pickerIndex, len(models), m.pickerRows())
			for index := start; index < end; index++ {
				marker := " "
				if index == m.pickerIndex {
					marker = "\u25b8"
				}
				lines = append(lines, pickerRow(marker, models[index].Reference, variantSummary(models[index].Variants), content, index == m.pickerIndex))
			}
		}
	}
	help := "[\u2191\u2193] move \u00b7 [Enter] open provider \u00b7 [Esc] close"
	if m.pickerStep == pickerModels {
		help = "[\u2191\u2193] move \u00b7 [Enter] assign model \u00b7 [Esc] back"
	}
	lines = append(lines, "", studioMuted.Render(help))
	return studioPanel.Width(content + 4).Render(strings.Join(lines, "\n"))
}

func pickerWindow(index, length, visible int) (int, int) {
	if length <= visible {
		return 0, length
	}
	start := index - visible/2
	if start < 0 {
		start = 0
	}
	if start > length-visible {
		start = length - visible
	}
	return start, start + visible
}

// overlayPicker centers the modal over the rendered base view.
func (m Model) overlayPicker(base string, width, height int) string {
	if !m.pickerOpen {
		return base
	}
	modal := m.modelPickerBox()
	lines := strings.Split(base, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	base = strings.Join(lines, "\n")
	x := max(0, (width-lipgloss.Width(modal))/2)
	y := max(0, (height-lipgloss.Height(modal))/2)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(1),
	).Render()
}

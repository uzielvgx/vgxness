package tui

import (
	"context"
	"image/color"
	"math"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type fakeBackend struct{}

func (fakeBackend) SetupStatus(context.Context, Request) (SetupStatus, error) {
	return SetupStatus{}, nil
}

func (fakeBackend) PlanSetup(context.Context, SetupRequest) (SetupPlan, error) {
	return SetupPlan{}, nil
}
func (fakeBackend) ApplySetup(context.Context, SetupRequest) (SetupResult, error) {
	return SetupResult{}, nil
}

func TestTUIStartsAtInstallationAndConfigurationOnly(t *testing.T) {
	model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	view := model.View().Content
	if !strings.Contains(view, "INSTALLATION") {
		t.Fatalf("installation landing page missing:\n%s", view)
	}
	for _, removed := range []string{"OVERVIEW", "SYSTEM", "MEMORY", "SESSION ACTIVITY", "RECENT PROJECT MEMORY"} {
		if strings.Contains(view, removed) {
			t.Fatalf("removed monitoring surface %q still rendered:\n%s", removed, view)
		}
	}
}

func TestInstallationViewRemainsUsableAtCompactAndWideSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
		t.Run("terminal", func(t *testing.T) {
			model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			plan := readySetupPlan("medium")
			model = updateModel(t, model, setupPlanLoadedMsg{generation: 1, request: model.setupRequest(), value: plan})
			content := model.View().Content
			assertMaximumWidth(t, content, size.width)
			for _, expected := range []string{"INSTALLATION STUDIO", "OPENCODE SETUP", "READY TO APPLY", "[a] apply", "[Tab] Recovery"} {
				if !strings.Contains(content, expected) {
					t.Fatalf("%dx%d view missing %q:\n%s", size.width, size.height, expected, content)
				}
			}
			for _, removed := range []string{"SESSION ACTIVITY", "RECENT PROJECT MEMORY", "SYSTEM HEALTH"} {
				if strings.Contains(content, removed) {
					t.Fatalf("%dx%d view includes %q:\n%s", size.width, size.height, removed, content)
				}
			}
			t.Logf("%dx%d installation fixture:\n%s", size.width, size.height, content)
		})
	}
}

func TestSoftbricBrandingUsesResponsiveBanner(t *testing.T) {
	for _, size := range []struct {
		width, height int
		want          string
		avoid         string
	}{
		{80, 24, "VGXNESS / INSTALLATION STUDIO", "██╗"},
		{120, 40, "██╗", "VGXNESS / INSTALLATION STUDIO"},
	} {
		t.Run(size.want, func(t *testing.T) {
			model := NewModel(context.Background(), fakeBackend{}, Options{Workspace: "/workspace"})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			content := model.View().Content
			if !strings.Contains(content, size.want) {
				t.Fatalf("%dx%d view missing responsive banner %q:\n%s", size.width, size.height, size.want, content)
			}
			if strings.Contains(content, size.avoid) {
				t.Fatalf("%dx%d view unexpectedly contains %q:\n%s", size.width, size.height, size.avoid, content)
			}
			assertMaximumWidth(t, content, size.width)
		})
	}
}

func TestSoftbricBannerHasSevenUniformRowsWithoutShadowGlyph(t *testing.T) {
	banner := softbricBanner()
	if len(banner) != 6 {
		t.Fatalf("banner has %d rows, want 6", len(banner))
	}
	for index, line := range banner {
		if strings.Contains(line, "▟") {
			t.Fatalf("banner row %d contains a decorative shadow glyph: %q", index, line)
		}
		if got := lipgloss.Width(line); got != 65 {
			t.Fatalf("banner row %d width=%d, want 65: %q", index, got, line)
		}
	}
}

func TestMutedInstructionTextMeetsContrastThresholdOnSoftbricSurfaces(t *testing.T) {
	foreground := studioMuted.GetForeground()
	for _, background := range []color.Color{softbricCanvas, softbricInk} {
		if ratio := contrastRatio(t, foreground, background); ratio < 4.5 {
			t.Fatalf("muted instruction contrast is %.2f:1, want at least 4.5:1", ratio)
		}
	}
}

func TestSanitizeTerminalEscapesControls(t *testing.T) {
	got := sanitizeTerminal("line\nreturn\rtab\tescape\x1bdel\x7fbidi\u202e")
	want := `line\nreturn\rtab\tescape\x1bdel\x7fbidi\u202e`
	if got != want {
		t.Fatalf("sanitizeTerminal()=%q want=%q", got, want)
	}
}

func updateModel(t *testing.T, model Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(msg)
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return result
}

func assertMaximumWidth(t *testing.T, content string, maximum int) {
	t.Helper()
	for index, line := range strings.Split(content, "\n") {
		if width := lipgloss.Width(line); width > maximum {
			t.Fatalf("line %d width=%d maximum=%d: %q", index+1, width, maximum, line)
		}
	}
}

func contrastRatio(t *testing.T, foreground, background color.Color) float64 {
	t.Helper()
	foregroundLuminance := relativeLuminance(t, foreground)
	backgroundLuminance := relativeLuminance(t, background)
	if foregroundLuminance < backgroundLuminance {
		foregroundLuminance, backgroundLuminance = backgroundLuminance, foregroundLuminance
	}
	return (foregroundLuminance + 0.05) / (backgroundLuminance + 0.05)
}

func relativeLuminance(t *testing.T, value color.Color) float64 {
	t.Helper()
	red, green, blue, _ := value.RGBA()
	channels := [3]float64{}
	for index, component := range []uint32{red, green, blue} {
		channel := float64(component) / 65535
		if channel <= 0.04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

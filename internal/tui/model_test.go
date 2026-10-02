package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestShellGuardsSmallTerminalsAndRendersHeader(t *testing.T) {
	m := NewModel(context.Background(), Options{Workspace: "/work/project"})
	for _, tc := range []struct {
		name          string
		width, height int
		want          string
	}{
		{name: "too small", width: 30, height: 8, want: "Terminal demasiado pequeña"},
		{name: "narrow", width: 80, height: 24, want: "CONSOLA"},
		{name: "wide", width: 120, height: 36, want: "██"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			plain := ansi.Strip(updated.(Model).render())
			if !strings.Contains(plain, tc.want) || !strings.Contains(plain, "[q] salir") {
				t.Fatalf("render %dx%d = %q", tc.width, tc.height, plain)
			}
			for _, line := range strings.Split(plain, "\n") {
				if ansi.StringWidth(line) > tc.width {
					t.Fatalf("line wider than %d: %q", tc.width, line)
				}
			}
		})
	}
}

func TestShellQuitKeysAndWorkspaceSanitizing(t *testing.T) {
	m := NewModel(nil, Options{Workspace: "/tmp/bad\x1b[31mpath‮"})
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		_, cmd := m.Update(tea.KeyPressMsg{Code: keyCode(key), Mod: keyMod(key)})
		if cmd == nil {
			t.Fatalf("%s did not quit", key)
		}
	}
	header := ansi.Strip(m.headerWorkspace(60))
	if strings.ContainsRune(header, '\x1b') || strings.ContainsRune(header, '‮') || !strings.Contains(header, `\x1b`) {
		t.Fatalf("workspace not sanitized: %q", header)
	}
	if got := ansi.Strip(m.headerWorkspace(8)); !strings.HasPrefix(strings.TrimPrefix(got, "workspace  "), "…") {
		t.Fatalf("long workspace not trimmed from the left: %q", got)
	}
}

func TestRunRequiresInteractiveTerminals(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &stderr, Options{})
	if code != 2 || !strings.Contains(stderr.String(), "interactive terminals") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestFitKeepsFooterWhenTruncating(t *testing.T) {
	lines := []string{"1", "2", "3", "4", "footer-a", "footer-b"}
	got := fit(lines, 10, 4)
	if got != "1\n2\nfooter-a\nfooter-b" {
		t.Fatalf("fit = %q", got)
	}
	if got := fit([]string{"a very long line that wraps"}, 6, 0); strings.Count(got, "\n") < 2 {
		t.Fatalf("fit did not wrap: %q", got)
	}
}

func keyCode(key string) rune {
	switch key {
	case "esc":
		return tea.KeyEscape
	case "ctrl+c":
		return 'c'
	default:
		return rune(key[0])
	}
}

func keyMod(key string) tea.KeyMod {
	if key == "ctrl+c" {
		return tea.ModCtrl
	}
	return 0
}

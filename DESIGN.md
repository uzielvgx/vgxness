# Design

Source of truth for how VGXNESS looks. UI work must match the canvas; a change
in one must be reflected in the other.

- Design system: https://claude.ai/artifact/5tnRiD2UwvqtFj58kyiKPf (brand book,
  tokens, 15 terminal components with previews and guidelines).
- Canvas: https://claude.ai/artifact/55i278YA64Yz3tKGM9zgvX. Pages: Índice
  (entry), Consola (22 artboards, 1040×760 = 120×36 cells, grouped by module:
  Inicio, Setup del plugin, Memoria, Sync, Doctor) and Arquitectura (system
  diagram). Artboard files are `tui-<module>-<screen>[-<state>].dc.html`.
- Decisions: `docs/decisions.md` D-001 (console) and D-002 (design system).

## Tokens

Themes: `midnight` (console, default) and `paper` (docs and light web surfaces;
the TUI never switches to it). Hex values are what `lipgloss.Color` receives;
lipgloss downsamples for 256- and 16-color terminals. The ANSI 256 column is
the index to expect after downsampling.

| Token | midnight | paper | ANSI 256 (midnight) | Use |
|---|---|---|---|---|
| `canvas` | `#071522` | `#f5f7f8` | 234 | Terminal and page background |
| `panel` | `#102231` | `#ffffff` | 235 | Panels, cards, picker fill |
| `panel-raised` | `#183447` | `#e9eef1` | 236 | Alternate or hovered row |
| `ink` | `#f5f7f8` | `#102231` | 255 | Primary text |
| `ink-muted` | `#9db1bc` | `#52616d` | 109 | Secondary text, labels, help bar, table headers |
| `ink-faint` | `#4f6675` | `#a7b3bb` | 60 | Dividers, placeholders, disabled rows (not for readable text) |
| `accent` | `#4dd4d4` | `#005f5c` | 80 | Section labels, panel titles, `[Tecla]`, focus and selection fill |
| `accent-strong` | `#008b87` | `#008b87` | 30 | Focused panel frame, banner gradient end (borders and large text only) |
| `frame` | `#005f5c` | `#c9d4d9` | 23 | Panel borders (box-drawing glyphs); decorative only |
| `on-accent` | `#071522` | `#ffffff` | 234 | Text on accent fills |
| `focus-bg` / `focus-fg` | = `accent` / `on-accent` | | | Selected row |
| `success` | `#25d366` | `#15803d` | 77 | ✓ |
| `warning` | `#f59e0b` | `#b45309` | 214 | ! |
| `error` | `#f87171` | `#b91c1c` | 203 | ✕ and destructive confirmations |
| `info` | `#60a5fa` | `#0369a1` | 75 | ◇ |
| `banner-from` / `banner-to` | = `accent` / `accent-strong` | | | ASCII banner gradient (`lipgloss.Blend1D`) |

16-color fallback: `accent` → bright cyan, `ink-muted` → bright black,
`success`/`warning`/`error`/`info` → green/yellow/red/blue.

Typography: the TUI uses the terminal font. Everything is `term` 14/20; emphasis
`term-strong` (700); section labels `term-label` (700, uppercase, 0.08em); the
six-row banner only at ≥ 120 columns. Docs: Space Grotesk 600 for `display`
(32/40) and `heading` (22/28); Inter for `body` (15/22), `caption` (13/18)
and `eyebrow` (12/16, 600, uppercase). Mockups render the terminal in
JetBrains Mono (cell 8.4px × row 20px).

Spacing in the TUI is cells and rows: two cells of indent inside a panel, one
cell of panel padding, one blank row between sections, help bar as the last
row. Minimum terminal 80×24; below that only the TooSmall screen renders.

## Rules the code must follow

- Every screen is Header, one Panel, KeyHelp. Modals (Picker, Confirm) overlay
  the panel and hide KeyHelp.
- Status is always glyph + color + word: `✓ ! ✕ ◇`; `◐` in progress, `◌`
  pending, `▸` cursor, `·` separator, `…` truncation. No emoji, no Nerd Font.
- Key hints: `[Tecla] acción` in `ink-muted` with the key in `accent`, joined
  by ` · `, at most six per screen.
- Destructive actions (forget, reset, reinstall) go through Confirm with the
  consequence stated and `[y]`/`[n]`; Enter never confirms destruction.
- Errors state what failed and add a line starting `Acción:`.
- UI copy in Spanish (`ui-copy-es` skill); commands, paths and identifiers in
  English.

# Decision log

One entry per decision that matters later. Never rewrite an old entry: mark it
`Status: superseded by D-xxx` and add a new one.

## D-001 · Keep the TUI as the Claude-only console (2026-10-02)
Status: active
Context: the claude-only plan removed `vgxness tui` together with the OpenCode,
Codex and Pi installers it served. The design session showed a TUI still has a
job once installation belongs to Claude Code. Decision: `vgxness tui` stays and
becomes the console: local state, guided plugin setup (prints the `claude
plugin` commands and the permission snippet, applies only on confirmation),
memory browsing (search, detail, forget with confirmation, session handoffs),
sync and doctor. Why: these are the parts the plugin cannot show on its own;
the alternative (CLI subcommands only) loses discoverability of state and
makes destructive confirmations and multi-step setup awkward. The console
never edits memories; Claude Code saves through MCP. Plan `docs/plans/claude-only.md`
decision 14 and task 17 carry this.

## D-002 · Design system derived from Softbric for the terminal (2026-10-02)
Status: active
Context: no VGXNESS design system existed; the TUI already used six Softbric
colors. Decision: the design system "VGXNESS"
(https://claude.ai/artifact/5tnRiD2UwvqtFj58kyiKPf, tokens mirrored in
`DESIGN.md`) takes Softbric's identity tokens from softbric.com as source of
truth and adds only what a terminal needs: a legible muted text (Softbric's
slate fails 4.5:1 on midnight), raised panel, frame, on-accent, and semantic
colors lightened one step where the originals missed 4.5:1 on panel. Two
themes: `midnight` (console, default) and `paper` (docs and light web; the
TUI never uses it). The TUI inherits the user's terminal font; JetBrains Mono
only represents it in mockups. UI copy is Spanish (neutral, "tú"); commands,
paths and identifiers stay English. Why: brand continuity with Softbric at
zero cost, WCAG AA inside the terminal, and one source for both the TUI and
the docs. Alternatives discarded: a light "clear paper" console (fights most
terminal themes) and a generic look unrelated to Softbric.

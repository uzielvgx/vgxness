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

## D-003 · Console layout patterns mapped to Charm v2 (2026-10-02)
Status: active
Context: the first canvas pass left most of the 120×35 screen empty and the
ASCII banner used double-line glyphs that many fonts render as outlines.
Decision: every console screen composes Header + one or two side-by-side
panels (main + 45-column side panel) + optional fixed ActionCards row +
KeyHelp; multi-step flows add a 28-column Stepper rail; the banner is generated
from a 5×7 bitmap into half blocks (`▀ ▄ █`, 4 rows); modals are Lip Gloss compositor layers; progress is
`bubbles/progress` with a `Blend1D(accent, accent-strong)` gradient; logs
scroll in a `viewport`. The full mapping lives in `DESIGN.md` ("Mapa a Bubble
Tea v2…") and in the design system README. Why: these are real capabilities
of the pinned libraries (bubbletea v2.0.8, bubbles v2.1.1, lipgloss v2.0.5),
so the design and the Go code can match one to one; single-panel screens
wasted space and read as unfinished.

## D-004 · The console shows only data the product really has (2026-10-02)
Status: active
Context: the canvas artboards use mockup copy that assumes features the
backend does not have: a device-pairing login and device list for sync, a
local sync daemon and sync history, memory tags, usage counters and related
memories, an activity log, a `memory restore` command, a `memory add`
command, and permission rules naming a non-existent `handoff_get` tool.
Decision: `vgxness tui` implements every module, screen and state of the
canvas with its layout and style, but each field comes from real data; a
field without a source is dropped, and a flow without a backend is replaced
by the real one (sync sign-in = endpoint + device ID + bearer on stdin, as
`memory sync configure` does; permissions = `vgxness claude-code setup`).
Claude Code state comes from `claude --version`, `claude plugin list --json`
and `claude plugin marketplace list --json`. The artboards are updated to
the implemented copy module by module. Why: a console that shows invented
counters or links to flows that do not exist breaks trust in every other
number on the screen. The missing features are listed as backlog in plan
`docs/plans/claude-only.md` (task 19); each needs its own plan.

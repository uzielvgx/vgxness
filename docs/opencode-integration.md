# OpenCode integration

VGXNESS installs one persistent primary profile, `vgxness-manager`, and five hidden read-only review profiles:

- `vgxness-review-risk`
- `vgxness-review-readability`
- `vgxness-review-reliability`
- `vgxness-review-resilience`
- `vgxness-review-refuter`

This is a native OpenCode projection. It does not install `vgxness.ts`, expose any `vgxness_*` tool, create ticket-bound governed agents, or require a secondary `provider/model`.

## Install and inspect

The guided path is:

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --status
```

The lower-level lifecycle remains available:

```sh
vgxness integrate opencode preview
vgxness integrate opencode install
vgxness integrate opencode status
vgxness integrate opencode uninstall
```

`--config-dir` can select a non-default OpenCode configuration directory. The deprecated `--model` flag is accepted temporarily for command compatibility but has no effect on the native projection.

Preview and status are read-only. Installation creates only absent exact managed artifacts and refuses foreign or drifted content. Uninstall removes only exact managed artifacts, writes recoverable hard-link backups, and refuses drift.

## Native capability routing

The manager uses OpenCode directly:

- ordinary workspace tools for inspection and implementation;
- built-in `explore` and `general` Task workers for bounded delegation;
- the native `skill` tool, addressed by skill name;
- persistent memory when available;
- optional SDD only when the user requests or accepts it;
- the five review profiles against one frozen candidate.

When a project has a healthy `.codegraph` index, the manager and reviewers may use one bounded `codegraph_explore` query for architecture, symbols, call paths, dependencies, blast radius, or affected tests. Exact source, Git diff, and test output remain authoritative. Missing or stale CodeGraph never blocks fallback reads and search.

Review profiles deny all tools by default and allow only `read`, `grep`, `glob`, `list`, `skill`, and the exact `codegraph_explore` tool. They cannot edit, run shell commands, delegate, access the network, commit, or push.

## Health contract

The integration is installed only when the manager and all five reviewer files match their managed identities exactly. Setup health combines:

1. the permanent VGXNESS launcher is installed and verified;
2. all six native profiles are installed without drift;
3. the OpenCode adapter handshake succeeds for the selected workspace.

Plugin state, a child execution model, and `BridgeConfigured` are not readiness requirements.

## Migration from the governed projection

Before activating this native-only projection over an older installation, use the old managed launcher to uninstall its exact projection. That recoverably removes the old `vgxness.ts` plugin and the governed Navigator, explorer, implementer, maintainer, and reviewer profiles. Then install the new projection.

The Go control-plane and legacy bridge CLI remain in the binary for compatibility and a later deprecation phase. They are no longer projected into OpenCode and are not part of the manager's normal workflow.

After installation or migration, restart OpenCode Desktop so it reloads the agent and plugin inventory.

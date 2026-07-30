---
name: vgxness-autonomous-stacked-pr
description: Use when autonomously delivering an eligible change as one review-ready pull request or a linear stack with native git and gh.
---

<!-- managed-by: vgxness; artifact: opencode-skill/vgxness-autonomous-stacked-pr; version: 1 -->

# VGXNESS autonomous stacked PR

Use this policy only from the top-level VGXNESS Manager. Managed general remains the sole workspace writer; the manager remains the sole Git and GitHub actor. Use native `git` and `gh` commands directly. Do not introduce an adapter, custom typed Git or GitHub tool, stack engine, worktree writer, delivery daemon, or durable delivery state.

## Eligibility and restrictions

An eligible task is an authorized implementation in one clean Git checkout whose intended changed paths can be isolated and whose freeze, focused checks, independent verification, and selected review have succeeded. Announce routine autonomous delivery when loading this skill. The announcement is disclosure, not a request for another approval.

Task restrictions always win and narrow transitively:

- `local-only` means no commit, no push, and no PR;
- `no commit` means no commit, no push, and no PR;
- `no push` means no push and no PR;
- `no PR` means no pull request.

Never infer that a narrower restriction authorizes an earlier operation. Never carry delivery authority to another task. If the user names a different branch, remote, draft flow, or delivery operation, it is outside routine autonomous delivery and remains governed by the manager's default-deny permissions.

## Sizing and stack plan

Resolve sizing with this precedence: an explicit task override, then an explicit durable project memory default, then the defaults below. Do not persist a one-task override.

- Target 400 effective changed lines per slice.
- Use one pull request at 800 effective changed lines or fewer.
- Stack when the planned change is more than 800 effective changed lines.

Estimate before implementation from the accepted scope and nearby evidence. The estimate guides only the initial plan. Initial branch creation uses the announced estimate and the verified start commit. After implementation, use numeric additions plus deletions from the worktree-inclusive one-commit `git diff --numstat` form for the exact intended candidate; this actual measurement supersedes the estimate. Treat binary entries, missing paths, unrelated changes, or an unexplained material estimate/actual mismatch as ineligible for routine delivery. If the actual total is more than 800 effective changed lines, re-plan before staging, commit, push, or PR creation.

Derive one deterministic delivery ID from the normalized task goal: lowercase ASCII letters and digits, replace each run of other characters with one hyphen, collapse and trim hyphens, use `task` if empty, and truncate to at most 48 characters without a trailing hyphen. Use branches named exactly `vgxness/<delivery-id>/slice-<ordinal>`, with positive decimal ordinals starting at 1.

Each slice must be independently reviewable, preserve behavior at every boundary, and form a linear immediate-parent topology. Slice 1 starts from the inspected base branch. Every later slice starts from the committed branch immediately before it. Keep one clean checkout and one writer; do not create another checkout. There is no automatic cleanup of local branches, remote branches, or pull requests.

## Freeze and native delivery

Before any routine mutation, establish the expected HEAD, base branch and upstream, exact porcelain-v1 status including untracked files, intended paths, candidate identity, successful developmental checks, independent verification, and review outcome. Verify the remote URL and GitHub repository identity, validate every branch ref, test local, cached remote, and live remote ref existence, and prove immediate-parent ancestry and zero unexpected divergence. After implementation, inspect worktree-inclusive one-commit base/path name-only, name-status, raw, check, and numstat diffs. Measure each specific untracked path with the permitted no-index numstat form. Before commit, inspect cached name-only, check, numstat, and full diff forms. Verify GitHub authentication and use PR list/view/checks readback to detect or report existing delivery state. Preserve unrelated changes and stage only intended paths.

After those gates succeed, a fresh branch, normal commit, first push, and non-draft pull request need no second approval unless a task restriction forbids them. Use only these conceptual native command forms, with each placeholder replaced by a previously verified value:

- `git switch -c <head> <verified-start-commit>`
- `git commit -m "<type>(<scope>): <summary> [slice <ordinal>/<total>]"`
- `git push --set-upstream <verified-remote> <head>`
- `gh pr create --head <head> --base <base> --title "<title>" --body "<body>"`

Use only validated, option-free tokens for head, base, remote name, and start commit. Use the one-line PR title format `<summary> [<ordinal>/<total>]`. Use the comma-separated one-line PR body format `Stack: <delivery-id>, Slice: <ordinal>/<total>, Base: <base>, Head: <head>, Depends-On: <previous-PR-URL-or-none>`. The body emits stack metadata even for a one-slice delivery and contains no semicolon. The commit message, title, and body must each be one safe single argument: no newline, carriage return, control character, quote, backslash, shell metacharacter, substitution syntax, or option-shaped ` -` segment. Set every later PR base to its immediate parent branch and record the created URL before constructing the next slice metadata.

Routine automation does not cover an existing delivery branch, an existing pull request, a non-fast-forward or rejected push, a changed remote, authentication repair, post-create mutation, merge, release, history rewriting, cleanup, worktree mutation, credential or configuration changes, or recovery by destructive Git. Stop mutations and use read-only resumption: inspect local and remote metadata that is already available, report the exact topology and blocker, and wait for an explicit new task.

OpenCode command globs do not prove argv semantics. They are static last-match policy rules, not a shell parser or a guarantee about host behavior. Keep refs and paths within the normalized forms above, reject shell metacharacters or ambiguous arguments, and do not claim that a matching permission proves what Git, GitHub, credentials, branch protection, hooks, or the network will do.

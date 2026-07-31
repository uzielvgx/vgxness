---
name: vgxness-autonomous-stacked-pr
description: Use when autonomously delivering an eligible change as one review-ready pull request or a linear stack with native git and gh.
---

<!-- managed-by: vgxness; artifact: opencode-skill/vgxness-autonomous-stacked-pr; version: 2 -->

# VGXNESS autonomous stacked PR

Use this policy only from the top-level VGXNESS Manager. Managed general remains the delegated implementation worker; the manager remains the Git and GitHub delivery actor by role. Use native `git` and `gh` commands directly. Do not introduce an adapter, custom typed Git or GitHub tool, stack engine, worktree writer, delivery daemon, or durable delivery state.

## Eligibility and restrictions

An eligible task is an authorized implementation in one clean Git checkout whose intended changed paths can be isolated and whose freeze, focused checks, independent verification, and selected review have succeeded. Announce routine autonomous delivery when loading this skill. The announcement is disclosure, not a request for another approval.

Task restrictions always win and narrow transitively:

- `local-only` means no commit, no push, no PR, and no merge;
- `no commit` means no commit, no push, no PR, and no merge;
- `no push` means no push, no PR, and no merge;
- `no PR` means no pull request and no merge;
- `no merge` means no merge; and
- `no cleanup` means no deletion of delivery branches, while merge and base synchronization remain eligible.

Never infer that a narrower restriction authorizes an earlier operation. Never carry delivery authority to another task. If the user names a different branch, remote, draft flow, or delivery operation, it is outside routine autonomous delivery and requires explicit current-task authorization; global tool permission does not supply that authorization.

## Sizing and stack plan

Resolve sizing with this precedence: an explicit task override, then an explicit durable project memory default, then the defaults below. Do not persist a one-task override.

- Target 400 effective changed lines per slice.
- Use one pull request at 800 effective changed lines or fewer.
- Stack when the planned change is more than 800 effective changed lines.

Estimate before implementation from the accepted scope and nearby evidence. The estimate guides only the initial plan. Initial branch creation uses the announced estimate and the verified start commit. After implementation, use numeric additions plus deletions from the worktree-inclusive one-commit `git diff --numstat` form for the exact intended candidate; this actual measurement supersedes the estimate. Treat binary entries, missing paths, unrelated changes, or an unexplained material estimate/actual mismatch as ineligible for routine delivery. If the actual total is more than 800 effective changed lines, re-plan before staging, commit, push, or PR creation.

Derive one deterministic delivery ID from the normalized task goal: lowercase ASCII letters and digits, replace each run of other characters with one hyphen, collapse and trim hyphens, use `task` if empty, and truncate to at most 48 characters without a trailing hyphen. Use branches named exactly `vgxness/<delivery-id>/slice-<ordinal>`, with positive decimal ordinals starting at 1.

Each slice must be independently reviewable, preserve behavior at every boundary, and form a linear immediate-parent topology. Slice 1 starts from the inspected base branch. Every later slice starts from the committed branch immediately before it. Keep one clean checkout and one writer; do not create another checkout. Every PR in a stack targets the same original inspected base branch, while `Depends-On` records the immediate predecessor PR. The merge commits preserve predecessor commits, so later PR diffs narrow after earlier slices land.

## Freeze and native delivery

Before any routine mutation, establish the expected HEAD, base branch and upstream, exact porcelain-v1 status including untracked files, intended paths, candidate identity, successful developmental checks, independent verification, and review outcome. Verify the remote URL and GitHub repository identity, validate every branch ref, test local, cached remote, and live remote ref existence, and prove immediate-parent ancestry and zero unexpected divergence. After implementation, inspect worktree-inclusive one-commit base/path name-only, name-status, raw, check, and numstat diffs. Measure each specific untracked path with the permitted no-index numstat form. Before commit, inspect cached name-only, check, numstat, and full diff forms. Verify GitHub authentication and use PR list/view/checks readback to detect or report existing delivery state. Preserve unrelated changes and stage only intended paths.

After those gates succeed, a fresh branch, normal commit, first push, and non-draft pull request need no second approval unless a task restriction forbids them. Use only these conceptual native command forms, with each placeholder replaced by a previously verified value:

- `git switch -c <head> <verified-start-commit>`
- `git commit -m "<type>(<scope>): <summary> [slice <ordinal>/<total>]"`
- `git push --set-upstream <verified-remote> <head>`
- `gh pr create --head <head> --base <base> --title "<title>" --body "<body>"`

Use only validated, option-free tokens for head, base, remote name, and start commit. Use the one-line PR title format `<summary> [<ordinal>/<total>]`. Use the comma-separated one-line PR body format `Stack: <delivery-id>, Slice: <ordinal>/<total>, Base: <base>, Head: <head>, Depends-On: <previous-PR-URL-or-none>`. The body emits stack metadata even for a one-slice delivery and contains no semicolon. The commit message, title, and body must each be one safe single argument: no newline, carriage return, control character, quote, backslash, shell metacharacter, substitution syntax, or option-shaped ` -` segment. Set every PR base to the original inspected base branch and record the created URL before constructing the next slice metadata.

## Current-task landing and cleanup

Ordinary eligible delivery grants current-task merge authorization only for PRs created by this same current task, after all freeze, verification, review, and delivery gates succeed and unless a restriction forbids merge. Existing branches and PRs are read-only resumption; they never receive retroactive merge or cleanup authority.

Land slices strictly in ordinal order, 1 through N. Before each merge, read back the exact current-task PR number, repository identity, head branch, original inspected base branch, non-draft state, expected head OID, predecessor merged state, conflict-free state, and successful required checks. `<repository>` is the verified GitHub `owner/repo` identity and `<expected-head-oid>` is a validated full commit OID. Set an expected base-tip OID per slice: slice 1 obtains it from the freshly fetched and read verified original base immediately before checks; after each successful predecessor merge, fetch and read back that base and advance the expected base-tip OID for the next slice. Check it before checks and again immediately before merge: the PR base ref and live verified remote base tip must equal that expected base-tip OID; stop on mismatch. A PR number must be a previously validated positive decimal PR number. Bounded waiting and readback may use `gh pr checks <number> --repo <repository> --watch --fail-fast`, but never merge failed, cancelled, pending, skipped-required, missing-required, or indeterminate checks. Re-read every value immediately before merge and stop on drift.

Use only the repository's allowed merge-commit method: `gh pr merge <number> --repo <repository> --merge --match-head-commit <expected-head-oid>`. Do not fall back to squash, rebase, force, admin, auto-merge, merge queue, or another merge method. Any rejected merge, host/auth or branch-protection ambiguity, conflict, identity/topology drift, changed remote, dirty worktree, or unsupported state stops further mutations and becomes read-only diagnosis and reporting.

After each merge, verify GitHub reports merged state and expected head/base/merge commit identity. Fetch and read back the original base branch, then prove it contains the merged slice head before continuing to the next ordinal slice.

Only after every slice lands and the worktree is clean, switch to the original base branch and update it strictly by fast-forward from the verified remote-tracking base. Unless `no cleanup` applies, delete only exact current-delivery local branches after proving they are merged and no open PR depends on them: use `git branch -d <head>`. The remote delivery branches are left intact. Never delete unrelated branches, worktrees, or content.

Routine automation does not cover any other post-create mutation, creating an additional checkout or worktree, release, history rewriting, credential or configuration changes, or recovery by destructive Git. Switching the existing checkout back to the original base is authorized only under the landing rules. Stop mutations and use read-only resumption: inspect local and remote metadata that is already available, report the exact topology and blocker, and wait for an explicit new task.

OpenCode command globs do not prove argv semantics. They are static last-match policy rules, not a shell parser or a guarantee about host behavior. Keep refs and paths within the normalized forms above, reject shell metacharacters or ambiguous arguments, and do not claim that a matching permission proves what Git, GitHub, credentials, branch protection, hooks, or the network will do.

---
name: stacked-pr
description: Use when autonomously delivering an eligible implementation as one review-ready pull request or a linear stack with native git and gh, including delivery narrowed by no merge or no cleanup; do not use for local-only, plan-only, read-only, or non-Git work.
license: MIT
compatibility: Agent Skills hosts with native git and gh access
metadata:
  version: "3"
  provenance: "VGXNESS portable global skill; migrated from the OpenCode-provider v3 policy"
---

<!-- managed-by: vgxness; artifact: global-skill/stacked-pr; version: 3 -->

# VGXNESS autonomous stacked PR

Use this policy only from the top-level VGXNESS Manager. Managed general remains the delegated implementation worker; the manager remains the Git and GitHub delivery actor by role. Use native `git` and `gh` commands directly. Do not introduce an adapter, custom typed Git or GitHub tool, stack engine, worktree writer, delivery daemon, or durable delivery state.

## Eligibility and restrictions

An eligible task is an authorized implementation in one clean Git checkout whose intended changed paths can be isolated. Do not announce routine autonomous delivery or delegate a workspace write until the pre-write gate below succeeds. The announcement is disclosure, not a request for another approval.

Task restrictions always win and narrow transitively: `local-only` and `no commit` mean no commit, push, PR, or merge; `no push` means no push, PR, or merge; `no PR` means no PR or merge; `no merge` means no merge; and `no cleanup` means no delivery-branch deletion while merge and base synchronization remain eligible. Never infer that a narrower restriction authorizes an earlier operation. Never carry delivery authority to another task. A different branch, remote, draft flow, or delivery operation requires explicit current-task authorization; global tool permission does not supply it.

## Sizing and stack plan

Resolve sizing with this precedence: an explicit task override, then an explicit durable project memory default, then these defaults. Do not persist a one-task override.

- Target 400 effective changed lines per slice.
- Use one pull request at 800 effective changed lines or fewer.
- Stack when the planned change is more than 800 effective changed lines.

Estimate before implementation from accepted scope and nearby evidence. After implementation, use numeric additions plus deletions from the worktree-inclusive one-commit `git diff --numstat` form for the exact intended candidate. Treat binary entries, missing paths, unrelated changes, or an unexplained material estimate/actual mismatch as ineligible. If actual size exceeds 800 lines, re-plan before staging, commit, push, or PR creation; remeasure after each bounded write transaction.

Derive a delivery ID from the normalized goal: lowercase ASCII letters and digits, replace runs of other characters with one hyphen, collapse and trim hyphens, use `task` if empty, and truncate to 48 characters without a trailing hyphen. Name branches `vgxness/<delivery-id>/slice-<ordinal>`, with positive decimal ordinals from 1. Each slice is independently reviewable and has linear immediate-parent topology in one clean checkout. Every PR targets the original inspected base; `Depends-On` identifies its immediate predecessor.

## Pre-write gate and native delivery

Before any source write, prove clean untracked-inclusive porcelain, expected HEAD, base/upstream/remote/repository/ref identity, intended paths, initial estimate and slice plan, deterministic fresh branch name, absence from the verified start commit, immediate-parent ancestry, and zero unexpected divergence. Then create the fresh branch before source writes. Dirty starts stop writes unless the bounded recovery gate succeeds.

Before staging, commit, push, PR, or merge, require candidate identity, successful developmental checks, independent verification, and review. Inspect worktree-inclusive one-commit base/path name-only, name-status, raw, check, and numstat diffs; measure untracked paths with permitted no-index numstat. Before commit inspect cached name-only, check, numstat, and full diffs. Verify GitHub authentication and read existing PR state. Stage only intended paths.

After these gates, a fresh branch, normal commit, first push, and non-draft PR need no second approval unless restricted. Use only:

- `git switch -c <head> <verified-start-commit>`
- `git commit -m "<type>(<scope>): <summary> [slice <ordinal>/<total>]"`
- `git push --set-upstream --force-with-lease=refs/heads/<head>: <verified-remote> refs/heads/<head>:refs/heads/<head>`
- `gh pr create --head <head> --base <base> --title "<title>" --body "<body>"`

Use validated option-free head, base, remote, and start-commit tokens. Titles are `<summary> [<ordinal>/<total>]`; bodies are the one-line comma-separated `Stack: <delivery-id>, Slice: <ordinal>/<total>, Base: <base>, Head: <head>, Depends-On: <previous-PR-URL-or-none>`, without a semicolon. Commit message, title, and body are each one safe argument: no newline, control character, quote, backslash, shell metacharacter, substitution syntax, or option-shaped ` -` segment. The create-only empty lease must fail if the remote ref exists; generic force, non-empty/ambiguous leases, normal-push fallback, and retry after branch appearance are forbidden.

## Recovery, landing, and cleanup

Bounded interrupted-local-slice recovery requires explicit current-task reauthorization, the deterministic branch name, exact verified base or predecessor OID, current HEAD and local branch equal to that OID, no upstream/live remote head/PR/staged change, unchanged identity, slice-only paths, a complete untracked-inclusive digest, actual size <=800, and repeated focused checks, independent verification, and review. It may create only a new current-task PR; never use stash, reset, another worktree, destructive recovery, or retroactive authority over existing remote branches or PRs.

Merge only PRs created by this task, in ordinal order, after exact repository/head/base/OID, predecessor, conflict, expected base-tip, and required-check readback. Use only `gh pr merge <number> --repo <repository> --merge --match-head-commit <expected-head-oid>`; never use squash, rebase, force, admin, auto-merge, or merge queue. After every merge verify merged state and base containment. After all slices land and the worktree is clean, fast-forward the original base from its verified remote-tracking branch. Unless `no cleanup`, delete only proven-merged current-delivery local branches with no open dependent PR; leave remote branches intact.

Any rejection, host/auth/protection ambiguity, conflict, topology drift, remote change, dirty worktree, or unsupported state stops mutation for read-only diagnosis. Command globs do not prove argv semantics or host behavior; reject shell metacharacters and ambiguous arguments, and never claim Git, GitHub, credentials, hooks, network, or branch protection will accept a command.

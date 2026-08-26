---
name: git-delivery
description: Use only when the Manager is authorized to deliver an eligible implementation as a review-ready PR or linear PR stack with native git and gh; do not use for local-only, no-push, no-PR, read-only, plan-only, or non-Git work.
license: MIT
compatibility: Agent Skills hosts with native git and gh access
metadata:
  version: "1"
---

<!-- managed-by: vgxness; artifact: global-skill/git-delivery; version: 1 -->

# VGXNESS Git delivery

Only the top-level Manager uses native `git` and `gh`; general never gains delivery authority. This is policy only, not a Git adapter, runtime worktree writer, daemon, durable delivery state, or host-enforcement claim.

## Gate and topology

Require current-task authorization, exact repository/common-dir/remote/start-OID identity, intended paths and scope, complete untracked-inclusive status digest, one writer, a fresh local/remote/PR-free branch, and an estimate before writes. `local-only`/`no commit` forbid commit, push, PR, and merge; `no push` forbids push, PR, and merge; `no PR` forbids PR and merge; `no merge` forbids merge; `no cleanup` forbids branch deletion. Restrictions narrow transitively and never authorize an earlier operation. Default to one clean checkout.

Resolve sizing by explicit task override, then explicit durable project default, then target 400 effective changed lines per slice: one PR at <=800, otherwise a linear stack. Estimate from accepted scope and nearby evidence; before staging and after each bounded write transaction, measure intended candidate additions plus deletions with worktree-inclusive one-commit `git diff --numstat` and permitted no-index untracked measurement. Binary, missing, unrelated, or materially unexplained entries block; >800 replans before delivery. Derive delivery ID from normalized lowercase ASCII goal (non-alphanumeric runs become one hyphen, trim, `task` if empty, max 48), and branches as `vgxness/<delivery-id>/slice-<positive ordinal>`.

Slice 1 targets the inspected original base; each later branch and PR targets its immediate predecessor and has `Depends-On: <previous PR URL>`. Slices are independently reviewable.

## Optional isolated worktree

Only after the gate may a dirty control checkout be isolated. Require exact control repo/common-dir/remote/start OID and complete status digest; explicit scope; one writer; no control branch or index mutation; canonical absolute non-symlink target under an authorized parent, absent and without case/platform ambiguity; `git worktree list --porcelain -z`; no target/branch collision; fresh local/remote/PR-free branch; and immediate-parent ancestry. Then use only `git worktree add -b <head> <path> <start>`.

Never adopt, overwrite, prune, force-remove, or mutate a foreign worktree. Session ownership is evidence only, never durable state. Recovery resumes only an exact task-created worktree; with explicit reauthorization it may create a fresh retry branch/worktree while preserving the interrupted worktree. Never stash, reset, destroy, or gain retroactive remote authority.

Cleanup follows merged readback only: prove owned path/gitdir/branch/HEAD, clean status, and no open dependent PR; use non-force worktree removal and delete only a proven-merged local branch. Leave remotes and unrelated worktrees intact. Any ambiguity blocks mutation for read-only diagnosis.

## Publication

Before staging, commit, push, PR, or merge require candidate identity, developmental checks, independent verification, review, and intended-path name-only/name-status/raw/check/numstat diffs; inspect cached equivalents before commit and stage only intended paths. Use only `git switch -c <head> <verified-start>`, one safe-argument normal commit, `git push --set-upstream --force-with-lease=refs/heads/<head>: <verified-remote> refs/heads/<head>:refs/heads/<head>`, and a non-draft `gh pr create --head <head> --base <base> --title <title> --body <body>`. Validate option-free tokens and safe single-argument message/title/body. The verified empty-expectation create-only lease and PR/readback are mandatory; never generic force, normal-push fallback, overwrite, or retry an appeared remote branch.

Merge only task-created PRs in ordinal order. Slice 1 may merge only after exact repository/head/base/OID, conflict, expected base-tip, and required-check readback. After a predecessor merges and is proven contained in the original base, explicitly retarget the next PR to that original base before its required checks and merge: validate option-free number and base tokens; read back the exact expected head and predecessor base; use only `gh pr edit <number> --base <original-base>`; then read back unchanged head and the original base. A wrong or premature base, changed head/base, failed checks, ambiguity, or host failure stops. No later-slice merge is permitted while its base remains the predecessor; retained remote branches do not establish landing eligibility. Use only `gh pr merge <number> --repo <repository> --merge --match-head-commit <expected-head-oid>`. Never squash, rebase, admin, auto-merge, queue, or force. Cleanup only after merged readback: prove owned path/gitdir/branch/HEAD, clean status, and no open dependent PR; use exact non-force `git worktree remove <path>` and `git branch -d <head>` only for a proven-merged local branch. Never prune or force-remove; leave remotes and unrelated worktrees intact. Any ambiguity, rejection, auth/protection/host uncertainty, topology drift, or remote change stops mutation for read-only diagnosis.

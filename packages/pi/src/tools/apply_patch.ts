import { chmod, lstat, mkdir, readFile, realpath, rename, rm, stat, writeFile } from "node:fs/promises";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { withFileMutationQueue } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { Value } from "typebox/value";
import { workerCanWrite } from "../workers/roles.ts";
import type { ToolHost } from "./memory.ts";

export const applyPatchSchema = Type.Object({ patch: Type.String({ minLength: 1, description: "Raw unified diff: --- old-path, +++ new-path, then numbered @@ hunks. Use workspace-relative paths. The parser rejects *** Begin Patch / *** Update File / *** End Patch wrappers. One-line replacement example:\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n" }) }, { additionalProperties: false });
type Hunk = { oldStart: number; oldCount: number; newStart: number; newCount: number; lines: string[]; oldNoNewline: boolean; newNoNewline: boolean };
type Edit = { oldPath: string | undefined; newPath: string | undefined; hunks: Hunk[] };
type Snapshot = { path: string; before: Buffer | undefined; after: Buffer | undefined; mode?: number; temporary?: string; restoreTemporary?: string; committed?: boolean; restored?: boolean };
export type ApplyPatchOptions = { fault?: (point: "after-stage" | "before-commit" | "after-commit" | "before-rollback") => void | Promise<void>; workerRole?: "general"; allowedTargets?: Record<string, string>; onWorkerWrite?: (targets: string[]) => Promise<void> };

function inWorkspace(root: string, path: string) {
  const value = relative(root, path);
  return value === "" || (!value.startsWith("..") && !isAbsolute(value));
}
function patchPath(value: string, root: string) {
  const path = value.trim().split("\t", 1)[0];
  if (path === "/dev/null") return undefined;
  const result = resolve(root, path.replace(/^[ab]\//, ""));
  if (!inWorkspace(root, result)) throw new Error("patch path escapes workspace");
  return result;
}
function parsePatch(patch: string, root: string): Edit[] {
  const lines = patch.replace(/\r\n/g, "\n").split("\n");
  const edits: Edit[] = [];
  let index = 0;
  while (index < lines.length) {
    if (lines[index] === "") { index++; continue; }
    if (!lines[index].startsWith("--- ")) throw new Error("expected old file header");
    const oldPath = patchPath(lines[index++].slice(4), root);
    if (!lines[index]?.startsWith("+++ ")) throw new Error("expected new file header");
    const newPath = patchPath(lines[index++].slice(4), root);
    if (!oldPath && !newPath) throw new Error("invalid null patch target");
    if (oldPath && newPath && oldPath !== newPath) throw new Error("patch rename targets are unsupported");
    const hunks: Hunk[] = [];
    while (index < lines.length && !lines[index].startsWith("--- ")) {
      if (lines[index] === "") { index++; continue; }
      const match = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?:.*)$/.exec(lines[index++]);
      if (!match) throw new Error("invalid hunk header");
      const hunk: Hunk = { oldStart: Number(match[1]), oldCount: Number(match[2] ?? 1), newStart: Number(match[3]), newCount: Number(match[4] ?? 1), lines: [], oldNoNewline: false, newNoNewline: false };
      while (index < lines.length && !lines[index].startsWith("@@ ") && !lines[index].startsWith("--- ")) {
        const line = lines[index++];
        if (line === "" && index === lines.length) break;
        if (line === "\\ No newline at end of file") {
          const previous = hunk.lines.at(-1);
          if (!previous) throw new Error("newline marker has no hunk line");
          hunk.oldNoNewline ||= previous[0] !== "+";
          hunk.newNoNewline ||= previous[0] !== "-";
          continue;
        }
        if (!/^[ +\-]/.test(line)) throw new Error("invalid hunk line");
        hunk.lines.push(line);
      }
      const oldLines = hunk.lines.filter((line) => line[0] !== "+").length;
      const newLines = hunk.lines.filter((line) => line[0] !== "-").length;
      if (oldLines !== hunk.oldCount || newLines !== hunk.newCount) throw new Error("hunk line count mismatch");
      hunks.push(hunk);
    }
    if (!hunks.length) throw new Error("patch has no hunks");
    edits.push({ oldPath, newPath, hunks });
  }
  const targets = edits.map((edit) => edit.newPath ?? edit.oldPath!);
  if (new Set(targets).size !== targets.length) throw new Error("patch contains duplicate targets");
  return edits;
}
function applyHunks(before: string, hunks: Hunk[]) {
  let trailing = before.endsWith("\n");
  const lines = before === "" ? [] : before.slice(0, trailing ? -1 : undefined).split("\n");
  let offset = 0;
  for (const hunk of hunks) {
    const at = (hunk.oldCount === 0 ? hunk.oldStart : hunk.oldStart - 1) + offset;
    const expected = hunk.lines.filter((line) => line[0] !== "+").map((line) => line.slice(1));
    const actual = lines.slice(at, at + hunk.oldCount);
    if (actual.length !== expected.length || actual.some((line, i) => line !== expected[i])) throw new Error("patch drift detected");
    if (hunk.oldNoNewline && (trailing || at + hunk.oldCount !== lines.length)) throw new Error("patch newline marker drift detected");
    const replacement = hunk.lines.filter((line) => line[0] !== "-").map((line) => line.slice(1));
    lines.splice(at, hunk.oldCount, ...replacement);
    offset += hunk.newCount - hunk.oldCount;
    if (at + hunk.newCount === lines.length) trailing = !hunk.newNoNewline;
    else if (hunk.newNoNewline) throw new Error("patch newline marker is not at file end");
  }
  return lines.length === 0 ? "" : lines.join("\n") + (trailing ? "\n" : "");
}
async function regular(path: string, root: string, absent = false) {
  const segments = relative(root, path).split(/[\\/]/).filter(Boolean);
  let ancestor = root;
  for (const segment of segments.slice(0, -1)) {
    ancestor = resolve(ancestor, segment);
    try {
      if ((await lstat(ancestor)).isSymbolicLink()) throw new Error("patch target has a symbolic-link ancestor");
    } catch (error: any) {
      if (error?.code !== "ENOENT") throw error;
      break;
    }
  }
  try {
    const info = await lstat(path);
    if (absent) throw new Error("patch add target already exists");
    if (!info.isFile() || info.isSymbolicLink() || !inWorkspace(root, await realpath(dirname(path)))) throw new Error("patch target is not a regular workspace file");
  } catch (error: any) {
    if (absent && error?.code === "ENOENT") return;
    throw error;
  }
}
async function owned(snapshot: Snapshot) {
  try {
    const info = await lstat(snapshot.path);
    if (!snapshot.after || !info.isFile() || info.isSymbolicLink()) return false;
    return (await readFile(snapshot.path)).equals(snapshot.after);
  } catch (error: any) { return snapshot.after === undefined && error?.code === "ENOENT"; }
}
export class PatchRecoveryError extends Error {
  readonly code = "recovery_pending";
  readonly retrySafe = false;
  readonly details: { code: string; retrySafe: boolean; affectedPaths: string[]; recoveryPaths: string[] };
  constructor(details: PatchRecoveryError["details"]) { super(`Patch recovery_pending; retrySafe=false; ${JSON.stringify(details)}`); this.name = "PatchRecoveryError"; this.details = details; }
}

export function createApplyPatchTool(host: ToolHost, options: ApplyPatchOptions = {}) {
  const tool = { name: "apply_patch", label: "Apply patch", description: "Apply one complete, preflighted unified diff inside the workspace. Supply ---/+++ file headers and numbered @@ -old,count +new,count @@ hunks; see the patch parameter example.", parameters: applyPatchSchema, executionMode: "sequential" as const,
    async execute(_id: string, input: { patch: string }, signal?: AbortSignal) {
      if (!Value.Check(applyPatchSchema, input)) throw new Error("invalid tool input");
      host.mutationGuard?.();
      signal?.throwIfAborted();
      const worker = options.workerRole !== undefined;
      if (host.mode !== "full" || (host.role !== "manager" && !worker) || (worker && (!options.allowedTargets || !workerCanWrite(options.workerRole!)))) throw new Error("patch requires authorized full authority");
      const root = await realpath(host.workspace);
      const rootIdentity = await stat(root);
      const verifyRoot = async () => { const current = await stat(root); if (current.dev !== rootIdentity.dev || current.ino !== rootIdentity.ino) throw new Error("workspace root changed during patch"); };
      await verifyRoot();
      const edits = parsePatch(input.patch, root);
      if (worker) for (const edit of edits) {
        const path = edit.newPath ?? edit.oldPath!;
        const target = relative(root, path);
        const expected = options.allowedTargets![target];
        if (!expected) throw new Error("worker patch target not authorized");
        try { const info = await lstat(path); if (info.isSymbolicLink()) throw new Error("worker patch target is symlink"); if (expected === "ABSENT" || createHash("sha256").update(await readFile(path)).digest("hex") !== expected) throw new Error("worker patch target drift"); } catch (error: any) { if (!(expected === "ABSENT" && error?.code === "ENOENT")) throw error; }
      }
      const snapshots: Snapshot[] = [];
      for (const edit of edits) {
        if (edit.oldPath) await regular(edit.oldPath, root);
        if (edit.newPath && edit.newPath !== edit.oldPath) await regular(edit.newPath, root, true);
        const before = edit.oldPath ? await readFile(edit.oldPath) : undefined;
        const applied = applyHunks((before ?? Buffer.alloc(0)).toString("utf8"), edit.hunks);
        if (!edit.newPath && applied !== "") throw new Error("delete patch must remove the complete file");
        const after = edit.newPath ? Buffer.from(applied) : undefined;
        snapshots.push({ path: edit.newPath ?? edit.oldPath!, before, after, mode: edit.oldPath ? (await stat(edit.oldPath)).mode & 0o7777 : undefined });
      }
      let recoveryPending = false;
      try {
        for (const snapshot of snapshots) {
          await verifyRoot();
          if (snapshot.before !== undefined) {
            snapshot.restoreTemporary = resolve(dirname(snapshot.path), `.${randomUUID()}.vgxness-restore`);
            await writeFile(snapshot.restoreTemporary, snapshot.before);
            await chmod(snapshot.restoreTemporary, snapshot.mode!);
          }
          if (snapshot.after === undefined) continue;
          await mkdir(dirname(snapshot.path), { recursive: true });
          await regular(snapshot.path, root, snapshot.before === undefined);
          snapshot.temporary = resolve(dirname(snapshot.path), `.${randomUUID()}.vgxness-patch`);
          await writeFile(snapshot.temporary, snapshot.after);
          if (snapshot.mode !== undefined) await chmod(snapshot.temporary, snapshot.mode);
        }
        await options.fault?.("after-stage");
        for (const snapshot of snapshots) {
          await verifyRoot();
          await regular(snapshot.path, root, snapshot.before === undefined);
          if (snapshot.before !== undefined && !(await readFile(snapshot.path)).equals(snapshot.before)) throw new Error("patch drift detected");
        }
        for (const snapshot of snapshots) {
          await verifyRoot();
          await options.fault?.("before-commit");
          await regular(snapshot.path, root, snapshot.before === undefined);
          if (snapshot.before !== undefined && !(await readFile(snapshot.path)).equals(snapshot.before)) throw new Error("patch drift detected");
          host.mutationGuard?.();
          signal?.throwIfAborted();
          if (snapshot.after === undefined) await rm(snapshot.path);
          else await rename(snapshot.temporary!, snapshot.path);
          snapshot.committed = true;
          await options.fault?.("after-commit");
        }
      } catch (error) {
        for (const snapshot of snapshots.filter((item) => item.committed).reverse()) {
          try {
            await verifyRoot();
            await options.fault?.("before-rollback");
            if (!(await owned(snapshot))) throw new Error("committed path drifted during recovery");
            if (snapshot.before === undefined) await rm(snapshot.path);
            else await rename(snapshot.restoreTemporary!, snapshot.path);
            snapshot.restored = true;
          } catch { recoveryPending = true; }
        }
        if (recoveryPending) {
          const affectedPaths = snapshots.filter((snapshot) => snapshot.committed && !snapshot.restored).map((snapshot) => snapshot.path).slice(0, 16);
          const recoveryPaths = snapshots.filter((snapshot) => snapshot.committed && !snapshot.restored && snapshot.restoreTemporary).map((snapshot) => snapshot.restoreTemporary!).slice(0, 16);
          throw new PatchRecoveryError({ code: "recovery_pending", retrySafe: false, affectedPaths, recoveryPaths });
        }
        throw error;
      } finally {
        // A replaced workspace root can make an old absolute temporary name
        // refer to an attacker-controlled new tree. Retain evidence instead
        // of deleting any path when the pinned root identity has changed.
        let rootStillOwned = true;
        try { await verifyRoot(); } catch { rootStillOwned = false; }
        if (rootStillOwned) await Promise.allSettled(snapshots.flatMap((snapshot) => [snapshot.temporary, snapshot.restoreTemporary].filter((path) => path && !(recoveryPending && snapshot.committed && !snapshot.restored && path === snapshot.restoreTemporary)).map((path) => rm(path!, { force: true }))));
      }
      if (worker) await options.onWorkerWrite?.(snapshots.map((snapshot) => relative(root, snapshot.path)));
      return { content: [{ type: "text", text: `Applied ${snapshots.length} file patch(es).` }] };
    },
  };
  const execute = tool.execute;
  tool.execute = async (id: string, input: { patch: string }, signal?: AbortSignal) => {
    if (!Value.Check(applyPatchSchema, input)) throw new Error("invalid tool input");
    const root = await realpath(host.workspace);
    const edits = parsePatch(input.patch, root);
    // Reject aliasing/symlink paths before acquiring multiple canonical Pi queues.
    for (const edit of edits) { if (edit.oldPath) await regular(edit.oldPath, root); if (edit.newPath && edit.newPath !== edit.oldPath) await regular(edit.newPath, root, true); }
    const targets = edits.map(edit => edit.newPath ?? edit.oldPath!).sort();
    const combined=host.mutationSignal?signal?AbortSignal.any([signal,host.mutationSignal()]):host.mutationSignal():signal; combined?.throwIfAborted();
    const locked = (index: number): Promise<any> => index === targets.length ? (combined?.throwIfAborted(), host.mutationGuard?.(), execute(id, input, combined)) : withFileMutationQueue(targets[index], () => locked(index + 1));
    return locked(0);
  };
  return tool;
}

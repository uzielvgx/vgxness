import assert from "node:assert/strict";
import test from "node:test";

import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { chmod, lstat, mkdtemp, mkdir, readFile, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { loadPiExtensions } from "./pi-fixture.mjs";
const { loadExtensions } = await loadPiExtensions();
import { loadNativeRuntime } from "./runtime-fixture.mjs";
const { createNativeDispatcher } = await loadNativeRuntime();

async function loadPatchTool(workspace: string, options: unknown = {}) {
  const { host: hostOptions, ...toolOptions } = options as any;
  (globalThis as any).__piTestHost = { workspace, mode: "full", role: "manager", backend: async () => ({ request: async () => ({ handle: "fixture", state: "active", leaseUntil: new Date(Date.now()+60000).toISOString() }) }), ...(hostOptions ?? {}) };
  (globalThis as any).__piTestOptions = toolOptions;
  const wrapper = join(workspace, "patch-extension.ts");
  const source = join(dirname(fileURLToPath(import.meta.url)), "../src/tools/apply_patch.ts");
  await writeFile(wrapper, `import { createApplyPatchTool } from ${JSON.stringify(source)}; export default async (pi) => pi.registerTool(createApplyPatchTool(globalThis.__piTestHost, globalThis.__piTestOptions));`);
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  return [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((item: any) => item.name === "apply_patch");
}

test("apply_patch checks authority immediately before each commit and still rolls back owned work", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-authority-"));
  await writeFile(join(workspace, "one.txt"), "one\n"); await writeFile(join(workspace, "two.txt"), "two\n");
  let guards = 0;
  const tool: any = await loadPatchTool(workspace, { host: { mutationGuard: () => { if (++guards >= 3) throw new Error("session authority unavailable"); } } });
  const patch = "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-one\n+ONE\n--- a/two.txt\n+++ b/two.txt\n@@ -1 +1 @@\n-two\n+TWO\n";
  await assert.rejects(tool.execute("authority", { patch }), /authority unavailable/);
  assert.equal(await readFile(join(workspace, "one.txt"), "utf8"), "one\n");
  assert.equal(await readFile(join(workspace, "two.txt"), "utf8"), "two\n");
});

test("apply_patch leaves a single file unchanged when authority expires before commit", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-authority-one-")); await writeFile(join(workspace, "one.txt"), "one\n");
  let guards = 0; const tool: any = await loadPatchTool(workspace, { host: { mutationGuard: () => { if (++guards >= 2) throw new Error("session authority unavailable"); } } });
  await assert.rejects(tool.execute("authority-one", { patch: "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-one\n+ONE\n" }), /authority unavailable/);
  assert.equal(await readFile(join(workspace, "one.txt"), "utf8"), "one\n");
});

test("Pi SDK registers native tools with closed operation schemas", async () => {
  const extension = join(dirname(fileURLToPath(import.meta.url)), "../src/extension.ts");
  const loaded = await loadExtensions([extension], process.cwd());
  assert.deepEqual(loaded.errors, []);
  const tools = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition);
  assert.equal(tools.length, 15);
  for (const tool of tools) {
    const schemas = tool.parameters.anyOf ?? [tool.parameters];
    assert.ok(schemas.every((schema: any) => schema.additionalProperties === false), tool.name);
  }
  assert.deepEqual(
    tools.map((tool) => tool.name).sort(),
    ["vgx_skill", "apply_patch", "memory", "memory_forget", "memory_get", "memory_recent", "memory_save", "memory_search", "model_resolve", "question", "sdd", "session_context", "session_handoff", "task", "todowrite"].sort(),
  );
});

test("apply_patch preflights multi-file patches, links, roles, and drift", async () => {
  const { symlink, readFile: read } = await import("node:fs/promises");
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-"));
  await writeFile(join(workspace, "one.txt"), "one\ntwo\nthree\n");
  await writeFile(join(workspace, "remove.txt"), "remove\n");
  (globalThis as any).__piTestHost = { workspace, mode: "full", role: "manager", backend: async () => ({ request: async () => ({ handle: "fixture", state: "active", leaseUntil: new Date(Date.now()+60000).toISOString() }) }) };
  const wrapper = join(workspace, "extension.ts");
  const source = join(dirname(fileURLToPath(import.meta.url)), "../src/extension.ts");
  await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piTestHost);`);
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  for (const handler of loaded.extensions[0].handlers.get("session_start") ?? []) await handler({ type: "session_start" }, { sessionManager: { getSessionId: () => "fixture" } });
  const tool: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((item: any) => item.name === "apply_patch");
  await tool.execute("multi", { patch: "--- a/one.txt\n+++ b/one.txt\n@@ -1,3 +1,3 @@\n-one\n+ONE\n two\n-three\n+THREE\n--- /dev/null\n+++ b/added.txt\n@@ -0,0 +1 @@\n+added\n--- a/remove.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-remove\n" });
  assert.equal(await read(join(workspace, "one.txt"), "utf8"), "ONE\ntwo\nTHREE\n");
  assert.equal(await read(join(workspace, "added.txt"), "utf8"), "added\n");
  await assert.rejects(read(join(workspace, "remove.txt"), "utf8"));
  const before = await read(join(workspace, "one.txt"), "utf8");
  await assert.rejects(tool.execute("drift", { patch: "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-wrong\n+nope\n" }));
  assert.equal(await read(join(workspace, "one.txt"), "utf8"), before);
  const addedBefore = await read(join(workspace, "added.txt"), "utf8");
  await assert.rejects(tool.execute("overwrite-add", { patch: "--- /dev/null\n+++ b/added.txt\n@@ -0,0 +1 @@\n+replacement\n" }));
  assert.equal(await read(join(workspace, "added.txt"), "utf8"), addedBefore);
  await assert.rejects(tool.execute("stale-delete", { patch: "--- a/added.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-not-added\n" }));
  assert.equal(await read(join(workspace, "added.txt"), "utf8"), addedBefore);
  await symlink(join(workspace, "one.txt"), join(workspace, "linked.txt"));
  await assert.rejects(tool.execute("link", { patch: "--- a/linked.txt\n+++ b/linked.txt\n@@ -1 +1 @@\n-ONE\n+nope\n" }));
  delete (globalThis as any).__piTestHost;
});

test("apply_patch rejects symlink ancestors and preserves modes and no-newline markers", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-boundaries-"));
  await mkdir(join(workspace, "real"));
  await writeFile(join(workspace, "real", "file.txt"), "before");
  await chmod(join(workspace, "real", "file.txt"), 0o754);
  await symlink(join(workspace, "real"), join(workspace, "linked-dir"));
  const tool: any = await loadPatchTool(workspace);
  await assert.rejects(tool.execute("ancestor-existing", { patch: "--- a/linked-dir/file.txt\n+++ b/linked-dir/file.txt\n@@ -1 +1 @@\n-before\n+after\n\\ No newline at end of file\n" }));
  await assert.rejects(tool.execute("ancestor-add", { patch: "--- /dev/null\n+++ b/linked-dir/add.txt\n@@ -0,0 +1 @@\n+new\n" }));
  await tool.execute("mode-newline", { patch: "--- a/real/file.txt\n+++ b/real/file.txt\n@@ -1 +1 @@\n-before\n\\ No newline at end of file\n+after\n\\ No newline at end of file\n" });
  assert.equal(await readFile(join(workspace, "real", "file.txt"), "utf8"), "after");
  assert.equal((await lstat(join(workspace, "real", "file.txt"))).mode & 0o777, 0o754);
  await writeFile(join(workspace, "real", "insert.txt"), "one\ntwo\n");
  await tool.execute("insertion", { patch: "--- a/real/insert.txt\n+++ b/real/insert.txt\n@@ -1,0 +2 @@\n+insert\n" });
  assert.equal(await readFile(join(workspace, "real", "insert.txt"), "utf8"), "one\ninsert\ntwo\n");
  await assert.rejects(tool.execute("partial-delete", { patch: "--- a/real/insert.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-one\n" }));
  assert.equal(await readFile(join(workspace, "real", "insert.txt"), "utf8"), "one\ninsert\ntwo\n");
  delete (globalThis as any).__piTestHost;
  delete (globalThis as any).__piTestOptions;
});

test("apply_patch restores delete-first commits and retains recovery evidence", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-recovery-"));
  await writeFile(join(workspace, "delete.txt"), "delete\n");
  await writeFile(join(workspace, "two.txt"), "two\n");
  let commits = 0;
  const rollbackTool: any = await loadPatchTool(workspace, { fault: (point: string) => { if (point === "after-commit" && ++commits === 1) throw new Error("injected commit failure"); } });
  const patch = "--- a/delete.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-delete\n--- a/two.txt\n+++ b/two.txt\n@@ -1 +1 @@\n-two\n+TWO\n";
  await assert.rejects(rollbackTool.execute("rollback", { patch }));
  assert.equal(await readFile(join(workspace, "delete.txt"), "utf8"), "delete\n");
  assert.equal(await readFile(join(workspace, "two.txt"), "utf8"), "two\n");
  const pendingTool: any = await loadPatchTool(workspace, { fault: (point: string) => { if (point === "after-commit") throw new Error("injected commit failure"); if (point === "before-rollback") throw new Error("injected recovery failure"); } });
  let result: any;
  await assert.rejects(pendingTool.execute("pending", { patch }), (error: any) => { result = error; return error.code === "recovery_pending" && /retrySafe=false/.test(error.message); });
  assert.equal(result.details.code, "recovery_pending");
  assert.equal(result.details.retrySafe, false);
  assert.deepEqual(result.details.affectedPaths, [join(workspace, "delete.txt")]);
  assert.equal(await readFile(result.details.recoveryPaths[0], "utf8"), "delete\n");
  await assert.rejects(readFile(join(workspace, "delete.txt"), "utf8"));
  delete (globalThis as any).__piTestHost;
  delete (globalThis as any).__piTestOptions;
});

test("full native manager initializes then saves and reads isolated memory", async () => {
  const root = join(dirname(fileURLToPath(import.meta.url)), "../../..");
  const temp = await mkdtemp(join(tmpdir(), "pi-tools-"));
  const binary = join(temp, "backend");
  const workspace = join(temp, "workspace");
  const storageRoot = join(temp, "storage");
  await mkdir(workspace);
  await mkdir(storageRoot);
  const client = await createNativeDispatcher({ workspace, storageRoot, mode: "full", role: "manager" });
  const binding = { workspace, mode: "full" as const, role: "manager" };
  try {
    await client.request("memory.project.initialize", {}, binding, "initialize");
    (globalThis as any).__piTestHost = { workspace, backend: async () => client };
    const wrapper = join(temp, "extension.ts");
    const source = join(root, "packages/pi/src/extension.ts").replace(/\\/g, "\\\\");
    await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piTestHost);`);
    const loaded = await loadExtensions([wrapper], workspace);
    assert.deepEqual(loaded.errors, []);
    for (const handler of loaded.extensions[0].handlers.get("session_start") ?? []) await handler({ type: "session_start" }, { sessionManager: { getSessionId: () => "fixture" } });
    const tools = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition);
    const save = tools.find((tool: any) => tool.name === "memory_save");
    const search = tools.find((tool: any) => tool.name === "memory_search");
    const applyPatch = tools.find((tool: any) => tool.name === "apply_patch");
    await save.execute("save", { title: "Pi fixture", content: "isolated full manager value", scope: "project" });
    const found: any = await search.execute("read", { query: "isolated full manager value", scope: "project", limit: 10 });
    assert.ok(JSON.stringify(found).includes("isolated full manager value"));
    await writeFile(join(workspace, "first.txt"), "before\n");
    await writeFile(join(workspace, "remove.txt"), "remove\n");
    await applyPatch.execute("patch", { patch: "--- a/first.txt\n+++ b/first.txt\n@@ -1 +1 @@\n-before\n+after\n--- /dev/null\n+++ b/added.txt\n@@ -0,0 +1 @@\n+added\n--- a/remove.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-remove\n" });
    assert.equal(await readFile(join(workspace, "first.txt"), "utf8"), "after\n");
    assert.equal(await readFile(join(workspace, "added.txt"), "utf8"), "added\n");
    await assert.rejects(applyPatch.execute("drift", { patch: "--- a/first.txt\n+++ b/first.txt\n@@ -1 +1 @@\n-before\n+again\n" }));
  } finally {
    delete (globalThis as any).__piTestHost;
    await client.close();
  }
});

test("apply_patch shares Pi's file mutation queue with built-in write", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-queue-"));
  await writeFile(join(workspace, "file.txt"), "before\n");
  let staged!: () => void, release!: () => void;
  const entered = new Promise<void>(resolve => { staged = resolve; }), gate = new Promise<void>(resolve => { release = resolve; });
  const patchTool = await loadPatchTool(workspace, { fault: async (point: string) => { if (point === "after-stage") { staged(); await gate; } } });
  assert.equal(patchTool.executionMode, "sequential");
  const wrapper = join(workspace, "builtin.ts");
  await writeFile(wrapper, 'import { createWriteToolDefinition } from "@earendil-works/pi-coding-agent"; export default pi => pi.registerTool(createWriteToolDefinition(' + JSON.stringify(workspace) + '));');
  const loaded = await loadExtensions([wrapper], workspace); assert.deepEqual(loaded.errors, []);
  const write: any = [...loaded.extensions[0].tools.values()][0].definition;
  const patch = patchTool.execute("patch", { patch: "--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-before\n+patched\n" });
  await entered;
  let written = false;
  const pending = write.execute("builtin", { path: "file.txt", content: "builtin\n" }).then(() => { written = true; });
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(written, false);
  assert.equal(await readFile(join(workspace, "file.txt"), "utf8"), "before\n");
  release(); await patch; await pending;
  assert.equal(await readFile(join(workspace, "file.txt"), "utf8"), "builtin\n");
  delete (globalThis as any).__piTestHost; delete (globalThis as any).__piTestOptions;
});
test("apply_patch cancellation preserves original files before and during commits",async t=>{const fs=await import("node:fs/promises"),root=await mkdtemp(join(tmpdir(),"pi-patch-cancel-"));t.after(()=>fs.rm(root,{recursive:true,force:true}));const one=join(root,"one"),two=join(root,"two");await writeFile(one,"one\n");await writeFile(two,"two\n");const patch="--- a/one\n+++ b/one\n@@ -1 +1 @@\n-one\n+ONE\n--- a/two\n+++ b/two\n@@ -1 +1 @@\n-two\n+TWO\n";const pre=new AbortController();pre.abort();await assert.rejects((await loadPatchTool(root)).execute("x",{patch},pre.signal));assert.equal(await readFile(one,"utf8"),"one\n");let commits=0;const c=new AbortController();const tool=await loadPatchTool(root,{fault:(p:string)=>{if(p==="before-commit"&&++commits===2)c.abort()}});await assert.rejects(tool.execute("x",{patch},c.signal),e=>{assert.doesNotMatch(String(e),/recovery_pending/);return true});assert.equal(await readFile(one,"utf8"),"one\n");assert.equal(await readFile(two,"utf8"),"two\n");});

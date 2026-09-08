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
import { startBackend } from "../src/backend/supervisor.ts";
import type { Hello } from "../src/backend/protocol.ts";

async function loadPatchTool(workspace: string, options: unknown = {}) {
  (globalThis as any).__piTestHost = { workspace, mode: "full", role: "manager", backend: async () => { throw new Error("not used"); } };
  (globalThis as any).__piTestOptions = options;
  const wrapper = join(workspace, "patch-extension.ts");
  const source = join(dirname(fileURLToPath(import.meta.url)), "../src/tools/apply_patch.ts");
  await writeFile(wrapper, `import { createApplyPatchTool } from ${JSON.stringify(source)}; export default async (pi) => pi.registerTool(createApplyPatchTool(globalThis.__piTestHost, globalThis.__piTestOptions));`);
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  return [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((item: any) => item.name === "apply_patch");
}

test("Pi SDK registers native tools with closed operation schemas", async () => {
  const extension = join(dirname(fileURLToPath(import.meta.url)), "../src/extension.ts");
  const loaded = await loadExtensions([extension], process.cwd());
  assert.deepEqual(loaded.errors, []);
  const tools = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition);
  assert.equal(tools.length, 14);
  for (const tool of tools) {
    const schemas = tool.parameters.anyOf ?? [tool.parameters];
    assert.ok(schemas.every((schema: any) => schema.additionalProperties === false), tool.name);
  }
  assert.deepEqual(
    tools.map((tool) => tool.name).sort(),
    ["apply_patch", "memory", "memory_forget", "memory_get", "memory_recent", "memory_save", "memory_search", "model_resolve", "question", "sdd", "session_context", "session_handoff", "task", "todowrite"].sort(),
  );
});

test("apply_patch preflights multi-file patches, links, roles, and drift", async () => {
  const { symlink, readFile: read } = await import("node:fs/promises");
  const workspace = await mkdtemp(join(tmpdir(), "pi-patch-"));
  await writeFile(join(workspace, "one.txt"), "one\ntwo\nthree\n");
  await writeFile(join(workspace, "remove.txt"), "remove\n");
  (globalThis as any).__piTestHost = { workspace, mode: "full", role: "manager", backend: async () => { throw new Error("not used"); } };
  const wrapper = join(workspace, "extension.ts");
  const source = join(dirname(fileURLToPath(import.meta.url)), "../src/extension.ts");
  await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piTestHost);`);
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
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
  const result = await pendingTool.execute("pending", { patch });
  assert.equal(result.isError, true);
  assert.equal(result.details.code, "recovery_pending");
  assert.equal(result.details.retrySafe, false);
  assert.deepEqual(result.details.affectedPaths, [join(workspace, "delete.txt")]);
  assert.equal(await readFile(result.details.recoveryPaths[0], "utf8"), "delete\n");
  await assert.rejects(readFile(join(workspace, "delete.txt"), "utf8"));
  delete (globalThis as any).__piTestHost;
  delete (globalThis as any).__piTestOptions;
});

test("full manager sidecar initializes then saves and reads isolated memory", async () => {
  const root = join(dirname(fileURLToPath(import.meta.url)), "../../..");
  const temp = await mkdtemp(join(tmpdir(), "pi-tools-"));
  const binary = join(temp, "backend");
  const workspace = join(temp, "workspace");
  const storageRoot = join(temp, "storage");
  await mkdir(workspace);
  await mkdir(storageRoot);
  const build = spawnSync("go", ["build", "-o", binary, "./cmd/vgxness-pi-backend"], { cwd: root, env: { ...process.env, GOPROXY: "off", GOSUMDB: "off" } });
  assert.equal(build.status, 0, build.stderr.toString());
  const probe = spawn(binary, ["--protocol", "vgxness-pi/v1", "--workspace", workspace, "--mode", "full", "--role", "manager", "--storage-root", storageRoot], { stdio: ["pipe", "pipe", "pipe"] });
  const hello = await new Promise<any>((resolve, reject) => {
    let buffer = "";
    probe.stdout.on("data", (chunk) => { buffer += chunk; const newline = buffer.indexOf("\n"); if (newline >= 0) resolve(JSON.parse(buffer.slice(0, newline))); });
    probe.on("error", reject);
  });
  const probeExit = new Promise<void>((resolve) => probe.once("exit", () => resolve()));
  probe.kill("SIGTERM");
  await Promise.race([probeExit, new Promise((resolve) => setTimeout(resolve, 100))]);
  if (probe.exitCode == null && probe.signalCode == null) probe.kill("SIGKILL");
  await probeExit;
  hello.implementation.sha256 = createHash("sha256").update(await readFile(binary)).digest("hex");
  const client = await startBackend({ binary, workspace, storageRoot, mode: "full", role: "manager", hello: hello as Hello });
  const binding = { workspace, mode: "full" as const, role: "manager" };
  try {
    await client.request("memory.project.initialize", {}, binding, "initialize");
    (globalThis as any).__piTestHost = { workspace, backend: async () => client };
    const wrapper = join(temp, "extension.ts");
    const source = join(root, "packages/pi/src/extension.ts").replace(/\\/g, "\\\\");
    await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piTestHost);`);
    const loaded = await loadExtensions([wrapper], workspace);
    assert.deepEqual(loaded.errors, []);
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

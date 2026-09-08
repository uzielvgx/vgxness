import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, mkdir, rm, symlink, stat, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { loadNativeRuntime } from "./runtime-fixture.mjs";
const { createNativeDispatcher } = await loadNativeRuntime();
test("native storage rejects missing read-only database and symlink storage without creating files", async t => {
  const root = await mkdtemp(join(tmpdir(), "pi-storage-")); t.after(() => rm(root, { recursive: true, force: true }));
  const workspace = join(root, "workspace"), storageRoot = join(root, "absent"); await mkdir(workspace);
  await assert.rejects(createNativeDispatcher({ workspace, storageRoot, mode: "read-only", role: "explore" }));
  await assert.rejects(stat(storageRoot));
  await mkdir(join(root, "outside")); await symlink(join(root, "outside"), join(root, "link"));
  await assert.rejects(createNativeDispatcher({ workspace, storageRoot: join(root, "link", "child"), mode: "full", role: "manager" }), /symlink/);
  await assert.rejects(stat(join(root, "outside", "child")));
  await assert.rejects(createNativeDispatcher({ workspace, storageRoot: "relative", mode: "full", role: "manager" }), /absolute/);
});
test("extension defaults directly to native dispatcher with no backend launcher imports", async () => {
  const source = await readFile(new URL("../src/extension.ts", import.meta.url), "utf8");
  assert.match(source, /createNativeDispatcher\(\{ workspace, storageRoot:/);
  assert.doesNotMatch(source, /selectBackend|startBackend|backend\/supervisor|backend\/select/);
});

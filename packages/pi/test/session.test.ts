import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createHash } from "node:crypto";
import { spawn, spawnSync } from "node:child_process";
import { loadPiExtensions } from "./pi-fixture.mjs";
const { loadExtensions } = await loadPiExtensions();
import { discoverSkillPaths } from "../src/skills/catalog.ts";
import { loadNativeRuntime } from "./runtime-fixture.mjs";
const { createNativeDispatcher } = await loadNativeRuntime();

test("session lifecycle uses private handles and skips read-only persistence", async (t) => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-session-"));
  t.after(() => rm(workspace, { recursive: true, force: true }));
  const calls: Array<{ operation: string; payload: any }> = [];
  const source = join(process.cwd(), "src/extension.ts");
  const wrapper = join(workspace, "extension.ts");
  await (await import("node:fs/promises")).writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piSessionOptions);`);
  (globalThis as any).__piSessionOptions = { workspace, mode: "read-only", role: "observer", backend: async () => ({ request: async (operation: string, payload: any) => { calls.push({ operation, payload }); return { handle: "private", updatedAt: new Date().toISOString() }; } }) };
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  const handlers = loaded.extensions[0].handlers;
  const ctx: any = { sessionManager: { getSessionId: () => "session-id" } };
  for (const handler of handlers.get("session_start") ?? []) await handler({ type: "session_start", reason: "new" }, ctx);
  for (const handler of handlers.get("session_compact") ?? []) await handler({ type: "session_compact" }, ctx);
  assert.deepEqual(calls, []);
  delete (globalThis as any).__piSessionOptions;
});

test("session lifecycle checkpoints, renews, and closes only its private handle", async (t) => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-session-full-"));
  t.after(() => rm(workspace, { recursive: true, force: true }));
  const calls: Array<{ operation: string; payload: any }> = [];
  const source = join(process.cwd(), "src/extension.ts");
  const wrapper = join(workspace, "extension.ts");
  await (await import("node:fs/promises")).writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piSessionOptions);`);
  (globalThis as any).__piSessionOptions = { workspace, mode: "full", role: "manager", backend: async () => ({ request: async (operation: string, payload: any) => { calls.push({ operation, payload }); return { handle: "private-handle", updatedAt: new Date().toISOString() }; } }) };
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  const handlers = loaded.extensions[0].handlers;
  const promptResult: any = await (handlers.get("before_agent_start") ?? [])[0]({ type: "before_agent_start", systemPrompt: "base" }, {});
  assert.match(promptResult.systemPrompt, /^base\n\n/);
  assert.match(promptResult.systemPrompt, /VGXNESS Manager/);
  const handoff: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((tool: any) => tool.name === "session_handoff");
  await assert.rejects(handoff.execute("unavailable", { summary: "safe" }), /no writable active session/);
  const ctx: any = { sessionManager: { getSessionId: () => "sdk-session" } };
  for (const handler of handlers.get("session_start") ?? []) await handler({ type: "session_start", reason: "resume" }, ctx);
  await handoff.execute("handoff", { summary: "explicit safe handoff" });
  for (const handler of handlers.get("session_compact") ?? []) await handler({ type: "session_compact" }, ctx);
  for (const handler of handlers.get("agent_settled") ?? []) await handler({ type: "agent_settled" }, ctx);
  for (const handler of handlers.get("session_shutdown") ?? []) await handler({ type: "session_shutdown", reason: "quit" }, ctx);
  assert.deepEqual(calls.map((call) => call.operation), ["memory.session.start", "memory.session.context", "memory.session.draft_save", "memory.session.checkpoint", "memory.session.renew", "memory.session.end"]);
  assert.equal(calls[0].payload.externalId, "sdk-session");
  assert.deepEqual(calls[2].payload, { handle: "private-handle", summary: "explicit safe handoff" });
  assert.deepEqual(calls[5].payload, { handle: "private-handle", state: "completed", summary: "" });
  delete (globalThis as any).__piSessionOptions;
});

test("skill discovery prefers compatible shared skills and de-duplicates bundled fallbacks", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "pi-skills-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const shared = join(root, "shared"), bundled = join(root, "bundled");
  for (const [base, name, body] of [[shared, "shared", "name: shared\ncompatibility: Agent Skills hosts with test\nprovenance: 'VGXNESS portable global skill'\n"], [bundled, "shared", "name: shared\ncompatibility: Agent Skills hosts with test\nprovenance: 'VGXNESS bundled skill'\n"], [bundled, "fallback", "name: fallback\ncompatibility: Agent Skills hosts with test\nmanaged-by: vgxness\n"]] as const) {
    await mkdir(join(base, name), { recursive: true });
    await writeFile(join(base, name, "SKILL.md"), body);
  }
  assert.deepEqual(await discoverSkillPaths({ sharedRoot: shared, bundledRoot: bundled }), [join(shared, "shared"), join(bundled, "fallback")]);
  assert.deepEqual(await discoverSkillPaths({ sharedRoot: shared, bundledRoot: bundled, existingNames: ["shared", "fallback"] }), []);
  await mkdir(join(shared, "linked"));
  await symlink(join(shared, "shared", "SKILL.md"), join(shared, "linked", "SKILL.md"));
  await mkdir(join(shared, "incompatible"));
  await writeFile(join(shared, "incompatible", "SKILL.md"), "name: incompatible\ncompatibility: test\nmanaged-by: vgxness\n");
  await mkdir(join(shared, "oversized"));
  await writeFile(join(shared, "oversized", "SKILL.md"), "name: oversized\ncompatibility: Agent Skills hosts with test\nmanaged-by: vgxness\n" + "x".repeat(262145));
  assert.deepEqual(await discoverSkillPaths({ sharedRoot: shared, bundledRoot: bundled, existingNames: ["shared", "fallback"] }), []);
});

test("temporary native storage accepts only explicit session handoff content", async (t) => {
  const root = join(process.cwd(), "../..");
  const temp = await mkdtemp(join(tmpdir(), "pi-session-go-"));
  t.after(() => rm(temp, { recursive: true, force: true }));
  const binary = join(temp, "backend"), workspace = join(temp, "workspace"), storageRoot = join(temp, "storage");
  await mkdir(workspace); await mkdir(storageRoot);
  const client = await createNativeDispatcher({ workspace, storageRoot, mode: "full", role: "manager" });
  t.after(() => client.close());
  const binding = { workspace, mode: "full" as const, role: "manager" };
  await client.request("memory.project.initialize", {}, binding, "initialize");
  const first: any = await client.request("memory.session.start", { externalId: "sdk-one" }, binding, "start");
  const context: any = await client.request("memory.session.context", { handle: first.handle }, binding, "context");
  assert.equal(context.handoff, "");
  await client.request("memory.session.draft_save", { handle: first.handle, summary: "explicit safe handoff" }, binding, "draft");
  await client.request("memory.session.checkpoint", { handle: first.handle }, binding, "checkpoint");
  await client.request("memory.session.renew", { handle: first.handle }, binding, "renew");
  await client.request("memory.session.end", { handle: first.handle, state: "completed", summary: "explicit safe handoff" }, binding, "end");
  const source = join(process.cwd(), "src/extension.ts");
  const wrapper = join(temp, "extension.ts");
  await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__piRealOptions);`);
  (globalThis as any).__piRealOptions = { workspace, mode: "full", role: "manager", backend: async () => client };
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  const ctx: any = { sessionManager: { getSessionId: () => "sdk-two" } };
  for (const handler of loaded.extensions[0].handlers.get("session_start") ?? []) await handler({ type: "session_start", reason: "resume" }, ctx);
  const contextTool: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((tool: any) => tool.name === "session_context");
  const handoff: any = await contextTool.execute("context", {});
  assert.match(JSON.stringify(handoff), /explicit safe handoff/);
  assert.doesNotMatch(JSON.stringify(handoff), /private-handle|raw transcript/);
  delete (globalThis as any).__piRealOptions;
});

test("extension rejects stale, tampered, or incomplete SDD task input references", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-sdd-bindings-"));
  const source = join(process.cwd(), "src/extension.ts"), wrapper = join(workspace, "extension.ts");
  const digest = (value: string) => createHash("sha256").update(value).digest("hex");
  const taskContent = "{}", task = { artifactId: "tasks", revisionId: "task", digest: digest(taskContent) };
  const specContent = "spec", designContent = "design";
  const spec = { artifactId: "spec", revisionId: "spec", digest: digest(specContent) }, design = { artifactId: "design", revisionId: "design", digest: digest(designContent) };
  const revisions: any = { task: { ...task, artifact: "tasks", id: "task", changeId: "change", content: Buffer.from(taskContent).toString("base64"), status: "accepted", artifactStatus: "accepted", inputs: [spec, design] }, spec: { ...spec, artifact: "spec", id: "spec", changeId: "change", content: Buffer.from(specContent).toString("base64"), status: "accepted", artifactStatus: "accepted" }, design: { ...design, artifact: "design", id: "design", changeId: "change", content: Buffer.from(designContent).toString("base64"), status: "accepted", artifactStatus: "accepted" } };
  await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__sddOptions);`);
  (globalThis as any).__sddOptions = { workspace, mode: "full", role: "manager", workerCli: process.execPath, backend: async () => ({ verifyCurrentAcceptedBinding: async () => true, request: async (operation: string, payload: any) => operation === "sdd.get_revision" ? revisions[payload.revisionId] : { phase: "apply", status: "active", stateVersion: 7 } }) };
  const loaded = await loadExtensions([wrapper], workspace); assert.deepEqual(loaded.errors, []);
  const tool: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((item: any) => item.name === "task");
  const base = { goal: "apply", nonce: crypto.randomUUID(), role: "sdd-apply", mode: "full", model: "provider/model", effort: "low", criteria: ["apply"], commands: [], resultLimit: 100, targets: {}, acceptedBindings: { changeId: "change", ...task, stateVersion: 7, inputs: [spec] } };
  await assert.rejects(tool.execute("task", base), /accepted binding rejected/);
  await assert.rejects(tool.execute("task", { ...base, nonce: crypto.randomUUID(), acceptedBindings: { ...base.acceptedBindings, inputs: [spec, { ...design, digest: "a".repeat(64) }] } }), /accepted binding rejected/);
  revisions.design.status = "draft";
  await assert.rejects(tool.execute("task", { ...base, nonce: crypto.randomUUID(), acceptedBindings: { ...base.acceptedBindings, inputs: [spec, design] } }), /accepted binding rejected/);
  delete (globalThis as any).__sddOptions;
});

test("model resolution uses only trusted scoped runtime candidates and native efforts", async () => {
  const workspace = await mkdtemp(join(tmpdir(), "pi-model-catalog-"));
  const source = join(process.cwd(), "src/extension.ts"), wrapper = join(workspace, "extension.ts");
  const calls: any[] = [];
  const model = { provider: "fixture", id: "family/model", name: "Fixture", reasoning: true, thinkingLevelMap: { low: null, medium: "medium", high: "high" } };
  await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(source)}; export default createPiExtension(globalThis.__modelOptions);`);
  (globalThis as any).__modelOptions = { workspace, backend: async () => ({ request: async (operation: string, payload: any) => { calls.push({ operation, payload }); return { ok: true }; } }) };
  const loaded = await loadExtensions([wrapper], workspace); assert.deepEqual(loaded.errors, []);
  const handler: any = (loaded.extensions[0].handlers.get("before_agent_start") ?? [])[0];
  await handler({ type: "before_agent_start", systemPrompt: "base" }, { model, scopedModels: [{ provider: "fixture", id: "family/model" }], modelRegistry: { getAvailable: () => [model] } });
  const resolve: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((item: any) => item.name === "model_resolve");
  await resolve.execute("resolve", { plan: "ultra" });
  assert.equal(calls[0].operation, "model.resolve");
  assert.deepEqual(calls[0].payload.catalog, { provider: "fixture", models: [{ provider: "fixture", id: "family/model", name: "Fixture", supportedEfforts: ["medium", "high"] }] });
  delete (globalThis as any).__modelOptions;
});

test("native role mapping preserves CARE roles and rejects manager", async () => {
  const { nativeWorkerRole } = await import("../src/models/plan.ts");
  assert.equal(nativeWorkerRole("implementation"), "general");
  assert.equal(nativeWorkerRole("care-specialist"), "care-specialist");
  assert.equal(nativeWorkerRole("sdd-apply"), "sdd-apply");
  assert.throws(() => nativeWorkerRole("manager"));
});

test("OpenSpec apply bindings verify exact tasks and input file bytes without following symlinks", async t => {
 const workspace = await mkdtemp(join(tmpdir(), "pi-external-binding-")); t.after(() => rm(workspace, { recursive: true, force: true }));
 const directory = join(workspace, "openspec", "changes", "change"); await mkdir(directory, { recursive: true });
 const tasks = Buffer.from("accepted tasks"), design = Buffer.from("accepted design"), digest = (value: Buffer) => createHash("sha256").update(value).digest("hex");
 await writeFile(join(directory, "tasks.md"), tasks); await writeFile(join(directory, "design.md"), design);
 const input = { artifactId: "design-artifact", revisionId: "design-revision", digest: digest(design) };
 const task = { artifactId: "tasks-artifact", revisionId: "tasks-revision", digest: digest(tasks) };
 const revisions: any = {
  "tasks-revision": { id: task.revisionId, artifactId: task.artifactId, artifact: "tasks", changeId: "change", digest: task.digest, externalLocation: "openspec/changes/change/tasks.md", status: "accepted", artifactStatus: "accepted", inputs: [input] },
  "design-revision": { id: input.revisionId, artifactId: input.artifactId, artifact: "design", changeId: "change", digest: input.digest, externalLocation: "openspec/changes/change/design.md", status: "accepted", artifactStatus: "accepted" },
 };
 const wrapper = join(workspace, "extension.ts");
 await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(join(process.cwd(), "src/extension.ts"))}; export default createPiExtension(globalThis.__externalBindingOptions);`);
 (globalThis as any).__externalBindingOptions = { workspace, workerCli: process.execPath, backend: async () => ({ verifyCurrentAcceptedBinding: async () => true, request: async (operation: string, payload: any) => operation === "sdd.get_revision" ? revisions[payload.revisionId] : { phase: "apply", status: "active", stateVersion: 7 } }) };
 t.after(() => { delete (globalThis as any).__externalBindingOptions; });
 const loaded = await loadExtensions([wrapper], workspace); assert.deepEqual(loaded.errors, []);
 const tool: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((item: any) => item.name === "task");
 const value = { goal: "apply", nonce: crypto.randomUUID(), role: "sdd-apply", mode: "full", model: "provider/model", effort: "low", criteria: ["apply"], commands: [], resultLimit: 100, targets: {}, acceptedBindings: { changeId: "change", ...task, stateVersion: 7, inputs: [input] } };
 // This reaches model validation only after accepted file bindings pass; it never calls a model.
 await assert.rejects(tool.execute("valid", value), /model or effort unsupported/);
 revisions["tasks-revision"].artifact = "design"; await assert.rejects(tool.execute("wrong-kind", value), /accepted binding rejected/); revisions["tasks-revision"].artifact = "tasks";
 await writeFile(join(directory, "tasks.md"), "drift"); await assert.rejects(tool.execute("drift", value), /accepted binding rejected/); await writeFile(join(directory, "tasks.md"), tasks);
 revisions["tasks-revision"].externalLocation = "../tasks.md"; await assert.rejects(tool.execute("escape", value), /accepted binding rejected/); revisions["tasks-revision"].externalLocation = "openspec/changes/change/tasks.md";
 await rm(join(directory, "design.md")); await writeFile(join(workspace, "external.md"), design); await symlink(join(workspace, "external.md"), join(directory, "design.md"));
 await assert.rejects(tool.execute("symlink", value), /accepted binding rejected/);
});

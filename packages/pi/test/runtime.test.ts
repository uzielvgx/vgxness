import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm, stat } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { loadPiExtensions } from "./pi-fixture.mjs";
import { loadNativeRuntime } from "./runtime-fixture.mjs";
const { loadExtensions } = await loadPiExtensions();
const { createNativeDispatcher } = await loadNativeRuntime();
async function fixture(t: test.TestContext) {
 const root = await mkdtemp(join(tmpdir(), "pi-runtime-")), workspace = join(root, "workspace"), storageRoot = join(root, "storage"); await mkdir(workspace);
 t.after(() => rm(root, { recursive: true, force: true }));
 const wrapper = join(root, "extension.ts");
 await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(new URL('../src/extension.ts', import.meta.url).pathname)}; export default createPiExtension(${JSON.stringify({ workspace, storageRoot })});`);
 const loaded = await loadExtensions([wrapper], workspace); assert.deepEqual(loaded.errors, []);
 const extension = loaded.extensions[0], ctx = { sessionManager: { getSessionId: () => "runtime-session" }, ui: { notify() {} } };
 const fire = async (name: string, event: any = {}) => { for (const handler of extension.handlers.get(name) ?? []) await handler({ type: name, ...event }, ctx); };
 const tool = (name: string): any => [...extension.tools.values()].map((x: any) => x.definition).find((x: any) => x.name === name);
 return { root, workspace, storageRoot, extension, ctx, fire, tool };
}
test("native views register alongside tools, report unavailable stores without creating them, and work without UI", async t => {
 const f = await fixture(t);
 for (const name of ["vgx-status", "vgx-workers", "vgx-memory", "vgx-sdd"]) assert.ok(f.extension.commands.has(name));
 const status = await f.extension.commands.get("vgx-status")!.handler("", {}); assert.match(JSON.stringify(status), /native-typescript/);
 const unavailable = await f.extension.commands.get("vgx-memory")!.handler("", {}); assert.match(JSON.stringify(unavailable), /unavailable/); await assert.rejects(stat(f.storageRoot));
 await f.fire("session_start", { reason: "startup" });
 await f.tool("memory_save").execute("save", { content: "visible durable evidence" });
 const memory = await f.extension.commands.get("vgx-memory")!.handler("", f.ctx); assert.match(JSON.stringify(memory), /visible durable evidence/);
 await f.fire("session_shutdown", { reason: "quit" });
 const binding = { workspace: f.workspace, mode: "read-only", role: "manager" }, client = await createNativeDispatcher({ ...binding, storageRoot: f.storageRoot });
 try { const recent = await client.request("memory.recent", {}, binding); assert.equal(recent.length, 1); assert.equal(recent[0].Type, "learning"); } finally { await client.close(); }
});
test("reload, compact, tree and resume preserve only an explicit draft and reacquire local authority", async t => {
 const f = await fixture(t);
 await f.fire("session_start", { reason: "startup" });
 await f.tool("session_handoff").execute("draft", { summary: "explicit handoff before reload" });
 await f.fire("session_compact", { reason: "manual" }); await f.fire("session_tree", { newLeafId: "untrusted-leaf-with-authority" });
 await f.fire("session_shutdown", { reason: "reload" });
 await f.fire("session_start", { reason: "reload" });
 await f.tool("session_handoff").execute("draft2", { summary: "explicit handoff after reload" });
 await f.fire("session_shutdown", { reason: "quit" });
 await f.fire("session_start", { reason: "resume" });
 const context = await f.tool("session_context").execute("context", {}); assert.match(JSON.stringify(context), /explicit handoff after reload/); assert.doesNotMatch(JSON.stringify(context), /untrusted-leaf-with-authority/);
 await f.fire("session_shutdown", { reason: "fork" });
});

test("session adapter renews a long active lease and stops on detach", async () => {
 const { SessionAdapter } = await import("../src/session/adapter.ts");
 const calls: string[] = [];
 const host: any = { workspace: "/fixture", mode: "full", role: "manager", backend: async () => ({ request: async (operation: string) => { calls.push(operation); return { handle: "private", state: "active" }; } }) };
 const adapter = new SessionAdapter(host, 5);
 await adapter.start("lease-session");
 await new Promise(resolve => setTimeout(resolve, 25));
 assert.ok(calls.filter(x => x === "memory.session.renew").length >= 1);
 await adapter.detach(); const count = calls.length;
 await new Promise(resolve => setTimeout(resolve, 15)); assert.equal(calls.length, count);
 assert.equal(adapter.status().state, "idle");
});

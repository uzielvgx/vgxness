import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm, writeFile, readFile, access } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { loadPiExtensions, piPaths } from "./pi-fixture.mjs";

const { loadExtensions } = await loadPiExtensions();

test("full task refuses a worker when the host did not issue a mutation grant", async t => {
  const root = await mkdtemp(join(tmpdir(), "pi-worker-authority-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  let launches = 0;
  const key = `__taskAuthority_${crypto.randomUUID()}`;
  (globalThis as any)[key] = { role: "manager", mode: "full", workspace: root, executeWorker: async () => { launches++; return "unexpected"; } };
  t.after(() => delete (globalThis as any)[key]);
  const wrapper = join(root, "task.ts");
  await writeFile(wrapper, `import { createTaskTool } from ${JSON.stringify(join(process.cwd(), "src/tools/task.ts"))}; export default pi => pi.registerTool(createTaskTool(globalThis[${JSON.stringify(key)}]));`);
  const loaded = await loadExtensions([wrapper], root);
  assert.deepEqual(loaded.errors, []);
  const tool: any = [...loaded.extensions[0].tools.values()][0].definition;
  await assert.rejects(tool.execute("full", { nonce: crypto.randomUUID(), role: "general", mode: "full", model: "fixture/model", effort: "low", goal: "write", criteria: ["write"], commands: [], targets: {}, resultLimit: 1024 }), /mutation grant unavailable/);
  assert.equal(launches, 0);
});


import { closeSync, openSync, writeSync } from "node:fs";
import { getEventListeners } from "node:events";
import { createHash } from "node:crypto";
import { SessionAdapter } from "../src/session/adapter.ts";
import { createWorkerMutationGuard, executePiWorker } from "../src/workers/runner.ts";
import { issueMission } from "../src/workers/mission.ts";

async function waitFile(path: string, timeout = 15000) {
  const until = Date.now() + timeout;
  while (Date.now() < until) {
    try { return await readFile(path, "utf8"); } catch (error: any) { if (error.code !== "ENOENT") throw error; }
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  throw new Error(`fixture timeout: ${path}`);
}

test("session grants keep the durable deadline and old failed signal after renewal", async t => {
  let lease = Date.now() + 60000, fail = false;
  const adapter = new SessionAdapter({ workspace: process.cwd(), mode: "full", role: "manager", backend: async () => ({ request: async (operation: string) => {
    if (operation === "memory.session.context") return {};
    if (fail) throw new Error("fixture renewal failure");
    return { handle: "h", state: "active", leaseUntil: new Date(lease).toISOString() };
  } }) } as any);
  t.after(() => adapter.detach());
  await adapter.start("fixture"); const first = adapter.mutationGrant();
  assert.equal(first.expiresAt, lease); assert.equal(Object.isFrozen(first), true);
  lease += 60000; await adapter.renew();
  assert.notEqual(adapter.mutationGrant().expiresAt, first.expiresAt);
  assert.equal(first.signal.aborted, false);
  fail = true; await assert.rejects(adapter.renew(), /fixture renewal failure/);
  assert.equal(first.signal.aborted, true); assert.throws(() => adapter.mutationGrant(), /unavailable/);
  fail = false; await adapter.renew();
  const recovered = adapter.mutationGrant(); assert.equal(recovered.signal.aborted, false);
  assert.notEqual(first.signal, recovered.signal); assert.equal(first.signal.aborted, true);
});

test("worker grant rejects expiry, revoked, short, and closed descriptors", async t => {
  const root = await mkdtemp(join(tmpdir(), "pi-grant-fd-"));
  let fd = openSync(join(root, "authority"), "wx+", 0o600);
  t.after(async () => { if (fd >= 0) closeSync(fd); await rm(root, { recursive: true, force: true }); });
  const missing = createWorkerMutationGuard({ expiresAt: 200, revocationFd: fd }, () => 100);
  assert.throws(missing, /revoked/); // A zero-byte record grants nothing.
  writeSync(fd, Buffer.from([0]), 0, 1, 0);
  let now = 100; const descriptor = { expiresAt: 200, revocationFd: fd };
  const guard = createWorkerMutationGuard(descriptor, () => now); guard();
  descriptor.expiresAt = 1000; now = 200; assert.throws(guard, /expired/);
  now = 100; writeSync(fd, Buffer.from([1]), 0, 1, 0); assert.throws(guard, /revoked/);
  closeSync(fd); fd = -1; assert.throws(guard, /unavailable/);
  assert.throws(() => createWorkerMutationGuard({ expiresAt: NaN, revocationFd: -1 }), /invalid/);
});

test("SDK patch checks expiry before commit and rolls back on mid-patch revocation", async t => {
  for (const variant of ["expiry", "revoke"]) await t.test(variant, async t => {
    const root = await mkdtemp(join(tmpdir(), "pi-grant-patch-"));
    const fd = openSync(join(root, "authority"), "wx+", 0o600); writeSync(fd, Buffer.from([0]), 0, 1, 0);
    t.after(async () => { closeSync(fd); await rm(root, { recursive: true, force: true }); });
    await writeFile(join(root, "one.txt"), "one\n"); await writeFile(join(root, "two.txt"), "two\n");
    let now = 100, commits = 0;
    const key = `__patchGrant_${crypto.randomUUID()}`;
    (globalThis as any)[key] = { host: { workspace: root, mode: "full", role: "manager", backend: async () => { throw new Error("unused"); }, mutationGuard: createWorkerMutationGuard({ expiresAt: 200, revocationFd: fd }, () => now) }, options: { fault: (point: string) => {
      if (point === "before-commit") { commits++; if (variant === "expiry") now = 200; else if (commits === 2) writeSync(fd, Buffer.from([1]), 0, 1, 0); }
    } } };
    t.after(() => delete (globalThis as any)[key]);
    const wrapper = join(root, "patch.ts");
    await writeFile(wrapper, `import {createApplyPatchTool} from ${JSON.stringify(join(process.cwd(), "src/tools/apply_patch.ts"))}; export default pi => pi.registerTool(createApplyPatchTool(globalThis[${JSON.stringify(key)}].host, globalThis[${JSON.stringify(key)}].options));`);
    const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []);
    const patch: any = [...loaded.extensions[0].tools.values()][0].definition;
    await assert.rejects(patch.execute("patch", { patch: "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-one\n+ONE\n--- a/two.txt\n+++ b/two.txt\n@@ -1 +1 @@\n-two\n+TWO\n" }), variant === "expiry" ? /expired/ : /revoked/);
    assert.equal(commits, variant === "expiry" ? 1 : 2);
    assert.equal(await readFile(join(root, "one.txt"), "utf8"), "one\n");
    assert.equal(await readFile(join(root, "two.txt"), "utf8"), "two\n");
  });
});


test("native worker rejects writes even when RPC ignores cancellation during startup", { skip: process.platform === "win32" ? "Pi worker processes unsupported on Windows" : false }, async t => {
  const root = await mkdtemp(join(tmpdir(), "pi-grant-rpc-")); t.after(() => rm(root, { recursive: true, force: true }));
  const target = join(root, "one.txt"), ready = join(root, "ready"), observed = join(root, "observed"), cli = join(root, "fake.mjs");
  await writeFile(target, "one\n");
  const patch = "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-one\n+TWO\n";
  await writeFile(cli, `import {writeFileSync} from "node:fs"; import {dirname} from "node:path"; import readline from "node:readline"; process.on("SIGTERM", () => {}); process.on("uncaughtException", error => {writeFileSync(${JSON.stringify(observed)}, String(error)); process.exit(1);}); await new Promise(resolve => setTimeout(resolve, 2200)); const extPath = process.argv[process.argv.indexOf("-e") + 1]; const {loadExtensions}=await import(${JSON.stringify(join((await piPaths()).root, "dist/core/extensions/loader.js"))}); const loaded=await loadExtensions([extPath], ${JSON.stringify(root)}); if(loaded.errors.length)throw new Error(JSON.stringify(loaded.errors)); const patch=[...loaded.extensions[0].tools.values()].map(item=>item.definition).find(tool=>tool.name==="apply_patch"); const out = value => process.stdout.write(JSON.stringify(value)+"\\n"); let attempted=false; readline.createInterface({input:process.stdin}).on("line", async line => { const req=JSON.parse(line); if(req.type === "get_state") {writeFileSync(${JSON.stringify(ready)}, JSON.stringify({pid:process.pid,root:dirname(extPath)})); return;} if(req.type === "abort" && !attempted) {attempted=true; let reason=""; try {await patch.execute("late",{patch:${JSON.stringify(patch)}});} catch(error){reason=String(error);} writeFileSync(${JSON.stringify(observed)},reason); } out({id:req.id,type:"response",success:true}); });`);
  const mission = issueMission({ workspace: root, mode: "full", role: "general", model: "test/model", effort: "low", goal: "patch", criteria: ["patch"], commands: [], resultLimit: 1024, nonce: crypto.randomUUID(), targets: { "one.txt": createHash("sha256").update("one\n").digest("hex") } });
  const controller = new AbortController(); const listeners = getEventListeners(controller.signal, "abort").length;
  const pending = executePiWorker(mission, { cli, runnerModule: join(process.cwd(), "src/workers/runner.ts"), signal: controller.signal, authority: { expiresAt: Date.now()+60000 }, timeout: 2000 });
  const settled = pending.then(() => { throw new Error("cancelled worker succeeded"); }, error => error);
  t.after(async () => { controller.abort(); await settled; });
  const started = JSON.parse(await Promise.race([waitFile(ready), settled.then(error => { throw new Error(`worker ended before ready: ${String(error)}`); })]).catch(async error => { throw new Error(String(error) + ": " + await readFile(observed, "utf8").catch(() => "no child diagnostic")); })); controller.abort();
  const error = await settled; assert.match(String(error), /cancelled/);
  assert.match(await waitFile(observed), /authority revoked/);
  assert.equal(await readFile(target, "utf8"), "one\n");
  assert.throws(() => process.kill(started.pid, 0), (error: any) => error.code === "ESRCH");
  await assert.rejects(access(started.root));
  assert.equal(getEventListeners(controller.signal, "abort").length, listeners);
});

test("read-only general worker needs no mutation descriptor and late abort has no listener", { skip: process.platform === "win32" ? "Pi worker processes unsupported on Windows" : false }, async t => {
  const root = await mkdtemp(join(tmpdir(), "pi-grant-readonly-")); t.after(() => rm(root, { recursive: true, force: true }));
  const cli = join(root, "fake.mjs");
  await writeFile(cli, `import readline from "node:readline"; const ext=await import(process.argv[process.argv.indexOf("-e")+1]); let check; await ext.default({registerProvider(){},registerTool(tool){if(tool.name==="worker_check")check=tool}}); const out=value=>process.stdout.write(JSON.stringify(value)+"\\n"); readline.createInterface({input:process.stdin}).on("line",async line=>{const req=JSON.parse(line); if(req.type==="get_state")return out({id:req.id,type:"response",success:true,data:{model:{provider:"test",id:"model"},thinkingLevel:"low"}}); if(req.type==="prompt"){ const value=await check.execute("version",{argv:[process.execPath,"--version"]}); out({id:req.id,type:"response",success:true}); out({type:"message_end",message:{role:"assistant",provider:"test",model:"model",content:[{type:"text",text:value.content[0].text}],stopReason:"stop"}});out({type:"agent_end"});out({type:"agent_settled"}); }else out({id:req.id,type:"response",success:true});});`);
  const mission = issueMission({ workspace: root, mode: "read-only", role: "general", model: "test/model", effort: "low", goal: "version", criteria: ["version"], commands: [[process.execPath,"--version"]], resultLimit: 1024, nonce: crypto.randomUUID(), targets: {} });
  const controller = new AbortController(), before = getEventListeners(controller.signal, "abort").length;
  const result = JSON.parse(await executePiWorker(mission, { cli, runnerModule: join(process.cwd(), "src/workers/runner.ts"), signal: controller.signal, timeout: 2000 }));
  assert.match(result.text, /^v/); assert.equal(getEventListeners(controller.signal, "abort").length, before); controller.abort();
});

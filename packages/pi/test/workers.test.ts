import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import test from "node:test";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { acceptMission, issueMission } from "../src/workers/mission.ts";
import { PiRpcRunner, executePiWorker, runWorkerCheck } from "../src/workers/runner.ts";
import { loadPiExtensions, piPaths } from "./pi-fixture.mjs";
const { loadExtensions } = await loadPiExtensions();
test("worker mission binds issued digest nonce role and target bytes", async () => { const workspace = await mkdtemp(join(tmpdir(), "pi-worker-")); const value: any = issueMission({ nonce: crypto.randomUUID(), role: "explore", workspace, mode: "read-only", model: "test", effort: "low", goal: "inspect", criteria: ["observe"], commands: [], resultLimit: 1024, targets: {} }); await acceptMission(value); await assert.rejects(acceptMission(value)); });
test("offline Pi RPC starts, receives selected auth only through fd3, and reaps", async (t) => { const root = await mkdtemp(join(tmpdir(), "pi-rpc-")); const extension = join(root, "worker.ts"), secret = "fixture-private-auth"; await writeFile(extension, `import {readFileSync} from "node:fs"; const auth=JSON.parse(readFileSync(3,"utf8")); export default async(pi)=>{ if(auth.apiKey!==${JSON.stringify(secret)}) throw new Error("missing private auth"); pi.registerProvider(auth.provider,{apiKey:auth.apiKey,baseUrl:auth.baseUrl}); };\n`); const { cli } = await piPaths(); const runner = new PiRpcRunner({ cli, extension, cwd: root, profile: join(root, "profile"), auth: { provider: "fixture", apiKey: secret, baseUrl: "https://invalid.example" } }); t.after(() => runner.stop()); const state = await runner.request("get_state", {}, 10000); assert.equal(state.type, "response"); assert.doesNotMatch(JSON.stringify(state), new RegExp(secret)); await runner.abort(); await runner.stop(); });
test("RPC waits for top-level agent_settled and rejects late or exited work", async (t) => { const root = await mkdtemp(join(tmpdir(), "pi-rpc-fake-")); const extension = join(root, "worker.ts"); const cli = join(root, "fake.mjs"); await writeFile(extension, "export default async () => {};\n"); await writeFile(cli, `import readline from 'node:readline'; const out=(value)=>process.stdout.write(JSON.stringify(value)+'\\n'); readline.createInterface({input:process.stdin}).on('line',(line)=>{const c=JSON.parse(line); if(c.type==='prompt'){out({id:c.id,type:'response'});setTimeout(()=>{out({type:'message_end',message:{role:'assistant',content:[{type:'text',text:'done'}]}});out({type:'agent_end'});out({type:'agent_settled'})},20)} else if(c.type==='abort'){out({id:c.id,type:'response'});setTimeout(()=>out({type:'agent_end',messages:['late']}),20)} else if(c.type==='set_model'){out({id:c.id,type:'response',success:false,message:'unsupported'})} else if(c.type==='exit'){process.exit(0)} else if(c.type==='malformed'){process.stdout.write('not-json\\n')} else out({id:c.id,type:'response'})});\n`); const runner = new PiRpcRunner({ cli, extension, cwd: root, profile: join(root, "profile") }); t.after(() => runner.stop()); assert.match(await runner.prompt("go", 500), /done/); const late = runner.prompt("cancel", 500); late.catch(() => {}); await runner.abort(); await assert.rejects(late, /aborted/); await assert.rejects(runner.select("provider/model", "low"), /unsupported/); await assert.rejects(runner.request("malformed", {}, 500), /malformed/); const exited = new PiRpcRunner({ cli, extension, cwd: root, profile: join(root, "exit-profile") }); t.after(() => exited.stop()); await assert.rejects(exited.request("exit", {}, 500), /closed/); });
test("task verifies SDD binding before launch", async () => { const root = await mkdtemp(join(tmpdir(), "pi-task-sdd-")); let launches = 0; let accepted = true; const binding = { changeId: "change", artifactId: "apply", revisionId: "revision", digest: "a".repeat(64), stateVersion: 3, inputs: [{ artifactId: "spec", revisionId: "spec-revision", digest: "b".repeat(64) }] }; (globalThis as any).__taskhost = { role: "manager", workspace: root, verifyAcceptedBinding: async (value: any) => accepted && JSON.stringify(value) === JSON.stringify(binding), executeWorker: async () => { launches++; return "done"; } }; const wrapper = join(root, "task.ts"); await writeFile(wrapper, `import { createTaskTool } from ${JSON.stringify(join(process.cwd(), "src/tools/task.ts"))}; export default async (pi) => pi.registerTool(createTaskTool(globalThis.__taskhost));`); const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []); const tool: any = [...loaded.extensions[0].tools.values()][0].definition; const input = { goal: "apply", nonce: crypto.randomUUID(), role: "sdd-apply", mode: "full", model: "provider/model", effort: "low", criteria: ["apply"], commands: [], resultLimit: 100, acceptedBindings: binding, targets: {} }; await tool.execute("task", input); assert.equal(launches, 1); accepted = false; await assert.rejects(tool.execute("task", { ...input, nonce: crypto.randomUUID() }), /accepted binding rejected/); assert.equal(launches, 1); await tool.execute("task", { ...input, nonce: crypto.randomUUID(), role: "general", acceptedBindings: undefined }); assert.equal(launches, 2); delete (globalThis as any).__taskhost; });
test("SDK worker surface scopes general patch and omits escalation tools", async () => { const root = await mkdtemp(join(tmpdir(), "pi-worker-sdk-")); const file = join(root, "one.txt"); await writeFile(file, "one\n"); const mission: any = issueMission({ nonce: crypto.randomUUID(), role: "general", workspace: root, mode: "full", model: "test", effort: "low", goal: "patch", criteria: ["patch"], commands: [[process.execPath, "--version"]], resultLimit: 1024, targets: { "one.txt": createHash("sha256").update("one\n").digest("hex") } }); (globalThis as any).__worker = await acceptMission(mission); const wrapper = join(root, "worker.ts"); await writeFile(wrapper, `import { createWorkerExtension } from ${JSON.stringify(join(process.cwd(), "src/workers/runner.ts"))}; export default createWorkerExtension(globalThis.__worker, async () => { throw new Error('unused'); });`); const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []); const tools: any[] = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition); const patch = tools.find((tool) => tool.name === "apply_patch"); await patch.execute("patch", { patch: "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-one\n+two\n" }); await patch.execute("patch", { patch: "--- a/one.txt\n+++ b/one.txt\n@@ -1 +1 @@\n-two\n+three\n" }); assert.equal(await (await import("node:fs/promises")).readFile(file, "utf8"), "three\n"); assert.ok(!tools.some((tool) => ["task", "sdd"].includes(tool.name))); assert.match((await tools.find((tool) => tool.name === "worker_check").execute("check", { argv: [process.execPath, "--version"] })).content[0].text, /^v/); await assert.rejects(tools.find((tool) => tool.name === "worker_check").execute("check", { argv: ["sh"] })); delete (globalThis as any).__worker; });
test("serialized worker bootstrap validates manager bytes in Pi's isolated module graph", async () => { const root = await mkdtemp(join(tmpdir(), "pi-worker-bootstrap-")); const mission: any = issueMission({ nonce: crypto.randomUUID(), role: "explore", workspace: root, mode: "read-only", model: "test", effort: "low", goal: "inspect", criteria: ["inspect"], commands: [], resultLimit: 1024, targets: {} }); await acceptMission(mission); const wrapper = join(root, "worker.ts"), runner = join(process.cwd(), "src/workers/runner.ts"); await writeFile(wrapper, `import { createWorkerExtension, bootstrapWorkerMission } from ${JSON.stringify(runner)}; const mission = await bootstrapWorkerMission(${JSON.stringify(mission)}); export default createWorkerExtension(mission, async () => { throw new Error("unused"); });`); const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []); const names = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition.name).sort(); assert.deepEqual(names, ["worker_read"]); });
test("native task tool lets a full manager issue a read-only worker and blocks escalation", async () => { const root = await mkdtemp(join(tmpdir(), "pi-task-native-")); let launches = 0; const host: any = { role: "manager", mode: "full", workspace: root, executeWorker: async () => { launches++; return "done"; } }; (globalThis as any).__nativeTaskHost = host; const wrapper = join(root, "task.ts"); await writeFile(wrapper, `import { createTaskTool } from ${JSON.stringify(join(process.cwd(), "src/tools/task.ts"))}; export default async (pi) => pi.registerTool(createTaskTool(globalThis.__nativeTaskHost));`); const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []); const tool: any = [...loaded.extensions[0].tools.values()][0].definition; const input = { goal: "inspect", nonce: crypto.randomUUID(), role: "explore", mode: "read-only", model: "provider/model", effort: "low", criteria: ["inspect"], commands: [], resultLimit: 100, targets: {} }; const result = await tool.execute("task", input); assert.match(result.content[0].text, /done/); assert.equal(launches, 1); host.mode = "read-only"; await assert.rejects(tool.execute("task", { ...input, nonce: crypto.randomUUID(), mode: "full", role: "general" }), /exceeds parent/); assert.equal(launches, 1); delete (globalThis as any).__nativeTaskHost; });

async function assertProcessGone(pid: number) {
  for (let attempt = 0; attempt < 20; attempt++) {
    try { process.kill(pid, 0); } catch { return; }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  assert.fail(`process ${pid} remained alive`);
}

async function assertProcessNotLive(pid: number) {
  // Descriptor EOF can precede the kernel's final process-table removal.
  // Keep the outcome strict: an unreaped/live PID at the deadline still fails.
  const deadline = Date.now() + 1000;
  while (Date.now() < deadline) {
    try { process.kill(pid, 0); } catch (error: any) { if (error?.code === "ESRCH") return; throw error; }
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  assert.throws(() => process.kill(pid, 0), /ESRCH/);
}

test("native worker_check abort reaps its process group", async () => {
  if (process.platform === "win32") return;
  const root = await mkdtemp(join(tmpdir(), "pi-worker-check-abort-"));
  const pidFile = join(root, "descendant.pid");
  const script = join(root, "check.mjs");
  await writeFile(script, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"]); writeFileSync(process.argv[2], String(child.pid)); setInterval(() => {}, 1000);`);
  const mission: any = await acceptMission(issueMission({ nonce: crypto.randomUUID(), role: "verifier", workspace: root, mode: "read-only", model: "test", effort: "low", goal: "check", criteria: ["check"], commands: [[process.execPath, script, pidFile]], resultLimit: 1024, targets: {} }));
  (globalThis as any).__worker = mission;
  const wrapper = join(root, "worker.ts");
  await writeFile(wrapper, `import { createWorkerExtension } from ${JSON.stringify(join(process.cwd(), "src/workers/runner.ts"))}; export default createWorkerExtension(globalThis.__worker, async () => { throw new Error("unused"); });`);
  const loaded = await loadExtensions([wrapper], root);
  assert.deepEqual(loaded.errors, []);
  const check: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((tool: any) => tool.name === "worker_check");
  const controller = new AbortController();
  const pending = check.execute("check", { argv: [process.execPath, script, pidFile] }, controller.signal);
  let pid = 0;
  for (let attempt = 0; attempt < 20 && !pid; attempt++) {
    try { pid = Number((await readFile(pidFile, "utf8")).trim()); } catch { await new Promise((resolve) => setTimeout(resolve, 25)); }
  }
  assert.ok(pid > 0, "descendant PID was not recorded");
  controller.abort();
  await assert.rejects(pending, /cancelled/);
  await assertProcessGone(pid);
  delete (globalThis as any).__worker;
});


test("native worker_check reaps descendants after a successful command leader exit", async () => {
  if (process.platform === "win32") return;
  const root = await mkdtemp(join(tmpdir(), "pi-worker-check-exit-"));
  const descendantFile = join(root, "descendant.pid");
  const supervisorFile = join(root, "supervisor.pid");
  const script = join(root, "check.mjs");
  await writeFile(script, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"]); writeFileSync(process.argv[2], String(child.pid)); writeFileSync(process.argv[3], String(process.ppid)); process.exit(0);`);
  const mission: any = await acceptMission(issueMission({ nonce: crypto.randomUUID(), role: "verifier", workspace: root, mode: "read-only", model: "test", effort: "low", goal: "check", criteria: ["check"], commands: [[process.execPath, script, descendantFile, supervisorFile]], resultLimit: 1024, targets: {} }));
  (globalThis as any).__worker = mission;
  const wrapper = join(root, "worker.ts");
  await writeFile(wrapper, `import { createWorkerExtension } from ${JSON.stringify(join(process.cwd(), "src/workers/runner.ts"))}; export default createWorkerExtension(globalThis.__worker, async () => { throw new Error("unused"); });`);
  const loaded = await loadExtensions([wrapper], root);
  assert.deepEqual(loaded.errors, []);
  const check: any = [...loaded.extensions[0].tools.values()].map((item: any) => item.definition).find((tool: any) => tool.name === "worker_check");
  await check.execute("check", { argv: [process.execPath, script, descendantFile, supervisorFile] });
  const descendant = Number((await readFile(descendantFile, "utf8")).trim());
  const supervisor = Number((await readFile(supervisorFile, "utf8")).trim());
  await assertProcessGone(descendant);
  await assertProcessGone(supervisor);
  delete (globalThis as any).__worker;
});

test("worker_check control EOF reaps its detached command group after abrupt RPC death", async () => {
  if (process.platform === "win32") return;
  const root = await mkdtemp(join(tmpdir(), "pi-worker-check-rpc-death-"));
  const descendantFile = join(root, "descendant.pid");
  const supervisorFile = join(root, "supervisor.pid");
  const command = join(root, "check.mjs");
  const host = join(root, "rpc-host.mjs");
  await writeFile(command, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"]); writeFileSync(process.argv[2], String(child.pid)); writeFileSync(process.argv[3], String(process.ppid)); setInterval(() => {}, 1000);`);
  await writeFile(host, `import { runWorkerCheck } from ${JSON.stringify(join(process.cwd(), "src/workers/runner.ts"))}; await runWorkerCheck({ workspace: process.argv[2], resultLimit: 1024 }, [process.execPath, process.argv[3], process.argv[4], process.argv[5]]);`);
  const rpc = spawn(process.execPath, [host, root, command, descendantFile, supervisorFile], { cwd: root, shell: false, stdio: "ignore" });
  let descendant = 0;
  let supervisor = 0;
  for (let attempt = 0; attempt < 40 && (!descendant || !supervisor); attempt++) {
    try {
      descendant = Number((await readFile(descendantFile, "utf8")).trim());
      supervisor = Number((await readFile(supervisorFile, "utf8")).trim());
    } catch { await new Promise((resolve) => setTimeout(resolve, 25)); }
  }
  assert.ok(descendant > 0 && supervisor > 0, "command group was not recorded");
  rpc.kill("SIGKILL");
  await assertProcessGone(descendant);
  await assertProcessGone(supervisor);
});

test("RPC stop kills a descendant after its TERM-handling leader exits", async () => {
  if (process.platform === "win32") return;
  const root = await mkdtemp(join(tmpdir(), "pi-rpc-group-"));
  const pidFile = join(root, "descendant.pid");
  const cli = join(root, "leader.mjs");
  await writeFile(cli, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"]); writeFileSync(${JSON.stringify(pidFile)}, String(child.pid)); process.on("SIGTERM", () => process.exit(0)); setInterval(() => {}, 1000);`);
  const runner = new PiRpcRunner({ cli, extension: join(root, "extension.ts"), cwd: root, profile: join(root, "profile") });
  let pid = 0;
  for (let attempt = 0; attempt < 20 && !pid; attempt++) {
    try { pid = Number((await readFile(pidFile, "utf8")).trim()); } catch { await new Promise((resolve) => setTimeout(resolve, 25)); }
  }
  assert.ok(pid > 0, "descendant PID was not recorded");
  await runner.stop();
  await assertProcessGone(pid);
});

test("executePiWorker retains its root until fd4-owned checks reap after RPC death", async () => {
  if (process.platform === "win32") return;
  const root = await mkdtemp(join(tmpdir(), "pi-execute-lifetime-"));
  const stateFile = join(root, "state.json");
  const command = join(root, "command.mjs");
  const cli = join(root, "fake-cli.mjs");
  const runner = join(process.cwd(), "src/workers/runner.ts");
  await writeFile(command, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"]); writeFileSync(process.argv[2], JSON.stringify({ descendant: child.pid, supervisor: process.ppid })); setInterval(() => {}, 1000);`);
  await writeFile(cli, `import { readFileSync, writeFileSync } from "node:fs"; import { dirname, join } from "node:path"; import readline from "node:readline"; const extension = process.argv[process.argv.indexOf("-e") + 1]; const command = ${JSON.stringify(command)}; const stateFile = ${JSON.stringify(stateFile)}; process.on("uncaughtException", (error) => writeFileSync(stateFile, JSON.stringify({ error: String(error) }))); const mod = await import(extension); let check; await mod.default({ registerProvider() {}, registerTool(tool) { if (tool.name === "worker_check") check = tool; } }); const out = (value) => process.stdout.write(JSON.stringify(value) + "\\n"); readline.createInterface({ input: process.stdin }).on("line", async (line) => { const request = JSON.parse(line); if (request.type === "prompt") { await check.execute("missing", { argv: [process.execPath, join(dirname(command), "missing.mjs")] }).catch(() => {}); void check.execute("check", { argv: [process.execPath, command, stateFile] }); for (let i = 0; i < 40; i++) { try { JSON.parse(readFileSync(stateFile, "utf8")); break; } catch { await new Promise((resolve) => setTimeout(resolve, 10)); } } writeFileSync(${JSON.stringify(join(root, "rpc-root"))}, dirname(extension)); process.kill(process.pid, "SIGKILL"); } else out({ id: request.id, type: "response", success: true }); });`);
  const mission: any = issueMission({ workspace: root, mode: "read-only", role: "verifier", model: "test/model", effort: "low", goal: "check", criteria: ["check"], commands: [[process.execPath, command, stateFile]], resultLimit: 1024, nonce: crypto.randomUUID(), targets: {} });
  await assert.rejects(executePiWorker(mission, { cli, runnerModule: runner, timeout: 1000 }), /worker RPC closed/);
  const recorded = JSON.parse(await readFile(stateFile, "utf8"));
  assert.equal(recorded.error, undefined, recorded.error);
  const { descendant, supervisor } = recorded;
  await assertProcessNotLive(descendant);
  await assertProcessNotLive(supervisor);
  const rpcRoot = (await readFile(join(root, "rpc-root"), "utf8")).trim();
  await assert.rejects((await import("node:fs/promises")).access(rpcRoot));
});

test("executePiWorker generated fd4 checks reap before live RPC terminal output on abort and timeout", async (t) => {
  if (process.platform === "win32") return;
  for (const mode of ["abort", "timeout"]) await t.test(mode, async () => {
    const root = await mkdtemp(join(tmpdir(), `pi-execute-fd4-${mode}-`));
    try {
    const stateFile = join(root, "state.json"), verdictFile = join(root, "verdict.json"), command = join(root, "check.mjs"), cli = join(root, "fake.mjs"), runner = join(process.cwd(), "src/workers/runner.ts");
    await writeFile(command, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"]); writeFileSync(process.argv[2], JSON.stringify({ child: child.pid, supervisor: process.ppid })); setInterval(() => {}, 1000);`);
    await writeFile(cli, `import { readFileSync, writeFileSync } from "node:fs"; import readline from "node:readline"; const extension = process.argv[process.argv.indexOf("-e") + 1], command = ${JSON.stringify(command)}, state = ${JSON.stringify(stateFile)}, verdict = ${JSON.stringify(verdictFile)}, mode = ${JSON.stringify(mode)}; const mod = await import(extension); let check; await mod.default({ registerProvider() {}, registerTool(tool) { if (tool.name === "worker_check") check = tool; } }); const out = (value) => process.stdout.write(JSON.stringify(value) + "\\n"); const processState = (pid) => { try { process.kill(pid, 0); } catch (error) { return error?.code === "ESRCH" ? "ESRCH" : String(error); } if (process.platform !== "linux") return "running"; try { const stat = readFileSync("/proc/" + pid + "/stat", "utf8"), at = stat.lastIndexOf(")"); return at < 0 ? "running" : stat.slice(at + 2, at + 3); } catch (error) { return String(error); } }; readline.createInterface({ input: process.stdin }).on("line", async (line) => { const request = JSON.parse(line); if (request.type !== "prompt") return out({ id: request.id, type: "response", success: true }); const controller = new AbortController(); const work = check.execute("check", { argv: [process.execPath, command, state] }, mode === "abort" ? controller.signal : undefined); for (let i = 0; i < 80; i++) { try { JSON.parse(readFileSync(state, "utf8")); break; } catch { await new Promise((resolve) => setTimeout(resolve, 10)); } } if (mode === "abort") controller.abort(); const toolError = await work.then(() => "", (error) => String(error)); if (!toolError.includes(mode === "abort" ? "cancelled" : "timeout")) throw new Error("unexpected worker check result: " + toolError); const pids = JSON.parse(readFileSync(state, "utf8")); const child = processState(pids.child), supervisor = processState(pids.supervisor); writeFileSync(verdict, JSON.stringify({ child, supervisor, toolError, live: supervisor !== "ESRCH" || !["ESRCH", "Z"].includes(child) })); out({ id: request.id, type: "response", success: true }); out({ type: "agent_end", messages: ["done"] }); out({ type: "agent_settled" }); });`);
    const mission: any = issueMission({ workspace: root, mode: "read-only", role: "verifier", model: "test/model", effort: "low", goal: mode, criteria: [mode], commands: [[process.execPath, command, stateFile]], resultLimit: 1024, nonce: crypto.randomUUID(), targets: {} });
    await executePiWorker(mission, { cli, runnerModule: runner, timeout: 5000 });
    const verdict = JSON.parse(await readFile(verdictFile, "utf8")); assert.equal(verdict.live, false, JSON.stringify(verdict)); assert.equal(verdict.supervisor, "ESRCH", JSON.stringify(verdict)); assert.ok(["ESRCH", "Z"].includes(verdict.child), JSON.stringify(verdict));
    } finally { await rm(root, { recursive: true, force: true }); }
  });
});

test("executePiWorker reports recovery_pending and retains its root while fd4 is held", async () => {
  if (process.platform === "win32") return;
  const root = await mkdtemp(join(tmpdir(), "pi-execute-held-fd-"));
  const holderFile = join(root, "holder.json");
  const cli = join(root, "fake-holder-cli.mjs");
  const runner = join(process.cwd(), "src/workers/runner.ts");
  await writeFile(cli, `import { spawn } from "node:child_process"; import { writeFileSync } from "node:fs"; import { dirname } from "node:path"; import readline from "node:readline"; const extension = process.argv[process.argv.indexOf("-e") + 1]; const holder = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], { detached: true, stdio: [4, "ignore", "ignore"] }); holder.unref(); writeFileSync(${JSON.stringify(holderFile)}, JSON.stringify({ pid: holder.pid, root: dirname(extension) })); readline.createInterface({ input: process.stdin }).on("line", (line) => { const request = JSON.parse(line); process.stdout.write(JSON.stringify({ id: request.id, type: "response", success: true }) + "\\n"); });`);
  const mission: any = issueMission({ workspace: root, mode: "read-only", role: "explore", model: "test/model", effort: "low", goal: "hold", criteria: ["hold"], commands: [], resultLimit: 1024, nonce: crypto.randomUUID(), targets: {} });
  await assert.rejects(executePiWorker(mission, { cli, runnerModule: runner, timeout: 25 }), /worker recovery_pending/);
  const held = JSON.parse(await readFile(holderFile, "utf8"));
  await (await import("node:fs/promises")).access(held.root);
  process.kill(held.pid, "SIGKILL");
  await assertProcessGone(held.pid);
  await rm(held.root, { recursive: true, force: true });
});


test("oversized worker auth is rejected before a fake CLI starts", async () => { const root = await mkdtemp(join(tmpdir(), "pi-auth-bound-")); const marker = join(root, "started"); const cli = join(root, "fake.mjs"); await writeFile(cli, `import{writeFileSync}from'node:fs';writeFileSync(${JSON.stringify(marker)},'started');`); await assert.rejects(executePiWorker({ workspace: root, mode: "read-only", role: "explore", model: "p/m", effort: "low", goal: "x", criteria: ["x"], commands: [], resultLimit: 10, nonce: crypto.randomUUID(), targets: {} } as any, { cli, runnerModule: join(process.cwd(), "src/workers/runner.ts"), auth: { value: "x".repeat(70000) } }), /auth record too large/); await assert.rejects((await import("node:fs/promises")).access(marker)); });

test('RPC waits through retry, accumulates text/usage, and rejects exhausted retry',async t=>{
 const root=await mkdtemp(join(tmpdir(),'pi-retry-'));t.after(()=>rm(root,{recursive:true,force:true}));const cli=join(root,'fake.mjs');
 await writeFile(cli,`import readline from 'node:readline';const out=v=>process.stdout.write(JSON.stringify(v)+'\\n');readline.createInterface({input:process.stdin}).on('line',line=>{const r=JSON.parse(line);out({type:'response',id:r.id,success:true});if(r.type==='prompt'){out({type:'message_end',message:{role:'assistant',stopReason:'error',errorMessage:'transient',content:[],usage:{input:3}}});out({type:'agent_end',willRetry:true});out({type:'auto_retry_start',attempt:1});setTimeout(()=>{if(r.message==='fail')out({type:'auto_retry_end',success:false,finalError:'exhausted'});else {out({type:'message_end',message:{role:'assistant',stopReason:'stop',content:[{type:'text',text:'after retry'}],usage:{input:2,output:5}}});out({type:'auto_retry_end',success:true});}out({type:'agent_settled'});},50)}});`);
 const runner=new PiRpcRunner({cli,extension:'unused',cwd:root,profile:join(root,'profile')});t.after(()=>runner.stop());
 const result=JSON.parse(await runner.prompt('ok',1000));assert.equal(result.text,'after retry');assert.equal(result.usage.input,5);assert.deepEqual(result.diagnostics,['retry 1']);
 await assert.rejects(runner.prompt('fail',1000),/exhausted/);
});

test("explore missions reject executable commands before bootstrap", async () => {
 const workspace = await mkdtemp(join(tmpdir(), "pi-explore-no-shell-"));
 const mission: any = issueMission({ nonce: crypto.randomUUID(), role: "explore", workspace, mode: "read-only", model: "test", effort: "low", goal: "inspect", criteria: [], commands: [[process.execPath, "--version"]], resultLimit: 100, targets: {} });
 await assert.rejects(acceptMission(mission), /explore missions cannot authorize commands/);
});

test("SDK task binds host-resolved skills and rejects model-supplied skill contents", async t => {
  const root = await mkdtemp(join(tmpdir(), "pi-task-skills-"));
  t.after(() => import("node:fs/promises").then(fs => fs.rm(root, { recursive: true, force: true })));
  const content = "Use observable evidence.";
  const skills = [{ name: "fixture", files: [{ path: "SKILL.md", content, sha256: createHash("sha256").update(content).digest("hex") }] }];
  let received: any, selections: any;
  const key = "__taskSkills_" + crypto.randomUUID();
  (globalThis as any)[key] = { role: "manager", mode: "full", workspace: root, resolveSkills: async (input: any) => { selections = input; return skills; }, executeWorker: async (mission: any) => { received = mission; return "observed"; } };
  t.after(() => { delete (globalThis as any)[key]; });
  const wrapper = join(root, "task.ts");
  await writeFile(wrapper, `import { createTaskTool } from ${JSON.stringify(join(process.cwd(), "src/tools/task.ts"))}; export default pi => pi.registerTool(createTaskTool(globalThis[${JSON.stringify(key)}]));`);
  const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []);
  const tool: any = [...loaded.extensions[0].tools.values()][0].definition;
  const input = { nonce: crypto.randomUUID(), role: "verifier", mode: "read-only", model: "fixture/model", effort: "low", goal: "Verify", criteria: ["Observed result"], commands: [], targets: {}, resultLimit: 8192, skills: [{ name: "fixture", sha256: skills[0].files[0].sha256 }] };
  await tool.execute("task", input);
  assert.deepEqual(selections, input.skills); assert.deepEqual(received.skills, skills); assert.ok(Object.isFrozen(received.skills));
  assert.match(received.digest, /^[0-9a-f]{64}$/);
  await assert.rejects(tool.execute("task", { ...input, nonce: crypto.randomUUID(), skills: [{ name: "fixture", content: "forged" }] }), /invalid task input/);
});

test("SDK skill tool returns a readable manifest identity and rejects arbitrary paths", async t => {
  const fs = await import("node:fs/promises"), root = await mkdtemp(join(tmpdir(), "pi-skill-tool-"));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  const sharedRoot = join(root, "shared"), bundledRoot = join(root, "missing");
  await fs.mkdir(join(sharedRoot, "fixture"), { recursive: true });
  const content = '---\nname: fixture\ndescription: Review fixtures.\ncompatibility: Agent Skills hosts\nprovenance: "VGXNESS portable global skill"\n---\nInspect actual evidence.';
  await writeFile(join(sharedRoot, "fixture", "SKILL.md"), content);
  const wrapper = join(root, "tool.ts");
  await writeFile(wrapper, `import { createSkillTool } from ${JSON.stringify(join(process.cwd(), "src/tools/skill.ts"))}; export default pi => pi.registerTool(createSkillTool(${JSON.stringify({ sharedRoot, bundledRoot })}));`);
  const loaded = await loadExtensions([wrapper], root); assert.deepEqual(loaded.errors, []);
  const tool: any = [...loaded.extensions[0].tools.values()][0].definition;
  const list = JSON.parse((await tool.execute("list", { operation: "list" })).content[0].text);
  const read = JSON.parse((await tool.execute("read", { operation: "read", name: "fixture" })).content[0].text);
  assert.equal(list[0].sha256, read.files[0].sha256); assert.equal(read.files[0].content, content);
  await assert.rejects(tool.execute("escape", { operation: "read", name: "fixture", resources: ["../outside"] }), /path/);
  await assert.rejects(tool.execute("forged", { operation: "read", name: "fixture", path: "/outside" }), /invalid skill input/);
});


test("SDK task canonicalizes every shared alias before authority and SDD checks", async t => {
 const root = await mkdtemp(join(tmpdir(), "pi-task-alias-"));
 t.after(() => import("node:fs/promises").then(fs => fs.rm(root,{recursive:true,force:true})));
 const { loadManagerContract } = await import("../src/orchestration/contract.ts");
 const contract=loadManagerContract(); let received:any; let bindingChecks=0;
 const binding={changeId:"c",artifactId:"a",revisionId:"r",digest:"a".repeat(64),stateVersion:1,inputs:[{artifactId:"s",revisionId:"sr",digest:"b".repeat(64)}]};
 const key="__taskAlias_"+crypto.randomUUID();
 const host:any={role:"manager",mode:"full",workspace:root,verifyAcceptedBinding:async(value:any)=>{bindingChecks++;return JSON.stringify(value)===JSON.stringify(binding);},executeWorker:async(mission:any)=>{received=mission;return "ok";}};
 (globalThis as any)[key]=host;t.after(()=>{delete(globalThis as any)[key];});
 const wrapper=join(root,"task.ts");
 await writeFile(wrapper,`import { createTaskTool } from ${JSON.stringify(join(process.cwd(),"src/tools/task.ts"))}; export default pi=>pi.registerTool(createTaskTool(globalThis[${JSON.stringify(key)}]));`);
 const loaded=await loadExtensions([wrapper],root);assert.deepEqual(loaded.errors,[]);
 const tool:any=[...loaded.extensions[0].tools.values()][0].definition;
 const input=(role:string,mode="read-only",acceptedBindings?:any)=>({nonce:crypto.randomUUID(),role,mode,model:"fixture/model",effort:"low",goal:"Exercise alias",criteria:["Canonical bounded mission"],commands:[],targets:{},resultLimit:8192,...(acceptedBindings?{acceptedBindings}:{})});
 for(const role of contract.roles)for(const alias of role.aliases){const before=bindingChecks;await tool.execute("alias",input(alias,role.writeAuthority?"full":"read-only",role.id==="sdd-apply"?binding:undefined));assert.equal(received.role,role.id);assert.match(received.digest,/^[0-9a-f]{64}$/);if(role.id==="sdd-apply")assert.equal(bindingChecks-before,2);}
 await assert.rejects(tool.execute("readonly",input("verification","full")),/full mode/);
 await assert.rejects(tool.execute("apply",input("apply","full")),/accepted binding/);
 await assert.rejects(tool.execute("invalid",input("manager")),/launchable|unsupported/);
 await assert.rejects(tool.execute("invalid",input("unknown")),/launchable|unsupported/);
 host.mode="read-only";await assert.rejects(tool.execute("parent",input("worker","full")),/parent authority/);
});

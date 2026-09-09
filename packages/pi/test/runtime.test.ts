import test from "node:test";
const mkdtemp = async (prefix: string) => realpath(await rawMkdtemp(prefix));
import assert from "node:assert/strict";
import { mkdtemp as rawMkdtemp, realpath, mkdir, writeFile, rm, stat } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { loadPiExtensions } from "./pi-fixture.mjs";
import { loadNativeRuntime } from "./runtime-fixture.mjs";
const { loadExtensions } = await loadPiExtensions();
const { createNativeDispatcher } = await loadNativeRuntime();
async function fixture(t: test.TestContext) {
 const root = await mkdtemp(join(tmpdir(), "pi-runtime-")), workspace = join(root, "workspace"), storageRoot = join(root, "storage"); await mkdir(workspace);
 t.after(() => rm(root, { recursive: true, force: true }));
 const wrapper = join(root, "extension.ts");
 await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(fileURLToPath(new URL('../src/extension.ts', import.meta.url)))}; export default createPiExtension(${JSON.stringify({ workspace, storageRoot })});`);
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

test("extension workers view retains the task-owned terminal result", async t => {
 const root=await mkdtemp(join(tmpdir(),"pi-terminal-view-"));t.after(()=>rm(root,{recursive:true,force:true}));const key="__piWorker_"+crypto.randomUUID();(globalThis as any)[key]=async(m:any)=>{if(m.goal==="fail") throw new Error("transport fail");if(m.goal==="unavailable")throw Object.assign(new Error("noauth"),{code:"worker_unavailable"});if(m.goal==="recovery")return JSON.stringify({text:"",recovery:{pending:true,refs:["retained"]}}); return JSON.stringify({text:"ok",usage:{output:1},selectedModel:"unknown",effectiveModel:"unknown",selectedEffort:"unknown",effectiveEffort:"unknown",diagnostics:[],truncated:false})};t.after(()=>delete (globalThis as any)[key]);const wrapper=join(root,"extension.ts");await writeFile(wrapper,`import { createPiExtension } from ${JSON.stringify(fileURLToPath(new URL('../src/extension.ts', import.meta.url)))}; export default createPiExtension({workspace:${JSON.stringify(root)},executeWorker:globalThis[${JSON.stringify(key)}]});`);const loaded=await loadExtensions([wrapper],root);assert.deepEqual(loaded.errors,[]);const ext=loaded.extensions[0],task:any=[...ext.tools.values()].map((x:any)=>x.definition).find((x:any)=>x.name==='task');const input:any={nonce:crypto.randomUUID(),role:'explore',mode:'read-only',model:'test/model',effort:'low',goal:'x',criteria:['x'],commands:[],targets:{},resultLimit:32};const value=await task.execute('worker',input);const terminal=value.details.result;assert.deepEqual(JSON.parse(value.content[0].text),terminal);const view=await ext.commands.get('vgx-workers').handler('',{});if(process.platform === "win32"){const v=JSON.parse(view.content[0].text);assert.equal(v.state,"unavailable");assert.match(v.reason,/Windows/);assert.deepEqual(v.workers,[]);}else{assert.match(JSON.stringify(view),new RegExp(terminal.nonce));}for(const goal of ['fail','unavailable','recovery']){const nonce=crypto.randomUUID();let terminal:any;await assert.rejects(task.execute(goal,{...input,nonce,goal}),(error:any)=>{terminal=error.result;assert.deepEqual(JSON.parse(error.message),terminal);assert.equal(terminal.state,goal==='unavailable'?'unavailable':'failed');if(goal==='recovery')assert.equal(terminal.reasonCode,'recovery_pending');return true;});const workers=JSON.parse((await ext.commands.get('vgx-workers').handler('',{})).content[0].text).workers;if(process.platform === "win32")assert.deepEqual(workers,[]);else assert.deepEqual(workers.find((w:any)=>w.nonce===nonce),terminal);}const cancelled=new AbortController();cancelled.abort();await assert.rejects(task.execute('cancel',{...input,nonce:crypto.randomUUID()},cancelled.signal),(error:any)=>{assert.equal(error.result.state,'cancelled');assert.deepEqual(JSON.parse(error.message),error.result);return true;});await assert.rejects(task.execute('invalid',{}));const finalWorkers=JSON.parse((await ext.commands.get('vgx-workers').handler('',{})).content[0].text).workers;assert.equal(finalWorkers.some((w:any)=>w.state==='running'),false);
});
test("native views state Windows worker availability without attempting a launch", async () => {
 const { nativeStatusView, nativeWorkersView } = await import("../src/views.ts");
 const status:any=nativeStatusView({workspace:"/fixture",mode:"full",role:"manager",session:{state:"idle"},workerCli:"pi",platform:"win32"});
 assert.equal(status.workers.state,"unavailable"); assert.match(status.workers.reason,/Windows/);
 const workers:any=nativeWorkersView("pi",[{state:"running"}],"win32");
 assert.equal(workers.state,"unavailable"); assert.equal(workers.workers.length,0);
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
 const host: any = { workspace: "/fixture", mode: "full", role: "manager", backend: async () => ({ request: async (operation: string) => { calls.push(operation); return { handle: "private", state: "active", leaseUntil: new Date(Date.now()+60000).toISOString() }; } }) };
 const adapter = new SessionAdapter(host, 5);
 await adapter.start("lease-session");
 await new Promise(resolve => setTimeout(resolve, 25));
 assert.ok(calls.filter(x => x === "memory.session.renew").length >= 1);
 await adapter.detach(); const count = calls.length;
 await new Promise(resolve => setTimeout(resolve, 15)); assert.equal(calls.length, count);
 assert.equal(adapter.status().state, "idle");
});
test("session adapter fails closed for absent leases, aborts expiry, and an explicit renewal recovers", async () => {
 const { SessionAdapter } = await import("../src/session/adapter.ts");
 let lease=true; const host:any={workspace:"/fixture",mode:"full",role:"manager",backend:async()=>({request:async(operation:string)=> {
   if(operation==="memory.session.context") return {};
   if(operation==="memory.session.renew") return {handle:"h",state:"active",leaseUntil:new Date(Date.now()+60000).toISOString()};
   return lease ? {handle:"h",state:"active",leaseUntil:new Date(Date.now()+80).toISOString()} : {handle:"h",state:"active"};
 }})};
 const adapter=new SessionAdapter(host,60000); lease=false; await assert.rejects(adapter.start("missing"),/lease unavailable/);
 lease=true; await adapter.start("short"); await new Promise(resolve=>setTimeout(resolve,100)); assert.equal(adapter.mutationSignal().aborted,true); assert.throws(()=>adapter.assertMutation(),/unavailable/);
 await adapter.renew(); assert.equal(adapter.mutationSignal().aborted,false); adapter.assertMutation(); await adapter.detach();
});
test("views use readonly injected backend snapshots without storage writes",async t=>{const root=await mkdtemp(join(tmpdir(),"pi-view-cache-")),key="__viewBackend_"+crypto.randomUUID();t.after(()=>rm(root,{recursive:true,force:true}));let fail=true;const calls:string[]=[];(globalThis as any)[key]=async()=>({request:async(op:string)=>{calls.push(op);if(fail)throw Error("read failed");if(op==="memory.recent")return[{ID:"1",Title:"fixture",Preview:"durable fixture",Producer:"p",SourceProvider:"s",SourceID:"id",UpdatedAt:"2026-01-01T00:00:00Z",References:[]}];return op==="sdd.list"?[]:{}}});t.after(()=>delete (globalThis as any)[key]);const wrapper=join(root,"extension.ts");await writeFile(wrapper,`import {createPiExtension} from ${JSON.stringify(fileURLToPath(new URL('../src/extension.ts',import.meta.url)))};export default createPiExtension({workspace:${JSON.stringify(root)},storageRoot:${JSON.stringify(join(root,'storage'))},backend:globalThis[${JSON.stringify(key)}]});`);const loaded=await loadExtensions([wrapper],root);const ext=loaded.extensions[0],notes:any[]=[];const ctx:any={ui:{notify:(x:any)=>notes.push(x)}};const memory=ext.commands.get("vgx-memory");let value:any=JSON.parse((await memory.handler("",ctx)).content[0].text);assert.equal(value.state,"unavailable");fail=false;value=JSON.parse((await memory.handler("",ctx)).content[0].text);assert.equal(value.trust,"UNTRUSTED");assert.equal(value.entries[0].preview,"durable fixture");const observed=value.observedAt;fail=true;value=JSON.parse((await memory.handler("",ctx)).content[0].text);assert.equal(value.state,"stale");assert.equal(value.observedAt,observed);assert.equal(value.reason,"read_failed");assert.ok(notes.some(x=>String(x).includes("loading")));assert.ok(calls.every(x=>["memory.recent"].includes(x)));});

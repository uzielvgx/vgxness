import test from "node:test"; import assert from "node:assert/strict";
import { createHash } from "node:crypto"; import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises"; import { tmpdir } from "node:os"; import { join } from "node:path";
import { canonicalContract, loadManagerContract, renderManagerPrompt, resolveRole } from "../src/orchestration/contract.ts";
import { workerAuthority, nativeCapabilities, renderPiManagerPrompt } from "../src/orchestration/adapter.ts";
import { workerCanWrite, workerRoles } from "../src/workers/roles.ts";
test("generated contract has all roles and authority",async()=>{const c=loadManagerContract();assert.equal(c.roles.length,6);assert.equal(resolveRole(c,"worker")?.id,"general");assert.equal(resolveRole(c,"apply")?.id,undefined);assert.equal(await workerAuthority("general",c),true);assert.equal(await workerAuthority("verifier",c),false);assert.deepEqual(workerRoles,c.roles.map(role=>role.id));assert.equal(workerCanWrite("general"),true);assert.equal(workerCanWrite("sdd-apply"),false);assert.equal(workerCanWrite("verifier"),false);assert.match(resolveRole(c,"care-challenger")!.instructions,/Challenge/);});
test("native adapter documents limits",async()=>{const caps=await nativeCapabilities();assert.equal(caps.authentication,"host-configured-only");});
test("generated prompt is the complete shared Manager projection", async()=>{ const c=loadManagerContract(); const prompt=await readFile(join(process.cwd(),"resources/prompts/manager.md"),"utf8"); assert.equal(prompt,renderPiManagerPrompt(c)); });
test("loader rejects missing roles, duplicate aliases, empty instructions, identity and digest drift", async(t)=>{ const root=await mkdtemp(join(tmpdir(),"pi-contract-")); t.after(()=>rm(root,{recursive:true,force:true})); const base:any=loadManagerContract(); let index=0; const emit=async(mutator:(value:any)=>void)=>{const value=structuredClone(base); mutator(value); value.sourceDigest=createHash("sha256").update(canonicalContract({schemaVersion:value.schemaVersion,identity:value.identity,manager:value.manager,roles:value.roles})).digest("hex"); const file=join(root,`${index++}.json`); await writeFile(file,JSON.stringify(value)); return file;}; for(const mutate of [(v:any)=>v.roles.pop(),(v:any)=>v.roles[2].aliases=["worker"],(v:any)=>v.roles[0].instructions="",(v:any)=>v.identity="wrong",(v:any)=>v.schemaVersion="wrong"]) { const file=await emit(mutate); assert.throws(()=>loadManagerContract(file),/manager contract drift/); } const digestDrift=structuredClone(base); digestDrift.sourceDigest="0".repeat(64); const digestFile=join(root,`${index++}.json`); await writeFile(digestFile,JSON.stringify(digestDrift)); assert.throws(()=>loadManagerContract(digestFile),/manager contract drift/); });

// Regression: model aliases and native skill guidance are sourced, not duplicated.
test("shared model roles and portable policy are complete", () => {
 const c:any=loadManagerContract();
 assert.equal(c.manager.modelRole,"manager");
 assert.equal(c.roles.find((r:any)=>r.id==="general").modelRole,"implementation");
 assert.equal(c.roles.find((r:any)=>r.id==="explore").modelRole,"research");
 assert.doesNotMatch(c.manager.instructions,/Pi uses/);
 assert.match(c.manager.instructions,/missing skill/i);
});

test("Pi projects every shared development scenario without claiming live model equivalence",async()=>{const corpus=JSON.parse(await readFile(new URL("../../../internal/orchestration/testdata/manager-scenarios.json",import.meta.url),"utf8"));assert.equal(corpus.evidenceKind,"deterministic-contract-conformance");assert.equal(corpus.partition,"development");assert.equal(corpus.cases.length,11);const prompt=renderPiManagerPrompt(loadManagerContract());for(const scenario of corpus.cases)assert.ok(prompt.includes(scenario.fragment),scenario.id);});

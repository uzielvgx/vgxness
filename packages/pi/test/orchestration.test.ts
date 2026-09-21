import test from "node:test"; import assert from "node:assert/strict";
import { createHash } from "node:crypto"; import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises"; import { tmpdir } from "node:os"; import { join } from "node:path";
import { canonicalContract, loadManagerContract, resolveRole } from "../src/orchestration/contract.ts";
import { renderPiManagerPrompt } from "../src/orchestration/adapter.ts";
import { workerCanWrite, workerRoles } from "../src/workers/roles.ts";
test("generated contract has all roles and authority",async()=>{const c=loadManagerContract();assert.equal(c.roles.length,6);assert.equal(resolveRole(c,"worker")?.id,"general");assert.equal(resolveRole(c,"apply")?.id,undefined);assert.deepEqual(workerRoles,c.roles.map(role=>role.id));assert.equal(workerCanWrite("general"),true);assert.equal(workerCanWrite("sdd-apply"),false);assert.equal(workerCanWrite("verifier"),false);assert.match(resolveRole(c,"care-challenger")!.instructions,/Challenge/);});
test("native adapter documents limits",()=>{const prompt=renderPiManagerPrompt(loadManagerContract());assert.match(prompt,/host credentials are required/);assert.match(prompt,/Windows worker process ownership and worker continuation are unavailable/);});
test("generated prompt is the complete shared Manager projection", async()=>{ const c=loadManagerContract(); const prompt=await readFile(join(process.cwd(),"resources/prompts/manager.md"),"utf8"); assert.equal(prompt,renderPiManagerPrompt(c)); });
test("loader rejects missing roles, duplicate aliases, empty instructions, identity and digest drift", async(t)=>{ const root=await mkdtemp(join(tmpdir(),"pi-contract-")); t.after(()=>rm(root,{recursive:true,force:true})); const base:any=loadManagerContract(); let index=0; const emit=async(mutator:(value:any)=>void)=>{const value=structuredClone(base); mutator(value); value.sourceDigest=createHash("sha256").update(canonicalContract({schemaVersion:value.schemaVersion,identity:value.identity,manager:value.manager,roles:value.roles})).digest("hex"); const file=join(root,`${index++}.json`); await writeFile(file,JSON.stringify(value)); return file;}; for(const mutate of [(v:any)=>v.roles.pop(),(v:any)=>v.roles[2].aliases=["worker"],(v:any)=>v.roles[0].instructions="",(v:any)=>v.identity="wrong",(v:any)=>v.schemaVersion="wrong"]) { const file=await emit(mutate); assert.throws(()=>loadManagerContract(file),/manager contract drift/); } const digestDrift=structuredClone(base); digestDrift.sourceDigest="0".repeat(64); const digestFile=join(root,`${index++}.json`); await writeFile(digestFile,JSON.stringify(digestDrift)); assert.throws(()=>loadManagerContract(digestFile),/manager contract drift/); });

test("Pi Manager delegates all exploration and scopes skill loads", () => {
  const c: any = loadManagerContract();
  assert.match(c.manager.instructions, /Delegate all project code exploration to the explore role: reading files, searching, listing, and read-only diagnosis of repository content, with no simple exception/);
  assert.match(c.manager.instructions, /local Git queries \(status, diff, log, refs, tracking, conflicts\)/);
  assert.match(c.manager.instructions, /never for general parallel exploration, never to bypass a native deny/);
  assert.match(c.manager.instructions, /fail closed with a missing-dependency report when the required capability is unavailable/);
  assert.match(c.manager.instructions, /observed denied only when a real tool error proved it, unavailable when the dependency or transport is absent/);
  assert.doesNotMatch(c.manager.instructions, /without a formal plan, delegation/);
  assert.doesNotMatch(c.manager.instructions, /Load only a relevant managed skill/);
  const prompt = renderPiManagerPrompt(c);
  assert.match(prompt, /not read a skill body on the worker's behalf/);
  assert.doesNotMatch(prompt, /read SKILL.md and only required relative resources/);
  assert.match(prompt, /Registry metadata is an authorized Manager orchestration query, not project exploration/);
  assert.match(prompt, /Pi has no VGXNESS Go process/);
});

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

test("Pi projects active CARE policy fragments without claiming model equivalence",async()=>{const corpus=JSON.parse(await readFile(new URL("../../../internal/orchestration/testdata/care-coverage-scenarios.json",import.meta.url),"utf8"));assert.equal(corpus.partition,"development");assert.equal(corpus.holdout,"not protected holdout");assert.equal(corpus.execution,"unexecuted behavior");const prompt=renderPiManagerPrompt(loadManagerContract());for(const scenario of corpus.cases)for(const assertion of scenario.policyAssertions)assert.ok(prompt.includes(assertion.fragment),`${scenario.id}:${assertion.role}`);});

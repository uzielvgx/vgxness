import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { applyMigrations } from "../src/sqlite/migrations.ts";
import { dispatchSdd, type ServiceContext } from "../src/service/sdd.ts";
import { createHash } from "node:crypto";

function fixture() { const dir=mkdtempSync(join(tmpdir(),"pi-sdd-")); const database=new SQLiteDatabase(join(dir,"memory.db")); applyMigrations(database); const ctx:ServiceContext={database,project:"project-a",workspace:dir,mode:"full",role:"manager",now:(() => { let n=1_700_000_000_000_000_000n; return () => ++n; })()}; return {ctx,close(){database.close();rmSync(dir,{recursive:true,force:true});}}; }
const b64=(s:string)=>Buffer.from(s).toString("base64"), sha=(s:string)=>createHash("sha256").update(s).digest("hex");
test("runs the memory lifecycle with CAS and accepted revision bindings", async () => { const f=fixture(); try {
  const c=await dispatchSdd(f.ctx,"create",{idempotencyKey:"key",title:"Title",backend:"memory",interactionMode:"automatic",plan:"low"}); assert.equal(c.phase,"explore");
  const r=await dispatchSdd(f.ctx,"save_revision",{changeId:c.id,artifact:"explore",content:b64("research"),expectedStateVersion:1}); assert.equal(r.content,b64("research"));
  const a=await dispatchSdd(f.ctx,"accept_revision",{changeId:c.id,revisionId:r.id,expectedStateVersion:2}); assert.equal(a.status,"accepted");
  const next=await dispatchSdd(f.ctx,"transition",{changeId:c.id,targetPhase:"proposal",expectedStateVersion:3}); assert.equal(next.phase,"proposal"); assert.equal(next.stateVersion,4);
  await assert.rejects(dispatchSdd(f.ctx,"set_interaction_mode",{changeId:c.id,interactionMode:"interactive",expectedStateVersion:3}),/stale SDD state version/);
} finally {f.close();} });
test("renders hybrid OpenSpec bytes and requires their digest when recording", async () => { const f=fixture(); try {
  const c=await dispatchSdd(f.ctx,"create",{idempotencyKey:"hybrid",title:"Hybrid",backend:"hybrid",interactionMode:"automatic",plan:"medium"});
  const r=await dispatchSdd(f.ctx,"save_revision",{changeId:c.id,artifact:"explore",content:b64("research"),expectedStateVersion:1}); await dispatchSdd(f.ctx,"accept_revision",{changeId:c.id,revisionId:r.id,expectedStateVersion:2});
  const doc=await dispatchSdd(f.ctx,"render_projection",{changeId:c.id,revisionId:r.id}); assert.equal(Buffer.from(doc.content,"base64").toString().endsWith("research"),true);
  const p=await dispatchSdd(f.ctx,"record_projection",{changeId:c.id,artifactId:r.artifactId,revisionId:r.id,status:"current",digest:doc.digest,location:doc.relativePath,expectedStateVersion:3}); assert.equal(p.status,"current");
  const observed=await dispatchSdd(f.ctx,"compare_projection",{changeId:c.id,revisionId:r.id,relativePath:doc.relativePath,projectionContent:Buffer.from(doc.content,"base64").toString("utf8")}); assert.equal(observed.state,"synced");
  for (const invalid of [doc.content, "research", Buffer.from(doc.content,"base64").toString("utf8").replace("schemaVersion: 1", "schemaVersion: 2")]) await assert.rejects(dispatchSdd(f.ctx,"compare_projection",{changeId:c.id,revisionId:r.id,relativePath:doc.relativePath,projectionContent:invalid}));
  const altered = Buffer.from(doc.content,"base64").toString("utf8").replace("research", "tampered");
  await assert.rejects(dispatchSdd(f.ctx,"compare_projection",{changeId:c.id,revisionId:r.id,relativePath:doc.relativePath,projectionContent:altered}), /digest mismatch/);
  assert.equal(sha("research"),r.digest);
} finally {f.close();} });
test("denies mutations from a non-manager or read-only context", async () => { const f=fixture(); try { await assert.rejects(dispatchSdd({...f.ctx,role:"apply"},"create",{idempotencyKey:"key",title:"Title",backend:"memory",interactionMode:"automatic",plan:"low"}),/manager authority/); } finally {f.close();} });
test("permits the verified phase to complete", async () => { const f=fixture(); try {
  const c=await dispatchSdd(f.ctx,"create",{idempotencyKey:"complete",title:"Complete",backend:"memory",interactionMode:"automatic",plan:"low"});
  const db=f.ctx.database.db; db.prepare("UPDATE sdd_changes SET phase='verify' WHERE id=?").run(c.id);
  db.prepare("INSERT INTO sdd_artifacts VALUES(?,?,?,?,?,?,?,?)").run("artifact-verify","project-a",c.id,"verify","accepted","revision-verify",1n,1n);
  db.prepare("INSERT INTO sdd_revisions VALUES(?,?,?,?,?,?,?,?,?,?,?)").run("revision-verify","project-a",c.id,"artifact-verify","accepted",Buffer.from("verify"),null,sha("verify"),sha(""),1n,1n);
  const completed=await dispatchSdd(f.ctx,"transition",{changeId:c.id,targetPhase:"complete",expectedStateVersion:1}); assert.equal(completed.status,"completed");
} finally {f.close();} });

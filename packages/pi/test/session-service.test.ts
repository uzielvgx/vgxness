import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { applyMigrations } from "../src/sqlite/migrations.ts";
import { dispatchSession } from "../src/service/session.ts";

async function fixture(t: test.TestContext) {
  const root=await mkdtemp(join(tmpdir(),"pi-session-service-")), workspace=join(root,"workspace"); await mkdir(workspace);
  const database=new SQLiteDatabase(join(root,"memory.db")); applyMigrations(database); let tick=1_700_000_000_000_000_000n;
  const ctx={database,project:"project",workspace,storageRoot:root,mode:"full" as const,role:"manager",now:()=>tick++,sessionSecrets:new Map<string,{token:string;externalId:string}>()};
  t.after(async()=>{database.close();await rm(root,{recursive:true,force:true});}); return ctx;
}

test("provider sessions lease, draft, completion, and bounded handoff without external identities", async (t) => {
  const ctx=await fixture(t); const first:any=await dispatchSession(ctx,"memory.session.start",{externalId:"sdk-one"});
  assert.match(first.handle,/^ps-/); assert.match(first.leaseToken,/^ps-/);
  const draft:any=await dispatchSession(ctx,"memory.session.draft_save",{handle:first.handle,summary:"safe completed summary"});
  await dispatchSession(ctx,"memory.session.checkpoint",{handle:first.handle});
  const ended:any=await dispatchSession(ctx,"memory.session.end",{handle:first.handle,state:"completed",summary:""});
  assert.equal(ended.state,"completed"); assert.ok(ended.finalObservationId);
  const next:any=await dispatchSession(ctx,"memory.session.start",{externalId:"sdk-two"});
  const context:any=await dispatchSession(ctx,"memory.session.context",{handle:next.handle});
  assert.match(context.handoff,/safe completed summary/); assert.doesNotMatch(context.handoff,/sdk-one|leaseToken/);
  await assert.rejects(dispatchSession(ctx,"memory.session.renew",{handle:first.handle}),/invalid/);
  assert.match(draft.updatedAt,/Z$/);
});

test("provider session rejects a draft with a stale optimistic timestamp", async (t) => {
  const ctx=await fixture(t); const current:any=await dispatchSession(ctx,"session.start",{externalId:"sdk"});
  const draft:any=await dispatchSession(ctx,"session.draft_save",{handle:current.handle,summary:"one"});
  await assert.rejects(dispatchSession(ctx,"session.draft_save",{handle:current.handle,summary:"two",expectedUpdatedAt:"2020-01-01T00:00:00Z"}),/invalid/);
  await dispatchSession(ctx,"session.draft_save",{handle:current.handle,summary:"two",expectedUpdatedAt:draft.updatedAt});
});

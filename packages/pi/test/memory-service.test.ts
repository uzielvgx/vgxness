import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { applyMigrations } from "../src/sqlite/migrations.ts";
import { dispatchMemory } from "../src/service/memory.ts";

async function fixture(t: test.TestContext) {
  const root = await mkdtemp(join(tmpdir(), "pi-memory-service-"));
  const workspace = join(root, "workspace"); await mkdir(workspace);
  const database = new SQLiteDatabase(join(root, "memory.db")); applyMigrations(database);
  let tick = 1_700_000_000_000_000_000n;
  const ctx = { database, project: "project", workspace, storageRoot: root, mode: "full" as const, role: "manager", now: () => tick++, sessionSecrets: new Map<string, { token: string; externalId: string }>() };
  t.after(async () => { database.close(); await rm(root, { recursive: true, force: true }); }); return ctx;
}

test("memory persists scoped FTS records, topic replacement, previews, and archive", async (t) => {
  const ctx = await fixture(t);
  const first: any = await dispatchMemory(ctx, "memory.remember", { content: "Durable apple learning", title: "fruit", topicKey: "fruit" });
  assert.equal(first.scope, "project"); assert.equal(first.content, "Durable apple learning");
  const updated: any = await dispatchMemory(ctx, "memory.remember", { content: "Durable pear learning", title: "fruit", topicKey: "fruit" });
  assert.equal(updated.id, first.id);
  const found: any[] = await dispatchMemory(ctx, "memory.recall", { query: "pear" });
  assert.equal(found.length, 1); assert.equal(found[0].content, undefined); assert.match(found[0].preview, /pear/);
  const archived: any = await dispatchMemory(ctx, "memory.forget", { id: first.id });
  assert.equal(archived.state, "archived");
  assert.deepEqual(await dispatchMemory(ctx, "memory.recall", { query: "pear" }), []);
  const read: any = await dispatchMemory(ctx, "memory.get", { id: first.id });
  assert.equal(read.state, "archived"); assert.equal(read.content, "Durable pear learning");
});

test("memory uses stable workspace identity and rejects read-only writes", async (t) => {
  const ctx = await fixture(t);
  const resolved: any = await dispatchMemory(ctx, "memory.project.resolve", {});
  assert.match(resolved.project, /^workspace-[0-9a-f]{12}$/);
  await assert.rejects(dispatchMemory({ ...ctx, mode: "read-only" }, "memory.remember", { content: "blocked" }), /invalid/);
});

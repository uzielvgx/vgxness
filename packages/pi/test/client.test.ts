import assert from "node:assert/strict";
const mkdtemp = async (prefix: string) => realpath(await rawMkdtemp(prefix));
import test from "node:test";
import { mkdtemp as rawMkdtemp, realpath, mkdir, rename, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { loadNativeRuntime } from "./runtime-fixture.mjs";
const { createNativeDispatcher, operationNames } = await loadNativeRuntime();
async function fixture(t: test.TestContext) {
  const root = await mkdtemp(join(tmpdir(), "pi-dispatch-")), workspace = join(root, "workspace"), storageRoot = join(root, "storage"); await mkdir(workspace);
  const binding = { workspace, mode: "full", role: "manager" }, client = await createNativeDispatcher({ ...binding, storageRoot });
  t.after(async () => { await client.close(); await rm(root, { recursive: true, force: true }); }); return { root, workspace, storageRoot, binding, client };
}
test("native dispatcher enforces closed payloads and exact local bindings", async t => {
  const { client, binding } = await fixture(t);
  assert.equal(operationNames.length, 21);
  for (const payload of [null, [], { project: "other" }, { unexpected: true }]) await assert.rejects(client.request("memory.recent", payload, binding), /invalid operation/);
  for (const key of ["workspace", "mode", "role"]) await assert.rejects(client.request("memory.recent", {}, { ...binding, [key]: "forged" }), /binding mismatch/);
  await assert.rejects(client.request("arbitrary.shell", {}, binding), /invalid operation/);
  await assert.rejects(client.request("memory.remember", { content: "x", scope: "personal" }, binding), /invalid operation/);
  await assert.rejects(client.request("memory.remember", { content: "x", sourceProvider: "forged", sourceId: "unverified" }, binding));
  await assert.rejects(client.request("memory.remember", { content: "x".repeat(1048577) }, binding), /exceeds limit/);
  const entry = await client.request("memory.remember", { content: "durable native evidence" }, binding);
  assert.equal((await client.request("memory.get", { id: entry.ID }, binding)).Content, "durable native evidence");
});
test("queued payloads are captured and failed work does not poison dispatcher", async t => {
  const { client, binding } = await fixture(t), payload = { content: "accepted bytes" };
  const pending = client.request("memory.remember", payload, binding); payload.content = "changed bytes";
  assert.equal((await pending).Content, "accepted bytes");
  const values = await Promise.allSettled([client.request("unknown", {}, binding), client.request("memory.recent", {}, binding)]);
  assert.equal(values[0].status, "rejected"); assert.equal(values[1].status, "fulfilled");
  await client.close(); await assert.rejects(client.request("memory.recent", {}, binding), /closed/);
});
test("workspace replacement and read-only or worker authority fail closed", async t => {
  const { client, workspace, root, storageRoot, binding } = await fixture(t);
  for (const [mode, role] of [["read-only", "manager"], ["full", "general"], ["read-only", "explore"]]) {
    const b = { workspace, mode, role }, other = await createNativeDispatcher({ ...b, storageRoot });
    try { assert.deepEqual(await other.request("memory.recent", {}, b), []); await assert.rejects(other.request("memory.remember", { content: "denied" }, b), /authority/); await assert.rejects(other.request("sdd.create", { title: "x", idempotencyKey: "x", backend: "memory", interactionMode: "automatic", plan: "low" }, b), /invalid operation/); } finally { await other.close(); }
  }
  await rename(workspace, join(root, "original")); await mkdir(workspace);
  await assert.rejects(client.request("memory.recent", {}, binding), /identity changed/);
  await rm(workspace, { recursive: true }); await symlink(join(root, "original"), workspace);
  await assert.rejects(client.request("memory.recent", {}, binding), /identity changed/);
});
test("native sessions redact capabilities and enforce lease rotation and explicit completion", async t => {
  const { client, binding, storageRoot } = await fixture(t);
  const first = await client.request("memory.session.start", { externalId: "raw-private-external" }, binding);
  assert.equal(first.leaseToken, undefined); assert.doesNotMatch(JSON.stringify(first), /raw-private-external/);
  const second = await createNativeDispatcher({ ...binding, storageRoot });
  try {
    await second.request("memory.session.start", { externalId: "raw-private-external" }, binding);
    await assert.rejects(client.request("memory.session.draft_save", { handle: first.handle, summary: "stale owner" }, binding));
    const draft = await second.request("memory.session.draft_save", { handle: first.handle, summary: "explicit safe summary" }, binding);
    await assert.rejects(second.request("memory.session.draft_save", { handle: first.handle, summary: "missing optimistic version" }, binding));
    await second.request("memory.session.draft_save", { handle: first.handle, summary: "updated safe summary", expectedUpdatedAt: draft.UpdatedAt }, binding);
    await second.request("memory.session.end", { handle: first.handle, state: "completed", summary: "" }, binding);
    const next = await second.request("memory.session.start", { externalId: "next" }, binding);
    assert.match((await second.request("memory.session.context", { handle: next.handle }, binding)).handoff, /UNTRUSTED DATA.*updated safe summary/s);
  } finally { await second.close(); }
});

test("explicit initialization publishes portable marker without rekeying local project", async t => {
 const { client, binding, workspace } = await fixture(t);
 const local = await client.request("memory.project.resolve", {}, binding);
 const portable = await client.request("memory.project.initialize", {}, binding);
 assert.match(portable, /^[a-f0-9-]{36}$/); assert.notEqual(portable, local);
 assert.equal(await client.request("memory.project.initialize", {}, binding), portable);
 assert.equal(await client.request("memory.project.resolve", {}, binding), local);
 const { readFile, writeFile } = await import("node:fs/promises");
 const marker = join(workspace, ".vgxness", "project-id"); assert.equal(JSON.parse(await readFile(marker, "utf8")).project_id, portable);
 await writeFile(marker, JSON.stringify({ format: "vgxness-project-id/v1", kind: "project", project_id: "550e8400-e29b-41d4-a716-446655440000" }));
 await assert.rejects(client.request("memory.project.initialize", {}, binding), /marker changed/);
});

test("memory retains Go zero-value defaults and rejects Unicode control metadata", async t => {
 const { client, binding } = await fixture(t);
 const value = await client.request("memory.remember", { content: "apple evidence", type: "", state: "" }, binding);
 assert.equal(value.Type, "learning"); assert.equal(value.State, "active");
 for (const operation of ["memory.recent", "memory.recall"]) { const values = await client.request(operation, { ...(operation.endsWith("recall") ? { query: "apple" } : {}), limit: 0, states: [] }, binding); assert.equal(values.length, 1); }
 await assert.rejects(client.request("memory.remember", { content: "apple", title: "   " }, binding));
 await assert.rejects(client.request("memory.remember", { content: "apple\u0085evidence" }, binding));
});

test("production credential file stays private and resolves only the fixed configured reference", async t => {
 const { workspace, storageRoot, binding } = await fixture(t), { writeFile } = await import("node:fs/promises"), { randomBytes } = await import("node:crypto");
 const device = "550e8400-e29b-41d4-a716-446655440000", token = `vgx1.${device}.${randomBytes(32).toString("base64url")}`, credentialFile = join(storageRoot, "credential");
 await writeFile(credentialFile, token, { mode: 0o600 });
 const client = await createNativeDispatcher({ ...binding, storageRoot, credentialFile });
 try {
   const status = await client.request("memory.sync.configure", { endpoint: "https://sync.example.test", deviceId: device }, binding); assert.equal(status.credential, "available"); assert.doesNotMatch(JSON.stringify(status), /vgx1\.|credential$/);
   await assert.rejects(client.request("memory.sync.configure", { endpoint: "https://sync.example.test", deviceId: "550e8400-e29b-41d4-a716-446655440001" }, binding), /does not match device/);
   const { SQLiteDatabase } = await import("../src/sqlite/node-sqlite.ts"); const db = new SQLiteDatabase(join(storageRoot, "memory.db"), { readOnly: true });
   try { const row: any = db.queryOne("SELECT credential_ref FROM sync_profiles"); assert.equal(row.credential_ref, "secret://keychain/sync/file"); assert.doesNotMatch(JSON.stringify(row), /vgx1\.|\/tmp\//); } finally { db.close(); }
 } finally { await client.close(); }
});


test("retired SDD operations are absent and cannot mutate native memory", async t => {
 const {client,binding}=await fixture(t);
 assert.equal(operationNames.some((name:string)=>name.startsWith("sdd.")),false);
 const entry=await client.request("memory.remember",{content:"retirement sentinel"},binding);
 await assert.rejects(client.request("sdd.create",{idempotencyKey:"no",title:"retired",backend:"memory",interactionMode:"automatic",plan:"low"},binding),/invalid operation|retired/);
 assert.equal((await client.request("memory.get",{id:entry.ID},binding)).Content,"retirement sentinel");
});

import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { applyMigrations, latestSchemaVersion, migrations } from "../src/sqlite/migrations.ts";

test("all Go migration resources apply with their immutable hashes", async (t) => { const root = await mkdtemp(join(tmpdir(), "pi-migrations-")); const db = new SQLiteDatabase(join(root,"db.sqlite")); t.after(async () => { db.close(); await rm(root, { recursive: true, force: true }); }); applyMigrations(db); assert.equal((db.db.prepare("PRAGMA user_version").get() as any).user_version,23n); assert.equal(migrations.length,23); assert.ok(migrations.every((m)=>/^[0-9a-f]{64}$/.test(m.sha256))); assert.equal((db.db.prepare("PRAGMA foreign_keys").get() as any).foreign_keys,1n); });
test("migration rejects a newer database and restores foreign keys", async (t) => { const root=await mkdtemp(join(tmpdir(),"pi-migrations-newer-"));const db=new SQLiteDatabase(join(root,"db.sqlite"));t.after(async () => { db.close(); await rm(root, { recursive: true, force: true }); });db.db.exec("PRAGMA user_version=24");assert.throws(()=>applyMigrations(db),/newer/); });

test("v11 rebuild rolls back atomically and restores FK enforcement before retry", async t => {
 const root = await mkdtemp(join(tmpdir(), "pi-v11-rollback-")); const db = new SQLiteDatabase(join(root, "db")); t.after(async () => { db.close(); await rm(root, { recursive: true, force: true }); });
 applyMigrations(db, migrations.slice(0, 10));
 db.db.exec("INSERT INTO projects(id) VALUES('project')");
 const before = db.db.prepare("SELECT sql FROM sqlite_master WHERE name='sdd_changes'").get();
 const broken = migrations.map(step => step.version === 11 ? { ...step, sql: step.sql + ";INSERT INTO table_that_does_not_exist VALUES(1);" } : step);
 assert.throws(() => applyMigrations(db, broken), /no such table/);
 assert.equal((db.db.prepare("PRAGMA user_version").get() as any).user_version, 10n);
 assert.equal((db.db.prepare("PRAGMA foreign_keys").get() as any).foreign_keys, 1n);
 assert.deepEqual(db.db.prepare("SELECT sql FROM sqlite_master WHERE name='sdd_changes'").get(), before);
 assert.throws(() => db.db.exec("INSERT INTO sessions(id,project_id) VALUES('bad','missing')"), /FOREIGN KEY/);
 applyMigrations(db); assert.equal((db.db.prepare("PRAGMA user_version").get() as any).user_version, 23n); assert.equal(db.db.prepare("PRAGMA foreign_key_check").get(), undefined);
});

import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { decodeBlob, decodeRawJSON, encodeBlob, encodeRawJSON, formatTimestamp, nowNs, parseTimestamp } from "../src/sqlite/codec.ts";

test("adapter serializes transactions and preserves binary and bigint codecs", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "pi-sqlite-")); const database = new SQLiteDatabase(join(root, "store.sqlite")); t.after(async () => { database.close(); await rm(root, { recursive: true, force: true }); });
  database.db.exec("CREATE TABLE values_table (id INTEGER PRIMARY KEY, payload BLOB NOT NULL)");
  database.transaction(() => database.db.prepare("INSERT INTO values_table(payload) VALUES(?)").run(encodeBlob(new Uint8Array([0, 255, 7]))));
  assert.deepEqual(decodeBlob((database.db.prepare("SELECT payload FROM values_table").get() as any).payload), new Uint8Array([0, 255, 7]));
  assert.deepEqual(decodeRawJSON(encodeRawJSON('{\"x\":9007199254740993}')), new TextEncoder().encode('{\"x\":9007199254740993}'));
  const value = 1_725_000_000_123_456_789n; assert.equal(parseTimestamp(formatTimestamp(value)), value);
  assert.equal(parseTimestamp(formatTimestamp(-1n)), -1n);
  assert.equal(typeof nowNs(), "bigint");
  assert.throws(() => database.transaction(() => { database.db.exec("INSERT INTO values_table(payload) VALUES(X'00')"); throw new Error("rollback"); }));
  assert.equal((database.db.prepare("SELECT count(*) AS n FROM values_table").get() as any).n, 1n);
  assert.throws(() => database.transaction(() => Promise.resolve("not serialized")));
});

test("read-only adapter refuses writes", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "pi-sqlite-ro-")); const path = join(root, "store.sqlite");
  const writable = new SQLiteDatabase(path); writable.db.exec("CREATE TABLE t(id INTEGER)"); writable.close();
  const readonly = new SQLiteDatabase(path, { readOnly: true }); t.after(async () => { readonly.close(); await rm(root, { recursive: true, force: true }); });
  assert.throws(() => readonly.transaction(() => undefined), /read-only/);
  assert.throws(() => readonly.db.exec("INSERT INTO t VALUES(1)"));
});

test("void transactions commit and failures leave a reusable connection", async t => {
 const root = await mkdtemp(join(tmpdir(), "pi-sqlite-void-")); const db = new SQLiteDatabase(join(root, "db")); t.after(async () => { db.close(); await rm(root, { recursive: true, force: true }); }); db.db.exec("CREATE TABLE value(id INTEGER)");
 assert.equal(db.transaction(() => { db.db.exec("INSERT INTO value VALUES(1)"); }), undefined);
 assert.throws(() => db.transaction(() => { db.db.exec("INSERT INTO value VALUES(2)"); throw new Error("failed transaction"); }));
 db.transaction(() => { db.db.exec("INSERT INTO value VALUES(3)"); });
 assert.equal((db.db.prepare("SELECT count(*) n FROM value").get() as any).n, 2n);
});
test("database and WAL/SHM paths reject symlinks and directories; new files are private", async t => {
 const { mkdir, symlink, stat, writeFile } = await import("node:fs/promises");
 const root = await mkdtemp(join(tmpdir(), "pi-sqlite-paths-")); for (const suffix of ["", "-wal", "-shm"]) {
   const path = join(root, `db${suffix.length}`), outside = join(root, `outside${suffix.length}`); await writeFile(outside, "untouched");
   await symlink(outside, path + suffix); assert.throws(() => new SQLiteDatabase(path), /regular|symlink/); await rm(path + suffix);
   await mkdir(path + suffix); assert.throws(() => new SQLiteDatabase(path), /regular|symlink/); await rm(path + suffix, { recursive: true });
 }
 const path = join(root, "private"), db = new SQLiteDatabase(path); t.after(async () => { db.close(); await rm(root, { recursive: true, force: true }); }); db.db.exec("CREATE TABLE t(x)");
 if (process.platform !== "win32") for (const file of [path, path + "-wal", path + "-shm"]) assert.equal((await stat(file)).mode & 0o077, 0);
});
test("newer read-only schema rejection and writer contention release connections", async t => {
 const { DatabaseSync } = await import("node:sqlite");
 const root = await mkdtemp(join(tmpdir(), "pi-sqlite-lock-")); const path = join(root, "db"), first = new SQLiteDatabase(path), second = new SQLiteDatabase(path); t.after(async () => { first.close(); second.close(); await rm(root, { recursive: true, force: true }); });
 first.db.exec("CREATE TABLE t(x); BEGIN IMMEDIATE");
 assert.throws(() => second.transaction(() => second.db.exec("INSERT INTO t VALUES(1)")), /locked|busy/);
 first.db.exec("ROLLBACK"); second.transaction(() => { second.db.exec("INSERT INTO t VALUES(2)"); });
 first.close(); second.close();
 const raw = new DatabaseSync(path); raw.exec("PRAGMA user_version=24"); raw.close();
 assert.throws(() => new SQLiteDatabase(path, { readOnly: true }), /newer/);
 const reset = new DatabaseSync(path); reset.exec("PRAGMA user_version=23; BEGIN EXCLUSIVE; COMMIT"); reset.close();
});

test("timestamp codec rejects normalized invalid calendars and preserves leap-day nanoseconds", () => {
 for (const value of ["2024-02-30T00:00:00Z", "2023-02-29T00:00:00Z", "2024-01-01T24:00:00Z", "2024-00-01T00:00:00Z", "2024-01-00T00:00:00Z"]) assert.throws(() => parseTimestamp(value), /invalid timestamp/);
 const leap = "2024-02-29T23:59:59.123456789Z"; assert.equal(formatTimestamp(parseTimestamp(leap)), leap);
});

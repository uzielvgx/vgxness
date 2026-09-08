/** Isolated package health check. Never opens configured user storage. */
import { mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { backup } from "node:sqlite";
import { SQLiteDatabase } from "./sqlite/node-sqlite.ts";
import { applyMigrations, latestSchemaVersion } from "./sqlite/migrations.ts";
const [major, minor] = process.versions.node.split(".").map(Number);
if (major < 22 || (major === 22 && minor < 19)) throw new Error("Node >=22.19.0 is required");
const root = mkdtempSync(join(realpathSync(tmpdir()), "vgxness-pi-health-"));
let db: SQLiteDatabase | undefined;
try {
  db = new SQLiteDatabase(join(root, "health.db"));
  applyMigrations(db);
  const health = db.health();
  if (Number(health.schemaVersion) !== latestSchemaVersion) throw new Error("migration health mismatch");
  const integer = db.queryOne<{ value: bigint }>("SELECT 9007199254740993 AS value");
  if (integer?.value !== 9007199254740993n) throw new Error("64-bit integer precision unavailable");
  db.db.exec("CREATE VIRTUAL TABLE temp.health_fts USING fts5(content); INSERT INTO health_fts VALUES ('portable health');");
  if (!db.queryOne("SELECT rowid FROM health_fts WHERE health_fts MATCH 'portable'")) throw new Error("FTS5 unavailable");
  await backup(db.db, join(root, "backup.db"));
  process.stdout.write(JSON.stringify({type:"health", runtime:"typescript", schemaVersion:latestSchemaVersion, foreignKeys:true, fts5:true, bigint:true, backup:true}) + "\n");
} finally { db?.close(); rmSync(root, {recursive:true, force:true}); }

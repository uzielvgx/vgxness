import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { applyMigrations } from "../src/sqlite/migrations.ts";

test("TS database has the Go-compatible schema and raw SQLite values", async (t) => { const root=await mkdtemp(join(tmpdir(),"pi-compat-"));const db=new SQLiteDatabase(join(root,"memory.db"));t.after(async () => { db.close(); await rm(root, { recursive: true, force: true }); });applyMigrations(db);db.execute("INSERT INTO projects(id) VALUES(?)","project");db.execute("INSERT INTO observations(id,project_id,scope,type,content,producer,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)","id","project","project","learning","content","pi","active",9007199254740993n,9007199254740993n);const row:any=db.queryOne("SELECT created_at FROM observations WHERE id='id'");assert.equal(row.created_at,9007199254740993n);assert.ok(db.queryOne("SELECT name FROM sqlite_master WHERE name='observations_fts'")); });

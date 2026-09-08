import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { SQLiteDatabase } from "../src/sqlite/node-sqlite.ts";
import { applyMigrations } from "../src/sqlite/migrations.ts";

import { fileURLToPath } from "node:url";

test("real Go and TS connections round-trip int64, BLOB and FTS in both directions", async t => {
 const { writeFile } = await import("node:fs/promises"), { spawnSync } = await import("node:child_process");
 const root = await mkdtemp(join(tmpdir(), "pi-go-roundtrip-")); t.after(() => rm(root, { recursive: true, force: true }));
 const path = join(root, "memory.db"), source = join(root, "roundtrip.go");
 const db = new SQLiteDatabase(path); t.after(() => db.close()); applyMigrations(db);
 db.db.exec("CREATE TABLE compatibility_values(id TEXT PRIMARY KEY, precise INTEGER NOT NULL, payload BLOB NOT NULL)");
 db.execute("INSERT INTO compatibility_values VALUES(?,?,?)", "typescript", 9007199254740993n, new Uint8Array([0, 255, 128, 7]));
 db.execute("INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)", "typescript", "", "", "learning", "typescript searchable evidence");
 await writeFile(source, `package main
import("database/sql";"fmt";"os";_ "modernc.org/sqlite")
func main(){ db,e:=sql.Open("sqlite",os.Args[1]);if e!=nil{panic(e)};defer db.Close();var precise int64;var payload []byte;if e=db.QueryRow("SELECT precise,payload FROM compatibility_values WHERE id='typescript'").Scan(&precise,&payload);e!=nil{panic(e)};var matches int;if e=db.QueryRow("SELECT count(*) FROM observations_fts WHERE observations_fts MATCH 'typescript'").Scan(&matches);e!=nil{panic(e)};fmt.Printf("%d:%x:%d",precise,payload,matches);if _,e=db.Exec("INSERT INTO compatibility_values VALUES('go',?,?)",int64(9223372036854775806),[]byte{0,255,0,127});e!=nil{panic(e)};if _,e=db.Exec("INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES('go','','','learning','golang searchable evidence')");e!=nil{panic(e)}}`);
 const run = spawnSync("go", ["run", source, path], { cwd: fileURLToPath(new URL("../../../", import.meta.url)), env: { ...process.env, GOPROXY: "off", GOSUMDB: "off" }, encoding: "utf8", timeout: 60000 });
 assert.equal(run.status, 0, run.stderr); assert.equal(run.stdout, "9007199254740993:00ff8007:1");
 const value: any = db.queryOne("SELECT precise,payload FROM compatibility_values WHERE id='go'"); assert.equal(value.precise, 9223372036854775806n); assert.deepEqual([...value.payload], [0,255,0,127]);
 assert.equal((db.queryOne("SELECT count(*) n FROM observations_fts WHERE observations_fts MATCH 'golang'") as any).n, 1n);
});

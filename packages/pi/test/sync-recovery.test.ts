import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { SQLiteDatabase } from '../src/sqlite/node-sqlite.ts';
import { applyMigrations } from '../src/sqlite/migrations.ts';
import { reseedProject, rejoinProject } from '../src/service/sync-recovery.ts';
const when = 1700000000123456789n;
function fixture(t: any) { const root = mkdtempSync(join(tmpdir(), 'pi-recovery-')); const database = new SQLiteDatabase(join(root, 'memory.db')); applyMigrations(database); t.after(() => { database.close(); rmSync(root, { recursive: true, force: true }); }); const db = database.db; db.prepare("INSERT INTO projects VALUES('local',5)").run(); const portable = '550e8400-e29b-41d4-a716-446655440000'; db.prepare('INSERT INTO portable_project_identities VALUES(?,?,?,?,?)').run(portable, 'local', 'a'.repeat(64), 'marker', '2025-01-01T00:00:00Z'); db.prepare("INSERT INTO observations(id,title,project_id,scope,type,content,producer,state,created_at,updated_at,sync_version) VALUES('obs','Title','local','project','fact','WAL snapshot','test','active',?,?,4)").run(when, when); return { ctx: { database, project: 'local', now: () => when }, db, portable }; }
test('transition requires binding without any mutation', async () => { const ctx: any = { project: 'local', database: { db: { prepare: () => ({ get: () => undefined }) } }, now: () => when }; await assert.rejects(() => reseedProject(ctx), /binding/); await assert.rejects(() => rejoinProject(ctx), /binding/); });
test('reseed snapshots WAL with backup intent before resetting versions and enqueuing source', async (t) => {
    const { ctx, db, portable } = fixture(t);
    const result = await reseedProject(ctx);
    assert.equal(result.status, 'publishing');
    const snapshot = new DatabaseSync(result.backup_path!);
    try {
        assert.equal((snapshot.prepare('SELECT content FROM observations').get() as any).content, 'WAL snapshot');
        assert.equal((snapshot.prepare('SELECT sync_version FROM projects').get() as any).sync_version, 5);
        assert.ok(snapshot.prepare('SELECT 1 FROM sync_project_backup_intents').get());
    }
    finally {
        snapshot.close();
    }
    assert.equal((db.prepare('SELECT sync_version FROM projects').get() as any).sync_version, 0n);
    assert.equal((db.prepare('SELECT count(*) n FROM sync_outbox').get() as any).n, 2n);
    assert.equal((db.prepare('SELECT count(*) n FROM sync_project_transition_records WHERE portable_project_id=?').get(portable) as any).n, 2n);
    assert.equal(db.prepare('SELECT 1 FROM sync_project_backup_intents').get(), undefined);
    assert.equal(result.transition_identity, when);
});
test('rejoin retains local materialization for pull and resumes same active generation', async (t) => { const { ctx, db } = fixture(t); const first = await rejoinProject(ctx); const again = await rejoinProject(ctx); assert.equal(first.transition_identity, again.transition_identity); assert.equal(first.status, 'pulling'); assert.equal((db.prepare('SELECT sync_version FROM projects').get() as any).sync_version, 5n); assert.equal((db.prepare('SELECT count(*) n FROM sync_outbox').get() as any).n, 0n); await assert.rejects(() => reseedProject(ctx), /another sync/); });
test('transition backup refuses active project claims and preserves materialized versions', async (t) => { const { ctx, db } = fixture(t); const id = '550e8400-e29b-41d4-a716-446655440001', token = '550e8400-e29b-41d4-a716-446655440002'; db.prepare("INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,last_error_code,created_at,updated_at) VALUES(?,'project','local','update',5,1,?,'pending',0,?,'',?,?)").run(id, Buffer.from('{}'), when, when, when); db.prepare('INSERT INTO sync_outbox_claims VALUES(?,?,?,?,?,?)').run(id, token, token, when, when, when + 30000000000n); await assert.rejects(() => reseedProject(ctx), /active sync claim/); assert.equal((db.prepare('SELECT sync_version FROM projects').get() as any).sync_version, 5n); assert.equal(db.prepare('SELECT 1 FROM sync_project_transitions').get(), undefined); assert.ok(db.prepare('SELECT backup_sha256 FROM sync_project_backup_intents').get()); });
test('database-supplied backup intent cannot write or read an unrelated path', async (t) => { const { ctx, db, portable } = fixture(t), outside = mkdtempSync(join(tmpdir(), 'pi-unrelated-')), path = join(outside, '.sync-project-backup-' + 'a'.repeat(32) + '.sqlite'); t.after(() => rmSync(outside, { recursive: true, force: true })); writeFileSync(path, 'untouched'); db.prepare('INSERT INTO sync_project_backup_intents VALUES(?,?,?,?,?,?,?)').run(portable, 'local', 'rejoin_merge', '550e8400-e29b-41d4-a716-446655440001', path, null, when); await assert.rejects(() => rejoinProject(ctx), /unsafe backup intent path/); assert.equal(readFileSync(path, 'utf8'), 'untouched'); assert.equal(db.prepare('SELECT 1 FROM sync_project_transitions').get(), undefined); });

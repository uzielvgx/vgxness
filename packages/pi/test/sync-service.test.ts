import { reseedProject } from '../src/service/sync-recovery.ts';
import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID, createHash } from 'node:crypto';
import { SQLiteDatabase } from '../src/sqlite/node-sqlite.ts';
import { applyMigrations } from '../src/sqlite/migrations.ts';
import { configureSync, backfillSyncProject, claim, finish, applyChange, dispatchSync } from '../src/service/sync.ts';
import { enqueueObservationChange, translateMutation, stableSyncUUID, syncBytes, enqueueMutation, queueSummary } from '../src/service/sync-state.ts';
import { parseProtocolJSON, canonicalChangeHash } from '../src/service/sync-wire.ts';
const when = 1700000000123456789n;
function fixture(t: any) { const root = mkdtempSync(join(tmpdir(), 'pi-sync-')); const database = new SQLiteDatabase(join(root, 'memory.db')); applyMigrations(database); t.after(() => { database.close(); rmSync(root, { recursive: true, force: true }); }); const db = database.db; db.prepare("INSERT INTO projects VALUES('local',0),('other',0)").run(); const portable = '550e8400-e29b-41d4-a716-446655440000'; db.prepare('INSERT INTO portable_project_identities VALUES(?,?,?,?,?)').run(portable, 'local', 'a'.repeat(64), 'marker', '2025-01-01T00:00:00Z'); const ctx = { database, project: 'local', workspace: root, storageRoot: root, mode: 'full' as const, role: 'manager', now: () => when }; configureSync(ctx, { endpoint: 'https://sync.example.test', deviceId: randomUUID(), credentialRef: 'secret://keychain/sync/file' }); return { ctx, db, portable }; }
function add(db: any, id = 'obs', project = 'local') { db.prepare("INSERT INTO observations(id,title,project_id,scope,type,content,producer,state,created_at,updated_at) VALUES(?,'Title',?,'project','fact','durable','test','active',?,?)").run(id, project, when, when); }
test('backfill keeps Go local identities, exact nanos, canonical payload and idempotent count', t => {
    const { ctx, db } = fixture(t);
    add(db);
    const first = backfillSyncProject(ctx, 100);
    assert.equal(first.queued, 2);
    assert.equal(backfillSyncProject(ctx, 100).queued, 0);
    for (const row of db.prepare('SELECT * FROM sync_outbox').all() as any[]) {
        const m = parseProtocolJSON(Buffer.from(row.payload).toString());
        assert.equal(m.mutation_id, row.mutation_id);
        assert.equal(m.record_id, row.record_id);
        assert.equal(row.created_at, when);
        assert.equal(m.mutation_id, stableSyncUUID(`vgxness/sync-backfill/v1\0${row.record_kind}\0${row.record_id}`));
    }
    assert.equal((db.prepare("SELECT sync_version FROM observations WHERE id='obs'").get() as any).sync_version, 0n);
});
test('claims isolate project and cannot steal live leases; receipt hashes match payload and unlock dependents', t => { const { ctx, db } = fixture(t); add(db); add(db, 'foreign', 'other'); backfillSyncProject({ ...ctx, project: 'other' }, 100); backfillSyncProject(ctx, 100); const first = claim(ctx, 16); assert.equal(first.length, 1); assert.equal(first[0].mutation.record_id, 'local'); assert.equal(claim(ctx, 16).length, 0); finish(ctx, first[0], { mutation_id: first[0].id, disposition: 'accepted', code: '', version: 1, sequence: 1, retryable: false }); const receipt = db.prepare('SELECT * FROM sync_push_results').get() as any; assert.deepEqual(Buffer.from(receipt.mutation_hash), createHash('sha256').update(syncBytes(first[0].mutation)).digest()); assert.equal(receipt.completed_at, when); const second = claim(ctx, 16); assert.equal(second.length, 1); assert.equal(second[0].mutation.record_id, 'obs'); assert.throws(() => finish(ctx, { ...second[0], token: randomUUID() }, { mutation_id: second[0].id }), /stale/); });
test('local writes preserve canonical versions and translate session/reference identities only on wire', t => { const { ctx, db, portable } = fixture(t); add(db, 'target'); add(db); db.prepare("INSERT INTO sessions VALUES('session','local',0)").run(); db.prepare("UPDATE observations SET session_id='session' WHERE id='obs'").run(); db.prepare("INSERT INTO observation_refs VALUES('obs','target')").run(); ctx.database.transaction(() => enqueueObservationChange(ctx, 'obs')); const row = db.prepare("SELECT payload FROM sync_outbox WHERE record_kind='observation'").get() as any; const local = parseProtocolJSON(Buffer.from(row.payload).toString()); assert.equal(local.observation.session_id, 'session'); assert.deepEqual(local.observation.references, ['target']); const wire = ctx.database.transaction(() => translateMutation(ctx, local)); assert.equal(wire.observation.project_id, portable); assert.equal(wire.record_id, stableSyncUUID(`vgxness/sync-portable-identity/v1\0${portable}\0observation\0obs`)); assert.equal((db.prepare("SELECT sync_version FROM observations WHERE id='obs'").get() as any).sync_version, 0n); });
test('pull exact int64 receipt indexes FTS and conflicts preserve canonical content', t => { const { ctx, db, portable } = fixture(t); const id = randomUUID(), history = randomUUID(); const mutation: any = { mutation_id: randomUUID(), record_id: id, record_kind: 'observation', kind: 'create', base_version: 0, observation: { id, title: 'Remote title', project_id: portable, scope: 'project', type: 'fact', content: 'remote content', provenance: { producer: 'test' }, lifecycle: 'active', review: 'clear', created_at: '2023-11-14T22:13:20.123456789Z', updated_at: '2023-11-14T22:13:20.123456789Z' } }; const c: any = { sequence: 9007199254740993n, canonical_version: 1, mutation }; c.change_hash = canonicalChangeHash(c); ctx.database.transaction(() => applyChange(ctx, portable, history, c)); const row = db.prepare('SELECT * FROM observations').get() as any; assert.equal(row.id, `sync-adopted:observation:${id}`); assert.equal(row.created_at, when); assert.equal((db.prepare("SELECT count(*) n FROM observations_fts WHERE observations_fts MATCH 'remote'").get() as any).n, 1n); const conflict: any = { sequence: 9007199254740994n, canonical_version: 1, hash_version: 2, change_disposition: 'conflict', conflict_id: randomUUID(), mutation: { ...mutation, mutation_id: randomUUID(), kind: 'update', base_version: 1, observation: { ...mutation.observation, content: 'competing' } } }; conflict.change_hash = canonicalChangeHash(conflict); ctx.database.transaction(() => applyChange(ctx, portable, history, conflict)); assert.equal((db.prepare('SELECT content FROM observations').get() as any).content, 'remote content'); assert.equal((db.prepare('SELECT state FROM observations').get() as any).state, 'needs_review'); assert.equal((db.prepare('SELECT record_id FROM sync_conflicts').get() as any).record_id, row.id); });
test('unknown operations fail closed', async () => { await assert.rejects(() => dispatchSync({} as any, 'unknown'), /unsupported/); });
test('disabled profile continues offline enqueue; tombstone and transition guards precede absent profile', t => { const { ctx, db } = fixture(t); add(db); db.prepare('UPDATE sync_profiles SET enabled=0').run(); ctx.database.transaction(() => enqueueObservationChange(ctx, 'obs')); assert.equal((db.prepare('SELECT count(*) n FROM sync_outbox').get() as any).n, 2n); db.prepare('DELETE FROM sync_profiles').run(); db.prepare("INSERT INTO sync_project_transitions VALUES(?,'local','rejoin_merge','pulling',?,NULL)").run('550e8400-e29b-41d4-a716-446655440000', when); assert.throws(() => ctx.database.transaction(() => enqueueObservationChange(ctx, 'obs')), /transition/); });
test('foreground sync completes through actual wire codec and polls new watermark on next run', async (t) => {
    const { ctx, db, portable } = fixture(t);
    add(db);
    backfillSyncProject(ctx, 100);
    const history = randomUUID(), changes: any[] = [];
    let seq = 0;
    const bearer = `vgx1.${randomUUID()}.${Buffer.alloc(32, 7).toString('base64url')}`;
    const options: any = { credentials: { get: () => bearer }, http: { request: async (request: any) => {
                const url = new URL(request.url);
                let body: any;
                if (url.pathname.endsWith('/capabilities'))
                    body = { protocol_version: 1, capabilities: ['project_state'] };
                else if (url.pathname.endsWith('/discovery'))
                    body = { protocol_version: 1, history_id: history, capabilities: ['bootstrap_discovery'] };
                else if (url.pathname.endsWith('/push')) {
                    const items = parseProtocolJSON(request.body).items;
                    body = { protocol_version: 1, results: items.map((mutation: any) => { assert.equal(mutation.project?.id ?? mutation.observation?.project_id, portable); const c: any = { sequence: ++seq, canonical_version: Number(mutation.base_version) + 1, mutation }; c.change_hash = canonicalChangeHash(c); changes.push(c); return { mutation_id: mutation.mutation_id, disposition: 'accepted', version: c.canonical_version, sequence: seq }; }) };
                }
                else {
                    const after = Number(url.searchParams.get('after')), watermark = Number(url.searchParams.get('watermark')) || seq;
                    body = { protocol_version: 1, history_id: history, project_id: portable, position: watermark, watermark, has_more: false, changes: changes.filter(c => c.sequence > after && c.sequence <= watermark) };
                }
                return { status: 200, headers: { 'content-type': 'application/vnd.vgxness.sync+json;version=1' }, body: JSON.stringify(body) };
            } } };
    const first = await dispatchSync(ctx, 'sync', {}, options);
    assert.equal(first.status, 'synced');
    assert.equal(first.pushed, 2);
    assert.equal((db.prepare('SELECT count(*) n FROM sync_outbox').get() as any).n, 0n);
    const next: any = { ...changes[1], sequence: ++seq, canonical_version: 2, mutation: { ...changes[1].mutation, mutation_id: randomUUID(), kind: 'update', base_version: 1, observation: { ...changes[1].mutation.observation, content: 'later remote' } } };
    next.change_hash = canonicalChangeHash(next);
    changes.push(next);
    assert.equal((await dispatchSync(ctx, 'sync', {}, options)).status, 'synced');
    assert.equal((db.prepare("SELECT content FROM observations WHERE id='obs'").get() as any).content, 'later remote');
});
test('bounded rejoin pull retains pulling state until final page and never publishes early', async (t) => {
    const { ctx, db, portable } = fixture(t), history = randomUUID();
    let pushes = 0;
    const changes: any[] = Array.from({ length: 81 }, (_, i) => { const id = i === 0 ? portable : randomUUID(); const mutation: any = { mutation_id: randomUUID(), record_id: id, record_kind: i === 0 ? 'project' : 'session', kind: 'create', base_version: 0, ...(i === 0 ? { project: { id } } : { session: { id, project_id: portable } }) }; const c: any = { sequence: i + 1, canonical_version: 1, mutation }; c.change_hash = canonicalChangeHash(c); return c; });
    const options: any = { credentials: { get: () => `vgx1.${randomUUID()}.${Buffer.alloc(32, 7).toString('base64url')}` }, http: { request: async (request: any) => {
                const url = new URL(request.url);
                let body: any;
                if (url.pathname.endsWith('/capabilities'))
                    body = { protocol_version: 1, capabilities: ['project_state'] };
                else if (url.pathname.endsWith('/discovery'))
                    body = { protocol_version: 1, history_id: history, capabilities: ['bootstrap_discovery'] };
                else if (url.pathname.endsWith('/push')) {
                    pushes++;
                    throw new Error('unexpected publication');
                }
                else {
                    const after = Number(url.searchParams.get('after')), page = changes.slice(after, after + 10), position = Math.min(after + 10, 81);
                    body = { protocol_version: 1, history_id: history, project_id: portable, position, watermark: 81, has_more: position < 81, changes: page };
                }
                return { status: 200, headers: { 'content-type': 'application/vnd.vgxness.sync+json;version=1' }, body: JSON.stringify(body) };
            } } };
    const first: any = await dispatchSync(ctx, 'rejoin', {}, options);
    assert.equal(first.status, 'pulling');
    assert.equal(first.schemaVersion, 1);
    assert.doesNotThrow(() => JSON.stringify(first));
    assert.deepEqual(Object.keys(first).sort(), ['schemaVersion', 'mode', 'status', 'projects', 'sessions', 'observations', 'queued'].sort());
    assert.equal((db.prepare('SELECT status FROM sync_project_transitions').get() as any).status, 'pulling');
    assert.equal((db.prepare('SELECT position FROM sync_project_cursor').get() as any).position, 80n);
    assert.equal(pushes, 0);
    const second = await dispatchSync(ctx, 'rejoin', {}, options);
    assert.equal(second.status, 'completed');
    assert.doesNotThrow(() => JSON.stringify(second));
    assert.equal(second.schemaVersion, 1);
    assert.equal((db.prepare('SELECT status FROM sync_project_transitions').get() as any).status, 'completed');
    assert.equal(pushes, 0);
});
test('expired claims preserve first token and reject former owner; retryable result has no terminal receipt', t => { const { ctx, db } = fixture(t); backfillSyncProject(ctx, 10); const original = claim(ctx, 1)[0]; ctx.now = () => when + 31000000000n; const replacement = claim(ctx, 1)[0]; assert.ok(replacement); assert.notEqual(original.token, replacement.token); const row = db.prepare('SELECT * FROM sync_outbox_claims').get() as any; assert.equal(row.first_claim_token, original.token); assert.equal(row.first_claimed_at, when); assert.throws(() => finish(ctx, original, { mutation_id: original.id, disposition: 'accepted', sequence: 1, version: 1 }), /stale/); finish(ctx, replacement, { mutation_id: replacement.id, disposition: 'rejected', retryable: true, code: 'unavailable' }); assert.equal(db.prepare('SELECT 1 FROM sync_push_results').get(), undefined); assert.equal((db.prepare('SELECT state FROM sync_outbox').get() as any).state, 'retry'); });
test('composite session IDs isolate claims, receipts, backfill and transition cleanup across projects', async (t) => {
    const { ctx, db } = fixture(t), other = { ...ctx, project: 'other' };
    db.prepare('UPDATE projects SET sync_version=1').run();
    db.prepare("INSERT INTO sessions VALUES('same','local',0),('same','other',0)").run();
    const foreign: any = { mutation_id: stableSyncUUID('vgxness/sync-backfill/v1\0session\0same'), record_id: 'same', record_kind: 'session', kind: 'create', base_version: 0, session: { id: 'same', project_id: 'other' } };
    ctx.database.transaction(() => enqueueMutation(other, foreign));
    assert.equal(claim(ctx, 16).length, 0);
    assert.equal(queueSummary(ctx).pending, 0);
    assert.throws(() => translateMutation(ctx, foreign), /crosses project/);
    assert.equal(backfillSyncProject(ctx, 100).sessions, 1);
    assert.equal(backfillSyncProject(ctx, 100).sessions, 0);
    assert.equal(backfillSyncProject(other, 100).sessions, 0);
    assert.equal((db.prepare("SELECT count(*) n FROM sync_outbox WHERE record_kind='session'").get() as any).n, 2n);
    const foreignClaim = claim(other, 1)[0];
    assert.equal(foreignClaim.mutation.session.project_id, 'other');
    assert.throws(() => finish(ctx, foreignClaim, { mutation_id: foreignClaim.id, disposition: 'accepted', version: 1, sequence: 1 }), /crosses project/);
    finish(other, foreignClaim, { mutation_id: foreignClaim.id, disposition: 'accepted', version: 1, sequence: 1 });
    assert.equal((db.prepare("SELECT sync_version FROM sessions WHERE id='same' AND project_id='local'").get() as any).sync_version, 0n);
    assert.equal((db.prepare("SELECT sync_version FROM sessions WHERE id='same' AND project_id='other'").get() as any).sync_version, 1n);
    const second = { ...foreign, mutation_id: randomUUID(), kind: 'update' as const, base_version: 1 };
    ctx.database.transaction(() => enqueueMutation(other, second));
    await reseedProject(ctx);
    assert.ok(db.prepare('SELECT 1 FROM sync_outbox WHERE mutation_id=?').get(second.mutation_id));
    assert.ok(db.prepare('SELECT 1 FROM sync_push_results WHERE mutation_id=?').get(foreign.mutation_id));
    assert.equal((db.prepare("SELECT sync_version FROM sessions WHERE id='same' AND project_id='other'").get() as any).sync_version, 1n);
});
test('failed foreground transition returns a serializable public DTO without private database state', async (t) => { const { ctx } = fixture(t); const result = await dispatchSync(ctx, 'rejoin', {}, { credentials: { get: () => `vgx1.${randomUUID()}.${Buffer.alloc(32, 7).toString('base64url')}` }, http: { request: async () => { throw new Error('offline'); } } }); assert.equal(result.status, 'pulling'); assert.deepEqual(Object.keys(result).sort(), ['schemaVersion', 'mode', 'status', 'projects', 'sessions', 'observations', 'queued'].sort()); assert.doesNotThrow(() => JSON.stringify(result)); });

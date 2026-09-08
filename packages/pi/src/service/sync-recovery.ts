import { createHash, randomBytes, randomUUID } from "node:crypto";
import { existsSync, readFileSync, lstatSync, chmodSync } from "node:fs";
import { dirname, join, basename, isAbsolute, resolve } from "node:path";
import { backup, DatabaseSync } from "node:sqlite";
import { enqueueMutation, observationSnapshot, syncBytes, outboxProjectPredicate, receiptBelongsToProject } from "./sync-state.ts";
import { nowNs } from "../sqlite/codec.ts";
export type RecoveryContext = {
    database: {
        db: {
            prepare(sql: string): {
                all(...p: unknown[]): any[];
                get(...p: unknown[]): any;
                run(...p: unknown[]): any;
            };
        };
        transaction<T>(fn: () => T): T;
    };
    project: string;
    now?: () => bigint;
};
const at = (ctx: RecoveryContext) => (ctx.now?.() ?? nowNs());
const portable = (ctx: RecoveryContext) => (ctx.database.db.prepare("SELECT portable_id FROM portable_project_identities WHERE project_id=? LIMIT 1").get(ctx.project) as any)?.portable_id ?? ctx.project;
export function repairProject(ctx: RecoveryContext, confirmedRemoteAbsent: boolean) {
    if (confirmedRemoteAbsent !== true)
        throw new TypeError("remote absence must be explicitly confirmed");
    return ctx.database.transaction(() => {
        const id = portable(ctx);
        const when = at(ctx);
        const db = ctx.database.db;
        const repair = randomUUID();
        const binding = db.prepare("SELECT project_id FROM portable_project_identities WHERE portable_id=?").get(id) as any;
        if (!binding || binding.project_id !== ctx.project)
            throw new Error("portable project binding is required");
        const existing = db.prepare("SELECT repair_mutation_id,status FROM sync_project_repairs WHERE portable_project_id=? AND local_project_id=?").get(id, ctx.project) as any;
        if (existing)
            return { schema_version: 1, portable_project_id: id, repair_mutation_id: existing.repair_mutation_id, status: existing.status, queued: 0 };
        const version = db.prepare("SELECT sync_version FROM projects WHERE id=?").get(ctx.project) as any;
        if (!version || BigInt(version.sync_version) !== 1n)
            throw new Error("project repair requires canonical version one");
        const original = db.prepare("SELECT mutation_id FROM sync_push_results WHERE disposition='accepted' AND retryable=0 AND record_kind='project' AND record_id=? AND mutation_kind='create' AND base_version=0 AND canonical_version=1").get(ctx.project) as any;
        if (!original?.mutation_id)
            throw new Error("project repair receipt is required");
        if (db.prepare("SELECT 1 FROM sync_project_transitions WHERE local_project_id=? AND status<>'completed'").get(ctx.project) || db.prepare('SELECT 1 FROM sync_project_backup_intents WHERE local_project_id=?').get(ctx.project))
            throw new Error('active transition');
        const originals = db.prepare("SELECT count(*) n FROM sync_push_results WHERE disposition='accepted' AND record_kind='project' AND record_id=? AND mutation_kind='create' AND base_version=0 AND canonical_version=1").get(ctx.project) as any;
        if (BigInt(originals.n) !== 1n)
            throw new Error('project repair requires unique receipt');
        const claimed = db.prepare(`SELECT count(*) AS count FROM sync_outbox_claims c JOIN sync_outbox o ON o.mutation_id=c.mutation_id WHERE c.lease_until>? AND(${outboxProjectPredicate()})`).get(when, ctx.project, ctx.project, ctx.project) as any;
        if (BigInt(claimed?.count ?? 0) !== 0n)
            throw new Error("active sync claim prevents project repair");
        const mutation = { mutation_id: repair, record_id: ctx.project, record_kind: "project", kind: "create", base_version: 0, project: { id: ctx.project } };
        db.prepare("INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,last_error_code,created_at,updated_at) VALUES(?,?,?,'create',0,1,?,'pending',0,?,'',?,?)").run(repair, "project", ctx.project, json(mutation), when, when, when);
        db.prepare("INSERT INTO sync_project_repairs(portable_project_id,local_project_id,original_mutation_id,repair_mutation_id,status,terminal_code,created_at) VALUES(?,?,?,?, 'pending','',?)").run(id, ctx.project, original.mutation_id, repair, when);
        return { schema_version: 1, portable_project_id: id, repair_mutation_id: repair, status: "pending", queued: 1 };
    });
}
function json(value: unknown) { return syncBytes(value); }
async function transition(ctx: RecoveryContext, mode: "reseed_source" | "rejoin_merge") {
    const db = ctx.database.db, id = portable(ctx), when = at(ctx);
    const binding = db.prepare("SELECT project_id FROM portable_project_identities WHERE portable_id=?").get(id) as any;
    if (binding?.project_id !== ctx.project)
        throw new Error("portable project binding is required");
    const active = db.prepare("SELECT * FROM sync_project_transitions WHERE portable_project_id=?").get(id) as any;
    if (active && active.status !== 'completed') {
        if (active.mode !== mode)
            throw new Error("another sync transition is active");
        return { schema_version: 1, mode, status: active.status, transition_identity: active.created_at };
    }
    const databasePath = (ctx.database as any).path;
    if (!databasePath)
        throw new Error("database path is required for transition backup");
    const intent = ctx.database.transaction(() => {
        if (db.prepare("SELECT 1 FROM sync_project_repairs WHERE portable_project_id=? AND status='pending'").get(id))
            throw new Error('project repair pending');
        const previous = db.prepare("SELECT * FROM sync_project_backup_intents WHERE portable_project_id=?").get(id) as any;
        if (previous) {
            if (previous.mode !== mode || previous.local_project_id !== ctx.project)
                throw new Error('backup intent mismatch');
            return previous;
        }
        const path = join(dirname(databasePath), `.sync-project-backup-${randomBytes(16).toString('hex')}.sqlite`), nonce = randomUUID();
        db.prepare("INSERT INTO sync_project_backup_intents VALUES(?,?,?,?,?,?,?)").run(id, ctx.project, mode, nonce, path, null, when);
        return { intent_id: nonce, backup_path: path, backup_sha256: null };
    });
    if (!isAbsolute(intent.backup_path) || dirname(resolve(intent.backup_path)) !== dirname(resolve(databasePath)) || !/^\.sync-project-backup-[0-9a-f]{32}\.sqlite$/.test(basename(intent.backup_path)))
        throw new Error('unsafe backup intent path');
    if (existsSync(intent.backup_path)) {
        const before = lstatSync(intent.backup_path);
        if (!before.isFile() || before.isSymbolicLink())
            throw new Error('unsafe backup path');
    }
    if (!existsSync(intent.backup_path)) {
        await backup(db as any, intent.backup_path);
        chmodSync(intent.backup_path, 0o600);
    }
    const st = lstatSync(intent.backup_path);
    if (!st.isFile() || st.isSymbolicLink())
        throw new Error('unsafe backup path');
    const digest = createHash('sha256').update(readFileSync(intent.backup_path)).digest();
    if (intent.backup_sha256 && !digest.equals(Buffer.from(intent.backup_sha256)))
        throw new Error('backup digest mismatch');
    const snapshot = new DatabaseSync(intent.backup_path, { readOnly: true });
    try {
        if ((snapshot.prepare('PRAGMA integrity_check').get() as any).integrity_check !== 'ok')
            throw new Error('invalid backup');
        if (!snapshot.prepare('SELECT 1 FROM sync_project_backup_intents WHERE intent_id=?').get(intent.intent_id))
            throw new Error('backup intent missing');
    }
    finally {
        snapshot.close();
    }
    return ctx.database.transaction(() => {
        const current = db.prepare("SELECT * FROM sync_project_backup_intents WHERE portable_project_id=?").get(id) as any;
        if (current?.intent_id !== intent.intent_id)
            throw new Error('stale backup intent');
        db.prepare('UPDATE sync_project_backup_intents SET backup_sha256=? WHERE portable_project_id=?').run(digest, id);
        const scope = outboxProjectPredicate();
        if (db.prepare(`SELECT 1 FROM sync_outbox o JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id WHERE c.lease_until>? AND(${scope})`).get(at(ctx), ctx.project, ctx.project, ctx.project))
            throw new Error('active sync claim prevents transition');
        const mutations: any[] = [{ record_kind: 'project', record_id: ctx.project, project: { id: ctx.project } }, ...db.prepare('SELECT id FROM sessions WHERE project_id=? ORDER BY id').all(ctx.project).map((r: any) => ({ record_kind: 'session', record_id: r.id, session: { id: r.id, project_id: ctx.project } })), ...db.prepare("SELECT id FROM observations o WHERE project_id=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id) ORDER BY id").all(ctx.project).map((r: any) => ({ record_kind: 'observation', record_id: r.id, observation: observationSnapshot(ctx as any, r.id) }))];
        db.prepare('DELETE FROM sync_project_transitions WHERE portable_project_id=?').run(id);
        const generation = active && BigInt(active.created_at) >= when ? BigInt(active.created_at) + 1n : when;
        db.prepare('INSERT INTO sync_project_transitions VALUES(?,?,?,?,?,NULL)').run(id, ctx.project, mode, mode === 'rejoin_merge' ? 'pulling' : 'publishing', generation);
        for (const m of mutations) {
            const canonical = { mutation_id: '', record_id: m.record_id, record_kind: m.record_kind, kind: 'create', base_version: 0, ...(m.project ? { project: m.project } : m.session ? { session: m.session } : { observation: m.observation }) };
            const hash = createHash('sha256').update(syncBytes(canonical)).digest();
            db.prepare('INSERT INTO sync_project_transition_records VALUES(?,?,?,?,0)').run(id, m.record_kind, m.record_id, hash);
        }
        db.prepare(`DELETE FROM sync_outbox AS o WHERE ${scope}`).run(ctx.project, ctx.project, ctx.project);
        for (const receipt of db.prepare('SELECT * FROM sync_push_results').all())
            if (receiptBelongsToProject(ctx as any, receipt))
                db.prepare('DELETE FROM sync_push_results WHERE mutation_id=?').run(receipt.mutation_id);
        db.prepare('DELETE FROM sync_project_repairs WHERE portable_project_id=?').run(id);
        db.prepare('DELETE FROM sync_project_inbox WHERE portable_project_id=?').run(id);
        db.prepare('DELETE FROM sync_project_cursor WHERE portable_project_id=?').run(id);
        if (mode === 'reseed_source') {
            for (const [table, column] of [['projects', 'id'], ['sessions', 'project_id'], ['observations', 'project_id']])
                db.prepare(`UPDATE ${table} SET sync_version=0 WHERE ${column}=?`).run(ctx.project);
            for (const m of mutations)
                enqueueMutation(ctx as any, { mutation_id: randomUUID(), record_id: m.record_id, record_kind: m.record_kind, kind: 'create', base_version: 0, ...(m.project ? { project: m.project } : m.session ? { session: m.session } : { observation: m.observation }) });
        }
        db.prepare('DELETE FROM sync_project_backup_intents WHERE portable_project_id=? AND intent_id=?').run(id, intent.intent_id);
        return { schema_version: 1, mode, status: mode === 'rejoin_merge' ? 'pulling' : 'publishing', transition_identity: generation, backup_path: intent.backup_path };
    });
}
export function reseedProject(ctx: RecoveryContext) { return transition(ctx, 'reseed_source'); }
export function rejoinProject(ctx: RecoveryContext) { return transition(ctx, 'rejoin_merge'); }

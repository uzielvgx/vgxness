import { createHash, randomUUID } from "node:crypto";
import { nowNs, formatTimestamp } from "../sqlite/codec.ts";
import { protocolJSON, validateMutation, type Mutation } from "./sync-wire.ts";
export type SyncStateContext = {
    database: {
        db: any;
    };
    project: string;
    now?: () => bigint;
};
export const syncClock = (ctx: SyncStateContext) => ctx.now?.() ?? nowNs();
export const syncBytes = (value: unknown) => Buffer.from(protocolJSON(value));
export function stableSyncUUID(value: string) {
    const b = createHash("sha256").update(value).digest().subarray(0, 16);
    b[6] = (b[6] & 15) | 80;
    b[8] = (b[8] & 63) | 128;
    const h = b.toString("hex");
    return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
export function portableProject(ctx: SyncStateContext): string {
    const p = ctx.database.db.prepare("SELECT portable_id FROM portable_project_identities WHERE project_id=?").get(ctx.project);
    if (!p)
        throw new Error("portable project binding is required");
    return p.portable_id;
}
export function portableIdentity(ctx: SyncStateContext, kind: string, local: string): string {
    const db = ctx.database.db, p = portableProject(ctx);
    if (!['session', 'observation'].includes(kind) || !db.prepare(`SELECT 1 FROM ${kind === 'session' ? 'sessions' : 'observations'} WHERE id=? AND project_id=?`).get(local, ctx.project))
        throw new Error('portable identity crosses project boundary');
    const existing = db.prepare("SELECT portable_id,origin_device_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND local_id=?").get(p, kind, local);
    if (existing) {
        const adoption = db.prepare('SELECT * FROM sync_portable_identity_adoptions WHERE portable_project_id=? AND record_kind=? AND local_id=?').get(p, kind, local);
        const want = stableSyncUUID(`vgxness/sync-portable-identity/v1\0${p}\0${kind}\0${local}`);
        if (adoption ? (adoption.portable_id !== existing.portable_id || adoption.adopting_device_id !== existing.origin_device_id || BigInt(adoption.adopted_at) <= 0n || local !== `sync-adopted:${kind}:${existing.portable_id}`) : existing.portable_id !== want)
            throw new Error('corrupt portable identity');
        return existing.portable_id;
    }
    const device = db.prepare("SELECT device_id FROM sync_profiles WHERE singleton=1").get();
    if (!device)
        throw new Error("sync device profile required");
    const id = stableSyncUUID(`vgxness/sync-portable-identity/v1\0${p}\0${kind}\0${local}`);
    db.prepare("INSERT INTO sync_portable_identities VALUES(?,?,?,?,?,?)").run(p, kind, local, id, device.device_id, syncClock(ctx));
    return id;
}
export function observationSnapshot(ctx: SyncStateContext, id: string): any {
    const db = ctx.database.db, o = db.prepare("SELECT * FROM observations WHERE id=? AND project_id=?").get(id, ctx.project);
    if (!o)
        throw new Error("observation unavailable");
    const refs = db.prepare("SELECT target_id FROM observation_refs WHERE observation_id=? ORDER BY target_id").all(id).map((r: any) => r.target_id);
    return { id: o.id, title: o.title, project_id: o.project_id, ...(o.session_id ? { session_id: o.session_id } : {}), scope: o.scope, type: o.type, content: o.content, ...(o.topic_key ? { topic_key: o.topic_key } : {}), ...(refs.length ? { references: refs } : {}), provenance: { producer: o.producer, ...(o.source_provider ? { source_provider: o.source_provider } : {}), ...(o.source_id ? { source_id: o.source_id } : {}) }, lifecycle: o.state === 'archived' ? 'archived' : 'active', review: o.state === 'needs_review' ? 'needs_review' : 'clear', created_at: formatTimestamp(o.created_at), updated_at: formatTimestamp(o.updated_at), ...(o.review_after ? { review_after: formatTimestamp(o.review_after) } : {}) };
}
export function enqueueMutation(ctx: SyncStateContext, mutation: Mutation) {
    const m = validateMutation(mutation), db = ctx.database.db, at = syncClock(ctx), payload = syncBytes(m);
    assertMutationProject(ctx, m);
    if (db.prepare("SELECT 1 FROM sync_push_results WHERE mutation_id=?").get(m.mutation_id))
        throw new Error("completed mutation identity");
    db.prepare("INSERT INTO sync_outbox(mutation_id,record_kind,record_id,mutation_kind,base_version,payload_version,payload,state,attempts,next_attempt_at,last_error_code,created_at,updated_at) VALUES(?,?,?,?,?,1,?,'pending',0,?,'',?,?)").run(m.mutation_id, m.record_kind, m.record_id, m.kind, m.base_version, payload, at, at, at);
    return m.mutation_id;
}
export function enqueueObservationChange(ctx: SyncStateContext, id: string): string | undefined {
    const db = ctx.database.db;
    if (db.prepare("SELECT 1 FROM sync_tombstones WHERE record_kind='observation' AND record_id=?").get(id))
        throw new Error('observation is tombstoned');
    if (db.prepare("SELECT 1 FROM sync_project_transitions WHERE local_project_id=? AND status<>'completed'").get(ctx.project) || db.prepare('SELECT 1 FROM sync_project_backup_intents WHERE local_project_id=?').get(ctx.project))
        throw new Error('active sync transition');
    const profile = db.prepare('SELECT enabled FROM sync_profiles WHERE singleton=1').get();
    if (!profile)
        return;
    if (![0n, 1n, 0, 1].includes(profile.enabled))
        throw new Error('corrupt sync profile');
    const o = db.prepare("SELECT sync_version,state FROM observations WHERE id=? AND project_id=?").get(id, ctx.project);
    if (!o)
        return;
    if (db.prepare("SELECT 1 FROM sync_project_transitions WHERE local_project_id=? AND status<>'completed'").get(ctx.project))
        throw new Error("active sync transition");
    const snap = observationSnapshot(ctx, id);
    for (const [kind, record, version] of [['project', ctx.project, db.prepare('SELECT sync_version FROM projects WHERE id=?').get(ctx.project)?.sync_version], ...(snap.session_id ? [['session', snap.session_id, db.prepare('SELECT sync_version FROM sessions WHERE id=? AND project_id=?').get(snap.session_id, ctx.project)?.sync_version]] : [])]) {
        if (BigInt(version as any) === 0n && !db.prepare("SELECT 1 FROM sync_outbox WHERE record_kind=? AND record_id=? AND mutation_kind='create' AND (record_kind<>'session' OR json_extract(CAST(payload AS TEXT),'$.session.project_id')=?)").get(kind, record, ctx.project))
            enqueueMutation(ctx, { mutation_id: randomUUID(), record_id: record as string, record_kind: kind as any, kind: 'create', base_version: 0, ...(kind === 'project' ? { project: { id: record } } : { session: { id: record, project_id: ctx.project } }) });
    }
    const base = BigInt(o.sync_version);
    return enqueueMutation(ctx, { mutation_id: randomUUID(), record_id: id, record_kind: 'observation', kind: base === 0n ? 'create' : o.state === 'archived' ? 'archive' : 'update', base_version: base, observation: observationSnapshot(ctx, id) });
}
export function translateMutation(ctx: SyncStateContext, input: Mutation): Mutation {
    assertMutationProject(ctx, input);
    const m = structuredClone(input), p = portableProject(ctx);
    m.record_id = m.record_kind === 'project' ? p : portableIdentity(ctx, m.record_kind, m.record_id);
    if (m.project)
        m.project.id = p;
    if (m.session) {
        m.session.id = m.record_id;
        m.session.project_id = p;
    }
    for (const o of [m.observation, m.resolution?.observation])
        if (o) {
            o.id = m.record_id;
            o.project_id = p;
            if (o.session_id)
                o.session_id = portableIdentity(ctx, 'session', o.session_id);
            if (o.references)
                o.references = o.references.map((id: string) => portableIdentity(ctx, 'observation', id));
        }
    if (m.tombstone)
        m.tombstone.project_id = p;
    return validateMutation(m);
}
export function queueSummary(ctx: SyncStateContext) {
    const db = ctx.database.db;
    const row = db.prepare(`SELECT COUNT(*) AS pending FROM sync_outbox o WHERE ${outboxProjectPredicate()}`).get(ctx.project, ctx.project, ctx.project);
    const conflict = db.prepare("SELECT COUNT(*) AS n FROM sync_conflicts WHERE status='unresolved' AND record_id IN(SELECT id FROM observations WHERE project_id=?)").get(ctx.project);
    return { pending: Number(row.pending), conflicts: Number(conflict.n) };
}
/** Session IDs are composite (project_id,id). The payload owns an outbox row. */
export function outboxProjectPredicate(alias = 'o'): string {
    return `(${alias}.record_kind='project' AND ${alias}.record_id=?) OR (${alias}.record_kind='session' AND json_extract(CAST(${alias}.payload AS TEXT),'$.session.project_id')=? AND json_extract(CAST(${alias}.payload AS TEXT),'$.session.id')=${alias}.record_id) OR (${alias}.record_kind='observation' AND ${alias}.record_id IN(SELECT id FROM observations WHERE project_id=?))`;
}
export function assertMutationProject(ctx: SyncStateContext, m: Mutation) {
    const owner = m.project?.id ?? m.session?.project_id ?? m.observation?.project_id ?? m.resolution?.observation?.project_id ?? m.tombstone?.project_id ?? (m.tombstone ? ctx.database.db.prepare('SELECT project_id FROM observations WHERE id=?').get(m.record_id)?.project_id : undefined);
    if (owner !== ctx.project)
        throw new Error('sync mutation crosses project boundary');
}
export function receiptBelongsToProject(ctx: SyncStateContext, r: any): boolean {
    if (r.record_kind === 'project')
        return r.record_id === ctx.project;
    if (r.record_kind === 'observation')
        return !!ctx.database.db.prepare('SELECT 1 FROM observations WHERE id=? AND project_id=?').get(r.record_id, ctx.project);
    if (r.record_kind !== 'session')
        return false;
    const m: Mutation = { mutation_id: r.mutation_id, record_id: r.record_id, record_kind: 'session', kind: r.mutation_kind, base_version: r.base_version, session: { id: r.record_id, project_id: ctx.project } };
    return createHash('sha256').update(syncBytes(validateMutation(m))).digest().equals(Buffer.from(r.mutation_hash));
}

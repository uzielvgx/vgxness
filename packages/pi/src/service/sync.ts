import { createHash, randomUUID } from "node:crypto";
import { validBearer, type CredentialPort } from "../ports/credentials.ts";
import type { HttpPort } from "../ports/http.ts";
import { SyncHttpError } from "../ports/http.ts";
import { nowNs, parseTimestamp } from "../sqlite/codec.ts";
import { SyncWire, normalizeSyncEndpoint, protocolJSON, parseProtocolJSON, validateMutation, type Mutation, type PullChange } from "./sync-wire.ts";
import { queueSummary, outboxProjectPredicate, assertMutationProject, stableSyncUUID, syncBytes, observationSnapshot, enqueueMutation, translateMutation } from "./sync-state.ts";
import { repairProject, reseedProject, rejoinProject } from "./sync-recovery.ts";
export type ServiceContext = {
    database: {
        readOnly?: boolean;
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
    workspace: string;
    storageRoot: string;
    mode: "read-only" | "full";
    role: string;
    now?: () => bigint;
    sessionSecrets?: CredentialPort;
    http?: HttpPort;
};
export type SyncOptions = {
    http?: HttpPort;
    credentials?: CredentialPort;
    credentialRef?: string;
    signal?: AbortSignal;
};
const timestamp = (ctx: ServiceContext) => (ctx.now?.() ?? nowNs());
const json = (value: unknown) => syncBytes(value);
const decode = (value: unknown) => parseProtocolJSON(new TextDecoder().decode(value as Uint8Array)) as any;
const uuid = (x: unknown): x is string => typeof x === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(x);
function assertWritable(ctx: ServiceContext) {
    if (ctx.mode !== "full" || ctx.database.readOnly)
        throw new Error("sync requires full writable memory context");
}
function profile(ctx: ServiceContext) { return ctx.database.db.prepare("SELECT enabled,endpoint,device_id,credential_ref,previous_credential_ref FROM sync_profiles WHERE singleton=1").get() as any; }
function portableProject(ctx: ServiceContext) {
    const id = (ctx.database.db.prepare("SELECT portable_id FROM portable_project_identities WHERE project_id=? LIMIT 1").get(ctx.project) as any)?.portable_id;
    if (!id)
        throw new Error("portable project binding required");
    return id;
}
function statusFor(error: unknown) {
    if (error instanceof SyncHttpError)
        return error.kind === "unauthorized" ? "unauthorized" : error.kind === "unavailable" ? "unreachable" : error.kind === "remote" || error.kind === "invalid" ? "incompatible" : "unavailable";
    return "unavailable";
}
function wire(ctx: ServiceContext, options: SyncOptions) {
    const p = profile(ctx);
    if (!p?.enabled)
        return undefined;
    const credentials = options.credentials ?? ctx.sessionSecrets;
    const token = credentials?.get(p.credential_ref);
    if (!token)
        throw new Error("sync credential unavailable");
    const http = options.http ?? ctx.http;
    if (!http)
        throw new Error("sync HTTP port unavailable");
    return new SyncWire(http, p.endpoint, token, options.signal);
}
export function configureSync(ctx: ServiceContext, payload: {
    endpoint: string;
    deviceId: string;
    credentialRef?: string;
}, options: SyncOptions = {}) {
    assertWritable(ctx);
    if (!uuid(payload.deviceId))
        throw new TypeError("deviceId must be a canonical UUID");
    const reference = payload.credentialRef ?? options.credentialRef;
    if (typeof reference !== 'string' || reference.length > 512 || !/^[!-~]+$/.test(reference) || /[%?#@]/.test(reference) || reference.toLowerCase().includes('bearer') || !/^secret:\/\/[a-z0-9][a-z0-9.-]{0,62}\/[A-Za-z0-9][A-Za-z0-9._-]{0,127}(?:\/[A-Za-z0-9][A-Za-z0-9._-]{0,127}){0,7}$/.test(reference))
        throw new TypeError('invalid credential reference');
    const endpoint = normalizeSyncEndpoint(payload.endpoint);
    const at = timestamp(ctx);
    ctx.database.transaction(() => ctx.database.db.prepare("INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,?,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET enabled=1,endpoint=excluded.endpoint,device_id=excluded.device_id,previous_credential_ref=CASE WHEN sync_profiles.credential_ref<>excluded.credential_ref THEN sync_profiles.credential_ref ELSE sync_profiles.previous_credential_ref END,credential_ref=excluded.credential_ref,updated_at=excluded.updated_at").run(endpoint, payload.deviceId, reference, at, at));
    return syncStatus(ctx, options);
}
export function syncStatus(ctx: ServiceContext, options: SyncOptions = {}) {
    const p = profile(ctx);
    if (!p)
        return { configured: false, enabled: false, credential: 'not_configured' };
    const port = options.credentials ?? ctx.sessionSecrets;
    let credential = 'unavailable';
    if (port) {
        try {
            const token = port.get(p.credential_ref);
            credential = token === undefined ? 'missing' : validBearer(token) ? 'available' : 'invalid';
        }
        catch {
            credential = 'unavailable';
        }
    }
    return { configured: true, enabled: !!p.enabled, credential };
}
export function backfillSyncProject(ctx: ServiceContext, limit: number) {
    assertWritable(ctx);
    if (!Number.isInteger(limit) || limit < 1 || limit > 1000)
        throw new TypeError("invalid backfill limit");
    return ctx.database.transaction(() => {
        const db = ctx.database.db;
        if (db.prepare("SELECT 1 FROM sync_project_transitions WHERE local_project_id=? AND status<>'completed'").get(ctx.project))
            throw new Error("active sync transition");
        const result = { schemaVersion: 1, limit, queued: 0, projects: 0, sessions: 0, observations: 0, remaining: false };
        const rows = [...db.prepare("SELECT 'project' kind,id,sync_version FROM projects WHERE id=?").all(ctx.project), ...db.prepare("SELECT 'session' kind,id,sync_version FROM sessions WHERE project_id=? ORDER BY id").all(ctx.project), ...db.prepare("SELECT 'observation' kind,id,sync_version FROM observations o WHERE project_id=? AND NOT EXISTS(SELECT 1 FROM sync_tombstones t WHERE t.record_kind='observation' AND t.record_id=o.id) ORDER BY id").all(ctx.project)];
        for (const row of rows) {
            if (result.queued >= limit) {
                result.remaining = true;
                break;
            }
            if (BigInt(row.sync_version) !== 0n)
                continue;
            const old = db.prepare("SELECT * FROM sync_outbox WHERE record_kind=? AND record_id=? AND mutation_kind='create' AND base_version=0 AND (record_kind<>'session' OR json_extract(CAST(payload AS TEXT),'$.session.project_id')=?) ORDER BY id LIMIT 1").get(row.kind, row.id, ctx.project) as any;
            let backfillID = stableSyncUUID(`vgxness/sync-backfill/v1\0${row.kind}\0${row.id}`);
            if (!old && row.kind === 'session' && (db.prepare('SELECT 1 FROM sync_outbox WHERE mutation_id=?').get(backfillID) || db.prepare('SELECT 1 FROM sync_push_results WHERE mutation_id=?').get(backfillID)))
                backfillID = stableSyncUUID(`vgxness/sync-backfill/v1\0session\0${ctx.project}\0${row.id}`);
            const mutation: Mutation = { mutation_id: old?.mutation_id ?? backfillID, record_id: row.id, record_kind: row.kind, kind: 'create', base_version: 0 };
            if (row.kind === 'project')
                mutation.project = { id: row.id };
            else if (row.kind === 'session')
                mutation.session = { id: row.id, project_id: ctx.project };
            else
                mutation.observation = observationSnapshot(ctx, row.id);
            if (old) {
                if (Buffer.from(old.payload).equals(json(validateMutation(mutation))))
                    continue;
                if (old.state !== 'pending' || BigInt(old.attempts) !== 0n || db.prepare("SELECT 1 FROM sync_outbox_claims WHERE mutation_id=?").get(old.mutation_id))
                    throw new Error("backfill already attempted");
                db.prepare("UPDATE sync_outbox SET payload=? WHERE mutation_id=?").run(json(validateMutation(mutation)), old.mutation_id);
                continue;
            }
            enqueueMutation(ctx, mutation);
            result.queued++;
            if (row.kind === 'project')
                result.projects++;
            else if (row.kind === 'session')
                result.sessions++;
            else
                result.observations++;
        }
        return result;
    });
}
export function claim(ctx: ServiceContext, limit: number) {
    const db = ctx.database.db, at = timestamp(ctx);
    if (db.prepare('SELECT 1 FROM sync_project_backup_intents WHERE local_project_id=?').get(ctx.project))
        throw new Error('backup transition pending');
    return ctx.database.transaction(() => db.prepare(`SELECT o.* FROM sync_outbox o LEFT JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id
 WHERE o.next_attempt_at<=? AND(c.mutation_id IS NULL OR c.lease_until<=?)
 AND NOT EXISTS(SELECT 1 FROM sync_outbox p WHERE p.record_kind=o.record_kind AND p.record_id=o.record_id AND (o.record_kind<>'session' OR json_extract(CAST(p.payload AS TEXT),'$.session.project_id')=json_extract(CAST(o.payload AS TEXT),'$.session.project_id')) AND p.id<o.id)
 AND NOT EXISTS(SELECT 1 FROM sync_conflicts f WHERE f.status='unresolved' AND f.record_kind=o.record_kind AND f.record_id=o.record_id)
 AND (EXISTS(SELECT 1 FROM sync_project_repairs r WHERE r.status='pending' AND r.repair_mutation_id=o.mutation_id) OR o.base_version=CASE o.record_kind WHEN 'project' THEN(SELECT sync_version FROM projects WHERE id=o.record_id) WHEN 'session' THEN(SELECT sync_version FROM sessions WHERE id=o.record_id AND project_id=json_extract(CAST(o.payload AS TEXT),'$.session.project_id')) ELSE(SELECT sync_version FROM observations WHERE id=o.record_id) END)
 AND (NOT EXISTS(SELECT 1 FROM sync_project_repairs r WHERE r.status='pending' AND r.local_project_id=?) OR EXISTS(SELECT 1 FROM sync_project_repairs r WHERE r.status='pending' AND r.repair_mutation_id=o.mutation_id))
 AND (o.record_kind='project' OR EXISTS(SELECT 1 FROM projects WHERE id=? AND sync_version>0))
 AND (o.record_kind<>'observation' OR NOT EXISTS(SELECT 1 FROM observations n JOIN sessions s ON s.id=n.session_id AND s.project_id=n.project_id WHERE n.id=o.record_id AND s.sync_version=0))
 AND (o.record_kind<>'observation' OR NOT EXISTS(SELECT 1 FROM observation_refs r JOIN observations t ON t.id=r.target_id WHERE r.observation_id=o.record_id AND t.sync_version=0))
 AND (${outboxProjectPredicate()})
 ORDER BY o.created_at,o.id LIMIT ?`).all(at, at, ctx.project, ctx.project, ctx.project, ctx.project, ctx.project, limit).map((row: any) => {
        const mutation = validateMutation(decode(row.payload));
        assertMutationProject(ctx, mutation);
        if (mutation.mutation_id !== row.mutation_id || mutation.record_id !== row.record_id || mutation.record_kind !== row.record_kind || mutation.kind !== row.mutation_kind || BigInt(mutation.base_version) !== BigInt(row.base_version))
            throw new Error("corrupt sync outbox");
        const token = randomUUID();
        db.prepare("INSERT INTO sync_outbox_claims VALUES(?,?,?,?,?,?) ON CONFLICT(mutation_id) DO UPDATE SET claim_token=excluded.claim_token,claimed_at=excluded.claimed_at,lease_until=excluded.lease_until WHERE sync_outbox_claims.lease_until<=?").run(row.mutation_id, token, token, at, at, at + 30000000000n, at);
        return { id: row.mutation_id, token, mutation };
    }));
}
export function finish(ctx: ServiceContext, claim: {
    id: string;
    token: string;
}, result: any) {
    const db = ctx.database.db, at = timestamp(ctx);
    ctx.database.transaction(() => {
        const row = db.prepare("SELECT o.* FROM sync_outbox o JOIN sync_outbox_claims c ON c.mutation_id=o.mutation_id WHERE o.mutation_id=? AND c.claim_token=? AND c.lease_until>?").get(claim.id, claim.token, at) as any;
        if (!row)
            throw new Error("stale sync claim");
        if (result.mutation_id !== claim.id)
            throw new Error("mismatched sync result");
        const mutation = validateMutation(decode(row.payload));
        assertMutationProject(ctx, mutation);
        if (result.retryable) {
            db.prepare("UPDATE sync_outbox SET state='retry',attempts=attempts+1,next_attempt_at=?,last_error_code=?,updated_at=? WHERE mutation_id=?").run(at + 1000000000n, result.code, at, claim.id);
            db.prepare("UPDATE sync_outbox_claims SET lease_until=? WHERE mutation_id=?").run(at, claim.id);
            return;
        }
        const hash = createHash('sha256').update(json(mutation)).digest();
        db.prepare("INSERT INTO sync_push_results VALUES(?,?,0,?,?,?,?,?,?,?,?,?)").run(claim.id, result.disposition, result.code ?? '', result.sequence ?? null, result.version ?? 0, row.record_kind, row.record_id, row.mutation_kind, row.base_version, hash, at);
        if (['accepted', 'previously_accepted'].includes(result.disposition)) {
            const table = { project: 'projects', session: 'sessions', observation: 'observations' }[row.record_kind as string];
            if (!table)
                throw new Error('invalid record kind');
            if (row.record_kind === 'session')
                db.prepare('UPDATE sessions SET sync_version=? WHERE id=? AND project_id=? AND sync_version=?').run(result.version, row.record_id, ctx.project, row.base_version);
            else
                db.prepare(`UPDATE ${table} SET sync_version=? WHERE id=? AND sync_version=?`).run(result.version, row.record_id, row.base_version);
        }
        if (['accepted', 'previously_accepted'].includes(result.disposition)) {
            for (const later of db.prepare("SELECT * FROM sync_outbox WHERE record_kind=? AND record_id=? AND id>? AND(record_kind<>'session' OR json_extract(CAST(payload AS TEXT),'$.session.project_id')=?) ORDER BY id").all(row.record_kind, row.record_id, row.id, ctx.project)) {
                if (db.prepare('SELECT 1 FROM sync_outbox_claims WHERE mutation_id=?').get(later.mutation_id))
                    throw new Error('cannot rebase attempted mutation');
                const m = validateMutation(decode(later.payload));
                m.base_version = result.version;
                if (m.kind === 'create')
                    m.kind = m.observation?.lifecycle === 'archived' ? 'archive' : 'update';
                db.prepare('UPDATE sync_outbox SET base_version=?,mutation_kind=?,payload=? WHERE mutation_id=?').run(m.base_version, m.kind, json(validateMutation(m)), later.mutation_id);
            }
        }
        db.prepare("DELETE FROM sync_outbox WHERE mutation_id=?").run(claim.id);
        db.prepare("UPDATE sync_project_repairs SET status=?,terminal_code=?,completed_at=? WHERE repair_mutation_id=? AND status='pending'").run(['accepted', 'previously_accepted'].includes(result.disposition) ? 'completed' : 'rejected', result.code ?? '', at, claim.id);
    });
}
function retry(ctx: ServiceContext, claims: {
    id: string;
    token: string;
}[]) {
    const db = ctx.database.db, at = timestamp(ctx);
    ctx.database.transaction(() => {
        for (const c of claims) {
            db.prepare("UPDATE sync_outbox SET state='retry',attempts=attempts+1,next_attempt_at=?,last_error_code='transport',updated_at=? WHERE mutation_id=? AND EXISTS(SELECT 1 FROM sync_outbox_claims WHERE mutation_id=? AND claim_token=?)").run(at + 1000000000n, at, c.id, c.id, c.token);
            db.prepare("UPDATE sync_outbox_claims SET lease_until=? WHERE mutation_id=? AND claim_token=?").run(at, c.id, c.token);
        }
    });
}
function localPortableID(ctx: ServiceContext, project: string, kind: "session" | "observation", remoteID: string, at: bigint) {
    const db = ctx.database.db;
    const row = db.prepare("SELECT local_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind=? AND portable_id=?").get(project, kind, remoteID) as any;
    if (row?.local_id) {
        return row.local_id as string;
    }
    const local = `sync-adopted:${kind}:${remoteID}`;
    if (kind === 'session' ? db.prepare('SELECT 1 FROM sessions WHERE id=? AND project_id=?').get(local, ctx.project) : db.prepare('SELECT 1 FROM observations WHERE id=?').get(local))
        throw new Error('portable identity local collision');
    const device = db.prepare("SELECT device_id FROM sync_profiles WHERE singleton=1").get() as any;
    if (!device?.device_id)
        throw new Error("sync identity requires device profile");
    db.prepare("INSERT INTO sync_portable_identities(portable_project_id,record_kind,local_id,portable_id,origin_device_id,created_at) VALUES(?,?,?,?,?,?)").run(project, kind, local, remoteID, device.device_id, at);
    db.prepare("INSERT INTO sync_portable_identity_adoptions(portable_project_id,record_kind,local_id,portable_id,adopting_device_id,adopted_at) VALUES(?,?,?,?,?,?)").run(project, kind, local, remoteID, device.device_id, at);
    return local;
}
export function applyChange(ctx: ServiceContext, project: string, history: string, change: PullChange) {
    const db = ctx.database.db, at = timestamp(ctx), hash = Buffer.from(change.change_hash, 'hex');
    const seen = db.prepare('SELECT change_hash FROM sync_project_inbox WHERE portable_project_id=? AND history_id=? AND seq=?').get(project, history, change.sequence) as any;
    if (seen) {
        if (!Buffer.from(seen.change_hash).equals(hash))
            throw new Error('conflicting pulled history receipt');
        return;
    }
    const m = structuredClone(change.mutation);
    const remoteProject = m.project?.id ?? m.session?.project_id ?? m.observation?.project_id ?? m.resolution?.observation?.project_id ?? m.tombstone?.project_id;
    if (remoteProject !== project)
        throw new Error('pulled project boundary');
    const local = m.record_kind === 'project' ? ctx.project : localPortableID(ctx, project, m.record_kind, m.record_id, at);
    m.record_id = local;
    if (m.project)
        m.project.id = ctx.project;
    if (m.session) {
        m.session.id = local;
        m.session.project_id = ctx.project;
    }
    const o = m.observation ?? m.resolution?.observation;
    if (o) {
        o.id = local;
        o.project_id = ctx.project;
        if (o.session_id) {
            const r = db.prepare("SELECT local_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind='session' AND portable_id=?").get(project, o.session_id) as any;
            if (!r || !db.prepare('SELECT 1 FROM sessions WHERE id=? AND project_id=?').get(r.local_id, ctx.project))
                throw new Error('session prerequisite unavailable');
            o.session_id = r.local_id;
        }
        if (o.references)
            o.references = o.references.map((id: string) => {
                const r = db.prepare("SELECT local_id FROM sync_portable_identities WHERE portable_project_id=? AND record_kind='observation' AND portable_id=?").get(project, id) as any;
                if (!r || !db.prepare('SELECT 1 FROM observations WHERE id=?').get(r.local_id))
                    throw new Error('reference target unavailable');
                return r.local_id;
            });
    }
    if (m.tombstone)
        m.tombstone.project_id = ctx.project;
    const table = { project: 'projects', session: 'sessions', observation: 'observations' }[m.record_kind];
    const existing = (m.record_kind === 'session' ? db.prepare('SELECT sync_version FROM sessions WHERE id=? AND project_id=?').get(local, ctx.project) : db.prepare(`SELECT sync_version FROM ${table} WHERE id=?`).get(local)) as any;
    const transition = db.prepare("SELECT mode,status FROM sync_project_transitions WHERE portable_project_id=? AND status<>'completed'").get(project) as any;
    if (transition) {
        const record = db.prepare('SELECT payload_hash FROM sync_project_transition_records WHERE portable_project_id=? AND record_kind=? AND local_id=?').get(project, m.record_kind, local) as any;
        if (record) {
            const snapshot = { ...validateMutation(m), mutation_id: '' };
            const hash = createHash('sha256').update(json(snapshot)).digest();
            if (m.kind !== 'create' || BigInt(m.base_version) !== 0n || BigInt(change.canonical_version) !== 1n || !hash.equals(Buffer.from(record.payload_hash)))
                throw new Error('project sync transition diverged');
            if (m.record_kind === 'session')
                db.prepare('UPDATE sessions SET sync_version=1 WHERE id=? AND project_id=?').run(local, ctx.project);
            else
                db.prepare(`UPDATE ${table} SET sync_version=1 WHERE id=?`).run(local);
            db.prepare('UPDATE sync_project_transition_records SET seen_remote=1 WHERE portable_project_id=? AND record_kind=? AND local_id=?').run(project, m.record_kind, local);
            db.prepare('INSERT INTO sync_project_inbox VALUES(?,?,?,?,?)').run(project, history, change.sequence, Buffer.from(change.change_hash, 'hex'), at);
            return;
        }
    }
    const own = db.prepare('SELECT * FROM sync_push_results WHERE mutation_id=?').get(m.mutation_id) as any;
    if (own) {
        if (BigInt(own.sequence ?? 0) !== BigInt(change.sequence) || BigInt(own.canonical_version) !== BigInt(change.canonical_version) || own.record_id !== local || !Buffer.from(own.mutation_hash).equals(createHash('sha256').update(json(validateMutation(m))).digest()))
            throw new Error('sync pull receipt mismatch');
        if (['accepted', 'previously_accepted'].includes(own.disposition) && change.change_disposition !== 'conflict') {
            db.prepare('INSERT INTO sync_project_inbox VALUES(?,?,?,?,?)').run(project, history, change.sequence, hash, at);
            return;
        }
    }
    if (existing && BigInt(existing.sync_version) >= BigInt(change.canonical_version) && change.change_disposition !== 'conflict')
        throw new Error('pulled canonical version replay without receipt');
    if (change.change_disposition === 'conflict') {
        if (existing && BigInt(existing.sync_version) !== BigInt(change.canonical_version))
            throw new Error('conflict version mismatch');
        db.prepare("UPDATE observations SET state='needs_review' WHERE id=? AND state='active'").run(local);
        db.prepare("INSERT INTO sync_conflicts(conflict_id,history_id,created_seq,record_kind,record_id,canonical_version,competing_version_id,status,payload_version,snapshot,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'unresolved',1,?,?,?)").run(change.conflict_id, history, change.sequence, m.record_kind, local, change.canonical_version, m.mutation_id, json(validateMutation(m)), at, at);
    }
    else if (!existing || BigInt(existing.sync_version) < BigInt(change.canonical_version)) {
        if (existing && BigInt(existing.sync_version) !== BigInt(m.base_version))
            throw new Error('pulled version mismatch');
        if (m.tombstone) {
            if (!existing)
                throw new Error('tombstone record missing');
            db.prepare('INSERT INTO sync_tombstones VALUES(?,?,?,?,?,1,?,?)').run(history, change.sequence, m.record_kind, local, change.canonical_version, json({ change_hash: change.change_hash, mutation_id: m.mutation_id, base_version: m.base_version, deleted_at: m.tombstone.deleted_at }), parseTimestamp(m.tombstone.deleted_at));
            db.prepare('UPDATE observations SET topic_key=NULL,sync_version=? WHERE id=?').run(change.canonical_version, local);
            db.prepare('DELETE FROM observations_fts WHERE id=?').run(local);
            db.prepare('DELETE FROM observation_refs WHERE observation_id=?').run(local);
        }
        else if (m.project)
            db.prepare('UPDATE projects SET sync_version=? WHERE id=?').run(change.canonical_version, local);
        else if (m.session)
            db.prepare('INSERT INTO sessions(id,project_id,sync_version) VALUES(?,?,?) ON CONFLICT(id,project_id) DO UPDATE SET sync_version=excluded.sync_version').run(local, ctx.project, change.canonical_version);
        else if (o) {
            db.prepare(`INSERT INTO observations(id,title,project_id,session_id,scope,type,content,topic_key,producer,source_provider,source_id,state,created_at,updated_at,review_after,sync_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,session_id=excluded.session_id,scope=excluded.scope,type=excluded.type,content=excluded.content,topic_key=excluded.topic_key,producer=excluded.producer,source_provider=excluded.source_provider,source_id=excluded.source_id,state=excluded.state,created_at=excluded.created_at,updated_at=excluded.updated_at,review_after=excluded.review_after,sync_version=excluded.sync_version`).run(local, o.title ?? '', ctx.project, o.session_id ?? null, o.scope, o.type, o.content, o.topic_key ?? null, o.provenance.producer, o.provenance.source_provider ?? '', o.provenance.source_id ?? '', o.lifecycle === 'archived' ? 'archived' : o.review === 'needs_review' ? 'needs_review' : 'active', parseTimestamp(o.created_at), parseTimestamp(o.updated_at), o.review_after ? parseTimestamp(o.review_after) : null, change.canonical_version);
            db.prepare('DELETE FROM observation_refs WHERE observation_id=?').run(local);
            for (const target of o.references ?? [])
                db.prepare('INSERT INTO observation_refs VALUES(?,?)').run(local, target);
            db.prepare('DELETE FROM observations_fts WHERE id=?').run(local);
            if (o.lifecycle !== 'archived')
                db.prepare('INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)').run(local, o.title ?? '', o.topic_key ?? '', o.type, o.content);
            for (const id of m.resolution?.conflict_ids ?? [])
                db.prepare("UPDATE sync_conflicts SET status='resolved',resolved_seq=?,updated_at=? WHERE conflict_id=? AND record_id=? AND status='unresolved'").run(change.sequence, at, id, local);
        }
    }
    db.prepare('INSERT INTO sync_project_inbox VALUES(?,?,?,?,?)').run(project, history, change.sequence, hash, at);
}
function publishUnseen(ctx: ServiceContext, p: string) {
    const db = ctx.database.db;
    if (!(db.prepare("SELECT seen_remote FROM sync_project_transition_records WHERE portable_project_id=? AND record_kind='project' AND local_id=?").get(p, ctx.project) as any)?.seen_remote)
        throw new Error('remote project absent');
    for (const row of db.prepare("SELECT * FROM sync_project_transition_records WHERE portable_project_id=? AND seen_remote=0 ORDER BY CASE record_kind WHEN 'session' THEN 1 ELSE 2 END,local_id").all(p)) {
        const m: any = { mutation_id: '', record_id: row.local_id, record_kind: row.record_kind, kind: 'create', base_version: 0 };
        if (row.record_kind === 'session')
            m.session = { id: row.local_id, project_id: ctx.project };
        else if (row.record_kind === 'observation')
            m.observation = observationSnapshot(ctx, row.local_id);
        else
            throw new Error('invalid unseen transition record');
        if (!createHash('sha256').update(json(m)).digest().equals(Buffer.from(row.payload_hash)))
            throw new Error('local transition snapshot changed');
        if (row.record_kind === 'session')
            db.prepare('UPDATE sessions SET sync_version=0 WHERE id=? AND project_id=?').run(row.local_id, ctx.project);
        else
            db.prepare('UPDATE observations SET sync_version=0 WHERE id=?').run(row.local_id);
        m.mutation_id = randomUUID();
        enqueueMutation(ctx, m);
    }
    db.prepare("UPDATE sync_project_transitions SET status='publishing' WHERE portable_project_id=? AND status='pulling'").run(p);
}
function recoverRejected(ctx: ServiceContext, p: string) {
    const db = ctx.database.db;
    if (!db.prepare("SELECT 1 FROM sync_project_transitions WHERE portable_project_id=? AND mode='rejoin_merge' AND status='publishing'").get(p))
        return;
    for (const row of db.prepare("SELECT local_id,payload_hash FROM sync_project_transition_records r WHERE portable_project_id=? AND record_kind='observation' AND seen_remote=0 AND NOT EXISTS(SELECT 1 FROM sync_outbox o WHERE o.record_kind='observation' AND o.record_id=r.local_id)").all(p)) {
        const receipts = db.prepare("SELECT * FROM sync_push_results WHERE record_kind='observation' AND record_id=? AND mutation_kind='create'").all(row.local_id);
        if (receipts.some((r: any) => ['accepted', 'previously_accepted'].includes(r.disposition)))
            continue;
        if (receipts.length !== 1)
            throw new Error('transition recovery receipt count');
        const receipt = receipts[0];
        if (receipt.disposition !== 'rejected' || receipt.code !== 'invalid_prerequisite' || BigInt(receipt.base_version) !== 0n || BigInt(receipt.canonical_version) !== 0n || receipt.sequence !== null)
            throw new Error('transition recovery receipt');
        const m: Mutation = { mutation_id: '', record_id: row.local_id, record_kind: 'observation', kind: 'create', base_version: 0, observation: observationSnapshot(ctx, row.local_id) };
        if (!createHash('sha256').update(json(m)).digest().equals(Buffer.from(row.payload_hash)))
            throw new Error('transition recovery snapshot changed');
        m.mutation_id = receipt.mutation_id;
        if (!createHash('sha256').update(json(validateMutation(m))).digest().equals(Buffer.from(receipt.mutation_hash)))
            throw new Error('transition recovery receipt hash');
        if (BigInt((db.prepare('SELECT sync_version FROM observations WHERE id=?').get(row.local_id) as any).sync_version) !== 0n)
            throw new Error('transition recovery version');
        if (m.observation.session_id && BigInt((db.prepare('SELECT sync_version FROM sessions WHERE id=? AND project_id=?').get(m.observation.session_id, ctx.project) as any)?.sync_version ?? 0) <= 0n)
            throw new Error('transition recovery session prerequisite');
        for (const id of m.observation.references ?? []) {
            const ref = db.prepare('SELECT project_id,sync_version FROM observations WHERE id=?').get(id) as any;
            if (!ref || ref.project_id !== ctx.project)
                throw new Error('transition recovery reference');
            if (BigInt(ref.sync_version) === 0n && !db.prepare("SELECT 1 FROM sync_outbox WHERE record_kind='observation' AND record_id=? AND mutation_kind='create' AND base_version=0").get(id))
                throw new Error('transition recovery reference prerequisite');
        }
        m.mutation_id = randomUUID();
        enqueueMutation(ctx, m);
    }
}
export async function syncProject(ctx: ServiceContext, options: SyncOptions = {}) {
    assertWritable(ctx);
    let pushed = 0, pulled = 0, previouslyAccepted = 0, rejectedCount = 0, retried = 0, conflicts = 0, batches = 0;
    const out = (status: string) => ({ mode: 'project_bidirectional', status, pushed, previouslyAccepted, rejected: rejectedCount, retried, conflicts, batches });
    try {
        const credential = syncStatus(ctx, options);
        if (credential.configured && credential.enabled && credential.credential !== 'available')
            return out(credential.credential === 'missing' ? 'credential_missing' : credential.credential === 'invalid' ? 'invalid' : 'credential_unavailable');
        const remote = wire(ctx, options);
        if (!remote)
            return out(profile(ctx) ? 'disabled' : 'absent');
        const p = portableProject(ctx), db = ctx.database.db;
        await remote.capabilities();
        const discovery = await remote.discover();
        let cursor: any = db.prepare('SELECT history_id,position,watermark FROM sync_project_cursor WHERE portable_project_id=?').get(p) ?? { history_id: discovery.history_id, position: 0, watermark: 0 };
        if (cursor.history_id !== discovery.history_id)
            throw new Error('remote history changed; explicit transition required');
        if (BigInt(cursor.position) === BigInt(cursor.watermark ?? 0))
            cursor = { ...cursor, watermark: 0 };
        const pull = async () => {
            for (let i = 0; i < 8; i++) {
                const page = await remote.pull(cursor, p);
                ctx.database.transaction(() => {
                    for (const c of page.changes ?? [])
                        applyChange(ctx, p, page.cursor.history_id, c);
                    db.prepare('INSERT INTO sync_project_cursor VALUES(?,?,?,?,?) ON CONFLICT(portable_project_id) DO UPDATE SET history_id=excluded.history_id,position=excluded.position,watermark=excluded.watermark,updated_at=excluded.updated_at').run(p, page.cursor.history_id, page.cursor.position, page.cursor.watermark ?? page.cursor.position, timestamp(ctx));
                });
                pulled += page.changes?.length ?? 0;
                cursor = page.cursor;
                if (!page.has_more)
                    return true;
            }
            return false;
        };
        const transition = db.prepare("SELECT status FROM sync_project_transitions WHERE portable_project_id=? AND local_project_id=?").get(p, ctx.project) as any;
        if (transition?.status === 'pulling') {
            if (!await pull())
                return out('partial');
            ctx.database.transaction(() => publishUnseen(ctx, p));
        }
        ctx.database.transaction(() => recoverRejected(ctx, p));
        for (let i = 0; i < 8; i++) {
            const claims = claim(ctx, 16);
            if (!claims.length)
                break;
            batches++;
            let results: any[];
            try {
                results = await remote.push(ctx.database.transaction(() => claims.map(c => translateMutation(ctx, c.mutation))));
            }
            catch (error) {
                retry(ctx, claims);
                retried += claims.length;
                throw error;
            }
            for (let n = 0; n < claims.length; n++) {
                finish(ctx, claims[n], results[n]);
                if (results[n].disposition === 'accepted')
                    pushed++;
                else if (results[n].disposition === 'previously_accepted')
                    previouslyAccepted++;
                else if (results[n].disposition === 'conflict')
                    conflicts++;
                else if (results[n].disposition === 'rejected')
                    rejectedCount++;
            }
        }
        if (BigInt(cursor.position) === BigInt(cursor.watermark ?? 0))
            cursor = { ...cursor, watermark: 0 };
        const completePull = await pull(), summary = queueSummary(ctx);
        const rejected = rejectedCount > 0;
        if (completePull && !summary.pending && !summary.conflicts && !rejected)
            ctx.database.transaction(() => {
                const unseen = db.prepare('SELECT 1 FROM sync_project_transition_records WHERE portable_project_id=? AND seen_remote=0').get(p);
                if (!unseen)
                    db.prepare("UPDATE sync_project_transitions SET status='completed',completed_at=? WHERE portable_project_id=? AND status='publishing'").run(timestamp(ctx), p);
            });
        return out(summary.conflicts ? 'conflict' : rejected ? 'rejected' : !completePull || summary.pending || db.prepare("SELECT 1 FROM sync_project_transitions WHERE portable_project_id=? AND status<>'completed'").get(p) ? 'partial' : 'synced');
    }
    catch (error) {
        const result = out(statusFor(error));
        return error instanceof SyncHttpError ? { ...result, failureOperation: error.operation, ...(error.status ? { failureHttpStatus: error.status } : {}), failureClass: error.kind } : result;
    }
}
function transitionDTO(ctx: ServiceContext) {
    const p = portableProject(ctx), db = ctx.database.db, row = db.prepare('SELECT mode,status FROM sync_project_transitions WHERE portable_project_id=? AND local_project_id=?').get(p, ctx.project) as any;
    if (!row)
        throw new Error('transition unavailable');
    const counts = db.prepare("SELECT SUM(record_kind='project') projects,SUM(record_kind='session') sessions,SUM(record_kind='observation') observations FROM sync_project_transition_records WHERE portable_project_id=?").get(p) as any;
    return { schemaVersion: 1, mode: row.mode, status: row.status, projects: Number(counts.projects ?? 0), sessions: Number(counts.sessions ?? 0), observations: Number(counts.observations ?? 0), queued: queueSummary(ctx).pending };
}
export async function dispatchSync(ctx: ServiceContext, operation: string, payload: any = {}, options: SyncOptions = {}) {
    switch (operation.replace(/^sync\./, "")) {
        case "configure": return configureSync(ctx, payload, options);
        case "status": return syncStatus(ctx, options);
        case "backfill": return backfillSyncProject(ctx, payload.limit);
        case "repair_project":
            assertWritable(ctx);
            {
                const result = repairProject(ctx, payload.confirmedRemoteAbsent);
                return { schemaVersion: 1, status: result.status, queued: result.queued };
            }
        case "reseed": {
            assertWritable(ctx);
            const remote = wire(ctx, options);
            if (!remote)
                throw new Error("enabled sync profile is required for reseed");
            const discovery = await remote.discover();
            const page = await remote.pull({ history_id: discovery.history_id, position: 0, watermark: 0 }, portableProject(ctx));
            if (page.has_more || (page.changes?.length ?? 0) !== 0 || page.cursor.history_id !== discovery.history_id || BigInt(page.cursor.position) !== 0n || BigInt(page.cursor.watermark ?? 0) !== 0n)
                throw new Error("reseed requires an empty remote project");
            await reseedProject(ctx);
            await syncProject(ctx, options);
            return transitionDTO(ctx);
        }
        case "rejoin": {
            assertWritable(ctx);
            await rejoinProject(ctx);
            await syncProject(ctx, options);
            return transitionDTO(ctx);
        }
        case "":
        case "sync": return syncProject(ctx, options);
        default: throw new TypeError(`unsupported sync operation: ${operation}`);
    }
}

import { createHash, randomUUID, timingSafeEqual } from "node:crypto";
import { clock, type ServiceContext, writable } from "./context.ts";
import { enqueueObservationChange } from "./sync-state.ts";
import { parseTimestamp } from "../sqlite/codec.ts";
const ttl = 86400000000000n;
function bad(): never { throw new Error("invalid"); }
const valid = (v: unknown, n: number) => typeof v === "string" && v.trim() !== "" && [...v].length <= n && !/[\x00-\x1f\x7f-\x9f]/.test(v);
const stamp = (n: bigint) => new Date(Number(n / 1000000n)).toISOString().replace(/\.\d{3}Z$/, "." + (n % 1000000000n).toString().padStart(9, "0").replace(/0+$/, "") + "Z").replace(".Z", "Z");
const ns = (v: any) => typeof v === "bigint" ? v : BigInt(v);
const select = "SELECT handle,project_id,provider,external_id_hash,state,checkpointed,COALESCE(final_observation_id,'') final_observation_id,lease_token,lease_until,created_at,updated_at,completed_at FROM local_provider_sessions";
function session(row: any) { if (!row)
    return undefined; return { handle: row.handle, project: row.project_id, provider: row.provider, state: row.state, checkpointed: Boolean(row.checkpointed), finalObservationId: row.final_observation_id, leaseToken: row.lease_token ?? "", leaseUntil: row.lease_until == null ? undefined : stamp(ns(row.lease_until)), createdAt: stamp(ns(row.created_at)), updatedAt: stamp(ns(row.updated_at)), completedAt: row.completed_at == null ? undefined : stamp(ns(row.completed_at)) }; }
function project(ctx: ServiceContext) { if (!ctx.project)
    bad(); return ctx.project; }
function secret(ctx: ServiceContext, handle: string) { const s = ctx.sessionSecrets?.get(handle); if (!s)
    bad(); return s; }
function monotonic(ctx: ServiceContext, previous: any) { const now = clock(ctx), old = ns(previous); return now > old ? now : old + 1n; }
function hash(v: string) { return createHash("sha256").update(v).digest(); }
function local(ctx: ServiceContext, row: any) { const out: any = session(row)!; out.draftPresent = Boolean(ctx.database.db.prepare("SELECT 1 FROM local_provider_session_drafts WHERE handle=? AND project_id=?").get(out.handle, ctx.project)); const secret = ctx.sessionSecrets?.get(out.handle); if (secret)
    out.leaseToken = secret.token;
else
    delete (out as any).leaseToken; return out; }
export async function dispatchSession(ctx: ServiceContext, operation: string, payload: any): Promise<any> {
    const p = payload ?? {}, id = project(ctx);
    if (operation.startsWith("memory."))
        operation = operation.slice(7);
    if (operation === "session.start") {
        writable(ctx);
        if (!valid(p.externalId, 4096))
            bad();
        const provider = String(p.provider ?? "pi").trim().toLowerCase();
        if (!valid(provider, 128))
            bad();
        return ctx.database.transaction(() => { ctx.database.db.prepare("INSERT OR IGNORE INTO projects(id) VALUES(?)").run(id); const digest = hash(p.externalId); const at = clock(ctx); for (const stale of ctx.database.db.prepare("SELECT handle,updated_at,created_at FROM local_provider_sessions WHERE project_id=? AND state='active' AND NOT(provider=? AND external_id_hash=?) AND (lease_until IS NULL OR lease_until<=?) ORDER BY COALESCE(lease_until,0),handle LIMIT 128").all(id, provider, digest, at) as any[]) {
            const updated = at > ns(stale.updated_at) ? at : ns(stale.updated_at) + 1n;
            ctx.database.db.prepare("UPDATE local_provider_sessions SET state='interrupted',lease_token=NULL,lease_until=NULL,updated_at=?,completed_at=? WHERE handle=?").run(updated, updated, stale.handle);
            ctx.database.db.prepare("DELETE FROM local_provider_session_drafts WHERE handle=?").run(stale.handle);
            ctx.sessionSecrets?.delete(stale.handle);
        } let row: any = ctx.database.db.prepare(`${select} WHERE project_id=? AND provider=? AND external_id_hash=?`).get(id, provider, digest); if (row) {
            if (row.state === "active") {
                const updated = monotonic(ctx, row.updated_at), token = `ps-${randomUUID()}`, until = updated + ttl;
                ctx.database.db.prepare("UPDATE local_provider_sessions SET lease_token=?,lease_until=?,updated_at=? WHERE handle=?").run(token, until, updated, row.handle);
                ctx.sessionSecrets ??= new Map();
                ctx.sessionSecrets.set(row.handle, { token, externalId: p.externalId });
                row = ctx.database.db.prepare(`${select} WHERE handle=?`).get(row.handle);
            }
            return local(ctx, row);
        } const now = clock(ctx), handle = `ps-${randomUUID()}`, token = `ps-${randomUUID()}`; ctx.database.db.prepare("INSERT INTO local_provider_sessions(handle,project_id,provider,external_id_hash,state,checkpointed,lease_token,lease_until,created_at,updated_at) VALUES(?,?,?,?, 'active',0,?,?,?,?)").run(handle, id, provider, digest, token, now + ttl, now, now); ctx.sessionSecrets ??= new Map(); ctx.sessionSecrets.set(handle, { token, externalId: p.externalId }); return local(ctx, ctx.database.db.prepare(`${select} WHERE handle=?`).get(handle)); });
    }
    if (!valid(p.handle, 128))
        bad();
    if (operation === "session.context") {
        const row = ctx.database.db.prepare(`${select} WHERE project_id=? AND handle=?`).get(id, p.handle);
        if (!row || row.state !== "active")
            bad();
        const prior = ctx.database.db.prepare("SELECT o.id,o.content FROM local_provider_sessions ps JOIN observations o ON o.id=ps.final_observation_id WHERE ps.project_id=? AND ps.state='completed' AND ps.handle<>? ORDER BY ps.completed_at DESC,ps.handle ASC LIMIT 1").get(id, p.handle) as any;
        const handoff = prior ? ([...(`UNTRUSTED DATA\nprior_completed_summary=${prior.id}\n${prior.content}`)].slice(0, 4096).join("")) : "";
        const draft = ctx.sessionSecrets?.has(p.handle) ? ctx.database.db.prepare("SELECT updated_at FROM local_provider_session_drafts WHERE handle=? AND project_id=?").get(p.handle, id) as any : undefined;
        return { session: local(ctx, row), handoff, ...(draft ? { draftUpdatedAt: stamp(ns(draft.updated_at)) } : {}) };
    }
    writable(ctx);
    const s = secret(ctx, p.handle);
    if (operation === "session.checkpoint" || operation === "session.renew") {
        return ctx.database.transaction(() => { const row = ctx.database.db.prepare(`${select} WHERE project_id=? AND handle=?`).get(id, p.handle) as any; if (!row || row.state !== "active" || row.lease_token !== s.token)
            bad(); const now = monotonic(ctx, row.updated_at); ctx.database.db.prepare(`UPDATE local_provider_sessions SET ${operation === "session.checkpoint" ? "checkpointed=1," : ""}lease_until=?,updated_at=? WHERE handle=? AND lease_token=?`).run(now + ttl, now, p.handle, s.token); return local(ctx, ctx.database.db.prepare(`${select} WHERE handle=?`).get(p.handle)); });
    }
    if (operation === "session.draft_save") {
        if (!valid(p.summary, 4096) || p.summary.includes(p.handle))
            bad();
        return ctx.database.transaction(() => { const row = ctx.database.db.prepare(`${select} WHERE project_id=? AND handle=?`).get(id, p.handle) as any; if (!row || row.state !== "active" || row.lease_token !== s.token)
            bad(); const old = ctx.database.db.prepare("SELECT updated_at FROM local_provider_session_drafts WHERE handle=? AND project_id=?").get(p.handle, id) as any; let expected: bigint | undefined; try {
            expected = p.expectedUpdatedAt ? parseTimestamp(p.expectedUpdatedAt) : undefined;
        }
        catch {
            bad();
        } if (Boolean(old) !== Boolean(expected) || old && ns(old.updated_at) !== expected)
            bad(); const now = old ? monotonic(ctx, old.updated_at) : clock(ctx); if (old)
            ctx.database.db.prepare("UPDATE local_provider_session_drafts SET summary=?,updated_at=? WHERE handle=?").run(p.summary, now, p.handle);
        else
            ctx.database.db.prepare("INSERT INTO local_provider_session_drafts(handle,project_id,summary,updated_at) VALUES(?,?,?,?)").run(p.handle, id, p.summary, now); return { handle: p.handle, project: id, updatedAt: stamp(now) }; });
    }
    if (operation === "session.end") {
        const state = p.state;
        if (!["completed", "cancelled", "interrupted"].includes(state) || !valid(s.externalId, 4096) || (state !== "completed" && p.summary !== ""))
            bad();
        return ctx.database.transaction(() => { const row = ctx.database.db.prepare(`${select} WHERE project_id=? AND handle=?`).get(id, p.handle) as any; if (!row)
            bad(); if (!timingSafeEqual(Buffer.from(row.external_id_hash), hash(s.externalId)))
            bad(); if (row.state !== "active") {
            if (row.state === state)
                return local(ctx, row);
            bad();
        } if (row.lease_token !== s.token)
            bad(); let final = ""; if (state === "completed") {
            let summary = p.summary;
            if (summary === "") {
                const draft = ctx.database.db.prepare("SELECT summary FROM local_provider_session_drafts WHERE handle=? AND project_id=?").get(p.handle, id) as any;
                if (!draft)
                    bad();
                summary = draft.summary;
            }
            if (!valid(summary, 4096) || summary.includes(p.handle) || summary.includes(s.externalId))
                bad();
            final = `obs-${randomUUID()}`;
            const now = clock(ctx);
            ctx.database.db.prepare("INSERT INTO observations(id,title,project_id,scope,type,content,topic_key,producer,source_provider,source_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)").run(final, "", id, "project", "summary", summary, `provider-session-summary-${final}`, "provider-session", "", "", "active", now, now);
            ctx.database.db.prepare("INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)").run(final, "", `provider-session-summary-${final}`, "summary", summary);
            enqueueObservationChange(ctx, final);
        } const now = monotonic(ctx, row.updated_at); ctx.database.db.prepare("DELETE FROM local_provider_session_drafts WHERE handle=?").run(p.handle); ctx.database.db.prepare("UPDATE local_provider_sessions SET state=?,final_observation_id=?,lease_token=NULL,lease_until=NULL,updated_at=?,completed_at=? WHERE handle=? AND lease_token=?").run(state, final || null, now, now, p.handle, s.token); ctx.sessionSecrets?.delete(p.handle); return session(ctx.database.db.prepare(`${select} WHERE handle=?`).get(p.handle)); });
    }
    bad();
}

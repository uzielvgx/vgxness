import { createHash, randomBytes } from "node:crypto";
import type { SQLiteDatabase } from "../sqlite/node-sqlite.ts";
import { nowNs, formatTimestamp } from "../sqlite/codec.ts";
export type ServiceContext = {
    database: SQLiteDatabase;
    project: string;
    workspace: string;
    storageRoot?: string;
    mode: "full" | "read-only";
    role: string;
    now?: () => bigint;
    sessionSecrets?: unknown;
};
type Row = Record<string, any>;
const phases = ["explore", "proposal", "spec", "design", "tasks", "apply", "verify", "complete"] as const;
const nonfinal = phases.slice(0, -1), digest = (v: Uint8Array) => createHash("sha256").update(v).digest("hex");
const id = (kind: string) => `${kind}-${randomBytes(16).toString("hex")}`;
function invalid(m = "invalid SDD request"): never { throw new Error(m); }
function conflict(m = "SDD conflict"): never { throw new Error(m); }
const text = (v: any, n: number) => typeof v === "string" && v.trim() === v && v.length > 0 && [...v].length <= n && !/[\x00-\x1f\x7f-\x9f]/.test(v);
const hashInputs = (values: any[] = []) => digest(Buffer.from([...values].sort((a, b) => {
    const left = Buffer.from(`${a.artifactId}\0${a.revisionId}`), right = Buffer.from(`${b.artifactId}\0${b.revisionId}`);
    return Buffer.compare(left, right);
}).map(v => `${v.artifactId}\0${v.revisionId}\0${v.digest}\0`).join("")));
const bytes = (v: any): Buffer => { try {
    const b = Buffer.from(String(v), "base64");
    if (!b.length || b.toString("base64").replace(/=+$/, "") !== String(v).replace(/=+$/, ""))
        invalid();
    return b;
}
catch {
    invalid();
} };
const projectionPath = (changeId: string, artifact: string) => { if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(changeId))
    invalid("unsafe OpenSpec projection path"); const names: Record<string, string> = { explore: "research.md", proposal: "proposal.md", spec: "spec.md", design: "design.md", tasks: "tasks.md", apply: "apply-result.md", verify: "verification.md" }; if (!names[artifact])
    invalid("unsafe OpenSpec projection path"); return `openspec/changes/${changeId}/${names[artifact]}`; };
const ts = (v: bigint) => formatTimestamp(v);
const changeColumns = "id,project_id,idempotency_key,title,backend,interaction_mode,model_plan,phase,status,state_version,CAST(created_at AS TEXT) created_at,CAST(updated_at AS TEXT) updated_at";
const outChange = (r: Row) => ({ id: r.id, project: r.project_id, title: r.title, backend: r.backend, interactionMode: r.interaction_mode, plan: r.model_plan, phase: r.phase, status: r.status, stateVersion: Number(r.state_version), createdAt: ts(BigInt(r.created_at)), updatedAt: ts(BigInt(r.updated_at)) });
function context(ctx: ServiceContext, write = false) { if (!ctx?.database || !text(ctx.project, 256))
    invalid(); if (write && (ctx.mode !== "full" || ctx.role !== "manager" || ctx.database.readOnly))
    throw new Error("SDD lifecycle mutation requires full manager authority"); return ctx.database; }
function change(ctx: ServiceContext, changeId: string) { const r = context(ctx).db.prepare(`SELECT ${changeColumns} FROM sdd_changes WHERE project_id=? AND id=?`).get(ctx.project, changeId) as Row | undefined; if (!r)
    throw new Error("SDD record not found: change"); return r; }
function active(ctx: ServiceContext, changeId: string, expected: any) { const c = change(ctx, changeId); if (c.status === "cancelled")
    throw new Error("SDD change is cancelled"); if (c.status !== "active")
    conflict(`SDD conflict: change is ${c.status}`); if (!Number.isInteger(expected) || expected < 1)
    invalid(); if (Number(c.state_version) !== expected)
    throw new Error(`stale SDD state version: expected ${expected}, current ${c.state_version}`); return c; }
function revision(ctx: ServiceContext, changeId: string, revisionId: string, content = true) { const r = context(ctx).db.prepare(`SELECT r.id,r.project_id,r.change_id,r.artifact_id,r.status,r.content,r.external_location,r.content_digest,r.input_digest,CAST(r.created_at AS TEXT) created_at,CAST(r.accepted_at AS TEXT) accepted_at,a.phase artifact,a.status artifact_status FROM sdd_revisions r JOIN sdd_artifacts a ON a.id=r.artifact_id AND a.project_id=r.project_id AND a.change_id=r.change_id WHERE r.project_id=? AND r.change_id=? AND r.id=?`).get(ctx.project, changeId, revisionId) as Row | undefined; if (!r)
    throw new Error("SDD record not found: revision"); const inputs = context(ctx).db.prepare("SELECT input_artifact_id artifactId,input_revision_id revisionId,input_digest digest FROM sdd_revision_links WHERE project_id=? AND change_id=? AND revision_id=? ORDER BY input_artifact_id").all(ctx.project, changeId, revisionId) as Row[]; return { id: r.id, project: r.project_id, changeId: r.change_id, artifactId: r.artifact_id, artifact: r.artifact, artifactStatus: r.artifact_status, status: r.status, ...(content && r.content !== null ? { content: Buffer.from(r.content).toString("base64") } : {}), ...(r.external_location ? { externalLocation: r.external_location } : {}), digest: r.content_digest, inputDigest: r.input_digest, inputs, stateVersion: Number((change(ctx, changeId)).state_version), createdAt: ts(BigInt(r.created_at)), ...(r.accepted_at !== null ? { acceptedAt: ts(BigInt(r.accepted_at)) } : {}) }; }
function bump(ctx: ServiceContext, c: Row, now: bigint) { const z = context(ctx).db.prepare("UPDATE sdd_changes SET state_version=state_version+1,updated_at=? WHERE id=? AND project_id=? AND state_version=? AND status='active'").run(now, c.id, ctx.project, c.state_version) as any; if (Number(z.changes) !== 1)
    throw new Error("stale SDD state version: change mutation"); return Number(c.state_version) + 1; }
function currentArtifact(ctx: ServiceContext, changeId: string, artifactId: string) { const r = context(ctx).db.prepare("SELECT id,project_id,change_id,phase,status,current_revision_id FROM sdd_artifacts WHERE project_id=? AND change_id=? AND id=?").get(ctx.project, changeId, artifactId) as Row | undefined; if (!r)
    throw new Error("SDD record not found: artifact"); return r; }
export async function dispatchSdd(ctx: ServiceContext, operation: string, payload: any): Promise<any> {
    const write = ["create", "set_interaction_mode", "save_revision", "accept_revision", "transition", "cancel", "record_projection"].includes(operation);
    context(ctx, write);
    const db = ctx.database, now = () => ctx.now?.() ?? nowNs();
    if (operation === "create") {
        const { idempotencyKey, title, backend, interactionMode, plan } = payload ?? {};
        if (!text(idempotencyKey, 256) || !text(title, 512) || !["memory", "openspec", "hybrid"].includes(backend) || !["automatic", "interactive"].includes(interactionMode) || !["low", "medium", "high", "ultra"].includes(plan))
            invalid();
        return db.transaction(() => { const old = db.db.prepare(`SELECT ${changeColumns} FROM sdd_changes WHERE project_id=? AND idempotency_key=?`).get(ctx.project, idempotencyKey) as Row | undefined; if (old) {
            if (old.title !== title || old.backend !== backend || old.interaction_mode !== interactionMode || old.model_plan !== plan)
                conflict("SDD conflict: create idempotency key");
            return outChange(old);
        } const t = now(), value = { id: id("change"), project_id: ctx.project, idempotency_key: idempotencyKey, title, backend, interaction_mode: interactionMode, model_plan: plan, phase: "explore", status: "active", state_version: 1, created_at: t, updated_at: t }; db.db.prepare("INSERT OR IGNORE INTO projects(id) VALUES(?)").run(ctx.project); db.db.prepare("INSERT INTO sdd_changes VALUES(?,?,?,?,?,?,?,?,?,?,?,?)").run(value.id, value.project_id, value.idempotency_key, title, backend, interactionMode, plan, "explore", "active", 1, t, t); return outChange(value); });
    }
    if (operation === "list") {
        const { status, limit } = payload ?? {};
        if (status !== undefined && !["active", "completed", "cancelled"].includes(status) || limit !== undefined && (!Number.isInteger(limit) || limit < 1 || limit > 100))
            invalid();
        return (db.db.prepare(`SELECT ${changeColumns} FROM sdd_changes WHERE project_id=?${status ? " AND status=?" : ""} ORDER BY updated_at DESC,id ASC LIMIT ?`).all(...(status ? [ctx.project, status, limit ?? 20] : [ctx.project, limit ?? 20])) as Row[]).map(outChange);
    }
    if (operation === "get") {
        if (!text(payload?.id, 256))
            invalid();
        return outChange(change(ctx, payload.id));
    }
    if (operation === "set_interaction_mode") {
        if (!text(payload?.changeId, 256) || !["automatic", "interactive"].includes(payload.interactionMode))
            invalid();
        return db.transaction(() => { const c = active(ctx, payload.changeId, payload.expectedStateVersion), t = now(); bump(ctx, c, t); db.db.prepare("UPDATE sdd_changes SET interaction_mode=? WHERE id=?").run(payload.interactionMode, c.id); return outChange({ ...c, interaction_mode: payload.interactionMode, state_version: Number(c.state_version) + 1, updated_at: t }); });
    }
    if (operation === "save_revision")
        return db.transaction(() => save(ctx, payload, now()));
    if (operation === "get_revision") {
        if (!text(payload?.changeId, 256) || !text(payload?.revisionId, 256))
            invalid();
        return revision(ctx, payload.changeId, payload.revisionId);
    }
    if (operation === "list_revisions") {
        if (!text(payload?.changeId, 256) || payload.artifact !== undefined && !nonfinal.includes(payload.artifact) || payload.limit !== undefined && (!Number.isInteger(payload.limit) || payload.limit < 1 || payload.limit > 100))
            invalid();
        return (db.db.prepare(`SELECT r.id FROM sdd_revisions r JOIN sdd_artifacts a ON a.id=r.artifact_id WHERE r.project_id=? AND r.change_id=?${payload.artifact ? " AND a.phase=?" : ""} ORDER BY r.created_at DESC,r.id ASC LIMIT ?`).all(...(payload.artifact ? [ctx.project, payload.changeId, payload.artifact, payload.limit ?? 50] : [ctx.project, payload.changeId, payload.limit ?? 50])) as Row[]).map(r => revision(ctx, payload.changeId, r.id, false));
    }
    if (operation === "accept_revision")
        return db.transaction(() => accept(ctx, payload, now()));
    if (operation === "transition" || operation === "cancel")
        return db.transaction(() => transition(ctx, operation === "cancel" ? { ...payload, cancel: true } : payload, now()));
    if (operation === "projection_status")
        return projectionStatus(ctx, payload);
    if (operation === "record_projection")
        return db.transaction(() => record(ctx, payload, now()));
    if (operation === "render_projection") {
        const r = revision(ctx, payload?.changeId, payload?.revisionId);
        return render(r);
    }
    if (operation === "compare_projection")
        return compare(ctx, payload);
    throw new Error("unknown SDD operation");
}
function save(ctx: ServiceContext, p: any, t: bigint) { const { changeId, artifact, externalLocation = "", expectedStateVersion } = p ?? {}; if (!text(changeId, 256) || !nonfinal.includes(artifact) || !Number.isInteger(expectedStateVersion) || expectedStateVersion < 1)
    invalid(); const content = bytes(p.content); if (content.length > 4 * 1024 * 1024 || externalLocation !== "" && !text(externalLocation, 1024))
    invalid(); const inputs = p.inputs ?? []; if (!Array.isArray(inputs) || new Set(inputs.map((x: any) => x.artifactId)).size !== inputs.length || inputs.some((x: any) => !text(x.artifactId, 256) || !text(x.revisionId, 256) || !/^[a-f0-9]{64}$/.test(x.digest)))
    invalid(); const c = active(ctx, changeId, expectedStateVersion); if (c.phase !== artifact)
    conflict("SDD conflict: artifact phase does not match current change phase"); const d = digest(content), ih = hashInputs(inputs); if (p.digest && p.digest !== d || p.inputDigest && p.inputDigest !== ih)
    throw new Error("SDD digest mismatch"); let stored: any = content, external: any = null; if (c.backend === "openspec") {
    if (externalLocation !== projectionPath(c.id, artifact))
        invalid("invalid SDD request: external OpenSpec revision location");
    stored = null;
    external = externalLocation;
}
else if (externalLocation)
    invalid("invalid SDD request: external revision requires openspec backend"); let a = ctx.database.db.prepare("SELECT id,status FROM sdd_artifacts WHERE project_id=? AND change_id=? AND phase=?").get(ctx.project, changeId, artifact) as Row | undefined; if (!a) {
    a = { id: id("artifact"), status: "draft" };
    ctx.database.db.prepare("INSERT INTO sdd_artifacts VALUES(?,?,?,?,?,?,?,?)").run(a.id, ctx.project, changeId, artifact, "draft", null, t, t);
} for (const x of inputs) {
    const r = ctx.database.db.prepare("SELECT a.phase,r.content_digest FROM sdd_artifacts a JOIN sdd_revisions r ON r.id=a.current_revision_id WHERE a.project_id=? AND a.change_id=? AND a.id=? AND a.current_revision_id=? AND a.status='accepted' AND r.status='accepted'").get(ctx.project, changeId, x.artifactId, x.revisionId) as Row | undefined;
    if (!r || r.content_digest !== x.digest || phases.indexOf(r.phase) >= phases.indexOf(artifact))
        throw new Error(`SDD input revisions changed: ${x.artifactId}`);
} const rid = id("revision"); ctx.database.db.prepare("INSERT INTO sdd_revisions VALUES(?,?,?,?,?,?,?,?,?,?,NULL)").run(rid, ctx.project, changeId, a.id, "candidate", stored, external, d, ih, t); for (const x of inputs)
    ctx.database.db.prepare("INSERT INTO sdd_revision_links VALUES(?,?,?,?,?,?)").run(ctx.project, changeId, rid, x.artifactId, x.revisionId, x.digest); bump(ctx, c, t); return revision(ctx, changeId, rid); }
function accept(ctx: ServiceContext, p: any, t: bigint) { if (!text(p?.changeId, 256) || !text(p?.revisionId, 256))
    invalid(); const c = active(ctx, p.changeId, p.expectedStateVersion), r = revision(ctx, p.changeId, p.revisionId), raw = currentArtifact(ctx, p.changeId, r.artifactId); if (c.phase !== r.artifact)
    conflict("SDD conflict: revision phase does not match current change phase"); if (r.status === "accepted")
    throw new Error(`accepted SDD revision is immutable: ${r.id}`); for (const x of r.inputs) {
    const a = currentArtifact(ctx, p.changeId, x.artifactId);
    if (a.status !== "accepted" || a.current_revision_id !== x.revisionId)
        throw new Error(`SDD input revisions changed: ${x.artifactId}`);
} ctx.database.db.prepare("UPDATE sdd_revisions SET status='accepted',accepted_at=? WHERE id=?").run(t, r.id); ctx.database.db.prepare("UPDATE sdd_artifacts SET status='accepted',current_revision_id=?,updated_at=? WHERE id=?").run(r.id, t, raw.id); ctx.database.db.prepare("UPDATE sdd_projections SET status='stale' WHERE project_id=? AND change_id=? AND artifact_id=?").run(ctx.project, p.changeId, raw.id); bump(ctx, c, t); return revision(ctx, p.changeId, r.id); }
function transition(ctx: ServiceContext, p: any, t: bigint) { if (!text(p?.changeId, 256))
    invalid(); const c = active(ctx, p.changeId, p.expectedStateVersion); let phase = c.phase, status = c.status; if (p.cancel === true) {
    status = "cancelled";
}
else {
    if (!phases.includes(p.targetPhase) || phases.indexOf(p.targetPhase) !== phases.indexOf(c.phase) + 1)
        throw new Error(`illegal SDD lifecycle transition: ${c.phase} to ${p.targetPhase}`);
    const a = ctx.database.db.prepare("SELECT a.id,a.current_revision_id FROM sdd_artifacts a JOIN sdd_revisions r ON r.id=a.current_revision_id WHERE a.project_id=? AND a.change_id=? AND a.phase=? AND a.status='accepted' AND r.status='accepted'").get(ctx.project, p.changeId, c.phase) as Row | undefined;
    if (!a)
        conflict(`SDD conflict: phase ${c.phase} has no accepted artifact`);
    if (c.backend !== "memory") {
        const q = ctx.database.db.prepare("SELECT revision_id FROM sdd_projections WHERE project_id=? AND change_id=? AND artifact_id=? AND status='current'").get(ctx.project, p.changeId, a.id) as Row | undefined;
        if (!q || q.revision_id !== a.current_revision_id)
            conflict(`SDD conflict: phase ${c.phase} projection is not current`);
    }
    phase = p.targetPhase;
    if (phase === "complete")
        status = "completed";
} ctx.database.db.prepare("UPDATE sdd_changes SET phase=?,status=?,state_version=state_version+1,updated_at=? WHERE id=?").run(phase, status, t, c.id); return outChange({ ...c, phase, status, state_version: Number(c.state_version) + 1, updated_at: t }); }
function projectionStatus(ctx: ServiceContext, p: any) { if (!text(p?.changeId, 256) || !text(p?.artifactId, 256))
    invalid(); const a = currentArtifact(ctx, p.changeId, p.artifactId), r = ctx.database.db.prepare("SELECT * FROM sdd_projections WHERE project_id=? AND change_id=? AND artifact_id=?").get(ctx.project, p.changeId, p.artifactId) as Row | undefined, c = change(ctx, p.changeId); return r ? { project: r.project_id, changeId: r.change_id, artifactId: r.artifact_id, revisionId: r.revision_id, status: r.status, digest: r.digest, location: r.location, stateVersion: Number(c.state_version), recordedAt: ts(BigInt(r.recorded_at)) } : { project: ctx.project, changeId: p.changeId, artifactId: a.id, status: "absent", stateVersion: Number(c.state_version) }; }
function render(r: any) { if (!r.content)
    throw new Error("SDD digest mismatch"); const content = Buffer.from(r.content, "base64"), meta = `<!-- vgxness-sdd\nschemaVersion: 1\nchangeId: ${r.changeId}\nartifact: ${r.artifact}\nrevisionId: ${r.id}\ncontentDigest: ${r.digest}\ninputDigest: ${r.inputDigest}\n-->\n`, document = Buffer.concat([Buffer.from(meta), content]); return { relativePath: projectionPath(r.changeId, r.artifact), content: document.toString("base64"), digest: digest(document), metadata: { schemaVersion: 1, changeId: r.changeId, artifact: r.artifact, revisionId: r.id, contentDigest: r.digest, inputDigest: r.inputDigest } }; }
function record(ctx: ServiceContext, p: any, t: bigint) { if (!text(p?.changeId, 256) || !text(p?.artifactId, 256) || !text(p?.revisionId, 256) || !["current", "stale", "drift", "failed"].includes(p.status) || !/^[a-f0-9]{64}$/.test(p.digest) || !text(p.location, 1024))
    invalid(); const c = active(ctx, p.changeId, p.expectedStateVersion), a = currentArtifact(ctx, p.changeId, p.artifactId), r = revision(ctx, p.changeId, p.revisionId); if (a.current_revision_id !== r.id || r.status !== "accepted")
    throw new Error("SDD input revisions changed: projection revision"); if (p.status === "current") {
    if (p.location !== projectionPath(c.id, r.artifact))
        invalid("invalid SDD request: projection location");
    const expected = c.backend === "hybrid" ? render(r).digest : r.digest;
    if (p.digest !== expected)
        throw new Error("SDD digest mismatch: projection");
} ctx.database.db.prepare("INSERT INTO sdd_projections VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(artifact_id) DO UPDATE SET revision_id=excluded.revision_id,status=excluded.status,digest=excluded.digest,location=excluded.location,recorded_at=excluded.recorded_at").run(ctx.project, p.changeId, a.id, r.id, p.status, p.digest, p.location, t); const v = bump(ctx, c, t); return { project: ctx.project, changeId: p.changeId, artifactId: a.id, revisionId: r.id, status: p.status, digest: p.digest, location: p.location, stateVersion: v, recordedAt: ts(t) }; }
function parseProjection(document: Buffer) {
    if (document.length > (4 << 20) + 2048 || !document.subarray(0, 17).equals(Buffer.from("<!-- vgxness-sdd\n")))
        invalid("invalid SDD request: malformed OpenSpec projection");
    const end = document.indexOf("\n-->\n", 17);
    if (end < 17 || end > 2048)
        invalid("invalid SDD request: malformed OpenSpec projection");
    if (document.length - end - 5 > 4 * 1024 * 1024)
        invalid("invalid SDD request: oversized OpenSpec projection");
    const fields: Record<string, string> = {};
    for (const line of document.subarray(17, end).toString().split("\n")) {
        const split = line.indexOf(": ");
        const pair = split < 0 ? [] : [line.slice(0, split), line.slice(split + 2)];
        if (pair.length !== 2 || !pair[0] || !pair[1] || fields[pair[0]])
            invalid("invalid SDD request: malformed OpenSpec projection");
        fields[pair[0]] = pair[1];
    }
    const content = document.subarray(end + 5);
    if (Object.keys(fields).length !== 6 || fields.schemaVersion !== "1" || !/^[a-f0-9]{64}$/.test(fields.contentDigest) || !/^[a-f0-9]{64}$/.test(fields.inputDigest) || digest(content) !== fields.contentDigest)
        invalid("SDD digest mismatch");
    if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(fields.changeId ?? "") || !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(fields.revisionId ?? "") || !nonfinal.includes(fields.artifact as any))
        invalid("invalid SDD request: malformed OpenSpec projection");
    return { fields, content };
}
function compare(ctx: ServiceContext, p: any) { if (!text(p?.changeId, 256) || !text(p?.revisionId, 256) || !text(p?.relativePath, 512) || p.missing === true && p.projectionContent !== undefined)
    invalid(); const r = revision(ctx, p.changeId, p.revisionId), c = change(ctx, p.changeId); if (c.backend !== "openspec" && c.backend !== "hybrid")
    invalid(); const canonical = render(r); if (p.symlink || p.relativePath.includes("\\") || p.relativePath.startsWith("/") || p.relativePath !== canonical.relativePath)
    invalid("unsafe OpenSpec projection path"); if (p.missing)
    return { state: "missing", relativePath: canonical.relativePath, canonicalDigest: canonical.digest, memoryCanonical: c.backend === "hybrid", options: ["render_canonical_memory_projection"], requiresSaveRevision: false }; if (typeof p.projectionContent !== "string")
    invalid(); const observed = Buffer.from(p.projectionContent, "utf8"), parsed = parseProjection(observed); if (parsed.fields.changeId !== r.changeId || parsed.fields.artifact !== r.artifact || parsed.fields.revisionId !== r.id)
    conflict("SDD conflict: OpenSpec projection identity"); if (parsed.fields.inputDigest !== r.inputDigest)
    throw new Error("SDD input revisions changed"); const observedDigest = digest(observed); if (observed.equals(Buffer.from(canonical.content, "base64")))
    return { state: "synced", relativePath: canonical.relativePath, canonicalDigest: canonical.digest, observedDigest, memoryCanonical: c.backend === "hybrid", options: [], requiresSaveRevision: false }; return { state: "drifted", relativePath: canonical.relativePath, canonicalDigest: canonical.digest, observedDigest, memoryCanonical: c.backend === "hybrid", options: ["render_canonical_memory_projection", "inspect_differences", "save_projection_as_candidate_revision"], requiresSaveRevision: true, candidateContent: parsed.content.toString("base64"), candidateDigest: parsed.fields.contentDigest }; }

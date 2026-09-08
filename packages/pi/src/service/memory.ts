import { createHash, randomUUID } from "node:crypto";
import { constants, realpathSync } from "node:fs";
import { lstat, mkdir, open, link, unlink } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
import { clock, type ServiceContext, writable } from "./context.ts";
import { enqueueObservationChange } from "./sync-state.ts";
type Row = Record<string, any>;
const cols = "o.id,o.title,o.project_id,COALESCE(o.session_id,'') session_id,o.scope,o.type,o.content,COALESCE(o.topic_key,'') topic_key,o.producer,o.source_provider,o.source_id,o.state,o.created_at,o.updated_at,COALESCE((SELECT group_concat(target_id,char(31)) FROM observation_refs WHERE observation_id=o.id),'') refs";
function fail(kind: string): never { throw new Error(kind); }
const text = (v: unknown, limit: number, optional = false) => typeof v === "string" && (optional || v.trim() !== "") && [...v].length <= limit && !/[\x00-\x1f\x7f-\x9f]/.test(v);
const metadata = (...vs: unknown[]) => vs.every(v => text(v, 256, true));
const scope = (v: unknown) => v === "project" || v === "personal";
const state = (v: unknown) => v === "active" || v === "needs_review" || v === "archived";
const stamp = (value: bigint) => new Date(Number(value / 1000000n)).toISOString().replace(/\.\d{3}Z$/, "." + (value % 1000000000n).toString().padStart(9, "0").replace(/0+$/, "") + "Z").replace(".Z", "Z");
const asNs = (v: unknown) => typeof v === "bigint" ? v : BigInt(v as number);
function entry(row: Row, content: boolean) {
    const out: Row = { id: row.id, title: row.title, project: row.project_id, scope: row.scope, type: row.type, topicKey: row.topic_key, session: row.session_id, producer: row.producer, sourceProvider: row.source_provider, sourceId: row.source_id, state: row.state, createdAt: stamp(asNs(row.created_at)), updatedAt: stamp(asNs(row.updated_at)), references: row.refs ? String(row.refs).split("\x1f") : [] };
    if (content)
        out.content = row.content;
    return out;
}
function query(value: unknown, any: boolean) {
    if (typeof value !== "string")
        fail("invalid");
    const terms = value.split(/[^\p{L}\p{N}_]+/u).filter(Boolean);
    if (!terms.length)
        fail("invalid");
    return terms.map(v => `"${v}"`).join(any ? " OR " : " ");
}
function getRow(ctx: ServiceContext, id: string, project: string, selectedScope?: string) {
    const where = selectedScope ? " AND o.scope=?" : "";
    const params = selectedScope ? [id, project, selectedScope] : [id, project];
    return ctx.database.db.prepare(`SELECT ${cols} FROM observations o WHERE o.id=? AND o.project_id=?${where}`).get(...params) as Row | undefined;
}
function projectId(ctx: ServiceContext) { return ctx.project || resolveProject(ctx); }
/** Go-compatible stable local project identity for a canonical workspace. */
export function resolveProject(ctx: ServiceContext) {
    let workspace: string;
    try {
        workspace = realpathSync(resolve(ctx.workspace));
    }
    catch {
        fail("invalid");
    }
    if (workspace === "/" || basename(workspace) === ".")
        fail("invalid");
    const hash = createHash("sha256").update(workspace).digest("hex");
    const legacy = basename(workspace), stable = `${legacy}-${hash.slice(0, 12)}`;
    const found = ctx.database.db.prepare("SELECT project_id FROM project_roots WHERE workspace_hash=?").get(hash) as Row | undefined;
    if (found)
        return String(found.project_id);
    const old = ctx.database.db.prepare("SELECT EXISTS(SELECT 1 FROM projects p WHERE p.id=? AND NOT EXISTS(SELECT 1 FROM project_roots r WHERE r.project_id=p.id)) present").get(legacy) as Row;
    if (ctx.mode === "read-only" || ctx.role !== "manager" || ctx.database.readOnly)
        return old.present ? legacy : stable;
    return ctx.database.transaction(() => {
        const again = ctx.database.db.prepare("SELECT project_id FROM project_roots WHERE workspace_hash=?").get(hash) as Row | undefined;
        if (again)
            return String(again.project_id);
        const id = old.present ? legacy : stable;
        ctx.database.db.prepare("INSERT OR IGNORE INTO projects(id) VALUES(?)").run(id);
        ctx.database.db.prepare("INSERT INTO project_roots(workspace_hash,project_id) VALUES(?,?)").run(hash, id);
        return id;
    });
}
async function initializeProject(ctx: ServiceContext) {
    writable(ctx);
    const workspace = realpathSync(ctx.workspace), local = resolveProject(ctx), hash = createHash("sha256").update(workspace).digest("hex");
    const bound = ctx.database.db.prepare("SELECT portable_id FROM portable_project_identities WHERE workspace_hash=?").get(hash) as Row | undefined;
    const directory = join(workspace, ".vgxness"), path = join(directory, "project-id");
    const validateDirectory = async () => { const info = await lstat(directory); if (!info.isDirectory() || info.isSymbolicLink())
        fail("invalid project marker directory"); };
    const readMarker = async (): Promise<string | undefined> => {
        try {
            await validateDirectory();
            const info = await lstat(path);
            if (!info.isFile() || info.isSymbolicLink() || info.size > 160)
                fail("invalid project marker");
            const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
            try {
                const bytes = await file.readFile();
                if (bytes.length > 160)
                    fail("invalid project marker");
                const raw = bytes.toString("utf8"), value = JSON.parse(raw);
                const keys = [...raw.matchAll(/"((?:[^"\\]|\\.)*)"\s*:/g)].map(match => JSON.parse(`"${match[1]}"`));
                if (keys.length !== 3 || new Set(keys).size !== 3 || Object.keys(value).sort().join(",") !== "format,kind,project_id" || value.format !== "vgxness-project-id/v1" || value.kind !== "project" || !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.project_id))
                    fail("invalid project marker");
                return value.project_id;
            }
            finally {
                await file.close();
            }
        }
        catch (error: any) {
            if (error?.code === "ENOENT")
                return undefined;
            throw error;
        }
    };
    let portable = await readMarker();
    if (portable && bound && portable !== bound.portable_id)
        fail("project marker changed");
    if (!portable) {
        await mkdir(directory, { mode: 0o700 }).catch(error => { if (error?.code !== "EEXIST")
            throw error; });
        await validateDirectory();
        portable = bound?.portable_id ?? randomUUID();
        const temporary = join(directory, `.project-id-${randomUUID()}`), file = await open(temporary, "wx", 0o600);
        try {
            await file.writeFile(JSON.stringify({ format: "vgxness-project-id/v1", kind: "project", project_id: portable }) + "\n");
            await file.sync();
            await file.close();
            try {
                await link(temporary, path);
            }
            catch (error: any) {
                if (error?.code !== "EEXIST")
                    throw error;
            }
        }
        finally {
            await file.close();
            await unlink(temporary).catch(() => { });
        }
        const actual = await readMarker();
        if (!actual || bound && actual !== bound.portable_id)
            fail("project marker changed");
        portable = actual;
    }
    for (const target of process.platform === "win32" ? [] : [workspace, directory]) {
        const file = await open(target, "r");
        try {
            await file.sync();
        }
        finally {
            await file.close();
        }
    }
    if (!portable)
        fail("invalid project marker");
    const portableId = portable;
    ctx.database.transaction(() => {
        const existing = ctx.database.db.prepare("SELECT portable_id,project_id FROM portable_project_identities WHERE portable_id=? OR workspace_hash=? LIMIT 1").get(portableId, hash) as Row | undefined;
        if (existing) {
            if (existing.portable_id !== portable || existing.project_id !== local)
                fail("portable project identity binding conflict");
            return;
        }
        ctx.database.db.prepare("INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES(?,?,?,?,?)").run(portableId, local, hash, "explicit-init", stamp(clock(ctx)));
    });
    return { project: portable };
}
export async function dispatchMemory(ctx: ServiceContext, operation: string, payload: any): Promise<any> {
    const project = projectId(ctx);
    if (operation === "memory.project.resolve" || operation === "project.resolve")
        return { project: resolveProject(ctx) };
    if (operation === "memory.project.initialize" || operation === "project.initialize")
        return initializeProject(ctx);
    const selectedScope = payload?.scope || "project";
    if (!scope(selectedScope))
        fail("invalid");
    if (operation === "memory.remember" || operation === "remember") {
        writable(ctx);
        const p = payload ?? {}, references = p.references ?? [];
        const finalState = p.state || "active", type = p.type || "learning", title = p.title ?? "";
        if (!text(p.content, 4096) || !text(title, 256, true) || title !== "" && title.trim() === "" || !metadata(project, type, p.topicKey ?? "", p.session ?? "", p.sourceProvider ?? "", p.sourceId ?? "") || !["active", "needs_review"].includes(finalState) || !Array.isArray(references) || references.length > 50 || !references.every((x: unknown) => text(x, 256)) || new Set(references).size !== references.length || Boolean(p.sourceProvider) || Boolean(p.sourceId))
            fail("invalid");
        return ctx.database.transaction(() => {
            const topic = p.topicKey ?? "";
            const existing = topic ? (ctx.database.db.prepare(`SELECT ${cols} FROM observations o WHERE o.project_id=? AND o.scope=? AND o.topic_key=?`).get(project, selectedScope, topic) as Row | undefined) : undefined;
            const now = clock(ctx), id = existing?.id ?? `obs-${randomUUID()}`;
            if (existing?.state === "archived")
                fail("invalid");
            if (existing) {
                ctx.database.db.prepare("UPDATE observations SET title=?,session_id=?,type=?,content=?,topic_key=?,state=?,updated_at=? WHERE id=?").run(title, p.session || null, type, p.content, topic || null, finalState, now, id);
                ctx.database.db.prepare("DELETE FROM observation_refs WHERE observation_id=?").run(id);
                ctx.database.db.prepare("DELETE FROM observations_fts WHERE id=?").run(id);
            }
            else
                ctx.database.db.prepare("INSERT INTO observations(id,title,project_id,session_id,scope,type,content,topic_key,producer,source_provider,source_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)").run(id, title, project, p.session || null, selectedScope, type, p.content, topic || null, "pi", p.sourceProvider ?? "", p.sourceId ?? "", finalState, now, now);
            for (const ref of references)
                ctx.database.db.prepare("INSERT INTO observation_refs(observation_id,target_id) VALUES(?,?)").run(id, ref);
            ctx.database.db.prepare("INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)").run(id, title, topic, type, p.content);
            enqueueObservationChange(ctx, id);
            return entry(getRow(ctx, id, project)!, true);
        });
    }
    if (operation === "memory.recall" || operation === "recall") {
        const p = payload ?? {}, limit = p.limit || 20, states = p.states?.length ? p.states : ["active"];
        if (!Number.isInteger(limit) || limit < 1 || limit > 50 || !Array.isArray(states) || !states.every(state) || !metadata(project, p.type ?? "", p.topicKey ?? ""))
            fail("invalid");
        const clauses = ["o.project_id=?", "o.scope=?", "observations_fts MATCH ?"], args: any[] = [project, selectedScope, query(p.query, Boolean(p.matchAny))];
        if (p.type) {
            clauses.push("o.type=?");
            args.push(p.type);
        }
        if (p.topicKey) {
            clauses.push("o.topic_key=?");
            args.push(p.topicKey);
        }
        clauses.push(`o.state IN (${states.map(() => "?").join(",")})`);
        args.push(...states, limit);
        const rows = ctx.database.db.prepare(`SELECT ${cols} FROM observations o JOIN observations_fts ON observations_fts.id=o.id WHERE ${clauses.join(" AND ")} ORDER BY bm25(observations_fts),o.updated_at DESC,o.id ASC LIMIT ?`).all(...args) as Row[];
        let remaining = 4096;
        return rows.map(r => { const out = entry(r, false); out.preview = [...String(r.content)].slice(0, Math.min(256, remaining)).join(""); remaining -= [...out.preview].length; return out; });
    }
    if (operation === "memory.recent" || operation === "recent") {
        const p = payload ?? {}, limit = p.limit || 20, states = p.states?.length ? p.states : ["active"];
        if (selectedScope !== "project" || !Number.isInteger(limit) || limit < 1 || limit > 50 || !Array.isArray(states) || !states.every(state))
            fail("invalid");
        const rows = ctx.database.db.prepare(`SELECT ${cols} FROM observations o WHERE o.project_id=? AND o.scope=? AND o.state IN (${states.map(() => "?").join(",")}) ORDER BY o.updated_at DESC,o.id ASC LIMIT ?`).all(project, selectedScope, ...states, limit) as Row[];
        let remaining = 4096;
        return rows.map(r => { const out = entry(r, false); out.preview = [...String(r.content)].slice(0, Math.min(256, remaining)).join(""); remaining -= [...out.preview].length; return out; });
    }
    if (operation === "memory.get" || operation === "get") {
        if (!text(payload?.id, 256) || !metadata(project))
            fail("invalid");
        const row = getRow(ctx, payload.id, project, selectedScope);
        if (!row)
            fail("not_found");
        return entry(row, true);
    }
    if (operation === "memory.forget" || operation === "forget") {
        writable(ctx);
        if (!text(payload?.id, 256))
            fail("invalid");
        return ctx.database.transaction(() => { const row = getRow(ctx, payload.id, project, selectedScope); if (!row)
            fail("not_found"); const now = clock(ctx); if (row.state !== "archived")
            ctx.database.db.prepare("UPDATE observations SET state='archived',updated_at=? WHERE id=?").run(now, row.id); ctx.database.db.prepare("DELETE FROM observations_fts WHERE id=?").run(row.id); enqueueObservationChange(ctx, row.id); return entry(getRow(ctx, row.id, project, selectedScope)!, true); });
    }
    fail("invalid");
}
/** Completed Pi sessions write an ordinary project observation through the same boundary. */
export async function remember(ctx: ServiceContext, payload: any) { return dispatchMemory(ctx, "memory.remember", payload); }

import { createHash } from "node:crypto";
import type { HttpPort } from "../ports/http.ts";
import { HttpBodyReadError, HttpResponseInvalid, SyncHttpError } from "../ports/http.ts";
import { validBearer } from "../ports/credentials.ts";

export const syncMediaType = "application/vnd.vgxness.sync+json;version=1";
export type Int64 = number | bigint;
export type Cursor = { history_id: string; position: Int64; watermark?: Int64 };
export type Mutation = { mutation_id: string; record_id: string; record_kind: "project" | "session" | "observation"; kind: "create" | "update" | "archive" | "tombstone" | "resolve"; base_version: Int64; [key: string]: any };
export type PushResult = { mutation_id: string; disposition: "accepted" | "previously_accepted" | "conflict" | "rejected"; retryable: boolean; code: string; sequence?: Int64; version: Int64 };
export type PullChange = { sequence: Int64; canonical_version: Int64; mutation: Mutation; change_hash: string; hash_version?: number; conflict_id?: string; change_disposition?: "accepted" | "conflict" };
export type PullPage = { cursor: Cursor; has_more: boolean; changes?: PullChange[] };
export type Discovery = { protocol_version: number; history_id: string; capabilities: string[] };
export type ProjectState = { status: "active" | "absent"; has_history: boolean; history_generation?: string; watermark?: Int64; active_observations?: Int64 };
const maxInt64 = 9223372036854775807n;
const integer = (v: unknown): v is Int64 => typeof v === "bigint" ? v >= 0n && v <= maxInt64 : typeof v === "number" && Number.isSafeInteger(v) && v >= 0;
const uuid = (v: unknown, canonical = false): v is string => typeof v === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(v) && (!canonical || v === v.toLowerCase());
function fail(): never { throw new TypeError("invalid sync protocol value"); }
function object(v: any, keys: string[], required: string[] = []) {
  if (!v || typeof v !== "object" || Array.isArray(v) || Object.keys(v).some(k => !keys.includes(k)) || required.some(k => v[k] === undefined || v[k] === null)) fail();
}
function stringFields(v: any, keys: string[]) { for (const k of keys) if (v[k] != null && typeof v[k] !== "string") fail(); }
/** Match the Go client's root-only HTTPS endpoint rule before WHATWG path normalization. */
export function normalizeSyncEndpoint(value: string): string {
  if (typeof value !== "string" || value.length > 2048 || !/^https:\/\/[^/?#\\\s]+\/?$/.test(value)) throw new TypeError("invalid sync endpoint");
  if (value.includes("@")) throw new TypeError("invalid sync endpoint");
  const url = new URL(value);
  if (!url.hostname || url.username || url.password || url.host.includes("%")) throw new TypeError("invalid sync endpoint");
  return url.origin;
}
/** JSON parser with duplicate-key/depth rejection and exact int64 lexemes on Node 22. */
export function parseProtocolJSON(source: string): any {
  let pos = 0;
  const ws = () => { while (/[\x20\t\r\n]/.test(source[pos] ?? "x")) pos++; };
  const string = () => {
    const start = pos++; let escaped = false;
    while (pos < source.length) { const c = source[pos++]; if (c === '"' && !escaped) { const value = JSON.parse(source.slice(start, pos)); if (!value.isWellFormed()) fail(); return value; } if (c === "\\" && !escaped) escaped = true; else escaped = false; }
    return fail();
  };
  const value = (depth: number): any => {
    ws(); const c = source[pos];
    if (c === '"') return string();
    if (c === "{" || c === "[") {
      if (depth >= 32) fail(); pos++; ws();
      const result: any = c === "{" ? Object.create(null) : [], end = c === "{" ? "}" : "]";
      if (source[pos] === end) { pos++; return result; }
      while (true) {
        ws();
        if (c === "{") { if (source[pos] !== '"') fail(); const key = string(); ws(); if (source[pos++] !== ":" || Object.hasOwn(result, key)) fail(); result[key] = value(depth + 1); }
        else result.push(value(depth + 1));
        ws(); if (source[pos] === end) { pos++; return result; } if (source[pos++] !== ",") fail();
      }
    }
    for (const [literal, result] of [["true", true], ["false", false], ["null", null]] as const) { if (source.startsWith(literal, pos)) { pos += literal.length; return result; } }
    const token = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/.exec(source.slice(pos))?.[0];
    if (!token) fail(); pos += token.length;
    // Protocol numeric fields are integers; reject exponent/fraction forms rather than round them.
    if (/[.eE]/.test(token)) fail();
    const n = BigInt(token); if (n < -9223372036854775808n || n > maxInt64) fail();
    return n >= BigInt(Number.MIN_SAFE_INTEGER) && n <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(n) : n;
  };
  const result = value(0); ws(); if (pos !== source.length) fail(); return result;
}
/** Go-compatible JSON escaping, without sentinel substitutions that could alter content. */
export function protocolJSON(value: any): string {
  if (typeof value === "bigint") { if (value < -9223372036854775808n || value > maxInt64) fail(); return String(value); }
  if (typeof value === "string") return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, c => `\\u${c.charCodeAt(0).toString(16).padStart(4, "0")}`);
  if (value === null || typeof value === "boolean") return JSON.stringify(value);
  if (typeof value === "number") { if (!Number.isSafeInteger(value)) fail(); return String(value); }
  if (Array.isArray(value)) return `[${value.map(v => protocolJSON(v ?? null)).join(",")}]`;
  if (value && typeof value === "object") return `{${Object.keys(value).filter(k => value[k] !== undefined).map(k => `${protocolJSON(k)}:${protocolJSON(value[k])}`).join(",")}}`;
  return fail();
}
function text(v: unknown, max = 512, required = false, content = false): v is string {
  return typeof v === "string" && v.isWellFormed() && Buffer.byteLength(v) <= max && (!required || !!v.trim()) && !(content ? /[\x00-\x08\x0b-\x1f\x7f]/ : /[\x00-\x1f\x7f]/).test(v);
}
const id = (v: unknown) => text(v, 1024, true);
function time(v: any): string {
  if (typeof v !== "string") return fail();
  const m = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(v);
  if (!m || !Number.isFinite(Date.parse(v)) || /T\d{2}:\d{2}:60/.test(v) || v.startsWith("0001-01-01T00:00:00") && !m[2] && m[3] === "Z") fail();
  const date = new Date(`${m[1]}Z`);
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 19) !== m[1]) fail();
  const fraction = (m[2] ?? "").replace(/0+$/, "");
  if (m[1] === "0001-01-01T00:00:00" && !fraction && ["Z", "+00:00", "-00:00"].includes(m[3])) fail();
  return `${m[1]}${fraction ? `.${fraction}` : ""}${m[3] === "+00:00" || m[3] === "-00:00" ? "Z" : m[3]}`;
}
function instant(v: string): bigint {
  const m = /\.(\d+)(?:Z|[+-]\d\d:\d\d)$/.exec(v); return BigInt(Date.parse(v.replace(/\.\d+/, ""))) * 1_000_000n + BigInt((m?.[1] ?? "").padEnd(9, "0"));
}
function observation(v: any) {
  const keys = ["id", "title", "project_id", "session_id", "scope", "type", "content", "topic_key", "references", "provenance", "lifecycle", "review", "created_at", "updated_at", "review_after"];
  object(v, keys, ["id", "project_id", "scope", "type", "content", "provenance", "lifecycle", "review", "created_at", "updated_at"]);
  stringFields(v, keys.filter(k => !["references", "provenance"].includes(k)));
  if (!id(v.id) || !id(v.project_id) || v.session_id && !id(v.session_id) || !text(v.title ?? "") || !["project", "personal"].includes(v.scope) || !text(v.type, 512, true) || !text(v.content, 65536, true, true) || !text(v.topic_key ?? "") || !["active", "archived"].includes(v.lifecycle) || !["clear", "needs_review"].includes(v.review)) fail();
  object(v.provenance, ["producer", "source_provider", "source_id"], ["producer"]);
  const p = v.provenance; stringFields(p, ["producer", "source_provider", "source_id"]); if (!text(p.producer, 512, true) || ((p.source_provider || p.source_id) && (!text(p.source_provider, 512, true) || !text(p.source_id, 512, true)))) fail();
  const refs = v.references ?? []; if (!Array.isArray(refs) || refs.length > 128 || refs.some((x: unknown) => !id(x)) || new Set(refs).size !== refs.length) fail();
  const created = time(v.created_at), updated = time(v.updated_at); if (instant(updated) < instant(created)) fail();
  return { id: v.id, title: v.title ?? "", project_id: v.project_id, ...(v.session_id ? { session_id: v.session_id } : {}), scope: v.scope, type: v.type, content: v.content, ...(v.topic_key ? { topic_key: v.topic_key } : {}), ...(refs.length ? { references: refs } : {}), provenance: { producer: p.producer, ...(p.source_provider ? { source_provider: p.source_provider } : {}), ...(p.source_id ? { source_id: p.source_id } : {}) }, lifecycle: v.lifecycle, review: v.review, created_at: created, updated_at: updated, ...(v.review_after != null ? { review_after: time(v.review_after) } : {}) };
}
/** Validates and returns the Go struct's canonical field ordering/omitempty representation. */
export function validateMutation(m: Mutation): Mutation {
  object(m, ["mutation_id", "record_id", "record_kind", "kind", "base_version", "project", "session", "observation", "tombstone", "resolution"], ["mutation_id", "record_id", "record_kind", "kind"]);
  stringFields(m, ["mutation_id", "record_id", "record_kind", "kind"]);
  const base = m.base_version ?? 0;
  if (!uuid(m.mutation_id) || !id(m.record_id) || !["project", "session", "observation"].includes(m.record_kind) || !["create", "update", ...(m.record_kind === "observation" ? ["archive", "tombstone", "resolve"] : [])].includes(m.kind) || !integer(base) || (m.kind === "create" ? BigInt(base) !== 0n : BigInt(base) < 1n)) fail();
  if ([m.project, m.session, m.observation, m.tombstone, m.resolution].filter(v => v != null).length !== 1) fail();
  const result: Mutation = { mutation_id: m.mutation_id, record_id: m.record_id, record_kind: m.record_kind, kind: m.kind, base_version: base };
  if (m.kind === "tombstone") { object(m.tombstone, ["deleted_at", "project_id"], ["deleted_at"]); stringFields(m.tombstone, ["deleted_at", "project_id"]); result.tombstone = { deleted_at: time(m.tombstone.deleted_at), ...(m.tombstone.project_id ? { project_id: m.tombstone.project_id } : {}) }; }
  else if (m.kind === "resolve") { object(m.resolution, ["conflict_ids", "observation"], ["conflict_ids", "observation"]); const ids = m.resolution.conflict_ids; if (!Array.isArray(ids) || !ids.length || ids.length > 128 || ids.some((x: unknown) => !uuid(x)) || new Set(ids).size !== ids.length) fail(); const o = observation(m.resolution.observation); if (o.id !== m.record_id || o.lifecycle !== "active" || o.review !== "clear") fail(); result.resolution = { conflict_ids: ids, observation: o }; }
  else if (m.record_kind === "project") { object(m.project, ["id"], ["id"]); if (m.project.id !== m.record_id) fail(); result.project = { id: m.project.id }; }
  else if (m.record_kind === "session") { object(m.session, ["id", "project_id"], ["id", "project_id"]); if (m.session.id !== m.record_id || !id(m.session.project_id)) fail(); result.session = { id: m.session.id, project_id: m.session.project_id }; }
  else { const o = observation(m.observation); if (o.id !== m.record_id || (m.kind === "archive" ? o.lifecycle !== "archived" || o.review !== "clear" : m.kind === "update" ? o.lifecycle !== "active" : o.lifecycle === "archived" && o.review !== "clear")) fail(); result.observation = o; }
  return result;
}
export function canonicalChangeHash(change: PullChange): string {
  stringFields(change, ["change_disposition", "conflict_id", "change_hash"]);
  const m = validateMutation(change.mutation), version = change.hash_version ?? 1;
  if (!integer(change.sequence) || BigInt(change.sequence) < 1n || !integer(change.canonical_version) || BigInt(change.canonical_version) < 1n) fail();
  if (version === 1) { if (change.change_disposition || change.conflict_id || ["tombstone", "resolve"].includes(m.kind)) fail(); }
  else if (version === 2) {
    if (["create", "update"].includes(m.kind)) { if (m.record_kind !== "observation" || change.change_disposition !== "conflict" || !uuid(change.conflict_id)) fail(); }
    else if (!["tombstone", "resolve"].includes(m.kind) || change.change_disposition !== "accepted" || change.conflict_id) fail();
  } else fail();
  return createHash("sha256").update(protocolJSON({ hash_version: version, sequence: change.sequence, canonical_version: change.canonical_version, ...(version === 2 ? { change_disposition: change.change_disposition, conflict_id: change.conflict_id ?? "" } : {}), mutation: m })).digest("hex");
}
const remoteCodes = ["invalid_input", "limit_exceeded", "unsupported_version", "unsupported_semantic", "unavailable", "unauthorized", "revoked", "conflict", "history_error", "cursor_error"];
const resultCodes = ["invalid_device", "revoked", "invalid_input", "unsupported_semantic", "mutation_id_hash_mismatch", "invalid_replay", "invalid_base", "stale_base", "invalid_prerequisite", "topic_collision"];
export class SyncWire {
  readonly base: string;
  private readonly http: HttpPort;
  private readonly bearer: string;
  private readonly signal?: AbortSignal;
  constructor(http: HttpPort, endpointValue: string, bearer: string, signal?: AbortSignal) {
    this.base = normalizeSyncEndpoint(endpointValue); this.http = http; this.bearer = bearer; this.signal = signal;
    if (!validBearer(bearer)) throw new TypeError("sync credential unavailable");
  }
  private async request(operation: string, method: "GET" | "POST", path: string, body?: string, limit = 1 << 20) {
    let response;
    if (this.signal?.aborted) throw new SyncHttpError(operation, "context");
    try { response = await this.http.request({ method, url: `${this.base}${path}`, headers: { authorization: `Bearer ${this.bearer}`, accept: syncMediaType, ...(body === undefined ? {} : { "content-type": syncMediaType }) }, ...(body === undefined ? {} : { body }), maxResponseBytes: limit, ...(this.signal ? { signal: this.signal } : {}) }); }
    catch (error) { throw new SyncHttpError(operation, this.signal?.aborted || (error as any)?.name === "AbortError" || (error as any)?.name === "TimeoutError" ? "context" : error instanceof HttpResponseInvalid ? "invalid" : error instanceof HttpBodyReadError && operation !== "push" ? "remote" : "unavailable", error instanceof HttpBodyReadError ? 200 : 0); }
    if (!response || !Number.isInteger(response.status)) throw new SyncHttpError(operation, "invalid");
    const contentTypes = Object.entries(response.headers ?? {}).filter(([k]) => k.toLowerCase() === "content-type");
    const validBody = typeof response.body === "string" && response.body.isWellFormed() && Buffer.byteLength(response.body) <= limit && contentTypes.length === 1 && contentTypes[0][1] === syncMediaType;
    if (response.status !== 200) {
      let code = "";
      if (validBody && Buffer.byteLength(response.body) <= 1 << 20) { try { const v = parseProtocolJSON(response.body); object(v, ["protocol_version", "error"]); if (v.protocol_version === 1 && remoteCodes.includes(v.error)) code = v.error; } catch {} }
      throw new SyncHttpError(operation, response.status === 401 ? "unauthorized" : response.status === 503 ? "unavailable" : operation === "project_discovery" && response.status === 404 ? "unsupported" : "remote", response.status, code);
    }
    try { if (!validBody) fail(); return parseProtocolJSON(response.body); } catch { throw new SyncHttpError(operation, "invalid", response.status); }
  }
  private validated<T>(operation: string, fn: () => T): T { try { return fn(); } catch { throw new SyncHttpError(operation, "invalid", 200); } }
  async capabilities(): Promise<{ protocol_version: 1; capabilities: string[] }> {
    const v = await this.request("capabilities", "GET", "/v1/sync/capabilities");
    return this.validated("capabilities", () => { object(v, ["protocol_version", "capabilities"]); if (v.protocol_version !== 1 || !Array.isArray(v.capabilities) || !v.capabilities.length || v.capabilities.length > 64 || v.capabilities.some((x: any) => typeof x !== "string" || !x.length || Buffer.byteLength(x) > 64) || new Set(v.capabilities).size !== v.capabilities.length) fail(); return v; });
  }
  async discover(): Promise<Discovery> {
    const v = await this.request("project_discovery", "GET", "/v1/sync/discovery");
    return this.validated("project_discovery", () => { object(v, ["protocol_version", "history_id", "capabilities"]); if (v.protocol_version !== 1 || !uuid(v.history_id, true) || !Array.isArray(v.capabilities) || v.capabilities.length !== 1 || v.capabilities[0] !== "bootstrap_discovery") fail(); return v; });
  }
  async projectState(projectId: string): Promise<ProjectState> {
    if (!uuid(projectId, true)) fail();
    if (!(await this.capabilities()).capabilities.includes("project_state")) throw new SyncHttpError("project_state", "unsupported");
    const v = await this.request("project_state", "GET", `/v1/sync/projects/${projectId}/state`);
    return this.validated("project_state", () => { object(v, ["status", "has_history", "history_generation", "watermark", "active_observations"], ["has_history"]); const w = v.watermark ?? 0, a = v.active_observations ?? 0; if (!["active", "absent"].includes(v.status) || typeof v.has_history !== "boolean" || !integer(w) || !integer(a) || (!v.has_history ? v.status !== "absent" || !!v.history_generation || BigInt(w) !== 0n : !uuid(v.history_generation, true)) || v.status !== "active" && BigInt(a) !== 0n) fail(); return v; });
  }
  async push(items: Mutation[]): Promise<PushResult[]> {
    if (!Array.isArray(items) || !items.length || items.length > 16) fail();
    const canonical = items.map(validateMutation), body = protocolJSON({ protocol_version: 1, items: canonical }); if (Buffer.byteLength(body) > 1 << 20) fail();
    let v: any;
    for (let attempt = 0; ; attempt++) { try { v = await this.request("push", "POST", "/v1/sync/push", body); break; } catch (error) { if (attempt === 1 || !(error instanceof SyncHttpError) || error.kind !== "unavailable") throw error; } }
    return this.validated("push", () => {
      object(v, ["protocol_version", "results"]); if (v.protocol_version !== 1 || !Array.isArray(v.results) || v.results.length !== canonical.length) fail();
      const sequences = new Map<string, string>();
      v.results.forEach((r: any, i: number) => {
        object(r, ["mutation_id", "disposition", "retryable", "code", "sequence", "version"]);
        r.retryable ??= false; r.code ??= ""; r.version ??= 0;
        if (r.mutation_id !== canonical[i].mutation_id || r.retryable !== false || !integer(r.version)) fail();
        if (r.disposition === "rejected") { if (r.sequence != null || BigInt(r.version) !== 0n || !resultCodes.includes(r.code)) fail(); }
        else if (["accepted", "previously_accepted", "conflict"].includes(r.disposition)) {
          if (!integer(r.sequence) || BigInt(r.sequence) < 1n || BigInt(r.version) < 1n || r.code !== "" || r.disposition !== "conflict" && BigInt(r.version) !== BigInt(canonical[i].base_version) + 1n) fail();
          const key = String(r.sequence); if (sequences.has(key) && sequences.get(key) !== r.mutation_id) fail(); sequences.set(key, r.mutation_id);
        } else fail();
      }); return v.results;
    });
  }
  async pull(cursor: Cursor, projectId = "", limit = 10): Promise<PullPage> {
    const watermark = cursor.watermark ?? 0;
    if (!uuid(cursor.history_id) || !integer(cursor.position) || !integer(watermark) || BigInt(watermark) > 0n && BigInt(watermark) < BigInt(cursor.position) || projectId !== "" && !uuid(projectId, true) || !Number.isInteger(limit) || limit < 1 || limit > 25) fail();
    const operation = projectId ? "project_pull" : "pull", q = new URLSearchParams({ history_id: cursor.history_id, after: String(cursor.position), limit: String(limit) });
    if (projectId) q.set("project_id", projectId); if (BigInt(watermark) > 0n) q.set("watermark", String(watermark)); q.sort();
    const v = await this.request(operation, "GET", `/v1/sync/pull?${q}`, undefined, 2 << 20);
    return this.validated(operation, () => {
      object(v, ["protocol_version", "history_id", "project_id", "position", "watermark", "has_more", "changes"], ["protocol_version", "history_id", "position", "has_more"]);
      const w = v.watermark ?? 0, changes = v.changes ?? [];
      if (v.protocol_version !== 1 || v.history_id !== cursor.history_id || (v.project_id ?? "") !== projectId || !integer(v.position) || !integer(w) || BigInt(v.position) < BigInt(cursor.position) || typeof v.has_more !== "boolean" || !Array.isArray(changes) || changes.length > limit || BigInt(watermark) > 0n && BigInt(w) !== BigInt(watermark) || BigInt(w) > 0n && (BigInt(v.position) > BigInt(w) || v.has_more !== (BigInt(v.position) < BigInt(w)))) fail();
      let previous = BigInt(cursor.position);
      for (const c of changes) {
        object(c, ["sequence", "canonical_version", "hash_version", "change_disposition", "conflict_id", "mutation", "change_hash"]);
        if (!integer(c.sequence) || BigInt(c.sequence) <= previous || !projectId && BigInt(c.sequence) !== previous + 1n || BigInt(w) > 0n && BigInt(c.sequence) > BigInt(w) || c.change_hash !== canonicalChangeHash(c)) fail();
        const m = c.mutation, bound = m.project?.id ?? m.session?.project_id ?? m.observation?.project_id ?? m.resolution?.observation?.project_id ?? m.tombstone?.project_id;
        if (projectId && bound !== projectId) fail(); previous = BigInt(c.sequence);
      }
      if (!changes.length && (v.has_more || !projectId && BigInt(v.position) !== BigInt(cursor.position)) || !projectId && changes.length && BigInt(v.position) !== previous || projectId && (v.has_more ? !changes.length || BigInt(v.position) !== previous : BigInt(v.position) !== BigInt(w)) || projectId && (changes.length || BigInt(v.position) > 0n || v.has_more) && BigInt(w) === 0n) fail();
      return { cursor: { history_id: v.history_id, position: v.position, watermark: w }, has_more: v.has_more, changes };
    });
  }
}

import { homedir } from "node:os";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { lstat, mkdir, realpath } from "node:fs/promises";
import { Type } from "typebox";
import { Value } from "typebox/value";
import { SQLiteDatabase } from "../sqlite/node-sqlite.ts";
import { applyMigrations } from "../sqlite/migrations.ts";
import { memorySchemas, memoryOperationSchema } from "../tools/memory.ts";
import { dispatchMemory, resolveProject } from "./memory.ts";
import { dispatchSession } from "./session.ts";
import { resolveModel } from "./model.ts";
import { dispatchSync, type SyncOptions } from "./sync.ts";
import { CredentialFile, validBearer } from "../ports/credentials.ts";
import { FetchHttp } from "../ports/http.ts";
import type { ServiceContext } from "./context.ts";
import { loadManagerContract, resolveRole } from "../orchestration/contract.ts";
export type RuntimeBinding = {
    workspace: string;
    mode: "full" | "read-only";
    role: string;
};
export type DispatcherOptions = RuntimeBinding & {
    storageRoot?: string;
    credentialFile?: string;
    now?: () => bigint;
    sync?: SyncOptions;
};
const managerContract = loadManagerContract();
const reads = new Set(["memory.recall", "memory.recent", "memory.get", "memory.project.resolve", "memory.sync.status", "memory.session.context", "model.resolve"]);
const schemas = new Map<string, any>([["memory.remember", memorySchemas.memory_save], ["memory.recall", memorySchemas.memory_search], ["memory.recent", memorySchemas.memory_recent], ["memory.get", memorySchemas.memory_get], ["memory.forget", memorySchemas.memory_forget]]);
// The direct service keeps Go's zero-value defaults; model-facing tool schemas remain narrow.
for (const name of ["memory.remember", "memory.recall", "memory.recent", "memory.get", "memory.forget"]) {
    const schema = schemas.get(name), properties = { ...schema.properties };
    if (properties.scope)
        properties.scope = Type.Optional(Type.Union([Type.Literal("project"), Type.Literal("")]));
    if (properties.type)
        properties.type = Type.Optional(Type.String());
    if (properties.topicKey)
        properties.topicKey = Type.Optional(Type.String());
    if (properties.state)
        properties.state = Type.Optional(Type.Union([Type.Literal("active"), Type.Literal("needs_review"), Type.Literal("")]));
    if (properties.limit)
        properties.limit = Type.Optional(Type.Integer({ minimum: 0, maximum: 50 }));
    if (properties.states)
        properties.states = Type.Optional(Type.Array(Type.Union([Type.Literal("active"), Type.Literal("needs_review"), Type.Literal("archived")]), { maxItems: 50 }));
    if (properties.references)
        properties.references = Type.Optional(Type.Array(Type.String({ minLength: 1 }), { maxItems: 50 }));
    schemas.set(name, Type.Object(properties, { additionalProperties: false }));
}
for (const [prefix, union] of [["memory", memoryOperationSchema]] as const) {
    for (const variant of (union as any).anyOf) {
        const { operation, ...properties } = variant.properties;
        schemas.set(`${prefix}.${operation.const}`, Type.Object(properties, { additionalProperties: false }));
    }
}
const effort = Type.Union(["low", "medium", "high", "ultra"].map(value => Type.Literal(value)));
const word = Type.String({ minLength: 1, maxLength: 256 });
schemas.set("model.resolve", Type.Object({ plan: effort, catalog: Type.Object({ provider: word, models: Type.Array(Type.Object({ provider: word, id: word, name: word, capability: Type.Optional(Type.Union(["efficient", "balanced", "frontier"].map(value => Type.Literal(value)))), supportedEfforts: Type.Array(effort, { minItems: 1, maxItems: 4 }) }, { additionalProperties: false }), { minItems: 1, maxItems: 3 }) }, { additionalProperties: false }) }, { additionalProperties: false }));
export const operationNames = Object.freeze([...schemas.keys()].sort());
/** In-process boundary: payloads cannot override workspace, project or authority. */
export class NativeDispatcher {
    #closed = false;
    #queue: Promise<unknown> = Promise.resolve();
    private readonly ctx: ServiceContext;
    private readonly identity: {
        dev: number;
        ino: number;
    };
    private readonly sync?: SyncOptions;
    private constructor(ctx: ServiceContext, identity: {
        dev: number;
        ino: number;
    }, sync?: SyncOptions) { this.ctx = ctx; this.identity = identity; this.sync = sync; }
    static async open(options: DispatcherOptions) {
        if (!resolveRole(managerContract, options.role) || !["full", "read-only"].includes(options.mode))
            throw new Error("invalid runtime binding");
        const workspace = await realpath(options.workspace), identity = await lstat(workspace);
        if (!identity.isDirectory() || identity.isSymbolicLink())
            throw new Error("invalid workspace");
        if (options.storageRoot && !isAbsolute(options.storageRoot))
            throw new Error("storage root must be absolute");
        const storageRoot = resolve(options.storageRoot ?? join(homedir(), ".vgxness"));
        for (let path = storageRoot;; path = dirname(path)) {
            try {
                if ((await lstat(path)).isSymbolicLink())
                    throw new Error("storage root has symlink ancestor");
            }
            catch (error: any) {
                if (error?.code !== "ENOENT")
                    throw error;
            }
            if (path === dirname(path))
                break;
        }
        const readOnly = options.mode === "read-only" || options.role !== "manager";
        if (!readOnly)
            await mkdir(storageRoot, { recursive: true, mode: 0o700 });
        const database = new SQLiteDatabase(join(storageRoot, "memory.db"), { readOnly });
        try {
            if (!readOnly)
                applyMigrations(database);
            const ctx: ServiceContext = { database, workspace, storageRoot, project: "", mode: options.mode, role: options.role, now: options.now, sessionSecrets: new Map() };
            ctx.project = resolveProject(ctx);
            const file = options.credentialFile ? new CredentialFile(options.credentialFile) : undefined;
            const credentials = options.sync?.credentials ?? (file ? { get: (reference: string) => reference === "secret://keychain/sync/file" ? file.get(reference) : undefined } : undefined);
            return new NativeDispatcher(ctx, identity, { http: new FetchHttp(), ...(file ? { credentialRef: "secret://keychain/sync/file" } : {}), ...options.sync, credentials });
        }
        catch (error) {
            database.close();
            throw error;
        }
    }
    request(operation: string, payload: unknown, binding: RuntimeBinding, control?: { beforeMutation?: () => void; signal?: AbortSignal }): Promise<any> {
        try {
            if (Buffer.byteLength(JSON.stringify(payload) ?? "", "utf8") > 1048576)
                return Promise.reject(new Error("operation payload exceeds limit"));
            payload = structuredClone(payload);
            binding = { workspace: binding.workspace, mode: binding.mode, role: binding.role };
            control = { beforeMutation: control?.beforeMutation, signal: control?.signal };
        }
        catch {
            return Promise.reject(new Error("invalid operation payload"));
        }
        const work = async () => {
            if (this.#closed)
                throw new Error("runtime closed");
            if (binding.workspace !== this.ctx.workspace || binding.mode !== this.ctx.mode || binding.role !== this.ctx.role)
                throw new Error("runtime binding mismatch");
            const identity = await lstat(this.ctx.workspace);
            if (identity.isSymbolicLink() || identity.dev !== this.identity.dev || identity.ino !== this.identity.ino)
                throw new Error("workspace identity changed");
            const schema = schemas.get(operation);
            if (!schema || !Value.Check(schema, payload))
                throw new Error("invalid operation payload");
            if (!reads.has(operation) && (this.ctx.mode !== "full" || this.ctx.role !== "manager"))
                throw new Error("mutation requires full manager authority");
            control?.signal?.throwIfAborted();
            if (!reads.has(operation))
                control?.beforeMutation?.();
            if (operation === "model.resolve")
                return resolveModel(payload);
            if (operation === "memory.sync.configure") {
                const reference = this.sync?.credentialRef;
                if (!reference || !this.sync?.credentials)
                    throw new Error("explicit sync credential configuration required");
                const bearer = this.sync.credentials.get(reference);
                if (!validBearer(bearer) || bearer.split(".")[1] !== (payload as any).deviceId)
                    throw new Error("sync credential does not match device");
                const previous: any = this.ctx.database.db.prepare("SELECT credential_ref FROM sync_profiles WHERE singleton=1").get();
                if (previous && reference === "secret://keychain/sync/file" && previous.credential_ref !== reference)
                    throw new Error("sync credential profile conflict");
            }
            if (operation.startsWith("memory.sync")) {
                const { sessionSecrets: _leases, ...ctx } = this.ctx;
                return dispatchSync(ctx, operation.slice(7), payload, this.sync);
            }
            if (operation.startsWith("memory.session.")) {
                const value = await dispatchSession(this.ctx, operation, payload);
                if (operation === "memory.session.draft_save")
                    return { Handle: value.handle, Project: value.project, UpdatedAt: value.updatedAt };
                const publicSession = (session: any) => ({ handle: session.handle, state: session.state, checkpointed: session.checkpointed, draftPresent: Boolean(session.draftPresent), ...(session.leaseUntil ? { leaseUntil: session.leaseUntil } : {}), createdAt: session.createdAt, updatedAt: session.updatedAt, ...(session.completedAt ? { completedAt: session.completedAt } : {}), ...(session.finalObservationId ? { finalObservationId: session.finalObservationId } : {}) });
                return operation === "memory.session.context" ? { session: publicSession(value.session), handoff: value.handoff, ...(value.draftUpdatedAt ? { draftUpdatedAt: value.draftUpdatedAt } : {}) } : publicSession(value);
            }
            const value = await dispatchMemory(this.ctx, operation, payload);
            if (operation === "memory.project.resolve" || operation === "memory.project.initialize")
                return value.project;
            const publicEntry = (entry: any) => ({ ID: entry.id, Title: entry.title, Project: entry.project, Type: entry.type, TopicKey: entry.topicKey, Session: entry.session, Producer: entry.producer, SourceProvider: entry.sourceProvider, SourceID: entry.sourceId, Scope: entry.scope, State: entry.state, CreatedAt: entry.createdAt, UpdatedAt: entry.updatedAt, Preview: entry.preview ?? "", Content: entry.content ?? "", References: entry.references?.length ? entry.references : null });
            return Array.isArray(value) ? value.map(publicEntry) : publicEntry(value);
        };
        // Include reads so none observe a partially awaited domain operation.
        const result = this.#queue.then(work);
        this.#queue = result.catch(() => { });
        return result;
    }
    async close() {
        await this.#queue;
        if (!this.#closed) {
            this.#closed = true;
            this.ctx.sessionSecrets?.clear();
            this.ctx.database.close();
        }
    }
}
export const createNativeDispatcher = (options: DispatcherOptions) => NativeDispatcher.open(options);

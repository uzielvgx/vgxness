import { DatabaseSync, type SQLInputValue } from "node:sqlite";
import { constants, closeSync, fchmodSync, fstatSync, lstatSync, openSync } from "node:fs";
import { dirname, resolve } from "node:path";
import type { DatabasePort } from "../ports/database.ts";
import { latestSchemaVersion } from "./migrations.ts";
function parameters(values: unknown[]): SQLInputValue[] {
    return values.map(value => {
        if (value === null || typeof value === "string" || typeof value === "number" || typeof value === "bigint" || ArrayBuffer.isView(value))
            return value as SQLInputValue;
        throw new TypeError("unsupported SQLite parameter");
    });
}
export type SQLiteDatabaseOptions = {
    readOnly?: boolean;
};
/** A single synchronous connection: Node executes calls serially on this instance. */
export class SQLiteDatabase implements DatabasePort {
    readonly db: DatabaseSync;
    readonly path: string;
    readonly readOnly: boolean;
    #closed = false;
    constructor(path: string, options: SQLiteDatabaseOptions = {}) {
        if (!path)
            throw new TypeError("database path is required");
        const absolute = resolve(path);
        let parent = dirname(absolute);
        for (;;) {
            const info = lstatSync(parent);
            if (info.isSymbolicLink() || !info.isDirectory())
                throw new Error("database parent must not be a symlink or non-directory");
            if (parent === dirname(parent))
                break;
            parent = dirname(parent);
        }
        this.path = absolute;
        this.readOnly = options.readOnly === true;
        const inspect = (file: string) => {
            try {
                const info = lstatSync(file);
                if (!info.isFile() || info.isSymbolicLink())
                    throw new Error("database and journal paths must be regular non-symlink files");
                if (process.platform !== "win32" && process.getuid && info.uid !== process.getuid())
                    throw new Error("database file has another owner");
                return info;
            }
            catch (error: any) {
                if (error?.code === "ENOENT")
                    return undefined;
                throw error;
            }
        };
        for (const file of [absolute, `${absolute}-wal`, `${absolute}-shm`])
            inspect(file);
        if (!this.readOnly) {
            try {
                const fd = openSync(absolute, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | (constants.O_NOFOLLOW ?? 0), 0o600);
                closeSync(fd);
            }
            catch (error: any) {
                if (error?.code !== "EEXIST")
                    throw error;
            }
            for (const file of [absolute, `${absolute}-wal`, `${absolute}-shm`]) {
                const before = inspect(file);
                if (!before)
                    continue;
                const fd = openSync(file, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0));
                try {
                    const after = fstatSync(fd);
                    if (!after.isFile() || after.dev !== before.dev || after.ino !== before.ino)
                        throw new Error("database path changed during open");
                    if (process.platform !== "win32")
                        fchmodSync(fd, 0o600);
                }
                finally {
                    closeSync(fd);
                }
            }
        }
        const pinned = inspect(absolute);
        this.db = new DatabaseSync(absolute, { readOnly: this.readOnly, enableForeignKeyConstraints: true, readBigInts: true });
        try {
            const current = inspect(absolute);
            if (!pinned || !current || pinned.dev !== current.dev || pinned.ino !== current.ino)
                throw new Error("database path changed during open");
            const precision = this.db.prepare("SELECT CAST(9007199254740993 AS INTEGER) AS value").get() as any;
            if (precision.value !== 9007199254740993n)
                throw new Error("native SQLite bigint reads unavailable; use Node 22.19+ or a compatible newer runtime");
            const schema = (this.db.prepare("PRAGMA user_version").get() as {
                user_version: bigint;
            }).user_version;
            if (schema > BigInt(latestSchemaVersion))
                throw new Error("database schema is newer than supported");
            this.db.exec("PRAGMA foreign_keys=ON");
            this.db.exec("PRAGMA busy_timeout=1000");
            if (!this.readOnly)
                this.db.exec("PRAGMA journal_mode=WAL");
            for (const file of [`${absolute}-wal`, `${absolute}-shm`])
                inspect(file);
        }
        catch (error) {
            this.db.close();
            this.#closed = true;
            throw error;
        }
    }
    transaction<T>(fn: () => T): T {
        this.assertOpen();
        if (this.readOnly)
            throw new Error("database is read-only");
        this.db.exec("BEGIN IMMEDIATE");
        try {
            const value = fn();
            if (value != null && typeof (value as any).then === "function")
                throw new TypeError("SQLite transactions must be synchronous");
            this.db.exec("COMMIT");
            return value;
        }
        catch (error) {
            try {
                this.db.exec("ROLLBACK");
            }
            catch { }
            throw error;
        }
    }
    prepare(sql: string) { this.assertOpen(); const statement = this.db.prepare(sql); statement.setReadBigInts(true); return statement; }
    query<T = Record<string, unknown>>(sql: string, ...values: unknown[]): T[] { return this.prepare(sql).all(...parameters(values)) as T[]; }
    queryOne<T = Record<string, unknown>>(sql: string, ...values: unknown[]): T | undefined { return this.prepare(sql).get(...parameters(values)) as T | undefined; }
    execute(sql: string, ...values: unknown[]) { return this.prepare(sql).run(...parameters(values)); }
    health(): {
        schemaVersion: bigint;
        foreignKeys: bigint;
    } { this.assertOpen(); const schemaVersion = (this.db.prepare("PRAGMA user_version").get() as any).user_version as bigint; const foreignKeys = (this.db.prepare("PRAGMA foreign_keys").get() as any).foreign_keys as bigint; if (foreignKeys !== 1n)
        throw new Error("foreign keys are disabled"); if (schemaVersion > BigInt(latestSchemaVersion))
        throw new Error("database schema is newer than supported"); if ((this.db.prepare("PRAGMA integrity_check").get() as any).integrity_check !== "ok")
        throw new Error("database integrity check failed"); return { schemaVersion, foreignKeys }; }
    close(): void { if (!this.#closed) {
        this.db.close();
        this.#closed = true;
    } }
    private assertOpen() { if (this.#closed)
        throw new Error("database is closed"); }
}

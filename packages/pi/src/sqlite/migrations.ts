import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { DatabaseSync } from "node:sqlite";
import type { SQLiteDatabase } from "./node-sqlite.ts";
export type Migration = {
    version: number;
    sql: string;
    sha256: string;
    requiresForeignKeysDisabled?: boolean;
};
export const latestSchemaVersion = 23;
const resources = join(dirname(fileURLToPath(import.meta.url)), "../../resources/migrations");
type Manifest = {
    version: number;
    migrations: Array<{
        version: number;
        file: string;
        sha256: string;
    }>;
};
const manifest = JSON.parse(readFileSync(join(resources, "manifest.json"), "utf8")) as Manifest;
export const migrations: readonly Migration[] = Array.from({ length: latestSchemaVersion }, (_, index) => {
    const version = index + 1;
    const filename = `${String(version).padStart(3, "0")}_${["memory", "observation_title", "global_projects", "project_roots", "sdd", "sync", "local_write_versions", "sync_inbox_cursor_conflicts_bootstrap", "sync_push_results", "sync_outbox_claims", "sdd_ultra_plan", "sync_enrollment_recovery", "project_identities", "sync_portable_identities", "sync_portable_identity_adoptions", "sync_project_cursors", "sync_project_repairs", "sync_project_transitions", "sync_project_backup_intents", "provider_sessions", "provider_session_drafts", "observation_fts_metadata", "provider_session_leases"][index]}.sql`;
    const expected = manifest.migrations[index];
    if (!expected || manifest.version !== latestSchemaVersion || expected.version !== version || expected.file !== filename)
        throw new Error("invalid migration manifest");
    const sql = readFileSync(join(resources, filename), "utf8");
    const sha256 = createHash("sha256").update(sql).digest("hex");
    if (sha256 !== expected.sha256)
        throw new Error(`migration resource hash mismatch for version ${version}`);
    return { version, sql, sha256, requiresForeignKeysDisabled: version === 11 };
});
function raw(value: SQLiteDatabase | DatabaseSync): DatabaseSync { return "db" in value ? value.db : value; }
function version(db: DatabaseSync): number { return Number((db.prepare("PRAGMA user_version").get() as {
    user_version: number;
}).user_version); }
function foreignKeyCheck(db: DatabaseSync) { if (db.prepare("PRAGMA foreign_key_check").get() !== undefined)
    throw new Error("migration: foreign key check failed"); }
/** Matches the Go v11 rebuild algorithm: disable FK before BEGIN, then verify and restore it. */
export function applyMigrations(value: SQLiteDatabase | DatabaseSync, steps: readonly Migration[] = migrations): void {
    const db = raw(value);
    const latest = steps.reduce((head, step) => Math.max(head, step.version), 0);
    db.exec("BEGIN IMMEDIATE");
    let inTransaction = true;
    let rebuilding = false;
    try {
        let head = version(db);
        if (head > latest)
            throw new Error(`migration: database schema version ${head} is newer than supported version ${latest}`);
        rebuilding = steps.some((step) => step.version > head && step.requiresForeignKeysDisabled);
        if (rebuilding) {
            db.exec("ROLLBACK");
            inTransaction = false;
            db.exec("PRAGMA foreign_keys=OFF");
            db.exec("BEGIN IMMEDIATE");
            inTransaction = true;
            head = version(db);
            if (head > latest)
                throw new Error(`migration: database schema version ${head} is newer than supported version ${latest}`);
        }
        for (const step of steps)
            if (step.version > head) {
                db.exec(step.sql);
                db.exec(`PRAGMA user_version=${step.version}`);
            }
        if (rebuilding)
            foreignKeyCheck(db);
        db.exec("COMMIT");
        inTransaction = false;
        if (rebuilding) {
            db.exec("PRAGMA foreign_keys=ON");
            if (Number((db.prepare("PRAGMA foreign_keys").get() as {
                foreign_keys: number;
            }).foreign_keys) !== 1)
                throw new Error("migration: foreign keys not enabled");
            foreignKeyCheck(db);
        }
    }
    finally {
        if (inTransaction)
            try {
                db.exec("ROLLBACK");
            }
            catch { }
        if (rebuilding)
            try {
                db.exec("PRAGMA foreign_keys=ON");
            }
            catch { }
    }
}

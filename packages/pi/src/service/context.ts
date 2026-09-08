import type { SQLiteDatabase } from "../sqlite/node-sqlite.ts";
/** Local bindings supplied by the Pi runtime; none are put on the backend wire. */
export interface ServiceContext {
    database: SQLiteDatabase;
    project: string;
    workspace: string;
    storageRoot: string;
    mode: "full" | "read-only";
    role: string;
    now?: () => bigint;
    sessionSecrets?: Map<string, {
        token: string;
        externalId: string;
    }>;
}
export const clock = (ctx: ServiceContext) => ctx.now?.() ?? BigInt(Date.now()) * 1000000n;
export function writable(ctx: ServiceContext) {
    if (ctx.mode !== "full" || ctx.role !== "manager" || ctx.database.readOnly)
        throw new Error("invalid");
}

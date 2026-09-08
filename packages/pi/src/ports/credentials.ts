import { constants, openSync, closeSync, lstatSync, fstatSync, readSync } from "node:fs";
import { dirname, isAbsolute, normalize } from "node:path";
/** Credentials deliberately live outside the SQLite database. */
export interface CredentialPort {
    get(reference: string): string | undefined;
}
export class MapCredentials implements CredentialPort {
    private readonly values: Map<string, string>;
    constructor(values = new Map<string, string>()) { this.values = values; }
    get(reference: string) { return this.values.get(reference); }
    set(reference: string, value: string) { this.values.set(reference, value); }
    delete(reference: string) { this.values.delete(reference); }
}
export function validBearer(value: unknown): value is string {
    if (typeof value !== "string" || value.length !== 85)
        return false;
    const match = /^vgx1\.([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.([A-Za-z0-9_-]{43})$/.exec(value);
    return !!match && match[1] !== "00000000-0000-0000-0000-000000000000" && Buffer.from(match[2], "base64url").toString("base64url") === match[2];
}
/** Linux pins directory descriptors. Other hosts recheck path and file identities before
 * and after reading; Node does not provide a portable atomic openat or Windows ACL API.
 */
export class CredentialFile implements CredentialPort {
    private readonly file: string;
    constructor(file: string) { this.file = file; }
    get(_reference: string): string | undefined {
        if (process.platform !== "linux")
            return this.portableRead();
        const descriptors: number[] = [];
        const same = (a: any, b: any) => a.dev === b.dev && a.ino === b.ino;
        const safe = (s: any) => s.isFile() && (s.mode & 63n) === 0n && s.uid === BigInt(process.getuid!()) && s.size > 0n && s.size <= 514n;
        try {
            if (process.platform !== "linux" || !isAbsolute(this.file))
                return undefined;
            const parts = normalize(this.file).split("/").filter(Boolean);
            if (!parts.length)
                return undefined;
            let dir = openSync("/", constants.O_RDONLY | constants.O_DIRECTORY);
            descriptors.push(dir);
            for (const part of parts.slice(0, -1)) {
                const path = `/proc/self/fd/${dir}/${part}`, before = lstatSync(path, { bigint: true });
                if (!before.isDirectory() || before.isSymbolicLink())
                    return undefined;
                dir = openSync(path, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
                descriptors.push(dir);
                if (!same(before, fstatSync(dir, { bigint: true })))
                    return undefined;
            }
            const leaf = `/proc/self/fd/${dir}/${parts.at(-1)}`, initial = lstatSync(leaf, { bigint: true });
            if (!safe(initial))
                return undefined;
            const fd = openSync(leaf, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
            descriptors.push(fd);
            const before = fstatSync(fd, { bigint: true });
            if (!safe(before) || !same(initial, before))
                return undefined;
            const bytes = Buffer.alloc(515);
            let size = 0;
            while (size < bytes.length) {
                const n = readSync(fd, bytes, size, bytes.length - size, null);
                if (!n)
                    break;
                size += n;
            }
            const after = fstatSync(fd, { bigint: true });
            if (size === 0 || size > 514 || !safe(after) || !same(before, after) || ["uid", "mode", "size", "mtimeNs", "ctimeNs"].some(k => (before as any)[k] !== (after as any)[k]))
                return undefined;
            const value = new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(0, size)).replace(/\r?\n$/, "");
            return value && Buffer.byteLength(value) <= 512 && !/[\r\n]/.test(value) ? value : undefined;
        }
        catch {
            return undefined;
        }
        finally {
            for (const fd of descriptors.reverse()) {
                try {
                    closeSync(fd);
                }
                catch { }
            }
        }
    }
    private portableRead(): string | undefined {
        let fd: number | undefined;
        const parents: Array<{
            path: string;
            dev: bigint;
            ino: bigint;
        }> = [];
        const same = (a: any, b: any) => a.dev === b.dev && a.ino === b.ino;
        const safe = (s: any) => s.isFile() && !s.isSymbolicLink() && s.size > 0n && s.size <= 514n && (process.platform === "win32" || ((s.mode & 63n) === 0n && typeof process.getuid === "function" && s.uid === BigInt(process.getuid())));
        try {
            if (!isAbsolute(this.file))
                return undefined;
            const path = normalize(this.file);
            for (let parent = dirname(path);; parent = dirname(parent)) {
                const info = lstatSync(parent, { bigint: true });
                if (!info.isDirectory() || info.isSymbolicLink())
                    return undefined;
                parents.push({ path: parent, dev: info.dev, ino: info.ino });
                if (parent === dirname(parent))
                    break;
            }
            const initial = lstatSync(path, { bigint: true });
            if (!safe(initial))
                return undefined;
            fd = openSync(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0));
            const before = fstatSync(fd, { bigint: true });
            if (!safe(before) || !same(before, initial))
                return undefined;
            const bytes = Buffer.alloc(515);
            let size = 0;
            while (size < bytes.length) {
                const count = readSync(fd, bytes, size, bytes.length - size, null);
                if (!count)
                    break;
                size += count;
            }
            const after = fstatSync(fd, { bigint: true }), final = lstatSync(path, { bigint: true });
            if (size < 1 || size > 514 || !safe(after) || !safe(final) || !same(initial, final) || ["dev", "ino", "uid", "mode", "size", "mtimeNs", "ctimeNs"].some(key => (before as any)[key] !== (after as any)[key]))
                return undefined;
            for (const parent of parents) {
                const info = lstatSync(parent.path, { bigint: true });
                if (!info.isDirectory() || info.isSymbolicLink() || !same(parent, info))
                    return undefined;
            }
            const value = new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(0, size)).replace(/\r?\n$/, "");
            return value && Buffer.byteLength(value) <= 512 && !/[\r\n]/.test(value) ? value : undefined;
        }
        catch {
            return undefined;
        }
        finally {
            if (fd !== undefined)
                try {
                    closeSync(fd);
                }
                catch { }
        }
    }
}

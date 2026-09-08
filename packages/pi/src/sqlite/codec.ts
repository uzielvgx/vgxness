/** Nanoseconds since the Unix epoch, without Number precision loss. */
export function nowNs(): bigint { return BigInt(Date.now()) * 1000000n; }
/** Go's time.RFC3339Nano representation for an epoch-nanosecond value. */
export function formatTimestamp(ns: bigint): string {
    let seconds = ns / 1000000000n;
    let remainder = ns % 1000000000n;
    if (remainder < 0n) {
        seconds -= 1n;
        remainder += 1000000000n;
    }
    const date = new Date(Number(seconds) * 1000);
    if (!Number.isFinite(date.getTime()))
        throw new RangeError("timestamp is outside JavaScript Date range");
    const iso = date.toISOString();
    if (iso.length !== 24)
        throw new RangeError("timestamp is outside RFC3339 year range");
    const base = iso.slice(0, 19);
    const fraction = remainder.toString().padStart(9, "0").replace(/0+$/, "");
    return `${base}${fraction ? `.${fraction}` : ""}Z`;
}
/** Parses the UTC RFC3339Nano timestamps persisted by the Go implementation. */
export function parseTimestamp(value: string): bigint {
    const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/.exec(value);
    if (!match)
        throw new TypeError("timestamp must be RFC3339Nano UTC");
    const milliseconds = Date.parse(`${match[1]}Z`);
    if (!Number.isFinite(milliseconds) || new Date(milliseconds).toISOString().slice(0, 19) !== match[1])
        throw new TypeError("invalid timestamp");
    const fraction = BigInt((match[2] ?? "").padEnd(9, "0") || "0");
    return BigInt(milliseconds / 1000) * 1000000000n + fraction;
}
export function encodeBlob(value: Uint8Array): Uint8Array { return new Uint8Array(value); }
export function decodeBlob(value: unknown): Uint8Array {
    if (value instanceof Uint8Array)
        return new Uint8Array(value);
    throw new TypeError("SQLite value is not a BLOB");
}
/** Preserve raw JSON bytes; callers decide when parsing is appropriate. */
export function encodeRawJSON(value: string | Uint8Array): Uint8Array {
    return typeof value === "string" ? new TextEncoder().encode(value) : new Uint8Array(value);
}
export function decodeRawJSON(value: unknown): Uint8Array {
    if (value instanceof Uint8Array)
        return new Uint8Array(value);
    if (typeof value === "string")
        return new TextEncoder().encode(value);
    throw new TypeError("SQLite value is not raw JSON");
}

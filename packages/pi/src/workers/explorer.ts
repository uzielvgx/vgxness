import { lstat, open, opendir } from "node:fs/promises";
import { isAbsolute, relative, resolve } from "node:path";
import { createHash } from "node:crypto";
import { workerTargetLedger, type WorkerMission } from "./mission.ts";

/** Cumulative input and output budgets; UTF-8 bytes conservatively bound tokens. */
export function createExplorerTools(mission: WorkerMission) {
  if (!mission.exploration) return [];
  workerTargetLedger(mission);
  const budget = mission.exploration;
  let bytes = 0, tokens = 0, files = 0;
  const identities = new Map<string, string>();
  async function checked(path: string) {
    if (typeof path !== "string" || isAbsolute(path) || path.split(/[\\/]/).includes("..")) throw new Error("explorer path rejected");
    const full = resolve(mission.workspace, path);
    if (!budget.roots.some(root => { const rel = relative(resolve(mission.workspace, root), full); return rel === "" || (rel !== ".." && !rel.startsWith("../") && !isAbsolute(rel)); })) throw new Error("explorer path outside allowed roots");
    let ancestor = resolve(mission.workspace);
    for (const part of ["", ...relative(ancestor, full).split(/[\\/]/).filter(Boolean)]) {
      ancestor = resolve(ancestor, part); const info = await lstat(ancestor);
      if (info.isSymbolicLink()) throw new Error("explorer symlink rejected");
      if (info.isDirectory()) { const identity = `${info.dev}:${info.ino}`; if (identities.has(ancestor) && identities.get(ancestor) !== identity) throw new Error("explorer directory identity changed"); identities.set(ancestor, identity); }
    }
    return full;
  }
  function response(result: unknown, diagnostics: string[] = []) {
    const text = JSON.stringify(result), cost = Buffer.byteLength(text);
    if (tokens + cost > budget.maxTokens) throw new Error("explorer output token budget exhausted");
    tokens += cost;
    return { content: [{ type: "text", text }], details: { usage: { files, bytes, tokens, tokenAccounting: "utf8-byte-upper-bound" }, diagnostics } };
  }
  // Re-read and charge the complete bounded file on each page, so stale cursors
  // are detected even when content changed without changing file identity.
  async function read(path: string) {
    if (files >= budget.maxFiles || bytes >= budget.maxBytes) throw new Error("explorer input budget exhausted");
    const full = await checked(path), info = await lstat(full);
    if (!info.isFile() || info.size > budget.maxBytes - bytes) throw new Error("explorer file exceeds input budget");
    const handle = await open(full, "r");
    try {
      const opened = await handle.stat(); if (opened.dev !== info.dev || opened.ino !== info.ino) throw new Error("explorer file identity changed");
      const buffer = Buffer.alloc(Math.min(info.size + 1, budget.maxBytes - bytes + 1));
      let count = 0;
      while (count < buffer.length) { const read = await handle.read(buffer, count, buffer.length - count, count); if (!read.bytesRead) break; count += read.bytesRead; }
      bytes += count; files++;
      if (count > info.size || bytes > budget.maxBytes) throw new Error("explorer file changed while reading");
      const data = buffer.subarray(0, count); if (data.includes(0)) throw new Error("explorer binary file rejected"); new TextDecoder("utf-8", { fatal: true }).decode(data); return data;
    } finally { await handle.close(); }
  }
  const schema = (properties: any, required: string[]) => ({ type: "object", properties, required, additionalProperties: false });
  const path = { type: "string", maxLength: 512 }, cursor = { type: "string", maxLength: 1024 };
  function offset(cursor: string | undefined, digest: string) { if (!cursor) return 0; const [hash, number] = cursor.split(":"); const at = Number(number); if (hash !== digest || !Number.isSafeInteger(at) || at < 0) throw new Error("explorer stale cursor"); return at; }
  const tools = [
    { name: "worker_list", label: "List authorized directory", parameters: schema({ path, cursor }, ["path"]), async execute(_id: string, input: any) {
      const full = await checked(input.path); if (!(await lstat(full)).isDirectory()) throw new Error("explorer directory required");
      const entries: any[] = [], directory = await opendir(full);
      let capped = false;
      for await (const entry of directory) {
        if (entries.length >= budget.maxFiles) { capped = true; break; }
        entries.push({ name: entry.name, type: entry.isSymbolicLink() ? "symlink" : entry.isDirectory() ? "directory" : entry.isFile() ? "file" : "other" });
      }
      entries.sort((a,b)=>Buffer.compare(Buffer.from(a.name),Buffer.from(b.name)));
      const digest = createHash("sha256").update(input.path + JSON.stringify(entries)).digest("hex");
      const start = offset(input.cursor, digest);
      const remaining = Math.max(0, budget.maxFiles - files);
      const count = Math.min(50, remaining), page = entries.slice(start, start + count);
      files += page.length;
      const exhausted = files >= budget.maxFiles;
      return response({ entries: page.filter(e=>e.type !== "symlink"), nextCursor: !exhausted && start + count < entries.length ? `${digest}:${start + count}` : null }, [...(capped ? ["directory truncated by file budget"] : []), ...(exhausted ? ["file budget exhausted"] : [])]);
    } },
    { name: "worker_read_page", label: "Read authorized file page", parameters: schema({ path, cursor, limit: { type: "integer", minimum: 1, maximum: 4096 } }, ["path"]), async execute(_id: string, input: any) {
      const data = await read(input.path), digest = createHash("sha256").update(input.path).update("\0").update(data).digest("hex"), start = offset(input.cursor, digest), limit = input.limit ?? 2048;
      if (!Number.isInteger(limit) || limit < 1 || limit > 4096) throw new Error("explorer limit rejected");
      const chars = Array.from(data.toString("utf8")), text = chars.slice(start, start + limit).join(""), end = start + Array.from(text).length;
      return response({ path: input.path, digest, text, nextCursor: end < chars.length ? `${digest}:${end}` : null });
    } },
    { name: "worker_search", label: "Search authorized file", parameters: schema({ path, query: { type: "string", minLength: 1, maxLength: 256 }, cursor }, ["path", "query"]), async execute(_id: string, input: any) {
      if (typeof input.query !== "string" || !input.query.length || input.query.length > 256) throw new Error("explorer query rejected");
      const data = await read(input.path), digest = createHash("sha256").update(input.path).update("\0").update(input.query).update("\0").update(data).digest("hex"), lines = data.toString("utf8").split("\n"), start = offset(input.cursor, digest);
      const matches: any[] = []; let next = null;
      for (let i = start; i < lines.length; i++) if (lines[i].includes(input.query)) { if (matches.length === 20) { next = `${digest}:${i}`; break; } matches.push({ line: i + 1, text: Array.from(lines[i]).slice(0, 256).join("") }); }
      return response({ path: input.path, digest, matches, nextCursor: next });
    } },
  ];
  return tools.map(tool => ({ ...tool, async execute(id: string, input: any) {
    if (!input || typeof input !== "object" || Array.isArray(input) || Object.keys(input).some(key => !(key in tool.parameters.properties)) || typeof input.path !== "string" || input.path.length > 512 || (input.cursor !== undefined && (typeof input.cursor !== "string" || input.cursor.length > 1024))) throw new Error("invalid explorer input");
    return await tool.execute(id, input);
  } }));
}

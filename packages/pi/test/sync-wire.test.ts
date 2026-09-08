import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, chmodSync, symlinkSync, rmSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SyncWire, syncMediaType, normalizeSyncEndpoint, canonicalChangeHash, protocolJSON, parseProtocolJSON, validateMutation, type Mutation } from "../src/service/sync-wire.ts";
import { CredentialFile, MapCredentials, validBearer } from "../src/ports/credentials.ts";
import { FetchHttp, SyncHttpError, HttpResponseInvalid } from "../src/ports/http.ts";
const project = "550e8400-e29b-41d4-a716-446655440000", history = "123e4567-e89b-12d3-a456-426614174000", mutationId = "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91";
const bearer = `vgx1.${project}.${Buffer.alloc(32, 1).toString("base64url")}`;
const mutation = (): Mutation => ({ mutation_id: mutationId, record_id: project, record_kind: "project", kind: "create", base_version: 0, project: { id: project } });
const response = (v: unknown, status = 200) => ({ status, headers: { "content-type": syncMediaType }, body: protocolJSON(v) });
const mock = (fn: (request: any) => any) => new SyncWire({ async request(r) { return fn(r); } }, "https://sync.example.test", bearer);
const accepted = (m = mutation(), sequence: number | bigint = 1) => ({ protocol_version: 1, results: [{ mutation_id: m.mutation_id, disposition: "accepted", retryable: false, code: "", sequence, version: BigInt(m.base_version) + 1n }] });
const observation = () => ({ id: "observation", title: "", project_id: project, scope: "project", type: "note", content: "line\n<&>\u2028@@sync-int:123", provenance: { producer: "test" }, lifecycle: "active", review: "clear", created_at: "2026-01-01T00:00:00.123456789Z", updated_at: "2026-01-01T00:00:00.123456789Z" });

test("request binds endpoint, exact headers, canonical body and ordered results", async () => {
  const calls: any[] = []; const wire = mock(r => { calls.push(r); return response(accepted()); });
  assert.equal((await wire.push([mutation()]))[0].version, 1);
  assert.equal(calls[0].url, "https://sync.example.test/v1/sync/push");
  assert.deepEqual(calls[0].headers, { authorization: `Bearer ${bearer}`, accept: syncMediaType, "content-type": syncMediaType });
  assert.deepEqual(JSON.parse(calls[0].body), { protocol_version: 1, items: [mutation()] });
});
test("endpoint validation rejects normalization tricks before URL parsing", () => {
  assert.equal(normalizeSyncEndpoint("https://EXAMPLE.test:443/"), "https://example.test");
  for (const endpoint of ["http://example.test", "https://u:p@example.test", "https://@example.test", "https://example.test/a/..", "https://example.test/%2f", "https://example.test/?", "https://example.test/#", "https://example.test\\", " https://example.test", "https://example.test/\n", "https://example.test/@"]) assert.throws(() => normalizeSyncEndpoint(endpoint), undefined, endpoint);
});
test("canonical bearer validation rejects malformed and noncanonical secrets", () => {
  assert.ok(validBearer(bearer));
  for (const token of ["token", bearer + "\n", bearer.replace("vgx1", "vgx2"), bearer.replace(project, project.toUpperCase()), bearer.slice(0, -1) + "B"]) assert.equal(validBearer(token), false);
  assert.throws(() => new SyncWire({ request: async () => response({}) }, "https://example.test", "token"), /credential/);
});
test("strict parser rejects duplicate escaped keys, unknown fields, depth and trailing data", async () => {
  for (const body of ['{"protocol_version":1,"protocol_\\u0076ersion":1,"capabilities":["push"]}', '{"protocol_version":1,"capabilities":["push"],"private":"secret"}', '{"protocol_version":1,"capabilities":["push"]} {}', '{"protocol_version":1.0,"capabilities":["push"]}', '{"protocol_version":1,"capabilities":["push"],"x":' + '['.repeat(33) + '0' + ']'.repeat(33) + '}', '{"protocol_version":1,"capabilities":["\\ud800"]}']) {
    await assert.rejects(() => mock(() => ({ ...response({}), body })).capabilities(), (e: any) => e.kind === "invalid" && !e.message.includes("secret"));
  }
  assert.throws(() => parseProtocolJSON('{"x":1,"x":2}'));
  assert.throws(() => parseProtocolJSON('{"__proto__":{},"__proto__":{}}'));
});
test("int64 parser and encoder never round or substitute strings", () => {
  const v = parseProtocolJSON('{"value":9223372036854775807,"s":"@@sync-int:123"}');
  assert.equal(v.value, 9223372036854775807n);
  assert.equal(protocolJSON(v), '{"value":9223372036854775807,"s":"@@sync-int:123"}');
  for (const bad of ["9223372036854775808", "1e0", "0.1", "01"]) assert.throws(() => parseProtocolJSON(bad));
  assert.throws(() => protocolJSON(9007199254740992));
});
test("push retries transport and 503 exactly once; never retries authentication, other statuses or invalid replies", async () => {
  for (const mode of ["transport", 503]) {
    let calls = 0; const wire = mock(() => { if (++calls === 1) { if (mode === "transport") throw new Error("private"); return response({ protocol_version: 1, error: "unavailable" }, 503); } return response(accepted()); });
    await wire.push([mutation()]); assert.equal(calls, 2);
  }
  for (const status of [201, 301, 401, 403, 408, 429, 500, 502, 504]) {
    let calls = 0;
    await assert.rejects(() => mock(() => { calls++; return response({ protocol_version: 1, error: "revoked" }, status); }).push([mutation()]), (e: any) => e.status === status && e.kind === (status === 401 ? "unauthorized" : "remote") && e.code === "revoked");
    assert.equal(calls, 1);
  }
  let calls = 0; await assert.rejects(() => mock(() => { calls++; throw new Error("secret"); }).push([mutation()]), /unavailable/); assert.equal(calls, 2);
  calls = 0; await assert.rejects(() => mock(() => { calls++; return response({}); }).push([mutation()]), /invalid/); assert.equal(calls, 1);
});
test("push validates semantic payloads and results before accepting state", async () => {
  let calls = 0; const wire = mock(() => { calls++; return response(accepted()); });
  for (const invalid of [{ ...mutation(), project: undefined }, { ...mutation(), base_version: 1 }, { ...mutation(), session: { id: "s", project_id: project } }, { ...mutation(), project: { id: "other" } }, { ...mutation(), extra: true }]) await assert.rejects(() => wire.push([invalid]));
  assert.equal(calls, 0);
  for (const patch of [{ mutation_id: history }, { sequence: 0 }, { version: 2 }, { retryable: true }, { code: "secret" }, { disposition: "unknown" }, { extra: true }]) {
    const v = accepted(); Object.assign(v.results[0], patch); await assert.rejects(() => mock(() => response(v)).push([mutation()]), /invalid/);
  }
  const second = { ...mutation(), mutation_id: history }, v = accepted(); v.results.push({ ...v.results[0], mutation_id: history }); await assert.rejects(() => mock(() => response(v)).push([mutation(), second]), /invalid/);
  const rejected = { protocol_version: 1, results: [{ mutation_id: mutationId, disposition: "rejected", version: 0, retryable: false, code: "stale_base" }] }; assert.equal((await mock(() => response(rejected)).push([mutation()]))[0].code, "stale_base");
  const large = { ...mutation(), kind: "update" as const, base_version: 9007199254740993n }; assert.equal((await mock(() => response(accepted(large, 9223372036854775807n))).push([large]))[0].version, 9007199254740994n);
});
test("Go v1 golden hash is unchanged with explicit or implicit hash version", () => {
  const m = { ...mutation(), record_id: "project", project: { id: "project" } };
  for (const hash_version of [undefined, 1]) assert.equal(canonicalChangeHash({ sequence: 1, canonical_version: 1, mutation: m, hash_version, change_hash: "" }), "11d715b99da25ca73ef871a74b0543901f57937632ca7ea47eee5ec4157bac08");
});
test("mutation validation enforces observation fields, times, references and versioned conflict rules", () => {
  const m: Mutation = { ...mutation(), record_id: "observation", record_kind: "observation", project: undefined, observation: observation() };
  assert.equal(validateMutation(m).observation.content, m.observation.content);
  for (const patch of [{ references: ["a", "a"] }, { content: "secret\u0000" }, { type: "" }, { updated_at: "2026-01-01T00:00:00.123456788Z" }, { provenance: { producer: "x", source_id: "a" } }, { lifecycle: "tombstoned" }, { created_at: "2026-02-30T00:00:00Z" }, { session_id: 0 }, { extra: true }]) assert.throws(() => validateMutation({ ...m, observation: { ...observation(), ...patch } }));
  const c = { sequence: 1, canonical_version: 1, mutation: m, change_hash: "", hash_version: 2, change_disposition: "conflict" as const, conflict_id: history };
  assert.match(canonicalChangeHash(c), /^[a-f0-9]{64}$/);
  assert.throws(() => canonicalChangeHash({ ...c, conflict_id: "" }));
  assert.throws(() => canonicalChangeHash({ ...c, hash_version: 1 }));
  const t: Mutation = { ...m, kind: "tombstone", base_version: 1, observation: undefined, tombstone: { deleted_at: "2026-01-01T00:00:00Z", project_id: project } };
  assert.throws(() => canonicalChangeHash({ ...c, mutation: t, hash_version: 1, change_disposition: undefined, conflict_id: undefined }));
  assert.match(canonicalChangeHash({ ...c, mutation: t, change_disposition: "accepted", conflict_id: "" }), /^[a-f0-9]{64}$/);
});
test("flat sparse project pull binds cursor, watermark, hash and project and uses exact Go query", async () => {
  const change = { sequence: 7, canonical_version: 1, mutation: mutation(), change_hash: "" }; change.change_hash = canonicalChangeHash(change);
  const page = { protocol_version: 1, history_id: history, project_id: project, position: 10, watermark: 10, has_more: false, changes: [change] };
  const calls: any[] = []; const got = await mock(r => { calls.push(r); return response(page); }).pull({ history_id: history, position: 3 }, project);
  assert.deepEqual(got.cursor, { history_id: history, position: 10, watermark: 10 });
  assert.equal(calls[0].url, `https://sync.example.test/v1/sync/pull?after=3&history_id=${history}&limit=10&project_id=${project}`);
  for (const patch of [{ history_id: project }, { project_id: history }, { position: 9 }, { has_more: true }, { watermark: 6 }, { changes: [{ ...change, change_hash: "0".repeat(64) }] }, { changes: [{ ...change, extra: true }] }, { changes: [] , has_more: true }]) await assert.rejects(() => mock(() => response({ ...page, ...patch })).pull({ history_id: history, position: 3 }, project), /invalid/);
  await assert.rejects(() => mock(() => response(page)).pull({ history_id: history, position: 3, watermark: 11 }, project), /invalid/);
  await assert.rejects(() => mock(() => response(page)).pull({ history_id: history, position: 0 }, project, 26));
});
test("unscoped pull requires contiguous history; empty project pages may advance to watermark; large positions stay exact", async () => {
  const page = { protocol_version: 1, history_id: history, position: 0, has_more: false };
  assert.equal((await mock(() => response(page)).pull({ history_id: history, position: 0 })).cursor.position, 0);
  await assert.rejects(() => mock(() => response({ ...page, position: 1 })).pull({ history_id: history, position: 0 }), /invalid/);
  const large = 9007199254740993n;
  assert.equal((await mock(() => response({ ...page, project_id: project, position: large, watermark: large })).pull({ history_id: history, position: 0 }, project)).cursor.position, large);
});
test("capabilities, discovery and project state negotiate strict authenticated representations", async () => {
  for (const capabilities of [[], ["a", "a"], ["a".repeat(65)], [1]]) await assert.rejects(() => mock(() => response({ protocol_version: 1, capabilities })).capabilities(), /invalid/);
  assert.equal((await mock(() => response({ protocol_version: 1, history_id: history, capabilities: ["bootstrap_discovery"] })).discover()).history_id, history);
  await assert.rejects(() => mock(() => response({ protocol_version: 1, history_id: history, capabilities: ["push"] })).discover(), /invalid/);
  await assert.rejects(() => mock(() => response({}, 404)).discover(), (e: any) => e.kind === "unsupported");
  const calls: string[] = []; const wire = mock(r => { calls.push(r.url); return response(r.url.endsWith("capabilities") ? { protocol_version: 1, capabilities: ["project_state"] } : { status: "active", has_history: true, history_generation: history, watermark: 2, active_observations: 1 }); });
  assert.equal((await wire.projectState(project)).status, "active"); assert.equal(calls[1], `https://sync.example.test/v1/sync/projects/${project}/state`);
  await assert.rejects(() => mock(() => response({ protocol_version: 1, capabilities: ["push"] })).projectState(project), /unsupported/);
});
test("response bounds, exact content type and cancellation fail closed", async () => {
  for (const headers of [undefined, { "content-type": "application/json" }, { "content-type": syncMediaType, "Content-Type": syncMediaType }, { "content-type": syncMediaType + ";charset=utf-8" }]) await assert.rejects(() => mock(() => ({ ...response({ protocol_version: 1, capabilities: ["push"] }), headers })).capabilities(), /invalid/);
  await assert.rejects(() => mock(() => ({ ...response({}), body: " ".repeat((1 << 20) + 1) })).capabilities(), /invalid/);
  const controller = new AbortController(); controller.abort(); let called = false;
  await assert.rejects(() => new SyncWire({ request: async () => { called = true; return response({}); } }, "https://example.test", bearer, controller.signal).push([mutation()]), (e: any) => e.kind === "context"); assert.equal(called, false);
});
test("FetchHttp streams bounded UTF-8 and disables credential-bearing redirects", async () => {
  let options: any;
  const http = new FetchHttp((async (_url: any, init: any) => { options = init; return new Response("{}", { headers: { "content-type": syncMediaType } }); }) as typeof fetch);
  assert.equal((await http.request({ method: "GET", url: "https://example.test", headers: {} })).body, "{}");
  assert.equal(options.redirect, "manual"); assert.ok(options.signal instanceof AbortSignal);
  await assert.rejects(() => http.request({ method: "GET", url: "https://example.test", headers: {}, maxResponseBytes: 1 }), HttpResponseInvalid);
  const invalid = new FetchHttp((async () => new Response(new Uint8Array([0xff]))) as typeof fetch);
  await assert.rejects(() => invalid.request({ method: "GET", url: "https://example.test", headers: {} }), HttpResponseInvalid);
});
test("credential files enforce line and metadata rules; injected credentials need no filesystem", () => {
  const map = new MapCredentials(); map.set("ref", bearer); assert.equal(map.get("ref"), bearer); map.delete("ref"); assert.equal(map.get("ref"), undefined);
  const dir = mkdtempSync(join(tmpdir(), "pi-wire-credential-")), file = join(dir, "credential");
  try {
    writeFileSync(file, bearer + "\r\n", { mode: 0o600 }); assert.equal(new CredentialFile(file).get("ref"), process.platform === "linux" ? bearer : undefined);
    if (process.platform !== "linux") return;
    chmodSync(file, 0o644); assert.equal(new CredentialFile(file).get("ref"), undefined); chmodSync(file, 0o600);
    symlinkSync(file, join(dir, "link")); assert.equal(new CredentialFile(join(dir, "link")).get("ref"), undefined);
    mkdirSync(join(dir, "nested")); writeFileSync(join(dir, "nested", "credential"), bearer, { mode: 0o600 }); symlinkSync(join(dir, "nested"), join(dir, "alias")); assert.equal(new CredentialFile(join(dir, "alias", "credential")).get("ref"), undefined);
    for (const content of ["", bearer + "\n\n", bearer + "\r", "x".repeat(515)]) { writeFileSync(file, content); assert.equal(new CredentialFile(file).get("ref"), undefined); }
    writeFileSync(file, " token \n"); assert.equal(new CredentialFile(file).get("ref"), " token ");
    assert.equal(new CredentialFile("relative").get("ref"), undefined); assert.equal(new CredentialFile(dir).get("ref"), undefined);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test("FetchHttp timeouts cancel work and remote error bodies cannot hide authentication status", async () => {
  const timeoutFetch = (async (_url: any, init: any) => new Promise((_resolve, reject) => {
    const keepAlive = setTimeout(() => reject(new Error("timeout was not delivered")), 1000);
    init.signal.addEventListener("abort", () => { clearTimeout(keepAlive); reject(init.signal.reason); }, { once: true });
  })) as typeof fetch;
  const wire = new SyncWire(new FetchHttp(timeoutFetch, 5), "https://example.test", bearer);
  await assert.rejects(() => wire.capabilities(), (e: any) => e.kind === "context");
  const unauthorized = new SyncWire(new FetchHttp((async () => new Response(new Uint8Array([0xff]), { status: 401 })) as typeof fetch), "https://example.test", bearer);
  await assert.rejects(() => unauthorized.capabilities(), (e: any) => e.kind === "unauthorized" && e.status === 401);
  let calls = 0;
  const broken = new FetchHttp((async () => { calls++; return new Response(new ReadableStream({ start(controller) { controller.error(new Error("private stream failure")); } }), { headers: { "content-type": syncMediaType } }); }) as typeof fetch);
  await assert.rejects(() => new SyncWire(broken, "https://example.test", bearer).capabilities(), (e: any) => e.kind === "remote" && !e.message.includes("private"));
  assert.equal(calls, 1);
  await assert.rejects(() => new SyncWire(broken, "https://example.test", bearer).push([mutation()]), (e: any) => e.kind === "unavailable");
  assert.equal(calls, 3);
});

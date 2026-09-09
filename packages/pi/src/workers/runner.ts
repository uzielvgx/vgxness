import { workerPrompt } from "./context.ts";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { closeSync, openSync, readSync, unlinkSync, writeSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { advanceWorkerTargets, bootstrapWorkerMission, readWorkerTarget, validateWorkerArgv, workerTargetLedger, type WorkerMission } from "./mission.ts";
export { bootstrapWorkerMission } from "./mission.ts";
import { createExplorerTools } from "./explorer.ts";
import { StringDecoder } from "node:string_decoder";
import { workerCanWrite } from "./roles.ts";
import { PiWorkerTransportError, type PiWorkerTransportReport } from "./result.ts";
type PendingTerminal = { resolve: (value: string) => void; reject: (error: Error) => void; timer: NodeJS.Timeout };
export type WorkerAuthority = Readonly<{ expiresAt: number; revocationFd: number }>;
export function createWorkerMutationGuard(authority: WorkerAuthority, now: () => number = Date.now) {
  if (!Number.isFinite(authority?.expiresAt) || authority.expiresAt <= 0 || !Number.isInteger(authority?.revocationFd) || authority.revocationFd < 0) throw new Error("worker mutation authority invalid");
  const { expiresAt, revocationFd } = authority;
  return () => {
    if (now() >= expiresAt) throw new Error("worker mutation authority expired");
    const value = Buffer.alloc(1);
    try { if (readSync(revocationFd, value, 0, 1, 0) !== 1 || value[0] !== 0) throw new Error("worker mutation authority revoked"); }
    catch (error) { throw error instanceof Error && /revoked/.test(error.message) ? error : new Error("worker mutation authority unavailable"); }
    if (now() >= expiresAt) throw new Error("worker mutation authority expired");
  };
}
type Scheduled = { mission: WorkerMission; work: (signal?: AbortSignal) => Promise<any>; resolve: (value: any) => void; reject: (error: unknown) => void; signal?: AbortSignal; cancelled: boolean };
export class WorkerRunner {
  #activeWriters = 0;
  #readers = 0;
  #queue: Scheduled[] = [];
  run<T>(mission: WorkerMission, work: (signal?: AbortSignal) => Promise<T>, signal?: AbortSignal): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const item: Scheduled = { mission, work, resolve, reject, signal, cancelled: Boolean(signal?.aborted) };
      if (item.cancelled) return reject(new Error("worker cancelled before launch"));
      signal?.addEventListener("abort", () => {
        item.cancelled = true;
        const index = this.#queue.indexOf(item);
        if (index >= 0) { this.#queue.splice(index, 1); reject(new Error("worker cancelled before launch")); }
      }, { once: true });
      this.#queue.push(item); this.#drain();
    });
  }
  #drain() {
    if (this.#activeWriters || !this.#queue.length) return;
    while (this.#queue[0]?.cancelled) this.#queue.shift();
    const next = this.#queue[0]; if (!next) return;
    if (workerCanWrite(next.mission.role)) {
      if (this.#readers) return;
      this.#queue.shift(); this.#start(next, true); return;
    }
    while (this.#queue.length && !workerCanWrite(this.#queue[0].mission.role)) this.#start(this.#queue.shift()!, false);
  }
  #start(item: Scheduled, writer: boolean) {
    if (item.cancelled || item.signal?.aborted) { item.reject(new Error("worker cancelled before launch")); this.#drain(); return; }
    if (writer) this.#activeWriters++; else this.#readers++;
    Promise.resolve().then(() => item.work(item.signal)).then((value) => { if (item.signal?.aborted) item.reject(new Error("worker cancelled")); else item.resolve(value); }, item.reject).finally(() => { if (writer) this.#activeWriters--; else this.#readers--; this.#drain(); });
  }
}
export class PiRpcRunner {
  #child: ChildProcess;
  #lifetime: any;
  #buffer = "";
  #decoder = new StringDecoder("utf8");
  #text = "";
  #partialText = "";
  #usage: Record<string, number> = {};
  #diagnostics: string[] = [];
  #failure?: string;
  #truncated = false;
  #dead = false;
  #waiters = new Map<string, { resolve: (value: any) => void; reject: (error: Error) => void; timer: NodeJS.Timeout }>();
  #closed = false;
  #stopping?: Promise<void>;
  #recoveryPending = false;
  #recoveryRefs: string[] = [];
  #selectedModel = "unknown";
  #selectedEffort = "unknown";
  #effectiveModel = "unknown";
  #effectiveProvider = "unknown";
  #effectiveEffort = "unknown";
  #terminal?: PendingTerminal;
  #exit: Promise<void>;
  constructor(options: { node?: string; cli: string; extension: string; cwd: string; profile: string; auth?: unknown; authorityFd?: number }) {
    if (process.platform === "win32") throw new Error("worker process tree ownership is unsupported on Windows");
    const auth = JSON.stringify(options.auth ?? null);
    if (Buffer.byteLength(auth, "utf8") > 65536) throw new Error("worker auth record too large");
    this.#child = spawn(options.node ?? process.execPath, [options.cli, "--mode", "rpc", "--offline", "--no-session", "--no-extensions", "-e", options.extension, "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-builtin-tools"], { cwd: options.cwd, shell: false, stdio: ["pipe", "pipe", "pipe", "pipe", "pipe", options.authorityFd ?? "ignore"], detached: true, env: { ...Object.fromEntries(["PATH", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR"].filter(key => process.env[key] !== undefined).map(key => [key, process.env[key]])), HOME: options.profile, PI_CODING_AGENT_DIR: options.profile, PI_CODING_AGENT_SESSION_DIR: options.profile } });
    this.#exit = new Promise((resolve) => this.#child.once("exit", () => {
      this.#dead = true; this.#fail(new Error("worker RPC closed"));
      resolve();
    }));
    const authPipe: any = this.#child.stdio?.[3]; authPipe?.on("error", () => {}); authPipe?.end(auth);
    this.#lifetime = this.#child.stdio?.[4]; this.#lifetime?.on("error", () => {});
    this.#child.stderr?.on("data", () => {});
    this.#child.once("error", (error) => this.#fail(error));
    this.#child.stdout?.on("data", (data) => {
      this.#buffer += this.#decoder.write(data);
      for (;;) {
        const at = this.#buffer.indexOf("\n");
        if (at < 0) { if (Buffer.byteLength(this.#buffer) > 1048576) this.#protocolFailure("oversized output"); return; }
        const line = this.#buffer.slice(0, at); this.#buffer = this.#buffer.slice(at + 1);
        if (Buffer.byteLength(line) > 1048576) { this.#protocolFailure("oversized output"); return; }
        try { this.#receive(JSON.parse(line)); } catch { this.#protocolFailure("malformed output"); return; }
      }
    });
  }
  #protocolFailure(message: string) { this.#dead = true; this.#fail(new Error(`worker RPC ${message}`)); void this.stop().catch(() => {}); }
  #receive(value: any) {
    const event = value?.type === "event" ? value.event : value;
    if (this.#terminal) {
      if (event?.type === "message_end" && event.message?.role === "assistant") {
        const message = event.message;
        // These are the only native completion fields that can attest the
        // effective provider/model. Thinking level has no completion evidence.
        if (typeof message.provider === "string") this.#effectiveProvider = message.provider.slice(0, 256);
        if (typeof message.model === "string") this.#effectiveModel = message.model.slice(0, 256);
        if (message.stopReason === "error" || message.stopReason === "aborted") this.#failure = message.errorMessage ?? message.stopReason;
        else this.#failure = undefined;
        for (const part of message.content ?? []) if (part.type === "text") {
          const bytes = Buffer.from(this.#text + part.text); this.#truncated ||= bytes.length > 49152;
          this.#text = bytes.subarray(0, 49152).toString("utf8");
        }
        this.#partialText = "";
        for (const [key, amount] of Object.entries(message.usage ?? {})) { if (typeof amount !== "number" || !Number.isFinite(amount) || amount < 0 || key.length > 64) { if (this.#diagnostics.length < 32) this.#diagnostics.push("invalid usage omitted"); continue; } if (!(key in this.#usage) && Object.keys(this.#usage).length >= 16) { if (this.#diagnostics.length < 32) this.#diagnostics.push("usage key limit reached"); continue; } const total = (this.#usage[key] ?? 0) + amount; if (!Number.isSafeInteger(total)) { if (this.#diagnostics.length < 32) this.#diagnostics.push("usage overflow omitted"); continue; } this.#usage[key] = total; }
      }
      if (event?.type === "message_update" && event.message?.role === "assistant") {
        const update = typeof event.delta?.text === "string" ? event.delta.text : (event.message.content ?? []).filter((part: any) => part?.type === "text").map((part: any) => part.text).join("");
        if (update) { const remaining = Math.max(0, 49152 - Buffer.byteLength(this.#text + this.#partialText)); const bytes = Buffer.from(update); this.#truncated ||= bytes.length > remaining; this.#partialText += bytes.subarray(0, remaining).toString("utf8"); }
      }
      if (event?.type === "tool_execution_end") {
        const detail = typeof event.error === "string" ? event.error : typeof event.result?.error === "string" ? event.result.error : (event.isError ? event.result?.content?.filter((part: any) => part?.type === "text").map((part: any) => part.text).join("\n") ?? "" : "");
        if (/recovery_pending/i.test(detail)) {
          this.#recoveryPending = true;
          this.#failure ??= "worker recovery_pending";
          const json = detail.match(/\{[\s\S]*\}$/)?.[0];
          try { const parsed = JSON.parse(json ?? ""); for (const path of [...(Array.isArray(parsed.affectedPaths) ? parsed.affectedPaths : []), ...(Array.isArray(parsed.recoveryPaths) ? parsed.recoveryPaths : [])]) if (typeof path === "string" && this.#recoveryRefs.length < 16) this.#recoveryRefs.push(path.slice(0, 512)); } catch {}
        }
      }
      if (event?.type === "auto_retry_start" && this.#diagnostics.length < 32) this.#diagnostics.push(`retry ${event.attempt}`);
      if (event?.type === "auto_retry_end") this.#failure = event.success ? undefined : String(event.finalError ?? "worker retry exhausted");
      if (event?.type === "agent_settled") {
        const terminal = this.#terminal; this.#terminal = undefined; clearTimeout(terminal.timer);
        if (this.#failure || this.#recoveryPending) terminal.reject(new PiWorkerTransportError(`worker failed: ${this.#failure ?? "worker recovery_pending"}`, this.snapshot(this.#failure ?? "worker recovery_pending")));
        else terminal.resolve(JSON.stringify(this.snapshot()));
        return;
      }
    }
    const waiter = this.#waiters.get(value?.id);
    if (waiter) {
      this.#waiters.delete(value.id); clearTimeout(waiter.timer);
      if (value.type === "error" || value.success === false) waiter.reject(new Error(typeof value.error === "string" ? value.error : value.error?.message ?? value.message ?? "worker RPC request rejected"));
      else waiter.resolve(value);
    }
  }
  snapshot(reason?: string): PiWorkerTransportReport {
    const reasonCode = /recovery_pending/i.test(reason ?? "") ? "recovery_pending" : /timeout/i.test(reason ?? "") ? "timeout" : /cancel|abort/i.test(reason ?? "") ? "cancelled" : /unavailable|closed|stopped/i.test(reason ?? "") ? "unavailable" : reason ? "failed" : undefined;
    return Object.freeze({
      text: Buffer.from(this.#text + this.#partialText).subarray(0, 49152).toString("utf8"), usage: { ...this.#usage }, diagnostics: this.#diagnostics.slice(0, 32), truncated: this.#truncated,
      selectedModel: this.#selectedModel, selectedEffort: this.#selectedEffort,
      effectiveModel: this.#effectiveModel, effectiveProvider: this.#effectiveProvider, effectiveEffort: this.#effectiveEffort,
      ...(reason ? { reason: reason.slice(0, 256) } : {}), ...(reasonCode ? { reasonCode } : {}),
      ...(this.#recoveryPending ? { recovery: { pending: true, refs: this.#recoveryRefs.slice(0, 16) } } : {}),
    });
  }
  #fail(error: Error) { if (this.#terminal) { clearTimeout(this.#terminal.timer); this.#terminal.reject(error); this.#terminal = undefined; } for (const [id, waiter] of this.#waiters) { this.#waiters.delete(id); clearTimeout(waiter.timer); waiter.reject(error); } }
  request(type: string, payload: Record<string, unknown> = {}, timeout = 2000) { if (this.#closed || this.#dead) return Promise.reject(new Error("worker RPC stopped")); const id = crypto.randomUUID(); return new Promise<any>((resolve, reject) => { const timer = setTimeout(() => { this.#waiters.delete(id); reject(new Error("worker RPC timeout")); }, timeout); this.#waiters.set(id, { resolve, reject, timer }); try { if (!this.#child.stdin) throw new Error("worker RPC stdin unavailable"); this.#child.stdin.write(`${JSON.stringify({ id, type, ...payload })}\n`); } catch (error) { this.#waiters.delete(id); clearTimeout(timer); reject(error); } }); }
  async abort() { if (this.#terminal) { clearTimeout(this.#terminal.timer); this.#terminal.reject(new Error("worker prompt aborted")); this.#terminal = undefined; } return await this.request("abort"); }
  async prompt(message: string, timeout = 30000) {
    if (this.#terminal) throw new Error("worker prompt already active");
    this.#text = ""; this.#partialText = ""; this.#usage = {}; this.#diagnostics = []; this.#failure = undefined; this.#truncated = false;
    this.#effectiveModel = "unknown"; this.#effectiveProvider = "unknown"; this.#effectiveEffort = "unknown";
    this.#recoveryPending = false; this.#recoveryRefs = [];
    const terminal = new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => { this.#terminal = undefined; reject(new Error("worker terminal timeout")); void this.stop().catch(() => {}); }, timeout);
      this.#terminal = { resolve, reject, timer };
    });
    terminal.catch(() => {});
    try { await this.request("prompt", { message }, timeout); return await terminal; }
    catch (error) {
      const pending = this.#terminal as PendingTerminal | undefined;
      if (pending) { clearTimeout(pending.timer); pending.reject(error as Error); this.#terminal = undefined; }
      if (error instanceof PiWorkerTransportError) throw error;
      throw new PiWorkerTransportError(error instanceof Error ? error.message : "worker prompt failed", this.snapshot(error instanceof Error ? error.message : "worker prompt failed"));
    }
  }
  async select(model: string, effort: string) {
    const slash = model.indexOf("/");
    if (slash <= 0 || slash === model.length - 1) throw new Error("worker model must be provider/id");
    const provider = model.slice(0, slash), modelId = model.slice(slash + 1);
    await this.request("set_model", { provider, modelId });
    await this.request("set_thinking_level", { level: effort });
    // set_thinking_level acknowledges without state. get_state is the native
    // source of truth for both selected values.
    const state = await this.request("get_state");
    const data = state?.data ?? {};
    const selected = data.model;
    const selectedProvider = typeof selected?.provider === "string" ? selected.provider : "";
    const selectedId = typeof selected?.id === "string" ? selected.id : typeof selected === "string" ? selected : "";
    const selectedFull = selectedProvider && selectedId ? `${selectedProvider}/${selectedId}` : selectedId;
    if (selectedFull !== model || data.thinkingLevel !== effort) throw new Error(`worker selection was not confirmed: ${model}/${effort}`);
    this.#selectedModel = selectedFull;
    this.#selectedEffort = effort;
  }
  stop(): Promise<void> { return this.#stopping ??= this.#stop(); }
  async #stop() {
    if (this.#closed) return;
    this.#closed = true;
    this.#child.stdin?.end();
    const signal = (value: NodeJS.Signals) => {
      try { if (this.#child.pid) process.kill(-this.#child.pid, value); } catch {}
      try { this.#child.kill(value); } catch {}
    };
    const wait = () => Promise.race([this.#exit, new Promise((resolve) => setTimeout(resolve, 1000))]);
    signal("SIGTERM");
    await wait();
    // The leader can exit while a descendant ignores SIGTERM. The owned process
    // group remains addressable, so always issue SIGKILL before returning.
    signal("SIGKILL");
    await wait();
    // fd4 is inherited only by the extension and its owned check supervisors.
    // Its remote EOF proves those supervisors have released the channel.
    const lifetime = this.#lifetime;
    if (lifetime && !lifetime.destroyed && !lifetime.readableEnded) {
      const eof = new Promise<void>((resolve) => { lifetime.once("end", resolve); lifetime.once("close", resolve); });
      lifetime.end();
      const complete = await Promise.race([eof.then(() => true), new Promise<boolean>((resolve) => setTimeout(() => resolve(false), 2000))]);
      if (!complete) this.#recoveryPending = true;
    }
    this.#fail(new Error("worker RPC stopped"));
    if (this.#recoveryPending) throw new Error("worker recovery_pending");
  }
}
const checkSupervisor = `import { spawn } from "node:child_process";
const argv = JSON.parse(process.argv[1]);
const child = spawn(argv[0], argv.slice(1), { shell: false, stdio: ["ignore", "inherit", "inherit"], detached: true });
let closing = false;
const pause = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
const signalChildGroup = (signal) => { try { process.kill(-child.pid, signal); } catch {} };
let resolveChildExit;
const childExited = new Promise((resolve) => { resolveChildExit = resolve; });
const childExitWithin = async (milliseconds) => await Promise.race([childExited.then(() => true), pause(milliseconds).then(() => false)]);
const close = async (code) => {
  if (closing) return;
  closing = true;
  signalChildGroup("SIGTERM");
  if (!await childExitWithin(500)) signalChildGroup("SIGKILL");
  await childExited;
  // A direct child may exit before descendants. SIGKILL the owned group after
  // reaping that child, then release fd4 only when this supervisor exits.
  signalChildGroup("SIGKILL");
  process.exit(code ?? 1);
};
// A failed spawn has no exit event. Resolve this branch explicitly so the
// supervisor releases fd4 instead of making the parent report a false hang.
child.once("error", () => { resolveChildExit(); void close(1); });
child.once("exit", (code) => { resolveChildExit(); void close(code ?? 1); });
// stdin is a private lifetime pipe owned by the RPC process. EOF also occurs
// when that process dies abruptly, without relying on a reusable PID.
process.stdin.once("end", () => { void close(1); });
process.once("SIGTERM", () => { void close(1); });
process.stdin.resume();`;

/** Explicit worker extension surface: no ambient resources, task, memory, or SDD lifecycle tools. */
export async function runWorkerCheck(mission: WorkerMission, argv: string[], signal?: AbortSignal, lifetimeFd?: number, mutationGuard?: () => void) {
  if (mission.role === "explore") throw new Error("explore missions cannot execute commands");
  if (process.platform === "win32") throw new Error("worker process tree ownership is unsupported on Windows");
  if (signal?.aborted) throw new Error("worker check cancelled");
  mutationGuard?.();
  return await new Promise<string>((resolve, reject) => {
    const child = spawn(process.execPath, ["--input-type=module", "--eval", checkSupervisor, JSON.stringify(argv)], { cwd: mission.workspace, shell: false, stdio: [lifetimeFd === undefined ? "pipe" : lifetimeFd, "pipe", "pipe"], detached: true });
    let output = "";
    let settled = false;
    let terminating = false;
    let timer: NodeJS.Timeout;
    let resolveExit!: () => void;
    const exited = new Promise<void>((resolve) => { resolveExit = resolve; });
    const finish = (error?: Error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      if (error) reject(error); else resolve(output);
    };
    const terminate = async (error: Error) => {
      if (terminating) return;
      terminating = true;
      // EOF asks the separate supervisor to terminate and reap only the command
      // group it owns. Do not signal the host RPC group from this boundary.
      if (lifetimeFd === undefined) child.stdin?.end(); else { try { child.kill("SIGTERM"); } catch {} }
      const complete = await Promise.race([exited.then(() => true), new Promise<boolean>((resolve) => setTimeout(() => resolve(false), 1000))]);
      finish(complete ? error : new Error("worker check recovery_pending"));
    };
    timer = setTimeout(() => { void terminate(new Error("worker check timeout")); }, 2000);
    const append = (data: Buffer) => {
      output = Buffer.from(`${output}${data}`).subarray(0, mission.resultLimit).toString("utf8");
    };
    child.stdout!.on("data", append);
    child.stderr!.on("data", append);
    child.once("error", (error) => { resolveExit(); finish(error); });
    child.once("exit", (code) => {
      resolveExit();
      if (!terminating) finish(code === 0 ? undefined : new Error(`worker check failed: ${code}: ${output}`));
    });
    signal?.addEventListener("abort", () => { void terminate(new Error("worker check cancelled")); }, { once: true });
  });
}
export function createWorkerExtension(mission: WorkerMission, backend: () => Promise<unknown>, auth?: any, lifetimeFd?: number, authority?: WorkerAuthority) { const writable = mission.mode === "full" && workerCanWrite(mission.role); const mutationGuard = writable ? (authority ? createWorkerMutationGuard(authority) : (() => { throw new Error("worker mutation authority unavailable"); })) : undefined; const host = Object.freeze({ workspace: mission.workspace, mode: mission.mode, role: mission.role, backend, mutationGuard }); return async (pi: any) => { for (const tool of createExplorerTools(mission)) pi.registerTool(tool); if (auth) pi.registerProvider(auth.provider, { apiKey: auth.apiKey, headers: auth.headers, baseUrl: auth.baseUrl, models: [auth.model] }); pi.registerTool({ name: "worker_read", label: "Read authorized file", parameters: { type: "object", properties: { path: { type: "string" } }, required: ["path"], additionalProperties: false }, async execute(_id: string, input: any) { if (!input || Object.keys(input).some(key => key !== "path") || typeof input.path !== "string") throw new Error("invalid worker read input"); const text = await readWorkerTarget(mission, input.path); if (Buffer.byteLength(text) > mission.resultLimit) throw new Error("worker read exceeds result budget; use explorer pages"); return { content: [{ type: "text", text }] }; } }); if (mission.role !== "explore") pi.registerTool({ name: "worker_check", label: "Run authorized check", parameters: { type: "object", properties: { argv: { type: "array", items: { type: "string" } } }, required: ["argv"], additionalProperties: false }, async execute(_id: string, input: any, signal?: AbortSignal) { if (!input || Object.keys(input).some(key => key !== "argv")) throw new Error("invalid worker check input"); mutationGuard?.(); return { content: [{ type: "text", text: await runWorkerCheck(mission, validateWorkerArgv(mission, input.argv), signal, lifetimeFd, mutationGuard) }] }; } }); if (writable) { const { createApplyPatchTool } = await import("../tools/apply_patch.ts"); pi.registerTool(createApplyPatchTool(host as any, { workerRole: mission.role as "general" | "sdd-apply", allowedTargets: workerTargetLedger(mission), onWorkerWrite: async (targets) => await advanceWorkerTargets(mission, targets), ...(mission.role === "sdd-apply" ? { acceptedBindings: mission.acceptedBindings } : {}) })); } }; }
export async function executePiWorker(mission: WorkerMission, options: { cli: string; runnerModule: string; timeout?: number; signal?: AbortSignal; auth?: unknown; authority?: Readonly<{ expiresAt: number }> }) {
  if (process.platform === "win32") throw new Error("worker process tree ownership is unsupported on Windows");
  const writable = mission.mode === "full" && workerCanWrite(mission.role);
  const expiresAt = options.authority?.expiresAt;
  const assertLaunch = () => {
    if (options.signal?.aborted) throw new Error("worker cancelled");
    if (writable && (expiresAt === undefined || !Number.isFinite(expiresAt) || expiresAt <= Date.now())) throw new Error("worker mutation authority unavailable");
  };
  assertLaunch();
  const root = await mkdtemp(join(tmpdir(), "vgxness-pi-worker-"));
  let rpc: PiRpcRunner | undefined, result: string | undefined, failure: PiWorkerTransportError | undefined, cleaned = false, revocationFd: number | undefined, revoked = false;
  let revokeFailure: Error | undefined;
  const revoke = () => { if (revoked || revocationFd === undefined) return; if (writeSync(revocationFd, Buffer.from([1]), 0, 1, 0) !== 1) throw new Error("worker revocation short write"); revoked = true; };
  const recovery = (error: unknown) => { const message = error instanceof Error ? error.message : "worker revocation failed"; const report = rpc?.snapshot("worker revocation recovery_pending") ?? { text: "", usage: {}, diagnostics: [], truncated: false, selectedModel: "unknown", selectedEffort: "unknown", effectiveModel: "unknown", effectiveProvider: "unknown", effectiveEffort: "unknown" }; return new PiWorkerTransportError("worker revocation recovery_pending", { ...report, reason: "worker revocation recovery_pending", reasonCode: "recovery_pending", diagnostics: [...report.diagnostics, message.slice(0, 256)].slice(0, 32), recovery: { pending: true, refs: [root] } }); };
  const onAbort = () => { try { revoke(); } catch (error) { revokeFailure = error instanceof Error ? error : new Error("worker revocation failed"); } void rpc?.abort().catch(() => {}); };
  let rejectCancelled: (error: Error) => void = () => {};
  const listener = () => { onAbort(); rejectCancelled(new Error("worker cancelled")); };
  try {
    const extension = join(root, "worker.mjs");
    if (writable) { revocationFd = openSync(join(root, "revocation"), "wx+", 0o600); if (writeSync(revocationFd, Buffer.from([0]), 0, 1, 0) !== 1) throw new Error("worker authority initialization failed"); unlinkSync(join(root, "revocation")); }
    await writeFile(extension, `import { readFileSync } from "node:fs"; import { createWorkerExtension, bootstrapWorkerMission } from ${JSON.stringify(options.runnerModule)}; const mission = await bootstrapWorkerMission(${JSON.stringify(mission)}); const auth = JSON.parse(readFileSync(3, "utf8")); export default createWorkerExtension(mission, async () => { throw new Error("worker backend unavailable"); }, auth, 4, ${writable ? `{ expiresAt: ${JSON.stringify(expiresAt)}, revocationFd: 5 }` : "undefined"});\n`);
    assertLaunch();
    rpc = new PiRpcRunner({ cli: options.cli, extension, cwd: mission.workspace, profile: join(root, "profile"), auth: options.auth, authorityFd: revocationFd });
    const cancelled = new Promise<never>((_, reject) => { rejectCancelled = reject; options.signal?.addEventListener("abort", listener, { once: true }); });
    result = await Promise.race([(async () => {
      assertLaunch();
      // SDK loading is part of startup, separate from normal 2-second RPC controls.
      await rpc!.request("get_state", {}, 10000);
      assertLaunch();
      await rpc!.select(mission.model, mission.effort);
      assertLaunch();
      return await rpc!.prompt(workerPrompt(mission), options.timeout);
    })(), cancelled]);
  } catch (error) {
    failure = error instanceof PiWorkerTransportError ? error : new PiWorkerTransportError(error instanceof Error ? error.message : "worker failed", rpc?.snapshot(error instanceof Error ? error.message : "worker failed") ?? { text: "", usage: {}, diagnostics: [], truncated: false, selectedModel: "unknown", selectedEffort: "unknown", effectiveModel: "unknown", effectiveProvider: "unknown", effectiveEffort: "unknown" });
  }
  try { revoke(); } catch (error) { revokeFailure = error instanceof Error ? error : new Error("worker revocation failed"); }
  if (revokeFailure) failure = recovery(revokeFailure);
  try { await rpc?.stop(); cleaned = true; }
  catch (error) {
    const reason = error instanceof Error ? error.message : "worker cleanup failed";
    // Cleanup/recovery evidence is authoritative even when there was an earlier
    // prompt error; retain the original message when possible.
    const report = rpc?.snapshot(reason) ?? { text: "", usage: {}, diagnostics: [], truncated: false, selectedModel: "unknown", selectedEffort: "unknown", effectiveModel: "unknown", effectiveProvider: "unknown", effectiveEffort: "unknown", reason, reasonCode: "recovery_pending" as const };
    if (failure?.message && report.diagnostics.length < 32) report.diagnostics.push(`prior failure: ${failure.message}`.slice(0, 256));
    if (report.recovery && report.recovery.refs.length < 16) report.recovery.refs.push(root);
    failure = new PiWorkerTransportError(reason, report);
  }
  options.signal?.removeEventListener("abort", listener);
  if (revocationFd !== undefined) try { closeSync(revocationFd); } catch (error) { failure = recovery(error); }
  if (cleaned && !failure?.report.recovery?.pending) try { await rm(root, { recursive: true, force: true }); } catch (error) {
    const reason = "worker recovery_pending";
    const report = rpc?.snapshot(reason) ?? { text: "", usage: {}, diagnostics: [], truncated: false, selectedModel: "unknown", selectedEffort: "unknown", effectiveModel: "unknown", effectiveProvider: "unknown", effectiveEffort: "unknown", reason, reasonCode: "recovery_pending" as const };
    if (report.recovery && report.recovery.refs.length < 16) report.recovery.refs.push(root);
    throw new PiWorkerTransportError(reason, { ...report, diagnostics: [...report.diagnostics, `cleanup failed: ${error instanceof Error ? error.message : "unknown"}`.slice(0, 256)].slice(0, 32), recovery: report.recovery ?? { pending: true, refs: [root] } });
  }
  if (failure) { const bytes = Buffer.from(failure.report.text); if (bytes.length > mission.resultLimit) throw new PiWorkerTransportError(failure.message, { ...failure.report, text: bytes.subarray(0, mission.resultLimit).toString("utf8").replace(/\uFFFD$/, ""), truncated: true }); throw failure; }
  const structured = JSON.parse(result!);
  const output = Buffer.from(structured.text ?? "");
  if (output.length > mission.resultLimit) { structured.text = output.subarray(0, mission.resultLimit).toString("utf8").replace(/\uFFFD$/, ""); structured.truncated = true; }
  return JSON.stringify(structured);
}

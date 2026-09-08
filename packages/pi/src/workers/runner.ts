import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { advanceWorkerTargets, bootstrapWorkerMission, readWorkerTarget, validateWorkerArgv, workerTargetLedger, type WorkerMission } from "./mission.ts";
export { bootstrapWorkerMission } from "./mission.ts";
import { createExplorerTools } from "./explorer.ts";
import { StringDecoder } from "node:string_decoder";
import { workerCanWrite } from "./roles.ts";
type PendingTerminal = { resolve: (value: string) => void; reject: (error: Error) => void; timer: NodeJS.Timeout };
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
  #usage: Record<string, number> = {};
  #diagnostics: string[] = [];
  #failure?: string;
  #truncated = false;
  #dead = false;
  #waiters = new Map<string, { resolve: (value: any) => void; reject: (error: Error) => void; timer: NodeJS.Timeout }>();
  #closed = false;
  #stopping?: Promise<void>;
  #recoveryPending = false;
  #terminal?: PendingTerminal;
  #exit: Promise<void>;
  constructor(options: { node?: string; cli: string; extension: string; cwd: string; profile: string; auth?: unknown }) {
    if (process.platform === "win32") throw new Error("worker process tree ownership is unsupported on Windows");
    const auth = JSON.stringify(options.auth ?? null);
    if (Buffer.byteLength(auth, "utf8") > 65536) throw new Error("worker auth record too large");
    this.#child = spawn(options.node ?? process.execPath, [options.cli, "--mode", "rpc", "--offline", "--no-session", "--no-extensions", "-e", options.extension, "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-builtin-tools"], { cwd: options.cwd, shell: false, stdio: ["pipe", "pipe", "pipe", "pipe", "pipe"], detached: true, env: { ...Object.fromEntries(["PATH", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR"].filter(key => process.env[key] !== undefined).map(key => [key, process.env[key]])), HOME: options.profile, PI_CODING_AGENT_DIR: options.profile, PI_CODING_AGENT_SESSION_DIR: options.profile } });
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
        if (message.stopReason === "error" || message.stopReason === "aborted") this.#failure = message.errorMessage ?? message.stopReason;
        else this.#failure = undefined;
        for (const part of message.content ?? []) if (part.type === "text") {
          const bytes = Buffer.from(this.#text + part.text); this.#truncated ||= bytes.length > 49152;
          this.#text = bytes.subarray(0, 49152).toString("utf8");
        }
        for (const [key, amount] of Object.entries(message.usage ?? {})) if (typeof amount === "number" && Number.isFinite(amount)) this.#usage[key] = (this.#usage[key] ?? 0) + amount;
      }
      if (event?.type === "auto_retry_start" && this.#diagnostics.length < 32) this.#diagnostics.push(`retry ${event.attempt}`);
      if (event?.type === "auto_retry_end") this.#failure = event.success ? undefined : String(event.finalError ?? "worker retry exhausted");
      if (event?.type === "agent_settled") {
        const terminal = this.#terminal; this.#terminal = undefined; clearTimeout(terminal.timer);
        if (this.#failure) terminal.reject(new Error(`worker failed: ${this.#failure}`));
        else terminal.resolve(JSON.stringify({ text: this.#text, usage: this.#usage, diagnostics: this.#diagnostics, truncated: this.#truncated }));
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
  #fail(error: Error) { if (this.#terminal) { clearTimeout(this.#terminal.timer); this.#terminal.reject(error); this.#terminal = undefined; } for (const [id, waiter] of this.#waiters) { this.#waiters.delete(id); clearTimeout(waiter.timer); waiter.reject(error); } }
  request(type: string, payload: Record<string, unknown> = {}, timeout = 2000) { if (this.#closed || this.#dead) return Promise.reject(new Error("worker RPC stopped")); const id = crypto.randomUUID(); return new Promise<any>((resolve, reject) => { const timer = setTimeout(() => { this.#waiters.delete(id); reject(new Error("worker RPC timeout")); }, timeout); this.#waiters.set(id, { resolve, reject, timer }); try { if (!this.#child.stdin) throw new Error("worker RPC stdin unavailable"); this.#child.stdin.write(`${JSON.stringify({ id, type, ...payload })}\n`); } catch (error) { this.#waiters.delete(id); clearTimeout(timer); reject(error); } }); }
  async abort() { if (this.#terminal) { clearTimeout(this.#terminal.timer); this.#terminal.reject(new Error("worker prompt aborted")); this.#terminal = undefined; } return await this.request("abort"); }
  async prompt(message: string, timeout = 30000) {
    if (this.#terminal) throw new Error("worker prompt already active");
    this.#text = ""; this.#usage = {}; this.#diagnostics = []; this.#failure = undefined; this.#truncated = false;
    const terminal = new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => { this.#terminal = undefined; reject(new Error("worker terminal timeout")); void this.stop().catch(() => {}); }, timeout);
      this.#terminal = { resolve, reject, timer };
    });
    terminal.catch(() => {});
    try { await this.request("prompt", { message }, timeout); return await terminal; }
    catch (error) { const pending = this.#terminal as PendingTerminal | undefined; if (pending) { clearTimeout(pending.timer); pending.reject(error as Error); this.#terminal = undefined; } throw error; }
  }
  async select(model: string, effort: string) { const slash = model.indexOf("/"); if (slash <= 0 || slash === model.length - 1) throw new Error("worker model must be provider/id"); const selected = await this.request("set_model", { provider: model.slice(0, slash), modelId: model.slice(slash + 1) }); if (selected?.type === "error") throw new Error(`worker model unsupported: ${model}`); const thinking = await this.request("set_thinking_level", { level: effort }); if (thinking?.type === "error") throw new Error(`worker effort unsupported: ${effort}`); }
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
export async function runWorkerCheck(mission: WorkerMission, argv: string[], signal?: AbortSignal, lifetimeFd?: number) {
  if (mission.role === "explore") throw new Error("explore missions cannot execute commands");
  if (process.platform === "win32") throw new Error("worker process tree ownership is unsupported on Windows");
  if (signal?.aborted) throw new Error("worker check cancelled");
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
export function createWorkerExtension(mission: WorkerMission, backend: () => Promise<unknown>, auth?: any, lifetimeFd?: number) { const host = Object.freeze({ workspace: mission.workspace, mode: mission.mode, role: mission.role, backend }); return async (pi: any) => { for (const tool of createExplorerTools(mission)) pi.registerTool(tool); if (auth) pi.registerProvider(auth.provider, { apiKey: auth.apiKey, headers: auth.headers, baseUrl: auth.baseUrl, models: [auth.model] }); pi.registerTool({ name: "worker_read", label: "Read authorized file", parameters: { type: "object", properties: { path: { type: "string" } }, required: ["path"], additionalProperties: false }, async execute(_id: string, input: any) { if (!input || Object.keys(input).some(key => key !== "path") || typeof input.path !== "string") throw new Error("invalid worker read input"); const text = await readWorkerTarget(mission, input.path); if (Buffer.byteLength(text) > mission.resultLimit) throw new Error("worker read exceeds result budget; use explorer pages"); return { content: [{ type: "text", text }] }; } }); if (mission.role !== "explore") pi.registerTool({ name: "worker_check", label: "Run authorized check", parameters: { type: "object", properties: { argv: { type: "array", items: { type: "string" } } }, required: ["argv"], additionalProperties: false }, async execute(_id: string, input: any, signal?: AbortSignal) { if (!input || Object.keys(input).some(key => key !== "argv")) throw new Error("invalid worker check input"); return { content: [{ type: "text", text: await runWorkerCheck(mission, validateWorkerArgv(mission, input.argv), signal, lifetimeFd) }] }; } }); if (workerCanWrite(mission.role)) { const { createApplyPatchTool } = await import("../tools/apply_patch.ts"); pi.registerTool(createApplyPatchTool(host as any, { workerRole: mission.role, allowedTargets: workerTargetLedger(mission), onWorkerWrite: async (targets) => await advanceWorkerTargets(mission, targets), ...(mission.role === "sdd-apply" ? { acceptedBindings: mission.acceptedBindings } : {}) })); } }; }
export async function executePiWorker(mission: WorkerMission, options: { cli: string; runnerModule: string; timeout?: number; signal?: AbortSignal; auth?: unknown }) { if (process.platform === "win32") throw new Error("worker process tree ownership is unsupported on Windows"); const root = await mkdtemp(join(tmpdir(), "vgxness-pi-worker-")); let rpc: PiRpcRunner | undefined; let recovered = false; try { const extension = join(root, "worker.mjs"); await writeFile(extension, `import { readFileSync } from "node:fs"; import { createWorkerExtension, bootstrapWorkerMission } from ${JSON.stringify(options.runnerModule)}; const mission = await bootstrapWorkerMission(${JSON.stringify(mission)}); const auth = JSON.parse(readFileSync(3, "utf8")); export default createWorkerExtension(mission, async () => { throw new Error("worker backend unavailable"); }, auth, 4);\n`); rpc = new PiRpcRunner({ cli: options.cli, extension, cwd: mission.workspace, profile: join(root, "profile"), auth: options.auth }); const cancelled = new Promise<never>((_, reject) => options.signal?.addEventListener("abort", () => { void rpc?.abort().catch(() => {}); reject(new Error("worker cancelled")); }, { once: true })); if (options.signal?.aborted) throw new Error("worker cancelled"); await rpc.request("get_state"); await rpc.select(mission.model, mission.effort); const result = await Promise.race([rpc.prompt(mission.goal, options.timeout), cancelled]);
    // Keep the structured envelope valid even when the mission requests a tiny text budget.
    const structured = JSON.parse(result);
    const output = Buffer.from(structured.text ?? "");
    if (output.length > mission.resultLimit) { structured.text = output.subarray(0, mission.resultLimit).toString("utf8").replace(/\uFFFD$/, ""); structured.truncated = true; }
    return JSON.stringify(structured); } finally { try { await rpc?.stop(); recovered = true; } finally { if (recovered) await rm(root, { recursive: true, force: true }); } } }

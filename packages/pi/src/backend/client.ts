import { type ChildProcess } from "node:child_process";
import {
  MAX_ACTIVE_REQUESTS,
  MAX_CORRELATION_IDS,
  MAX_RECORD_BYTES,
  type ErrorRecord,
  type Hello,
  type Mode,
  type Record,
  type Result,
  decodeRecord,
  encodeRecord,
  sameHello,
  validId,
} from "./protocol.ts";

type Pending = { resolve(value: unknown): void; reject(error: Error): void };

export class BackendClient {
  #child: ChildProcess;
  #expected: Hello;
  #buffer = new Uint8Array();
  #ready = false;
  #closed = false;
  #seen = new Set<string>();
  #active = new Map<string, Pending>();
  #readyWait: Promise<void>;
  #resolveReady!: () => void;
  #rejectReady!: (error: Error) => void;
  #exited: Promise<void>;
  #resolveExited!: () => void;
  #closing: Promise<void> | undefined;

  constructor(child: ChildProcess, expected: Hello) {
    this.#child = child;
    this.#expected = Object.freeze(JSON.parse(JSON.stringify(expected))) as Hello;
    this.#readyWait = new Promise((resolve, reject) => { this.#resolveReady = resolve; this.#rejectReady = reject; });
    this.#exited = new Promise((resolve) => { this.#resolveExited = resolve; });
    child.once("exit", () => { this.#resolveExited(); this.#fail(new Error("backend unavailable")); });
    child.once("error", () => { this.#resolveExited(); this.#fail(new Error("backend unavailable")); });
    child.stdout?.on("data", (value: Buffer) => this.#read(new Uint8Array(value)));
    child.stdout?.on("end", () => this.#fail(new Error("backend unavailable")));
    this.#send(this.#expected);
  }

  get ready() { return this.#ready; }
  get active() { return this.#active.size; }

  #read(next: Uint8Array) {
    const buffer = new Uint8Array(this.#buffer.length + next.length);
    buffer.set(this.#buffer); buffer.set(next, this.#buffer.length); this.#buffer = buffer;
    for (;;) {
      const newline = this.#buffer.indexOf(10);
      if (newline < 0) { if (this.#buffer.length > MAX_RECORD_BYTES) this.#fail(new Error("invalid record framing")); return; }
      if (newline + 1 > MAX_RECORD_BYTES) return this.#fail(new Error("invalid record framing"));
      const line = this.#buffer.slice(0, newline + 1); this.#buffer = this.#buffer.slice(newline + 1);
      try { this.#receive(decodeRecord(line)); } catch (error) { this.#fail(error as Error); return; }
    }
  }

  #receive(record: Record) {
    if (!this.#ready) {
      if (record.type !== "hello" || !sameHello(record, this.#expected)) return this.#fail(new Error("handshake rejected"));
      this.#ready = true; this.#resolveReady(); return;
    }
    if (record.type !== "result" && record.type !== "error") return this.#fail(new Error("invalid response"));
    const pending = this.#active.get(record.id!);
    if (!pending) return this.#fail(new Error("unknown terminal"));
    this.#active.delete(record.id!);
    if (record.type === "result") pending.resolve((record as Result).result);
    else pending.reject(Object.assign(new Error((record as ErrorRecord).message), { code: (record as ErrorRecord).code, retrySafe: (record as ErrorRecord).retrySafe, recoveryState: (record as ErrorRecord).recoveryState }));
  }

  waitReady(timeoutMs = 1000): Promise<void> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.#fail(new Error("backend handshake timeout")); reject(new Error("backend handshake timeout")); }, timeoutMs);
      this.#readyWait.then(() => { clearTimeout(timer); resolve(); }, (error) => { clearTimeout(timer); reject(error); });
    });
  }

  request(operation: string, payload: unknown, binding: { workspace: string; mode: Mode; role: string }, id = crypto.randomUUID()): Promise<unknown> {
    if (binding.workspace !== this.#expected.workspace || binding.mode !== this.#expected.mode || binding.role !== this.#expected.role) return Promise.reject(new Error("binding mismatch"));
    if (!this.#ready || this.#closed) return Promise.reject(new Error("backend unavailable"));
    if (!validId(id) || this.#seen.has(id) || this.#seen.size >= MAX_CORRELATION_IDS || this.#active.size >= MAX_ACTIVE_REQUESTS) return Promise.reject(new Error("request rejected"));
    this.#seen.add(id);
    // Hosts carry local-only configuration (for example storageRoot).  The
    // protocol record is closed, so copy only the three negotiated bindings.
    const { workspace, mode, role } = binding;
    return new Promise((resolve, reject) => { this.#active.set(id, { resolve, reject }); this.#send({ type: "request", id, operation, workspace, mode, role, payload }); });
  }

  cancel(id: string) { if (this.#active.has(id)) this.#send({ type: "cancel", id }); }
  #send(record: Record) { try { this.#child.stdin?.write(encodeRecord(record)); } catch { this.#fail(new Error("backend unavailable")); } }
  #fail(error: Error) {
    if (this.#closed) return;
    this.#closed = true; this.#rejectReady(error);
    for (const pending of this.#active.values()) pending.reject(error);
    this.#active.clear();
    void this.close(error);
  }

  async close(error = new Error("backend unavailable")) {
    this.#fail(error);
    if (this.#closing) return this.#closing;
    this.#closing = (async () => {
      if (this.#child.exitCode != null || this.#child.signalCode != null) return;
      try { this.#child.stdin?.end(); } catch {}
      await Promise.race([this.#exited, new Promise((resolve) => setTimeout(resolve, 100))]);
      if (this.#child.exitCode == null && this.#child.signalCode == null) {
        try { if (process.platform !== "win32" && this.#child.pid) process.kill(-this.#child.pid, "SIGKILL"); } catch {}
        try { this.#child.kill("SIGKILL"); } catch {}
        await this.#exited;
      }
      try { this.#child.stdin?.destroy(); this.#child.stdout?.destroy(); this.#child.stderr?.destroy(); } catch {}
    })();
    return this.#closing;
  }
}

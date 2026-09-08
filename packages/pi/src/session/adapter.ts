import { randomUUID } from "node:crypto";
import type { ToolHost } from "../tools/memory.ts";

type Session = { handle: string; externalId: string; draftUpdatedAt?: string; hasDraft: boolean; handoff: string };
type Backend = { request(operation: string, payload: unknown, binding: ToolHost): Promise<any> };

/** Only local session events can establish a lease; tree entries are never authority. */
export class SessionAdapter {
  #current: Session | undefined;
  #renewal?: ReturnType<typeof setInterval>;
  #queue: Promise<unknown> = Promise.resolve();
  #failure?: string;
  private readonly host: ToolHost;
  private readonly renewalMs: number;
  constructor(host: ToolHost, renewalMs = 60 * 60 * 1000) { this.host = host; this.renewalMs = renewalMs; }
  private async backend(): Promise<Backend> { return await this.host.backend() as Backend; }
  private writable() { return this.host.mode === "full" && this.host.role === "manager"; }
  private serial<T>(fn: () => Promise<T>): Promise<T> { const value = this.#queue.then(fn); this.#queue = value.catch(() => {}); return value; }
  private stopTimer() { if (this.#renewal) clearInterval(this.#renewal); this.#renewal = undefined; }
  status() { return { state: this.#failure ? "unavailable" : this.#current ? "active" : this.writable() ? "idle" : "read-only", handoffAvailable: Boolean(this.#current?.handoff), draftSaved: Boolean(this.#current?.hasDraft), ...(this.#failure ? { error: this.#failure } : {}) }; }
  async start(externalId: string) {
    return this.serial(async () => {
      if (!this.writable()) return;
      if (this.#current?.externalId === externalId) return;
      if (this.#current) throw new Error("active session must be detached before replacement");
      const backend = await this.backend();
      let value = await backend.request("memory.session.start", { externalId }, this.host);
      // Resuming a terminal SDK session begins a fresh lease instead of reviving it.
      if (value.state && value.state !== "active") value = await backend.request("memory.session.start", { externalId: `${externalId}:${randomUUID()}` }, this.host);
      const context = await backend.request("memory.session.context", { handle: value.handle }, this.host);
      this.#current = { handle: value.handle, externalId, hasDraft: Boolean(context?.draftUpdatedAt), draftUpdatedAt: context?.draftUpdatedAt, handoff: typeof context?.handoff === "string" ? context.handoff.slice(0, 4096) : "" };
      this.#failure = undefined;
      this.stopTimer();
      this.#renewal = setInterval(() => { void this.renew().catch(error => { this.#failure = error instanceof Error ? error.message : "session lease unavailable"; this.stopTimer(); }); }, this.renewalMs);
      this.#renewal.unref?.();
    });
  }
  async checkpoint() { return this.serial(async () => { if (this.writable() && this.#current) await (await this.backend()).request("memory.session.checkpoint", { handle: this.#current.handle }, this.host); }); }
  async renew() { return this.serial(async () => { if (this.writable() && this.#current) await (await this.backend()).request("memory.session.renew", { handle: this.#current.handle }, this.host); }); }
  async saveDraft(summary: string) {
    return this.serial(async () => {
      if (!this.writable() || !this.#current) throw new Error("no writable active session");
      if (summary.length > 4096) throw new Error("session handoff exceeds limit");
      const payload: Record<string, string> = { handle: this.#current.handle, summary };
      if (this.#current.draftUpdatedAt) payload.expectedUpdatedAt = this.#current.draftUpdatedAt;
      const value = await (await this.backend()).request("memory.session.draft_save", payload, this.host);
      this.#current.draftUpdatedAt = value.UpdatedAt ?? value.updatedAt;
      this.#current.hasDraft = true;
    });
  }
  context() { if (!this.writable() || !this.#current) throw new Error("no writable active session"); return { handoff: this.#current.handoff, trust: "UNTRUSTED" }; }
  /** Reload preserves the durable draft; the next runtime must acquire a new lease. */
  async detach() { this.stopTimer(); return this.serial(async () => { if (this.#current) await (await this.backend()).request("memory.session.checkpoint", { handle: this.#current.handle }, this.host); this.#current = undefined; }); }
  async end(state: "completed" | "cancelled" | "interrupted", summary = "") {
    this.stopTimer();
    return this.serial(async () => {
      if (!this.writable() || !this.#current) return;
      const current = this.#current;
      // Quit without an explicit draft cannot create durable memory.
      const finalState = state === "completed" && !summary && !current.hasDraft ? "interrupted" : state;
      await (await this.backend()).request("memory.session.end", { handle: current.handle, state: finalState, summary: finalState === "completed" ? summary : "" }, this.host);
      this.#current = undefined;
    });
  }
}

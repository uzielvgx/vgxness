import { randomUUID } from "node:crypto";
import type { ToolHost } from "../tools/memory.ts";

type Session = { handle: string; externalId: string; leaseUntil: number; draftUpdatedAt?: string; hasDraft: boolean; handoff: string };
type Backend = { request(operation: string, payload: unknown, binding: ToolHost): Promise<any> };
export type MutationGrant = Readonly<{ expiresAt: number; signal: AbortSignal }>;

/** Only local session events can establish a lease; tree entries are never authority. */
export class SessionAdapter {
  #current: Session | undefined;
  #renewal?: ReturnType<typeof setInterval>;
  #deadline?: ReturnType<typeof setTimeout>;
  #queue: Promise<unknown> = Promise.resolve();
  #failure?: string;
  #abort = new AbortController();
  private readonly host: ToolHost;
  private readonly renewalMs: number;
  constructor(host: ToolHost, renewalMs = 60 * 60 * 1000) { this.host = host; this.renewalMs = renewalMs; }
  private async backend(): Promise<Backend> { return await this.host.backend() as Backend; }
  private writable() { return this.host.mode === "full" && this.host.role === "manager"; }
  private serial<T>(fn: () => Promise<T>): Promise<T> { const value = this.#queue.then(fn); this.#queue = value.catch(() => {}); return value; }
  private stopTimer() { if (this.#renewal) clearInterval(this.#renewal); if (this.#deadline) clearTimeout(this.#deadline); this.#renewal = undefined; this.#deadline = undefined; }
  private startRenewalTimer() { if (this.#renewal || !this.#current) return; this.#renewal = setInterval(() => { void this.renew().catch(() => {}); }, this.renewalMs); this.#renewal.unref?.(); }
  private armLease() { if (!this.#current) return; if (this.#deadline) clearTimeout(this.#deadline); const delay=Math.max(0,this.#current.leaseUntil-Date.now()); this.#deadline=setTimeout(()=>this.fail(new Error("session lease expired")),delay); this.#deadline.unref?.(); }
  private fail(error: unknown) { this.#failure = error instanceof Error ? error.message : "session lease unavailable"; this.stopTimer(); this.#abort.abort(error); }
  private validLease(value: any) { const until = typeof value?.leaseUntil === "string" ? Date.parse(value.leaseUntil) : Number.NaN; if (!Number.isFinite(until) || until <= Date.now()) throw new Error("session lease unavailable"); return until; }
  status() { return { state: this.#failure ? "unavailable" : this.#current ? "active" : this.writable() ? "idle" : "read-only", handoffAvailable: Boolean(this.#current?.handoff), draftSaved: Boolean(this.#current?.hasDraft), ...(this.#failure ? { error: this.#failure } : {}) }; }
  /** A failed renewal is latched: reads remain available, writes require a fresh start. */
  assertMutation() { if (this.#current && Date.now() >= this.#current.leaseUntil) this.fail(new Error("session lease expired")); if (this.#failure) throw new Error(`session authority unavailable: ${this.#failure}`); if (!this.writable() || !this.#current) throw new Error("no writable active session"); }
  mutationSignal() { return this.#abort.signal; }
  /** Capture a host-issued, non-extendable lease before handing work to a child. */
  mutationGrant(): MutationGrant { this.assertMutation(); return Object.freeze({ expiresAt: this.#current!.leaseUntil, signal: this.#abort.signal }); }
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
      const leaseUntil = this.validLease(value);
      this.#abort = new AbortController();
      this.#current = { handle: value.handle, externalId, leaseUntil, hasDraft: Boolean(context?.draftUpdatedAt), draftUpdatedAt: context?.draftUpdatedAt, handoff: typeof context?.handoff === "string" ? context.handoff.slice(0, 4096) : "" };
      this.#failure = undefined;
      this.stopTimer();
      this.armLease();
      this.startRenewalTimer();
    });
  }
  async checkpoint() { return this.serial(async () => { if (this.writable() && this.#current) try { this.assertMutation(); const value=await (await this.backend()).request("memory.session.checkpoint", { handle: this.#current.handle }, this.host); this.#current.leaseUntil=this.validLease(value); this.armLease(); } catch (error) { this.fail(error); throw error; } }); }
  /** Renewal is the sole control path allowed to recover a transient failure. */
  async renew() { return this.serial(async () => { if (!this.writable() || !this.#current) return; try { const value=await (await this.backend()).request("memory.session.renew", { handle: this.#current.handle }, this.host); this.#current.leaseUntil=this.validLease(value); this.#failure=undefined; if (this.#abort.signal.aborted) this.#abort=new AbortController(); this.armLease(); this.startRenewalTimer(); } catch (error) { this.fail(error); throw error; } }); }
  async saveDraft(summary: string) {
    return this.serial(async () => {
      this.assertMutation();
      const current = this.#current!;
      if (summary.length > 4096) throw new Error("session handoff exceeds limit");
      const payload: Record<string, string> = { handle: current.handle, summary };
      if (current.draftUpdatedAt) payload.expectedUpdatedAt = current.draftUpdatedAt;
      const value = await (await this.backend()).request("memory.session.draft_save", payload, this.host);
      current.draftUpdatedAt = value.UpdatedAt ?? value.updatedAt;
      current.hasDraft = true;
    });
  }
  context() { if (!this.writable() || !this.#current) throw new Error("no writable active session"); return { handoff: this.#current.handoff, trust: "UNTRUSTED" }; }
  /** Reload preserves the durable draft; the next runtime must acquire a new lease. */
  async detach() { this.stopTimer(); this.#abort.abort(new Error("session detached")); return this.serial(async () => { const current=this.#current; try { if (current) await (await this.backend()).request("memory.session.checkpoint", { handle: current.handle }, this.host); } finally { this.#current = undefined; } }); }
  async end(state: "completed" | "cancelled" | "interrupted", summary = "") {
    this.stopTimer(); this.#abort.abort(new Error("session ended"));
    return this.serial(async () => {
      if (!this.writable() || !this.#current) return;
      const current = this.#current;
      // Quit without an explicit draft cannot create durable memory.
      const finalState = state === "completed" && !summary && !current.hasDraft ? "interrupted" : state;
      try { await (await this.backend()).request("memory.session.end", { handle: current.handle, state: finalState, summary: finalState === "completed" ? summary : "" }, this.host); }
      finally { this.#current = undefined; }
    });
  }
}

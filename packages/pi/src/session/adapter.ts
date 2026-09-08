import type { ToolHost } from "../tools/memory.ts";

type Session = { handle: string; draftUpdatedAt?: string; handoff: string };
type Backend = { request(operation: string, payload: unknown, binding: ToolHost): Promise<any> };

/** Keeps server-issued leases private to this extension runtime. */
export class SessionAdapter {
  #current: Session | undefined;
  constructor(private readonly host: ToolHost) {}
  private async backend(): Promise<Backend> { return await this.host.backend() as Backend; }
  private writable() { return this.host.mode === "full" && this.host.role === "manager"; }
  async start(externalId: string) {
    if (!this.writable() || this.#current) return;
    const value = await (await this.backend()).request("memory.session.start", { externalId }, this.host);
    this.#current = { handle: value.handle, handoff: "" };
    const context = await (await this.backend()).request("memory.session.context", { handle: value.handle }, this.host);
    this.#current.handoff = typeof context?.handoff === "string" ? context.handoff.slice(0, 4096) : "";
  }
  async checkpoint() {
    if (!this.writable() || !this.#current) return;
    const value = await (await this.backend()).request("memory.session.checkpoint", { handle: this.#current.handle }, this.host);
  }
  async renew() {
    if (!this.writable() || !this.#current) return;
    const value = await (await this.backend()).request("memory.session.renew", { handle: this.#current.handle }, this.host);
  }
  async saveDraft(summary: string) {
    if (!this.writable() || !this.#current) throw new Error("no writable active session");
    if (summary.length > 4096) throw new Error("session handoff exceeds limit");
    const payload: Record<string, string> = { handle: this.#current.handle, summary };
    if (this.#current.draftUpdatedAt) payload.expectedUpdatedAt = this.#current.draftUpdatedAt;
    const value = await (await this.backend()).request("memory.session.draft_save", payload, this.host);
    this.#current.draftUpdatedAt = value.updatedAt;
  }
  context() {
    if (!this.writable() || !this.#current) throw new Error("no writable active session");
    return { handoff: this.#current.handoff, trust: "UNTRUSTED" };
  }
  async end(state: "completed" | "cancelled" | "interrupted", summary = "") {
    if (!this.writable() || !this.#current) return;
    const current = this.#current;
    await (await this.backend()).request("memory.session.end", { handle: current.handle, state, summary: state === "completed" ? summary : "" }, this.host);
    this.#current = undefined;
  }
}

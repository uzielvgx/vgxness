/** Injectable HTTP boundary. Production fetch is bounded and never follows redirects. */
export type HttpRequest = { method: "GET" | "POST"; url: string; headers: Record<string, string>; body?: string; signal?: AbortSignal; maxResponseBytes?: number };
export type HttpResponse = { status: number; headers?: Record<string, string>; body: string };
export interface HttpPort { request(request: HttpRequest): Promise<HttpResponse>; }
export type SyncErrorKind = "unauthorized" | "unavailable" | "remote" | "invalid" | "unsupported" | "context";
export class SyncHttpError extends Error {
  readonly operation: string;
  readonly kind: SyncErrorKind;
  readonly status: number;
  readonly code: string;
  constructor(operation: string, kind: SyncErrorKind, status = 0, code = "") {
    super(`sync ${operation} ${kind}${status ? ` (${status})` : ""}`);
    this.operation = operation; this.kind = kind; this.status = status; this.code = code;
  }
}
export class HttpResponseInvalid extends Error { constructor() { super("invalid sync HTTP response"); } }
export class HttpBodyReadError extends Error { constructor() { super("sync HTTP body read failed"); } }
export class FetchHttp implements HttpPort {
  private readonly fetcher: typeof fetch;
  private readonly timeoutMs: number;
  constructor(fetcher: typeof fetch = globalThis.fetch, timeoutMs = 30_000) {
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1) throw new TypeError("invalid HTTP timeout");
    this.fetcher = fetcher; this.timeoutMs = timeoutMs;
  }
  async request(request: HttpRequest): Promise<HttpResponse> {
    const signal = AbortSignal.any([AbortSignal.timeout(this.timeoutMs), ...(request.signal ? [request.signal] : [])]);
    const response = await this.fetcher(request.url, { method: request.method, headers: request.headers, body: request.body, redirect: "manual", signal });
    if (!response) throw new HttpResponseInvalid();
    const headers = Object.fromEntries(response.headers.entries());
    if (!response.body) return { status: response.status, headers, body: "" };
    const reader = response.body.getReader(), chunks: Uint8Array[] = [];
    let size = 0;
    try {
      while (true) {
        const part = await reader.read(); if (part.done) break;
        size += part.value.byteLength;
        if (size > (request.maxResponseBytes ?? (1 << 20))) throw new HttpResponseInvalid();
        chunks.push(part.value);
      }
      let body: string;
      try { body = new TextDecoder("utf-8", { fatal: true }).decode(Buffer.concat(chunks)); } catch { throw new HttpResponseInvalid(); }
      return { status: response.status, headers, body };
    } catch (error) {
      // Remote error bodies are optional diagnostics; their contents cannot override the status.
      if (response.status !== 200 && !signal.aborted) return { status: response.status, headers, body: "" };
      if (!signal.aborted && !(error instanceof HttpResponseInvalid)) throw new HttpBodyReadError();
      throw error;
    } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
  }
}

export type ViewState = "loading" | "available" | "unavailable" | "stale";
export type ReadonlySnapshot<T> = { state: "available"; observedAt: string; value: T } | { state: "stale"; observedAt: string; value: T; reason: "read_failed" } | { state: "unavailable"; reason: "read_unavailable" };

export async function readonlySnapshot<T>(read: () => Promise<T>, previous?: ReadonlySnapshot<T>): Promise<ReadonlySnapshot<T>> {
  try { const value=await read(); return { state: "available", observedAt: new Date().toISOString(), value }; }
  catch { return previous && (previous.state === "available" || previous.state === "stale") ? { state: "stale", observedAt: previous.observedAt, value: previous.value, reason: "read_failed" } : { state: "unavailable", reason: "read_unavailable" }; }
}
export const loadingView = () => ({ state: "loading" as ViewState });
const clip = (value: unknown, limit: number) => { if(typeof value!=="string") return "unknown"; let out="",used=0; for(const cp of value){const size=Buffer.byteLength(cp);if(used+size>limit)break;out+=cp;used+=size;} return out; };
const timestamp = (value: unknown) => typeof value === "string" && Number.isFinite(new Date(value).getTime()) ? value : "unknown";
export function nativeMemoryView(value: unknown, observedAt = new Date().toISOString()) {
  if (!Array.isArray(value)) throw new Error("memory view data unavailable");
  const entries = value;
  return { state: "available" as ViewState, observedAt, trust: "UNTRUSTED", entries: entries.slice(0, 10).filter(item => item && typeof item === "object").map((item: any) => ({ id: clip(item.ID, 256), title: clip(item.Title, 256), preview: clip(item.Preview, 1024), producer: clip(item.Producer, 256), source: { provider: clip(item.SourceProvider, 256), id: clip(item.SourceID, 256) }, createdAt: timestamp(item.CreatedAt), updatedAt: timestamp(item.UpdatedAt), references: (Array.isArray(item.References) ? item.References : []).filter((ref: unknown) => typeof ref === "string").slice(0, 8).map((ref: string) => clip(ref, 256)) })) };
}

/** Keep native command output bounded and serializable.  Builders never invoke
 * lifecycle operations; callers supply already-read authority data. */
export function nativeStatusView(input: {
  workspace: string; mode: string; role: string; session: unknown; workerCli?: string;
  backendVersion?: string; packageVersion?: string; piVersion?: string; platform?: string; arch?: string; modelCatalog?: unknown;
}) {
  const windows = (input.platform ?? process.platform) === "win32";
  return {
    state: "available" as ViewState, runtime: "native-typescript", workspace: input.workspace,
    mode: input.mode, role: input.role, backendVersion: input.backendVersion ?? "unknown", packageVersion: input.packageVersion ?? "unknown", piVersion: input.piVersion ?? "unknown", nodeVersion: process.version,
    platform: input.platform ?? process.platform, arch: input.arch ?? process.arch, session: input.session,
    workers: windows ? { state: "unavailable" as ViewState, reason: "worker process tree ownership is unsupported on Windows" } :
      input.workerCli ? { state: "available" as ViewState } : { state: "unavailable" as ViewState, reason: "Pi CLI not discovered" },
    modelCatalog: input.modelCatalog ? { state: "available" as ViewState, value: input.modelCatalog } : { state: "unavailable" as ViewState, reason: "runtime model catalog unavailable" },
  };
}

export function nativeWorkersView(workerCli: string | undefined, workers: unknown[], platform = process.platform) {
  if (platform === "win32") return { state: "unavailable" as ViewState, reason: "worker process tree ownership is unsupported on Windows", workers: [] };
  return { state: workerCli ? "available" as ViewState : "unavailable" as ViewState, ...(workerCli ? {} : { reason: "Pi CLI not discovered" }), workers: workers.slice(-32) };
}

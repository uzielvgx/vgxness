import { frameReadOutcome } from "./tools/read-outcome.ts";
import { renderPiManagerPrompt } from "./orchestration/adapter.ts";
import { createSkillTool } from "./tools/skill.ts";
import { loadManagerContract, renderManagerPrompt } from "./orchestration/contract.ts";
import { fileURLToPath, pathToFileURL } from "node:url";
import { VERSION as PI_VERSION } from "@earendil-works/pi-coding-agent";
import { createHash } from "node:crypto";
import { delimiter, dirname, join } from "node:path";
import { constants } from "node:fs";
import { lstat, open, readFile, realpath } from "node:fs/promises";
import { Type } from "typebox";
import { Value } from "typebox/value";
import { createApplyPatchTool } from "./tools/apply_patch.ts";
import { createMemoryTools, type ToolHost } from "./tools/memory.ts";
import { createModelTool } from "./tools/model.ts";
import { createQuestionTool } from "./tools/question.ts";
import { createSddTool } from "./tools/sdd.ts";
import { createTodoWriteTool } from "./tools/todowrite.ts";
import { createNativeDispatcher } from "./service/dispatcher.ts";
import { SessionAdapter } from "./session/adapter.ts";
import { discoverSkillPaths } from "./skills/catalog.ts";
import { createTaskTool } from "./tools/task.ts";
import { executePiWorker } from "./workers/runner.ts";
import { nativeStatusView, nativeWorkersView, nativeMemoryView, readonlySnapshot, loadingView } from "./views.ts";
import { WorkerResultError } from "./workers/result.ts";

const handoffSchema = Type.Object({ summary: Type.String({ minLength: 1, maxLength: 4096 }) }, { additionalProperties: false });
type ExtensionApi = { registerCommand?(name: string, command: any): void; getCommands?(): any[]; registerTool(tool: unknown): void; on(event: string, handler: (event: any, ctx: any) => unknown): void; appendEntry(type: string, data: unknown): void };
export type PiToolHost = ToolHost & { storageRoot?: string };


/** Discover the currently installed Pi CLI without spawning a package manager. */
export async function discoverWorkerCli() {
  const candidates = [process.argv[1], ...(process.env.PATH ?? "").split(delimiter).filter(Boolean).map(path => join(path, process.platform === "win32" ? "pi.cmd" : "pi"))];
  for (const candidate of candidates) {
    if (!candidate) continue;
    try {
      const cli = await realpath(candidate);
      for (const relative of ["../..", ".."]) {
        const pkg = JSON.parse(await readFile(join(dirname(cli), relative, "package.json"), "utf8").catch(() => "null"));
        if (pkg?.name === "@earendil-works/pi-coding-agent" && cli.endsWith(".js")) return cli;
      }
    } catch {}
  }
  return undefined;
}

function runtimeModels(context: any) {
  const selected = context?.model;
  const available = context?.modelRegistry?.getAvailable?.();
  if (!selected?.provider || !Array.isArray(available)) return [];
  const scoped = context?.scopedModels;
  const allowed = Array.isArray(scoped) && scoped.length ? new Set(scoped.map((item: any) => `${item.provider ?? item.model?.provider}/${item.id ?? item.model?.id}`)) : undefined;
  return available.filter((item: any) => item?.provider && item?.id && (!allowed || allowed.has(`${item.provider}/${item.id}`)));
}

function nativeEffort(model: any, effort: string) {
  return ["low", "medium", "high"].includes(effort) && model?.reasoning === true && model.thinkingLevelMap?.[effort] !== null;
}

function modelCatalog(context: any) {
  const selected = context?.model;
  if (!selected?.provider) return undefined;
  const available = runtimeModels(context);
  const current = available.find((item: any) => item.provider === selected.provider && item.id === selected.id);
  if (!current) return undefined;
  const scoped = Array.isArray(context?.scopedModels) && context.scopedModels.length > 0;
  const candidates = scoped ? available.filter((item: any) => item.provider === current.provider) : [current];
  if (candidates.length === 0 || candidates.length > 3) return undefined;
  return { provider: current.provider, models: candidates.map((item: any) => ({ provider: item.provider, id: item.id, name: item.name, supportedEfforts: ["low", "medium", "high"].filter((level) => nativeEffort(item, level)) })) };
}

function acceptedBindingVerifier(host: PiToolHost) {
  return async (binding: any) => {
    const client = await host.backend();
    const revisionFor = async (ref: any): Promise<any> => await client.request("sdd.get_revision", { changeId: binding.changeId, revisionId: ref.revisionId }, host);
    const valid = async (revision: any, ref: any) => {
      if (revision?.id !== ref.revisionId || revision?.changeId !== binding.changeId || revision?.artifactId !== ref.artifactId || revision?.digest !== ref.digest || revision?.status !== "accepted" || revision?.artifactStatus !== "accepted") return false;
      let content: Buffer;
      if (typeof revision.content === "string") content = Buffer.from(revision.content, "base64");
      else {
        // OpenSpec-only revisions retain their canonical bytes in the accepted file.
        const names: Record<string, string> = { explore: "research.md", proposal: "proposal.md", spec: "spec.md", design: "design.md", tasks: "tasks.md", apply: "apply-result.md", verify: "verification.md" };
        if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(binding.changeId) || !names[revision.artifact] || revision.externalLocation !== `openspec/changes/${binding.changeId}/${names[revision.artifact]}`) return false;
        try {
          let path = host.workspace;
          for (const part of revision.externalLocation.split("/")) { path = join(path, part); const info = await lstat(path); if (info.isSymbolicLink()) return false; }
          const before = await lstat(path);
          if (!before.isFile() || before.size > 4 * 1024 * 1024) return false;
          const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
          try { const opened = await file.stat(); if (!opened.isFile() || opened.dev !== before.dev || opened.ino !== before.ino) return false; content = await file.readFile(); } finally { await file.close(); }
          const after = await lstat(path);
          if (after.isSymbolicLink() || before.dev !== after.dev || before.ino !== after.ino || before.size !== after.size || before.mtimeMs !== after.mtimeMs || before.ctimeMs !== after.ctimeMs) return false;
        } catch { return false; }
      }
      return createHash("sha256").update(content).digest("hex") === ref.digest;
    };
    const taskRevision = await revisionFor(binding);
    const key = (ref: any) => `${ref.artifactId}\u0000${ref.revisionId}\u0000${ref.digest}`;
    const required = Array.isArray(taskRevision?.inputs) ? taskRevision.inputs : [];
    const supplied = Array.isArray(binding.inputs) ? binding.inputs : [];
    const requiredKeys = required.map(key).sort(), suppliedKeys = supplied.map(key).sort();
    const exactInputs = requiredKeys.length > 0 && requiredKeys.length === suppliedKeys.length && requiredKeys.every((value: string, index: number) => value === suppliedKeys[index]) && new Set(suppliedKeys).size === suppliedKeys.length;
    const revisions = await Promise.all(supplied.map(revisionFor));
    const change: any = await client.request("sdd.get", { id: binding.changeId }, host);
    return taskRevision?.artifact === "tasks" && await valid(taskRevision, binding) && exactInputs && (await Promise.all(revisions.map((revision: any, index: number) => valid(revision, supplied[index])))).every(Boolean) && change?.phase === "apply" && change?.status === "active" && change?.stateVersion === binding.stateVersion && !!client.verifyCurrentAcceptedBinding && await client.verifyCurrentAcceptedBinding(binding);
  };
}

function supportsWorkerModel(context: any, selected: string, effort: string) {
  const slash = selected.indexOf("/");
  if (slash <= 0 || slash === selected.length - 1) return false;
  const provider = selected.slice(0, slash);
  const id = selected.slice(slash + 1);
  return runtimeModels(context).some((model: any) => model.provider === provider && model.id === id && nativeEffort(model, effort));
}

async function prepareWorkerAuthentication(context: any, mission: any) {
  const slash = mission.model.indexOf("/");
  if (slash <= 0 || slash === mission.model.length - 1) throw new Error("selected worker model invalid");
  const provider = mission.model.slice(0, slash);
  const id = mission.model.slice(slash + 1);
  const registry = context?.modelRegistry;
  const model = runtimeModels(context).find((item: any) => item.provider === provider && item.id === id);
  if (!model || !registry?.find?.(provider, id) || !registry?.hasConfiguredAuth?.(model)) throw Object.assign(new Error("worker authentication unavailable"), { code: "worker_unavailable" });
  const auth = await registry.getApiKeyAndHeaders(model);
  if (!auth?.ok || auth.env || typeof auth.apiKey !== "string") throw Object.assign(new Error("worker authentication unavailable"), { code: "worker_unavailable" });
  return { provider, apiKey: auth.apiKey, headers: auth.headers, baseUrl: auth.baseUrl, model: { id: model.id, name: model.name, api: model.api, baseUrl: model.baseUrl, reasoning: model.reasoning, thinkingLevelMap: model.thinkingLevelMap, input: model.input, cost: model.cost, contextWindow: model.contextWindow, maxTokens: model.maxTokens, samplingParams: model.samplingParams, headers: model.headers, compat: model.compat } };
}

function runtimeTaskTool(host: PiToolHost, context: () => any, workerCli?: string, injected?: (mission: any, signal?: AbortSignal, auth?: unknown, authority?: Readonly<{ expiresAt: number }>) => Promise<string>) {
  const runnerModule = pathToFileURL(join(dirname(fileURLToPath(import.meta.url)), "workers/runner.ts")).href;
  return createTaskTool({
    ...host,
    supportsModel: injected ? undefined : (selected, effort) => supportsWorkerModel(context(), selected, effort),
    prepareWorker: injected ? undefined : async (mission) => await prepareWorkerAuthentication(context(), mission),
    executeWorker: injected ?? (workerCli ? async (mission, signal, auth, authority) => await executePiWorker(mission, { cli: workerCli, runnerModule, signal, auth, authority }) : undefined),
    verifyAcceptedBinding: acceptedBindingVerifier(host),
  });
}

export function createPiTools(host: PiToolHost, pi: ExtensionApi) {
  return [createQuestionTool(), createTodoWriteTool(pi), createApplyPatchTool(host), ...createMemoryTools(host), createSddTool(host), createModelTool(host), ...(host.role === "manager" ? [createTaskTool(host), createSkillTool()] : [])];
}
export function createPiExtension(options: { workspace?: string; storageRoot?: string; credentialFile?: string; mode?: "full" | "read-only"; role?: string; resolvePackage?: (name: string) => Promise<string>; backend?: () => Promise<any>; workerCli?: string; executeWorker?: (mission: any, signal?: AbortSignal, auth?: unknown, authority?: Readonly<{ expiresAt: number }>) => Promise<string> } = {}) {
  return async function extension(pi: ExtensionApi) {
    const workspace = await realpath(options.workspace ?? process.cwd());
    let startup: Promise<any> | undefined;
    const mode = options.mode ?? "full";
    const role = options.role ?? "manager";
    const managerPrompt = role === "manager" ? await readFile(join(dirname(fileURLToPath(import.meta.url)), "../resources/prompts/manager.md"), "utf8") : "";
    if (role === "manager" && managerPrompt !== renderPiManagerPrompt(loadManagerContract())) throw new Error("generated Manager prompt drift");
    let runtimeContext: any;
    let sessions: SessionAdapter;
    const host: PiToolHost & { modelCatalog: () => any; mutationGrant: () => import("./session/adapter.ts").MutationGrant } = { workspace, mode, role, storageRoot: options.storageRoot, modelCatalog: () => modelCatalog(runtimeContext), mutationGuard: () => sessions.assertMutation(), mutationSignal: () => sessions.mutationSignal(), mutationGrant: () => sessions.mutationGrant(), backend: () => {
      if (!startup) startup = (async () => {
        if (options.backend) return await options.backend();
        return createNativeDispatcher({ workspace, storageRoot: options.storageRoot, credentialFile: options.credentialFile ?? process.env.VGXNESS_PI_CREDENTIAL_FILE, mode, role });
      })();
      return startup;
    } };
    sessions = new SessionAdapter(host);
    const workerCli = options.workerCli ?? process.env.VGXNESS_PI_CLI ?? await discoverWorkerCli();
    const workerStates = new Map<string, any>();
    const tools = createPiTools(host, pi).map((tool: any) => tool.name === "task" ? runtimeTaskTool(host, () => runtimeContext, workerCli, options.executeWorker) : tool);
    for (const tool of tools) {
      if (tool.name === "task") {
        const execute = tool.execute;
        tool.execute = async (id: string, input: any, ...args: any[]) => {
          const started = Date.now();
          workerStates.set(id, { role: input?.role ?? "unknown", state: "running", startedAt: new Date(started).toISOString() });
          while (workerStates.size > 32) workerStates.delete(workerStates.keys().next().value!);
          try { const result = await execute(id, input, ...args); const terminal=(result as any)?.details?.result; if (terminal) workerStates.set(id, terminal); while(workerStates.size>32)workerStates.delete(workerStates.keys().next().value!); return result; }
          catch (error) { const terminal=(error as any)?.result; if (terminal) workerStates.set(id, terminal); else workerStates.delete(id); while(workerStates.size>32)workerStates.delete(workerStates.keys().next().value!); throw error; }
        };
      }
      pi.registerTool(tool);
    }
    const viewRequest = async (operation: string, payload: unknown) => {
      if (options.backend) return (await host.backend()).request(operation, payload, host);
      const binding = { workspace, mode: "read-only" as const, role };
      const client = await createNativeDispatcher({ ...binding, storageRoot: options.storageRoot, credentialFile: options.credentialFile ?? process.env.VGXNESS_PI_CREDENTIAL_FILE });
      try { return await client.request(operation, payload, binding); } finally { await client.close(); }
    };
    const packageVersion = JSON.parse(await readFile(join(dirname(fileURLToPath(import.meta.url)), "../package.json"), "utf8")).version ?? "unknown";
    const commonRuntimeMetadata = { runtime: "native-typescript", packageVersion, backendVersion: packageVersion, piVersion: PI_VERSION, nodeVersion: process.version, platform: process.platform, arch: process.arch };
    const snapshots = new Map<string, any>();
    const snapshot = async (name: string, read: () => Promise<any>) => { const value=await readonlySnapshot(read,snapshots.get(name)); if(value.state==="available")snapshots.set(name,value); return value; };
    const views: Record<string, () => Promise<unknown>> = {
      "vgx-status": async () => await snapshot("vgx-status",async()=>{await viewRequest("memory.project.resolve",{});return nativeStatusView({ workspace, mode, role, session: sessions.status(), workerCli, packageVersion, backendVersion: packageVersion, piVersion: PI_VERSION, modelCatalog: modelCatalog(runtimeContext) });}),
      "vgx-workers": async () => await snapshot("vgx-workers",async()=>nativeWorkersView(workerCli,[...workerStates.values()])),
      "vgx-memory": async () => await snapshot("vgx-memory",async()=>nativeMemoryView(await viewRequest("memory.recent",{limit:10}))),
      "vgx-sdd": async () => await snapshot("vgx-sdd",async()=>({changes:await viewRequest("sdd.list",{status:"active",limit:10})})),
    };
    for (const [name, read] of Object.entries(views)) pi.registerCommand?.(name, { description: `Read VGXNESS ${name.slice(4)} status`, async handler(_args: string, ctx: any) {
      let value: unknown;
      ctx?.ui?.notify?.(JSON.stringify(loadingView()), "info");
      try { value = await read(); } catch (error) { value = { ...commonRuntimeMetadata, state: "unavailable", reason: "read_unavailable" }; }
      if(value && typeof value === "object" && "value" in (value as any)){const snap:any=value; const nested=snap.value&&typeof snap.value==="object"?snap.value:{}; value={...commonRuntimeMetadata,...nested,state:snap.state==="available"&&nested.state?snap.value.state:snap.state,observedAt:snap.observedAt,...(snap.reason?{reason:snap.reason}:{})};} else if(value && typeof value === "object") value={...commonRuntimeMetadata,...(value as any)};
      const text = JSON.stringify(value, null, 2);
      ctx?.ui?.notify?.(text, "info");
      return { content: [{ type: "text", text }] };
    } });
    pi.registerTool({ name: "session_handoff", label: "Save session handoff", description: "Save an explicit, sanitized summary for the current manager session.", parameters: handoffSchema, async execute(_id: string, input: { summary: string }) { if (!Value.Check(handoffSchema, input)) throw new Error("invalid tool input"); if (mode !== "full" || role !== "manager") throw new Error("session handoff requires full manager authority"); await sessions.saveDraft(input.summary); return { content: [{ type: "text", text: "Saved session handoff." }] }; } });
    pi.registerTool({ name: "session_context", label: "Read session handoff", description: "Read the bounded, untrusted handoff for the current manager session.", parameters: Type.Object({}, { additionalProperties: false }), async execute(_id: string, input: Record<string, never>) { if (Object.keys(input).length) throw new Error("invalid tool input"); return { content: [{ type: "text", text: JSON.stringify(sessions.context()) }] }; } });
    pi.on("resources_discover", async () => ({ skillPaths: await discoverSkillPaths({ existingNames: pi.getCommands?.().filter((command: any) => command.source === "skill").map((command: any) => command.name) }), promptPaths: [join(dirname(fileURLToPath(import.meta.url)), "../resources/prompts")] }));
    pi.on("tool_result", (event) => frameReadOutcome(event));
    pi.on("before_agent_start", async (event, ctx) => { runtimeContext = ctx; return managerPrompt ? { systemPrompt: `${event.systemPrompt}\n\n${managerPrompt}` } : undefined; });
    pi.on("session_start", async (_event, ctx) => { runtimeContext = ctx; await sessions.start(ctx.sessionManager.getSessionId()); });
    pi.on("session_tree", async () => { await sessions.checkpoint(); });
    pi.on("session_compact", async () => { await sessions.checkpoint(); });
    pi.on("agent_settled", async () => { await sessions.renew(); });
    pi.on("session_shutdown", async (event) => { let failure: unknown; try { if (event.reason === "reload" || event.reason === "resume") await sessions.detach(); else await sessions.end(event.reason === "quit" ? "completed" : "interrupted"); } catch (error) { failure = error; throw error; } finally { try { await startup?.then((client) => client.close?.()); } catch (closeError) { if (!failure) throw closeError; } finally { startup = undefined; } } });
  };
}
export default createPiExtension();

import { fileURLToPath, pathToFileURL } from "node:url";
import { createHash } from "node:crypto";
import { dirname, join } from "node:path";
import { readFile, realpath } from "node:fs/promises";
import { Type } from "typebox";
import { Value } from "typebox/value";
import { createApplyPatchTool } from "./tools/apply_patch.ts";
import { createMemoryTools, type ToolHost } from "./tools/memory.ts";
import { createModelTool } from "./tools/model.ts";
import { createQuestionTool } from "./tools/question.ts";
import { createSddTool } from "./tools/sdd.ts";
import { createTodoWriteTool } from "./tools/todowrite.ts";
import { selectBackend } from "./backend/select.ts";
import { startBackend } from "./backend/supervisor.ts";
import { SessionAdapter } from "./session/adapter.ts";
import { discoverSkillPaths } from "./skills/catalog.ts";
import { createTaskTool } from "./tools/task.ts";
import { executePiWorker } from "./workers/runner.ts";

const capabilities = ["memory.forget", "memory.get", "memory.project.initialize", "memory.project.resolve", "memory.recall", "memory.recent", "memory.remember", "memory.session.checkpoint", "memory.session.context", "memory.session.draft_save", "memory.session.end", "memory.session.renew", "memory.session.start", "memory.sync", "memory.sync.backfill", "memory.sync.configure", "memory.sync.rejoin", "memory.sync.repair_project", "memory.sync.reseed", "memory.sync.status", "model.resolve", "sdd.accept_revision", "sdd.cancel", "sdd.compare_projection", "sdd.create", "sdd.get", "sdd.get_revision", "sdd.list", "sdd.list_revisions", "sdd.projection_status", "sdd.record_projection", "sdd.render_projection", "sdd.save_revision", "sdd.set_interaction_mode", "sdd.transition"];
const handoffSchema = Type.Object({ summary: Type.String({ minLength: 1, maxLength: 4096 }) }, { additionalProperties: false });
type ExtensionApi = { registerTool(tool: unknown): void; on(event: string, handler: (event: any, ctx: any) => unknown): void; appendEntry(type: string, data: unknown): void };
export type PiToolHost = ToolHost & { storageRoot?: string };

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
    const revisionFor = async (ref: any) => await client.request("sdd.get_revision", { changeId: binding.changeId, revisionId: ref.revisionId }, host);
    const valid = (revision: any, ref: any) => {
      const content = typeof revision?.content === "string" ? Buffer.from(revision.content, "base64") : Buffer.alloc(0);
      return revision?.id === ref.revisionId && revision?.changeId === binding.changeId && revision?.artifactId === ref.artifactId && revision?.digest === ref.digest && revision?.status === "accepted" && revision?.artifactStatus === "accepted" && createHash("sha256").update(content).digest("hex") === ref.digest;
    };
    const taskRevision = await revisionFor(binding);
    const key = (ref: any) => `${ref.artifactId}\u0000${ref.revisionId}\u0000${ref.digest}`;
    const required = Array.isArray(taskRevision?.inputs) ? taskRevision.inputs : [];
    const supplied = Array.isArray(binding.inputs) ? binding.inputs : [];
    const requiredKeys = required.map(key).sort(), suppliedKeys = supplied.map(key).sort();
    const exactInputs = requiredKeys.length > 0 && requiredKeys.length === suppliedKeys.length && requiredKeys.every((value: string, index: number) => value === suppliedKeys[index]) && new Set(suppliedKeys).size === suppliedKeys.length;
    const revisions = await Promise.all(supplied.map(revisionFor));
    const change = await client.request("sdd.get", { id: binding.changeId }, host);
    return valid(taskRevision, binding) && exactInputs && revisions.every((revision: any, index: number) => valid(revision, supplied[index])) && change?.phase === "apply" && change?.status === "active" && change?.stateVersion === binding.stateVersion;
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
  if (!model || !registry?.find?.(provider, id) || !registry?.hasConfiguredAuth?.(model)) throw new Error("selected worker model authentication unavailable");
  const auth = await registry.getApiKeyAndHeaders(model);
  if (!auth?.ok || auth.env || typeof auth.apiKey !== "string") throw new Error("selected worker authentication transport unsupported");
  return { provider, apiKey: auth.apiKey, headers: auth.headers, baseUrl: auth.baseUrl, model: { id: model.id, name: model.name, api: model.api, baseUrl: model.baseUrl, reasoning: model.reasoning, thinkingLevelMap: model.thinkingLevelMap, input: model.input, cost: model.cost, contextWindow: model.contextWindow, maxTokens: model.maxTokens, samplingParams: model.samplingParams, headers: model.headers, compat: model.compat } };
}

function runtimeTaskTool(host: PiToolHost, context: () => any, workerCli?: string) {
  const runnerModule = pathToFileURL(join(dirname(fileURLToPath(import.meta.url)), "workers/runner.ts")).href;
  return createTaskTool({
    ...host,
    supportsModel: (selected, effort) => supportsWorkerModel(context(), selected, effort),
    prepareWorker: async (mission) => await prepareWorkerAuthentication(context(), mission),
    executeWorker: workerCli ? async (mission, signal, auth) => await executePiWorker(mission, { cli: workerCli, runnerModule, signal, auth }) : undefined,
    verifyAcceptedBinding: acceptedBindingVerifier(host),
  });
}

export function createPiTools(host: PiToolHost, pi: ExtensionApi) {
  return [createQuestionTool(), createTodoWriteTool(pi), createApplyPatchTool(host), ...createMemoryTools(host), createSddTool(host), createModelTool(host), ...(host.role === "manager" ? [createTaskTool(host)] : [])];
}
export function createPiExtension(options: { workspace?: string; storageRoot?: string; mode?: "full" | "read-only"; role?: string; resolvePackage?: (name: string) => Promise<string>; backend?: () => Promise<any>; workerCli?: string } = {}) {
  return async function extension(pi: ExtensionApi) {
    const workspace = await realpath(options.workspace ?? process.cwd());
    let startup: Promise<any> | undefined;
    const mode = options.mode ?? "full";
    const role = options.role ?? "manager";
    const managerPrompt = role === "manager" ? await readFile(join(dirname(fileURLToPath(import.meta.url)), "../resources/prompts/manager.md"), "utf8") : "";
    let runtimeContext: any;
    const host: PiToolHost & { modelCatalog: () => any } = { workspace, mode, role, storageRoot: options.storageRoot, modelCatalog: () => modelCatalog(runtimeContext), backend: () => {
      if (!startup) startup = (async () => {
        if (options.backend) return await options.backend();
        const selected = await selectBackend({ root: dirname(fileURLToPath(import.meta.url)), version: "0.1.0", resolvePackage: options.resolvePackage ?? (async (name) => dirname(fileURLToPath(import.meta.resolve(`${name}/package.json`)))) });
        return startBackend({ binary: selected.binary, workspace, storageRoot: options.storageRoot, mode, role, hello: { type: "hello", protocol: "vgxness-pi/v1", implementation: { name: "vgxness-pi-backend", version: selected.manifest.version, sha256: selected.manifest.sha256 }, workspace, mode, role, capabilities, limits: { maxRecordBytes: 1048576, maxActiveRequests: 32, maxCorrelationIds: 4096 } } });
      })();
      return startup;
    } };
    const sessions = new SessionAdapter(host);
    const workerCli = options.workerCli ?? process.env.VGXNESS_PI_CLI;
    const tools = createPiTools(host, pi).map((tool: any) => tool.name === "task" ? runtimeTaskTool(host, () => runtimeContext, workerCli) : tool);
    for (const tool of tools) pi.registerTool(tool);
    pi.registerTool({ name: "session_handoff", label: "Save session handoff", description: "Save an explicit, sanitized summary for the current manager session.", parameters: handoffSchema, async execute(_id: string, input: { summary: string }) { if (!Value.Check(handoffSchema, input)) throw new Error("invalid tool input"); if (mode !== "full" || role !== "manager") throw new Error("session handoff requires full manager authority"); await sessions.saveDraft(input.summary); return { content: [{ type: "text", text: "Saved session handoff." }] }; } });
    pi.registerTool({ name: "session_context", label: "Read session handoff", description: "Read the bounded, untrusted handoff for the current manager session.", parameters: Type.Object({}, { additionalProperties: false }), async execute(_id: string, input: Record<string, never>) { if (Object.keys(input).length) throw new Error("invalid tool input"); return { content: [{ type: "text", text: JSON.stringify(sessions.context()) }] }; } });
    pi.on("resources_discover", async () => ({ skillPaths: await discoverSkillPaths({ existingNames: pi.getCommands?.().filter((command: any) => command.source === "skill").map((command: any) => command.name) }), promptPaths: [join(dirname(fileURLToPath(import.meta.url)), "../resources/prompts")] }));
    pi.on("before_agent_start", async (event, ctx) => { runtimeContext = ctx; return managerPrompt ? { systemPrompt: `${event.systemPrompt}\n\n${managerPrompt}` } : undefined; });
    pi.on("session_start", async (_event, ctx) => { await sessions.start(ctx.sessionManager.getSessionId()); });
    pi.on("session_compact", async () => { await sessions.checkpoint(); });
    pi.on("agent_settled", async () => { await sessions.renew(); });
    pi.on("session_shutdown", async (event) => { let failure: unknown; try { await sessions.end(event.reason === "quit" ? "completed" : "interrupted"); } catch (error) { failure = error; throw error; } finally { try { await startup?.then((client) => client.close?.()); } catch (closeError) { if (!failure) throw closeError; } finally { startup = undefined; } } });
  };
}
export default createPiExtension();

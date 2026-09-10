import { nativeWorkerRole } from "../models/plan.ts";
import { snapshotWorkerSkills } from "../workers/context.ts";
import { Type } from "typebox";
import { Value } from "typebox/value";
import { acceptMission, issueMission, revalidateWorkerMission, type WorkerMission } from "../workers/mission.ts";
import { WorkerRunner } from "../workers/runner.ts";
import { workerCanWrite } from "../workers/roles.ts";
import type { MutationGrant } from "../session/adapter.ts";
import { PiWorkerTransportError, WorkerResultError, workerTerminalEnvelope } from "../workers/result.ts";
const revisionRef = Type.Object({ artifactId: Type.String({ minLength: 1 }), revisionId: Type.String({ minLength: 1 }), digest: Type.String({ minLength: 64, maxLength: 64 }) }, { additionalProperties: false });
export const taskRequestSchema = Type.Object({ candidate: Type.Optional(Type.Object({ commit: Type.Optional(Type.String({ minLength: 40, maxLength: 40, pattern: "^[0-9a-f]{40}$" })), snapshot: Type.Optional(Type.String({ minLength: 64, maxLength: 64, pattern: "^[0-9a-f]{64}$" })) }, { minProperties: 1, additionalProperties: false, description: "Full candidate references supplied by the Manager. Include commit and snapshot when both are known; copy exact lowercase hex values, never abbreviate them or put them in targets. Propagated with the mission and terminal, not proof of checkout identity. Omit only when no candidate is known." })), skills: Type.Optional(Type.Array(Type.Object({ name: Type.String({ minLength: 1, maxLength: 64 }), sha256: Type.String({ pattern: "^[0-9a-f]{64}$" }), resources: Type.Optional(Type.Array(Type.String({ minLength: 1, maxLength: 512 }), { maxItems: 7 })) }, { additionalProperties: false }), { maxItems: 8 })), goal: Type.String({ minLength: 1, maxLength: 4096, description: "Bounded mission, at most 4096 characters. Put inline review content and other metadata here, never in targets. Use candidate for the full known commit/snapshot reference. Summarize long designs or authorize a real file instead." }), nonce: Type.String({ minLength: 1, maxLength: 128 }), role: Type.String({ minLength: 1, maxLength: 32 }), mode: Type.Union([Type.Literal("full"), Type.Literal("read-only")]), model: Type.String({ minLength: 1, maxLength: 256 }), effort: Type.String({ minLength: 1, maxLength: 32 }), criteria: Type.Array(Type.String({ minLength: 1, maxLength: 512 }), { maxItems: 16 }), commands: Type.Array(Type.Array(Type.String({ minLength: 1, maxLength: 512 }), { minItems: 1, maxItems: 32 }), { maxItems: 16, description: "Executable argv allowlist for command-capable roles. Always use [] for explore; use native file tools authorized by targets or exploration instead, and do not put tool names such as read, list, or search into commands." }), resultLimit: Type.Integer({ minimum: 1, maximum: 65536 }), acceptedBindings: Type.Optional(Type.Object({ changeId: Type.String(), artifactId: Type.String(), revisionId: Type.String(), digest: Type.String({ minLength: 64, maxLength: 64 }), stateVersion: Type.Integer({ minimum: 0 }), inputs: Type.Array(revisionRef, { minItems: 1, maxItems: 8 }) }, { additionalProperties: false })), exploration: Type.Optional(Type.Object({ roots: Type.Array(Type.String({ minLength: 1, maxLength: 512 }), { minItems: 1, maxItems: 16, description: "Existing workspace-relative directory paths only, never file paths. Symlinks and parent traversal are rejected." }), maxFiles: Type.Integer({ minimum: 1, maximum: 10000 }), maxBytes: Type.Integer({ minimum: 1, maximum: 16777216 }), maxTokens: Type.Integer({ minimum: 1, maximum: 65536 }) }, { additionalProperties: false, description: "Optional directory traversal budget for the explore role. Omit exploration for exact-file-only read missions and use targets instead. Directory roots authorize exploration beyond the files listed in targets; include only directories whose broader exploration is authorized." })), targets: Type.Record(Type.String({ minLength: 1, maxLength: 512 }), Type.Union([Type.String({ minLength: 64, maxLength: 64, pattern: "^[0-9a-f]{64}$" }), Type.Literal("ABSENT")]), { maxProperties: 64, description: "File ledger only: workspace-relative file paths mapped to their observed lowercase SHA-256 (64 hex characters), or ABSENT for an authorized new file. Never add candidate, commit, manifest or other metadata entries. For review of content provided entirely in goal, use {}. Existing file paths and bytes are checked before launch." }) }, { additionalProperties: false });
export function createTaskTool(host: { role: string; mode?: "full" | "read-only"; workspace: string; mutationGuard?: () => void; mutationSignal?: () => AbortSignal; mutationGrant?: () => MutationGrant; resolveSkills?: typeof snapshotWorkerSkills; runner?: WorkerRunner; executeWorker?: (mission: WorkerMission, signal?: AbortSignal, auth?: unknown, authority?: Readonly<{ expiresAt: number }>) => Promise<string>; prepareWorker?: (mission: WorkerMission) => Promise<unknown>; supportsModel?: (model: string, effort: string) => boolean; verifyAcceptedBinding?: (binding: any) => Promise<boolean> }) {
  const runner = host.runner ?? new WorkerRunner();
  return {
    name: "task", label: "Run worker", description: "Delegate one bounded mission with explicit criteria, targets, commands, and optional managed skills. Skills resources are relative paths selected after reading SKILL.md; they never expand authority.", parameters: taskRequestSchema,
    async execute(_id: string, input: any, signal?: AbortSignal) {
      if (host.role !== "manager") throw new Error("task requires manager authority");
      if (!Value.Check(taskRequestSchema, input)) throw new Error("invalid task input");
      if (host.mode === "read-only" && input.mode !== "read-only") throw new Error("worker mode exceeds parent authority");
      const candidate = input.candidate === undefined ? undefined : Object.freeze({ ...input.candidate });
      const role = nativeWorkerRole(input.role);
      if (input.mode === "full" && !workerCanWrite(role)) throw new Error("worker role cannot request full mode");
      // Capture the lease before any awaited binding or skill work; renewal must
      // never make this already-issued task inherit a later authority signal.
      let grant: MutationGrant | undefined;
      let grantError: unknown;
      if (input.mode === "full") try { grant = host.mutationGrant?.(); } catch (error) { grantError = error; }
      if (role === "sdd-apply" && (!input.acceptedBindings || !host.verifyAcceptedBinding || !(await host.verifyAcceptedBinding(input.acceptedBindings)))) throw new Error("sdd worker accepted binding rejected");
      const started = Date.now();
      const unavailable = (detail: string) => { throw new WorkerResultError(workerTerminalEnvelope({ candidate, nonce: input.nonce, role, mode: input.mode, scope: Object.keys(input.targets ?? {}), skills: input.skills, state: "unavailable", reasonCode: "unavailable", detail, startedAt: started, model: input.model, effort: input.effort, resultLimit: input.resultLimit })); };
      if (!host.executeWorker) unavailable("worker execution transport unavailable");
      if (host.supportsModel && !host.supportsModel(input.model, input.effort)) unavailable("worker model or effort unsupported");
      if (input.mode === "full" && (grantError || !grant || !Number.isFinite(grant.expiresAt) || grant.expiresAt <= Date.now() || grant.signal.aborted)) unavailable("mutation grant unavailable");
      if (input.mode === "full") host.mutationGuard?.();
      const skills = input.skills?.length ? await (host.resolveSkills ?? snapshotWorkerSkills)(input.skills) : undefined;
      const mission = await acceptMission(issueMission({ ...(candidate !== undefined ? { candidate } : {}), ...(skills ? { skills } : {}), nonce: input.nonce, role, workspace: host.workspace, mode: input.mode, model: input.model, effort: input.effort, goal: input.goal, criteria: input.criteria, commands: input.commands, resultLimit: input.resultLimit, ...(input.acceptedBindings ? { acceptedBindings: input.acceptedBindings } : {}), ...(input.exploration ? { exploration: input.exploration } : {}), targets: input.targets }));
      const sessionSignal = input.mode === "full" ? grant!.signal : undefined;
      const combined = signal && sessionSignal ? AbortSignal.any([signal, sessionSignal]) : signal ?? sessionSignal;
      try { const result = await runner.run(mission, async (workerSignal) => {
        if (mission.mode === "full") { host.mutationGuard?.(); if (!grant || grant.signal.aborted || Date.now() >= grant.expiresAt) throw new Error("mutation grant unavailable"); }
        await revalidateWorkerMission(mission);
        if (mission.role === "sdd-apply" && (!host.verifyAcceptedBinding || !(await host.verifyAcceptedBinding(mission.acceptedBindings)))) throw new Error("sdd worker accepted binding rejected before launch");
        const auth = await host.prepareWorker?.(mission);
        if (mission.mode === "full") { host.mutationGuard?.(); if (!grant || grant.signal.aborted || Date.now() >= grant.expiresAt) throw new Error("mutation grant unavailable"); }
        if (workerSignal?.aborted) throw new Error("worker cancelled before launch");
        return await host.executeWorker!(mission, workerSignal, auth, grant ? Object.freeze({ expiresAt: grant.expiresAt }) : undefined);
      }, combined); const report = (()=>{try{return JSON.parse(result)}catch{return {text:result}}})(); const terminal=workerTerminalEnvelope({candidate:mission.candidate,nonce:mission.nonce,role:mission.role,mode:mission.mode,scope:Object.keys(mission.targets),skills:input.skills,state:"succeeded",reasonCode:"succeeded",detail:"worker settled",startedAt:started,model:mission.model,effort:mission.effort,resultLimit:mission.resultLimit,report}); if(terminal.state!=="succeeded")throw new WorkerResultError(terminal); return { content: [{ type: "text", text: JSON.stringify(terminal) }], details:{result:terminal} };
      } catch (error) {
        const previous = error && typeof error === "object" && (error as any).result?.nonce === mission.nonce ? (error as any).result : undefined;
        const report = previous ? { text: previous.text, usage: previous.usage, diagnostics: previous.diagnostics, truncated: previous.truncated, selectedModel: previous.model?.selected, effectiveModel: previous.model?.effective, effectiveProvider: previous.model?.provider, selectedEffort: previous.effort?.selected, effectiveEffort: previous.effort?.effective, recovery: previous.recovery } : error && typeof error === "object" && (error as any).report && typeof (error as any).report === "object" ? (error as any).report : undefined;
        const unavailable = (error as any)?.code === "worker_unavailable" || report?.reasonCode === "unavailable";
        const cancelled = combined?.aborted && !report?.recovery?.pending;
        const terminal = workerTerminalEnvelope({ candidate: mission.candidate, nonce: mission.nonce, role: mission.role, mode: mission.mode, scope: Object.keys(mission.targets), skills: input.skills, state: unavailable ? "unavailable" : cancelled ? "cancelled" : "failed", reasonCode: unavailable ? "unavailable" : cancelled ? "cancelled" : report?.reasonCode ?? "failed", detail: error instanceof Error ? error.message : "worker failed", startedAt: started, model: mission.model, effort: mission.effort, resultLimit: mission.resultLimit, report });
        throw new WorkerResultError(terminal);
      }
    },
  };
}

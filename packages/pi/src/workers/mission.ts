import { isCandidateReference, type CandidateReference } from "./candidate.ts";
import { validateWorkerSkills, type WorkerSkill } from "./context.ts";
import { createHash } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";
import { isAbsolute, relative, resolve } from "node:path";
import { assertWorkerRole, type WorkerRole } from "./roles.ts";
export type WorkerMission = { candidate?: CandidateReference; skills?: WorkerSkill[]; nonce: string; digest: string; role: WorkerRole; workspace: string; mode: "full" | "read-only"; model: string; effort: string; goal: string; criteria: string[]; commands: string[][]; resultLimit: number; exploration?: { roots: string[]; maxFiles: number; maxBytes: number; maxTokens: number }; targets: Record<string, string> };
const used = new Set<string>();
const issued = new Map<string, string>();
const ledgers = new WeakMap<object, Record<string, string>>();
// Pi loads extensions in a separate module graph. Keep the acceptance brand
// process-local, but shared across those graphs; a frozen caller object alone
// is never sufficient to obtain a writable ledger.
const acceptedMissions: WeakSet<object> = ((globalThis as any)[Symbol.for("vgxness.pi.accepted-worker-missions")] ??= new WeakSet<object>());
const acceptedBrand = Symbol.for("vgxness.pi.accepted-worker-mission");
const canonical = (value: any): string => Array.isArray(value) ? `[${value.map(canonical).join(",")}]` : value && typeof value === "object" ? `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}` : JSON.stringify(value);
const freeze = (value: any): any => { if (value && typeof value === "object" && !Object.isFrozen(value)) { for (const key of Object.keys(value)) freeze(value[key]); Object.freeze(value); } return value; };
export async function acceptMission(value: WorkerMission, allowIssuedMissionBootstrap = false) {
  validateWorkerSkills(value.skills);
  if (value.candidate !== undefined && !isCandidateReference(value.candidate)) throw new Error("worker candidate reference rejected");
  assertWorkerRole(value.role); if (!value.nonce || used.has(value.nonce)) throw new Error("worker mission nonce rejected");
  if (!value.workspace || !Number.isSafeInteger(value.resultLimit) || value.resultLimit < 1 || value.resultLimit > 65536 || !Array.isArray(value.criteria) || !Array.isArray(value.commands) || !value.goal || !value.model || !value.effort) throw new Error("worker mission shape rejected");
  if (value.role === "explore" && value.commands.length !== 0) throw new Error("explore missions cannot authorize commands");
  const copy = { ...value }; delete (copy as any).digest;
  const digest = createHash("sha256").update(canonical(copy)).digest("hex");
  if (digest !== value.digest || (!allowIssuedMissionBootstrap && issued.get(value.nonce) !== digest)) throw new Error("worker mission digest rejected");
  const workspace = resolve(value.workspace); const workspaceInfo = await lstat(workspace); if (!workspaceInfo.isDirectory() || workspaceInfo.isSymbolicLink()) throw new Error("worker workspace identity rejected");
  for (const [target, hash] of Object.entries(value.targets)) { const path = resolve(workspace, target); if (!target || isAbsolute(target) || relative(workspace, path).startsWith("..")) throw new Error("worker target escapes workspace"); let ancestor = workspace; for (const segment of target.split(/[\\/]/).filter(Boolean).slice(0, -1)) { ancestor = resolve(ancestor, segment); const info = await lstat(ancestor); if (info.isSymbolicLink()) throw new Error("worker target has symlink ancestor"); } try { const info = await lstat(path); if (info.isSymbolicLink()) throw new Error("worker target is a symlink"); if (hash === "ABSENT" || createHash("sha256").update(await readFile(path)).digest("hex") !== hash) throw new Error("worker target drift"); } catch (error: any) { if (!(hash === "ABSENT" && error?.code === "ENOENT")) throw error; } }
  if (value.exploration) {
    const e = value.exploration;
    if (value.role !== "explore" || !Array.isArray(e.roots) || !e.roots.length || e.roots.length > 16 || !Number.isInteger(e.maxFiles) || e.maxFiles < 1 || e.maxFiles > 10000 || !Number.isInteger(e.maxBytes) || e.maxBytes < 1 || e.maxBytes > 16777216 || !Number.isInteger(e.maxTokens) || e.maxTokens < 1 || e.maxTokens > 65536) throw new Error("worker exploration budget rejected");
    for (const root of e.roots) {
      if (typeof root !== "string" || isAbsolute(root) || root.split(/[\\/]/).includes("..")) throw new Error("worker exploration root rejected");
      let path = workspace;
      for (const segment of root.split(/[\\/]/).filter(Boolean)) { path = resolve(path, segment); const info = await lstat(path); if (!info.isDirectory() || info.isSymbolicLink()) throw new Error("worker exploration root rejected"); }
    }
  }
  used.add(value.nonce); const accepted = JSON.parse(JSON.stringify(value)); Object.defineProperty(accepted, acceptedBrand, { value: true, enumerable: false, configurable: false, writable: false }); freeze(accepted); acceptedMissions.add(accepted); ledgers.set(accepted, { ...value.targets }); return accepted;
}
/** Revalidates the manager-issued bytes inside Pi's isolated extension graph. */
export async function bootstrapWorkerMission(value: WorkerMission) { return await acceptMission(value, true); }
export function issueMission(value: Omit<WorkerMission, "digest">): WorkerMission {
  if (value.candidate !== undefined && !isCandidateReference(value.candidate)) throw new Error("worker candidate reference rejected");
  assertWorkerRole(value.role); if (!value.nonce || issued.has(value.nonce) || used.has(value.nonce)) throw new Error("worker mission nonce rejected");
  const digest = createHash("sha256").update(canonical(value)).digest("hex");
  issued.set(value.nonce, digest);
  return freeze(JSON.parse(JSON.stringify({ ...value, digest }))) as WorkerMission;
}
export async function readWorkerTarget(mission: WorkerMission, target: string) {
  const targets = workerTargetLedger(mission); if (!(target in targets) || targets[target] === "ABSENT") throw new Error("worker target not authorized");
  const path = resolve(mission.workspace, target); let ancestor = mission.workspace;
  for (const part of target.split(/[\\/]/).slice(0, -1)) { ancestor = resolve(ancestor, part); if ((await lstat(ancestor)).isSymbolicLink()) throw new Error("worker target has symlink ancestor"); }
  const info = await lstat(path);
  if (!info.isFile() || info.isSymbolicLink()) throw new Error("worker target is not regular");
  const bytes = await readFile(path); if (createHash("sha256").update(bytes).digest("hex") !== targets[target]) throw new Error("worker target drift");
  return bytes.toString("utf8");
}
/** Recheck the accepted target ledger immediately before a queued worker starts. */
export async function revalidateWorkerMission(mission: WorkerMission) {
  const ledger = workerTargetLedger(mission);
  for (const [target, hash] of Object.entries(ledger)) {
    if (hash !== "ABSENT") { await readWorkerTarget(mission, target); continue; }
    try { await lstat(resolve(mission.workspace, target)); throw new Error("worker target drift"); }
    catch (error: any) { if (error?.code !== "ENOENT") throw error; }
  }
}
/** Mutable only inside this process after a verified worker-owned patch; mission bytes remain immutable. */
export function workerTargetLedger(mission: WorkerMission) { let ledger = ledgers.get(mission); if (!ledger) { if (!(mission as any)[acceptedBrand] || !Object.isFrozen(mission) || !acceptedMissions.has(mission)) throw new Error("worker mission was not accepted"); ledger = { ...mission.targets }; ledgers.set(mission, ledger); } return ledger; }
export async function advanceWorkerTargets(mission: WorkerMission, targets: string[]) { const ledger = workerTargetLedger(mission); for (const target of targets) { if (!(target in ledger)) throw new Error("worker target not authorized"); const path = resolve(mission.workspace, target); try { const info = await lstat(path); if (!info.isFile() || info.isSymbolicLink()) throw new Error("worker target is not regular"); ledger[target] = createHash("sha256").update(await readFile(path)).digest("hex"); } catch (error: any) { if (error?.code === "ENOENT") ledger[target] = "ABSENT"; else throw error; } } }
export function validateWorkerArgv(mission: WorkerMission & { commands?: string[][] }, argv: string[]) {
  if (mission.role === "explore") throw new Error("explore missions cannot authorize commands");
  if (!Array.isArray(argv) || argv.length === 0 || !mission.commands?.some((allowed) => allowed.length === argv.length && allowed.every((part, index) => part === argv[index]))) throw new Error("worker command not authorized");
  return [...argv];
}

export type PiModel = { provider: string; id: string; name?: string; capability?: "efficient" | "balanced" | "frontier"; supportedEfforts?: string[] };

/** Go plan assignments use lifecycle role IDs; Pi workers use only these native roles. */
import { loadManagerContract, resolveRole } from "../orchestration/contract.ts";
const managerContract = loadManagerContract();
export function nativeWorkerRole(role: string) { const mapped = resolveRole(managerContract, role)?.id; if (!mapped || !managerContract.roles.some(item => item.id === mapped)) throw new Error("resolved role is not launchable as a Pi worker"); return mapped; }

const efforts = new Set(["low", "medium", "high", "ultra"]);
const capabilities = new Set(["efficient", "balanced", "frontier"]);
export function normalizePiCatalog(provider: string, models: PiModel[]) {
  const normalized = models.map((model) => {
    const supportedEfforts = (model.supportedEfforts ?? []).filter((effort) => efforts.has(effort));
    if (!model.id || !model.name || supportedEfforts.length === 0) throw new Error("model catalog cannot preserve resolver semantics");
    if (model.capability !== undefined && !capabilities.has(model.capability)) throw new Error("model catalog cannot preserve resolver semantics");
    return { provider, id: model.id, name: model.name, ...(model.capability ? { capability: model.capability } : {}), supportedEfforts };
  });
  if (!provider || normalized.length === 0) throw new Error("model catalog unavailable");
  return { provider, models: normalized };
}

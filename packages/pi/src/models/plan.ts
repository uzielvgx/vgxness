export type PiModel = { provider: string; id: string; name?: string; capability?: "efficient" | "balanced" | "frontier"; supportedEfforts?: string[] };

/** Go plan assignments use lifecycle role IDs; Pi workers use only these native roles. */
const nativeRoles: Record<string, string> = { implementation: "general", verification: "verifier", research: "sdd-research", proposal: "sdd-proposal", spec: "sdd-spec", design: "sdd-design", tasks: "sdd-tasks", apply: "sdd-apply", explore: "explore", general: "general", verifier: "verifier", "care-reviewer": "care-reviewer", "care-specialist": "care-specialist", "care-challenger": "care-challenger", "sdd-research": "sdd-research", "sdd-proposal": "sdd-proposal", "sdd-spec": "sdd-spec", "sdd-design": "sdd-design", "sdd-tasks": "sdd-tasks", "sdd-apply": "sdd-apply" };
export function nativeWorkerRole(role: string) { const mapped = nativeRoles[role]; if (!mapped) throw new Error("resolved role is not launchable as a Pi worker"); return mapped; }

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

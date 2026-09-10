import { loadManagerContract } from "../orchestration/contract.ts";
import type { Capability, Catalog, Effort, Model, Plan, ResolvedPlan, Role } from "../ports/model.ts";
const capabilities: Capability[] = ["efficient", "balanced", "frontier"];
const efforts: Effort[] = ["low", "medium", "high", "ultra"];
const contract = loadManagerContract();
const roles = [contract.manager, ...contract.roles].flatMap(role => role.modelRole ? [role.modelRole as Role] : []);
const rank = (v: string) => efforts.indexOf(v as Effort) + 1;
const matrix: Record<Plan, Record<Role, [
    Capability,
    Effort
]>> = {
    low: Object.fromEntries(roles.map(r => [r, ["efficient", "medium"]])) as any,
    medium: Object.fromEntries(roles.map(r => [r, ["balanced", "medium"]])) as any,
    high: Object.fromEntries(roles.map(r => [r, ["frontier", "high"]])) as any,
    ultra: Object.fromEntries(roles.map(r => [r, ["frontier", "high"]])) as any,
};
Object.assign(matrix.low, { research: ["efficient", "low"], tasks: ["efficient", "low"], verification: ["efficient", "low"], manager: ["balanced", "high"], design: ["balanced", "medium"], apply: ["balanced", "low"], "care-specialist": ["balanced", "medium"], "care-challenger": ["balanced", "medium"], implementation: ["balanced", "low"] });
Object.assign(matrix.medium, { manager: ["frontier", "high"], research: ["efficient", "medium"], proposal: ["balanced", "medium"], spec: ["balanced", "high"], design: ["frontier", "medium"], tasks: ["balanced", "medium"], apply: ["balanced", "medium"], "care-reviewer": ["frontier", "medium"], "care-specialist": ["balanced", "high"], "care-challenger": ["frontier", "medium"], implementation: ["balanced", "medium"], verification: ["efficient", "medium"] });
Object.assign(matrix.high, { manager: ["frontier", "ultra"], research: ["balanced", "high"], proposal: ["balanced", "high"], spec: ["frontier", "high"], design: ["frontier", "high"], tasks: ["balanced", "high"], apply: ["balanced", "high"], "care-reviewer": ["frontier", "high"], "care-specialist": ["frontier", "high"], "care-challenger": ["frontier", "high"], implementation: ["frontier", "high"], verification: ["balanced", "high"] });
for (const role of roles)
    matrix.ultra[role] = ["frontier", role === "manager" ? "ultra" : "high"];
function validText(v: unknown, max: number) { return typeof v === "string" && v.trim() === v && v.length > 0 && [...v].length <= max && !/[\x00-\x1f\x7f-\x9f]/.test(v); }
function fail(message = "invalid model request"): never { throw new Error(message); }
export function resolveModel(payload: unknown): ResolvedPlan {
    const { catalog, plan } = payload as {
        catalog?: Catalog;
        plan?: Plan;
    };
    if (!catalog || !validText(catalog.provider, 128) || !Array.isArray(catalog.models) || !catalog.models.length || !efforts.includes(plan as Effort))
        fail();
    for (const m of catalog.models)
        if (m.provider !== catalog.provider || !validText(m.id, 256) || !validText(m.name, 256) || (m.capability && !capabilities.includes(m.capability)) || !Array.isArray(m.supportedEfforts) || !m.supportedEfforts.length || m.supportedEfforts.some(e => !efforts.includes(e)))
            fail(m.provider !== catalog.provider ? "model provider mismatch" : undefined);
    const models = catalog.models.slice(0, 3);
    const slots = {} as Record<Capability, Model>;
    const used = new Set<number>();
    models.forEach((model, index) => { if (model.capability && !slots[model.capability]) {
        slots[model.capability] = model;
        used.add(index);
    } });
    for (const capability of capabilities)
        if (!slots[capability]) {
            const index = models.findIndex((_model, candidate) => !used.has(candidate));
            if (index >= 0) {
                slots[capability] = models[index];
                used.add(index);
            }
        }
    for (const [index, capability] of capabilities.entries())
        if (!slots[capability])
            slots[capability] = models[Math.min(index, models.length - 1)];
    const assignments = {} as ResolvedPlan["roles"];
    for (const role of roles) {
        const [capability, requestedEffort] = matrix[plan!][role];
        const model = slots[capability];
        const effort = model.supportedEfforts.includes(requestedEffort) ? requestedEffort : model.supportedEfforts.reduce((best, value) => rank(value) > rank(best) ? value : best, model.supportedEfforts[0]);
        const degraded = effort !== requestedEffort;
        assignments[role] = { role, capability, model, requestedEffort, effort, degradation: degraded ? { degraded, reason: `requested effort ${requestedEffort} is unsupported by ${model.id}; using highest declared effort ${effort}` } : { degraded } };
    }
    return { provider: catalog.provider, plan: plan!, slots, roles: assignments };
}
export const dispatchModel = async (_ctx: unknown, operation: string, payload: unknown) => { if (operation !== "resolve")
    throw new Error("unknown model operation"); return resolveModel(payload); };

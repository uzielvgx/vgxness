import type { ResolvedPlan, ResolvedAssignment } from "../ports/model.ts";
import type { ModelSelection } from "../models/selection.ts";
import { Type } from "typebox";
import { validate, type ToolHost } from "./memory.ts";
import { normalizePiCatalog, type PiModel } from "../models/plan.ts";

function withTaskModels(value: ResolvedPlan) {
  return { ...value, roles: Object.fromEntries(Object.entries(value.roles).map(([role, assignment]: [string, ResolvedAssignment]) => [role, { ...assignment, taskModel: `${assignment.model.provider}/${assignment.model.id}` }])) };
}

const effort = Type.Union([Type.Literal("low"), Type.Literal("medium"), Type.Literal("high"), Type.Literal("ultra")]);
const capability = Type.Union([Type.Literal("efficient"), Type.Literal("balanced"), Type.Literal("frontier")]);
export const modelSchema = Type.Object({ plan: Type.Union([Type.Literal("low"), Type.Literal("medium"), Type.Literal("high"), Type.Literal("ultra")]) }, { additionalProperties: false });

export function createModelTool(host: ToolHost & { modelSelection?: () => ModelSelection; modelCatalog?: () => { provider: string; models: PiModel[] } | undefined }) {
  return { name: "model_resolve", label: "Configured agent models", description: "Read the user's exact single-model or per-agent selection. Copy the selected role taskModel and effort into task. No plans or automatic model substitutions.", parameters: Type.Object({}, { additionalProperties: false }),
    async execute(_id: string, payload: unknown) { validate(Type.Object({}, { additionalProperties: false }), payload); const selection = host.modelSelection?.(); if (!selection) throw new Error("Pi model selection unavailable"); return { content: [{ type: "text", text: JSON.stringify({ mode: selection.mode, roles: Object.fromEntries(Object.entries(selection.assignments).map(([role,a]) => [role,{ role, taskModel:a.model, effort:a.effort }])) }) }] }; },
  };
}
export function createModelPlanTool(host: ToolHost, provider: string, models: PiModel[]) {
  const catalog = normalizePiCatalog(provider, models);
  return { ...createModelTool({ ...host, modelCatalog: () => catalog }), async execute(_id: string, input: { plan: "low" | "medium" | "high" | "ultra" }) {
    const payload = { plan: input.plan };
    validate(modelSchema, payload);
    const value = await (await host.backend()).request("model.resolve", { catalog, ...payload }, host);
    return { content: [{ type: "text", text: JSON.stringify(withTaskModels(value as ResolvedPlan)) }] };
  } };
}

import { Type } from "typebox";
import { validate, type ToolHost } from "./memory.ts";
import { normalizePiCatalog, type PiModel } from "../models/plan.ts";

const effort = Type.Union([Type.Literal("low"), Type.Literal("medium"), Type.Literal("high"), Type.Literal("ultra")]);
const capability = Type.Union([Type.Literal("efficient"), Type.Literal("balanced"), Type.Literal("frontier")]);
export const modelSchema = Type.Object({ plan: Type.Union([Type.Literal("low"), Type.Literal("medium"), Type.Literal("high"), Type.Literal("ultra")]) }, { additionalProperties: false });

export function createModelTool(host: ToolHost & { modelCatalog?: () => { provider: string; models: PiModel[] } | undefined }) {
  return { name: "model_resolve", label: "Resolve model plan", description: "Resolve the configured model plan without changing its effort semantics.", parameters: modelSchema,
    async execute(_id: string, payload: unknown) { validate(modelSchema, payload); const catalog = host.modelCatalog?.(); if (!catalog) throw new Error("current Pi model catalog unavailable"); const value = await (await host.backend()).request("model.resolve", { catalog: normalizePiCatalog(catalog.provider, catalog.models), plan: (payload as any).plan }, host); return { content: [{ type: "text", text: JSON.stringify(value) }] }; },
  };
}
export function createModelPlanTool(host: ToolHost, provider: string, models: PiModel[]) {
  const catalog = normalizePiCatalog(provider, models);
  return { ...createModelTool({ ...host, modelCatalog: () => catalog }), async execute(_id: string, input: { plan: "low" | "medium" | "high" | "ultra" }) {
    const payload = { plan: input.plan };
    validate(modelSchema, payload);
    const value = await (await host.backend()).request("model.resolve", { catalog, ...payload }, host);
    return { content: [{ type: "text", text: JSON.stringify(value) }] };
  } };
}

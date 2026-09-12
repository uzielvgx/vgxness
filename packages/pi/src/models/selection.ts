import { readFile, lstat } from "node:fs/promises";
import { join } from "node:path";

export const agentRoles = ["manager", "explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger"] as const;
export type ModelAssignment = { model: string; effort: string };
export type ModelSelection = { schemaVersion: 1; mode: "single" | "per-agent"; assignments: Record<string, ModelAssignment> };
function closedObject(value: unknown, keys: readonly string[]): boolean {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    && Object.keys(value).length === keys.length
    && Object.keys(value).every(key => keys.includes(key));
}
function validReference(value: unknown): value is string {
  if (typeof value !== "string" || value.length > 385 || value.startsWith("@")) return false;
  const segments = value.split("/");
  return segments.length >= 2 && segments.every(segment => segment.length <= 256 && /^[a-zA-Z0-9_.:@+-]+$/.test(segment));
}
export function validateSelection(value: unknown): ModelSelection {
  const c = value as ModelSelection;
  if (!closedObject(c, ["schemaVersion", "mode", "assignments"]) || c.schemaVersion !== 1 || !["single", "per-agent"].includes(c.mode) || !closedObject(c.assignments, agentRoles)) throw new Error("invalid Pi model selection");
  for (const role of agentRoles) {
    const a = c.assignments[role];
    if (!closedObject(a, ["model", "effort"]) || !validReference(a.model) || !["off", "minimal", "low", "medium", "high", "xhigh"].includes(a.effort)) throw new Error("invalid Pi agent model");
    if (c.mode === "single" && (a.model !== c.assignments.manager.model || a.effort !== c.assignments.manager.effort)) throw new Error("single model assignments differ");
  }
  return structuredClone(c);
}
export async function readSelection(agentDir: string): Promise<ModelSelection | undefined> {
  const path = join(agentDir, "settings.json");
  try {
    const stat = await lstat(path);
    if (!stat.isFile() || stat.isSymbolicLink() || stat.size > 1024 * 1024) throw new Error("unsafe Pi model settings");
    const settings = JSON.parse(await readFile(path, "utf8"));
    return settings.vgxnessModels === undefined ? undefined : validateSelection(settings.vgxnessModels);
  } catch (error: any) { if (error.code === "ENOENT") return undefined; throw error; }
}
export function selectedModels(selection: ModelSelection | undefined, context: any) {
  if (selection) return selection;
  const model = context?.model;
  if (!model?.provider || !model?.id) throw new Error("select a Pi model before delegating");
  return validateSelection({ schemaVersion: 1, mode: "single", assignments: Object.fromEntries(agentRoles.map(role => [role, { model: `${model.provider}/${model.id}`, effort: "off" }])) });
}

export function supportsEffort(model: any, effort: string) {
  return effort === "off" || (["minimal", "low", "medium", "high", "xhigh"].includes(effort) && model?.reasoning === true && model.thinkingLevelMap?.[effort] !== null);
}
export async function activateManagerModel(selection: ModelSelection, registry: any, api: any) {
  const a = selection.assignments.manager, slash = a.model.indexOf("/");
  const model = registry?.find?.(a.model.slice(0, slash), a.model.slice(slash + 1));
  if (!model || !supportsEffort(model, a.effort) || !(await api.setModel?.(model))) throw new Error("configured Manager model unavailable");
  api.setThinkingLevel?.(a.effort);
  if (api.getThinkingLevel?.() !== a.effort) throw new Error("configured Manager effort unavailable");
}

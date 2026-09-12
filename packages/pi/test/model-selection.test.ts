import test from "node:test";
import assert from "node:assert/strict";
import { agentRoles, validateSelection, selectedModels, activateManagerModel } from "../src/models/selection.ts";
import { createModelTool } from "../src/tools/model.ts";
import { createTaskTool } from "../src/tools/task.ts";

test("single and per-agent selections retain exact providers without plans", async () => {
  const single = selectedModels(undefined, { model: { provider: "fixture", id: "family/model" } });
  assert.equal(Object.keys(single.assignments).length, 7);
  const c = validateSelection({ ...single, mode: "per-agent", assignments: { ...single.assignments, explore: { model: "other/fast", effort: "off" } } });
  const tool = createModelTool({ modelSelection: () => c } as any);
  const result = JSON.parse((await tool.execute("models", {})).content[0].text);
  assert.equal(result.roles.explore.taskModel, "other/fast");
  assert.equal(result.roles.manager.taskModel, "fixture/family/model");
  await assert.rejects(tool.execute("models", { plan: "high" }));
  assert.throws(() => validateSelection({ ...c, mode: "single" }));
  for (const role of agentRoles) { const missing = structuredClone(c); delete missing.assignments[role]; assert.throws(() => validateSelection(missing)); }
});

test("task rejects a model override before starting or authenticating a worker", async () => {
  let called = false;
  const tool = createTaskTool({ role: "manager", workspace: "/fixture", configuredModel: () => ({ model: "chosen/model", effort: "off" }), executeWorker: async () => { called=true; return ""; } });
  await assert.rejects(tool.execute("task", { role: "explore", mode: "read-only", model: "other/model", effort: "off", goal: "read", nonce: "model-selection-fixture", targets: {}, commands: [], criteria: [], resultLimit: 100 }), /differs from user configuration/);
  assert.equal(called,false);
});

test("Manager activates the exact configured provider/model and checks effort readback", async () => {
  const c = selectedModels(undefined, { model: { provider: "chosen", id: "family/model" } });
  const model = { provider: "chosen", id: "family/model", reasoning: false };
  let selected: unknown, effort = "high";
  const registry = { find: (provider: string, id: string) => provider === "chosen" && id === "family/model" ? model : undefined };
  const api = { setModel: async (value: unknown) => { selected=value; return true; }, setThinkingLevel: (value: string) => { effort=value; }, getThinkingLevel: () => effort };
  await activateManagerModel(c, registry, api);
  assert.equal(selected, model); assert.equal(effort, "off");
  await assert.rejects(activateManagerModel(c, { find: () => undefined }, api), /model unavailable/);
  await assert.rejects(activateManagerModel(c, registry, { ...api, getThinkingLevel: () => "high" }), /effort unavailable/);
});

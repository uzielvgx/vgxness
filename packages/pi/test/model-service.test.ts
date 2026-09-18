import test from "node:test";
import assert from "node:assert/strict";
import { resolveModel } from "../src/service/model.ts";
import { loadManagerContract } from "../src/orchestration/contract.ts";

const catalog = { provider:"openai", models:[
  {provider:"openai",id:"luna",name:"Luna",capability:"efficient",supportedEfforts:["low","medium"]},
  {provider:"openai",id:"terra",name:"Terra",capability:"balanced",supportedEfforts:["low","medium","high"]},
  {provider:"openai",id:"sol",name:"Sol",capability:"frontier",supportedEfforts:["low","medium","high"]},
] };
test("resolves every active model role and preserves a declared effort downgrade", () => {
  const value = resolveModel({ catalog, plan:"high" });
  assert.equal(value.roles.manager.model.id, "sol");
  assert.equal(value.roles.manager.effort, "high");
  assert.equal(value.roles.manager.degradation.degraded, true);
  assert.equal(Object.keys(value.roles).length, 7);
});
test("rejects a catalog that crosses providers", () => assert.throws(() => resolveModel({ catalog:{...catalog,models:[{...catalog.models[0],provider:"other"}]},plan:"low" }), /provider mismatch/));

test("low plan preserves low effort for research and verification", () => { const value = resolveModel({ catalog, plan:"low" }); for (const role of ["research", "verification"]) { assert.equal(value.roles[role].requestedEffort, "low"); assert.equal(value.roles[role].effort, "low"); } });

test("all plans preserve active assignments without retired lifecycle roles", () => {
  const contract = loadManagerContract();
  const activeRoles = [contract.manager, ...contract.roles].map(role => role.modelRole!).sort();
  const expected = {
    low: { manager: ["balanced", "high"], research: ["efficient", "low"], implementation: ["balanced", "low"], verification: ["efficient", "low"], "care-reviewer": ["efficient", "medium"], "care-specialist": ["balanced", "medium"], "care-challenger": ["balanced", "medium"] },
    medium: { manager: ["frontier", "high"], research: ["efficient", "medium"], implementation: ["balanced", "medium"], verification: ["efficient", "medium"], "care-reviewer": ["frontier", "medium"], "care-specialist": ["balanced", "high"], "care-challenger": ["frontier", "medium"] },
    high: { manager: ["frontier", "ultra"], research: ["balanced", "high"], implementation: ["frontier", "high"], verification: ["balanced", "high"], "care-reviewer": ["frontier", "high"], "care-specialist": ["frontier", "high"], "care-challenger": ["frontier", "high"] },
    ultra: { manager: ["frontier", "ultra"], research: ["frontier", "high"], implementation: ["frontier", "high"], verification: ["frontier", "high"], "care-reviewer": ["frontier", "high"], "care-specialist": ["frontier", "high"], "care-challenger": ["frontier", "high"] },
  };
  for (const [plan, assignments] of Object.entries(expected)) {
    const value = resolveModel({ catalog, plan });
    assert.deepEqual(Object.keys(value.roles).sort(), activeRoles, plan);
    assert.deepEqual(Object.fromEntries(Object.entries(value.roles).map(([role, assignment]) => [role, [assignment.capability, assignment.requestedEffort]])), assignments, plan);
    for (const role of ["proposal", "spec", "design", "tasks", "apply"]) assert.equal(Object.hasOwn(value.roles, role), false, `${plan}: ${role}`);
  }
});

test("model tools return copyable provider-qualified task models", async () => {
  const { createModelTool, createModelPlanTool } = await import("../src/tools/model.ts");
  const host = { modelCatalog: () => catalog, backend: async () => ({ request: async (_operation: string, payload: unknown) => resolveModel(payload) }) } as any;
  for (const tool of [createModelPlanTool(host, catalog.provider, catalog.models as any)]) {
    const result = JSON.parse((await tool.execute("resolve", { plan: "low" })).content[0].text);
    assert.equal(result.roles.research.taskModel, "openai/luna");
    assert.equal(result.roles.research.effort, "low");
    assert.equal(result.roles.research.model.id, "luna");
  }
});

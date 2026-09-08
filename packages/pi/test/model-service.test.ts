import test from "node:test";
import assert from "node:assert/strict";
import { resolveModel } from "../src/service/model.ts";

const catalog = { provider:"openai", models:[
  {provider:"openai",id:"luna",name:"Luna",capability:"efficient",supportedEfforts:["low","medium"]},
  {provider:"openai",id:"terra",name:"Terra",capability:"balanced",supportedEfforts:["low","medium","high"]},
  {provider:"openai",id:"sol",name:"Sol",capability:"frontier",supportedEfforts:["low","medium","high"]},
] };
test("resolves every lifecycle role and preserves a declared effort downgrade", () => {
  const value = resolveModel({ catalog, plan:"high" });
  assert.equal(value.roles.manager.model.id, "sol");
  assert.equal(value.roles.manager.effort, "high");
  assert.equal(value.roles.manager.degradation.degraded, true);
  assert.equal(Object.keys(value.roles).length, 12);
});
test("rejects a catalog that crosses providers", () => assert.throws(() => resolveModel({ catalog:{...catalog,models:[{...catalog.models[0],provider:"other"}]},plan:"low" }), /provider mismatch/));

test("low plan preserves low effort for research, tasks and verification", () => { const value = resolveModel({ catalog, plan:"low" }); for (const role of ["research", "tasks", "verification"]) { assert.equal(value.roles[role].requestedEffort, "low"); assert.equal(value.roles[role].effort, "low"); } });

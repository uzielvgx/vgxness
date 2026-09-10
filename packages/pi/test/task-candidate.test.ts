import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Value } from "typebox/value";
import { createTaskTool, taskRequestSchema } from "../src/tools/task.ts";
import { acceptMission, bootstrapWorkerMission, issueMission } from "../src/workers/mission.ts";
import { workerPrompt } from "../src/workers/context.ts";
import { workerTerminalEnvelope } from "../src/workers/result.ts";

const candidate = { commit: "a".repeat(40), snapshot: "b".repeat(64) };
const request = () => ({ candidate, nonce: crypto.randomUUID(), goal: "Read supplied evidence", role: "explore", mode: "read-only" as const, model: "fixture/model", effort: "low", criteria: ["Report evidence"], targets: {}, commands: [], resultLimit: 4096 });

test("candidate schema accepts full structured references and legacy omission", async () => {
  for (const value of [candidate, { commit: candidate.commit }, { snapshot: candidate.snapshot }]) assert.equal(Value.Check(taskRequestSchema, { ...request(), candidate: value }), true);
  const { candidate: _candidate, ...legacy } = request();
  assert.equal(Value.Check(taskRequestSchema, legacy), true);
  for (const value of ["a".repeat(40), {}, { extra: candidate.commit }, { commit: "a".repeat(39) }, { commit: "A".repeat(40) }, { commit: candidate.commit + "\n" }, { commit: "a".repeat(39) + "\n" }, { snapshot: "b".repeat(63) }, { snapshot: candidate.snapshot + "\n" }, { commit: candidate.commit, extra: "value" }]) {
    assert.equal(Value.Check(taskRequestSchema, { ...request(), candidate: value }), false);
    assert.throws(() => issueMission({ ...request(), candidate: value, workspace: "/unused" } as any), /candidate reference/);
    await assert.rejects(bootstrapWorkerMission({ ...request(), candidate: value, workspace: "/unused", digest: "c".repeat(64) } as any), /candidate reference/);
  }
});

test("candidate binds mission and prompt while worker output cannot replace it", async t => {
  const workspace = await realpath(await mkdtemp(join(tmpdir(), "pi-candidate-")));
  t.after(() => rm(workspace, { recursive: true, force: true }));
  const mission = issueMission({ ...request(), workspace } as any);
  const tampered = { ...mission, candidate: { commit: "c".repeat(40) } };
  await assert.rejects(acceptMission(tampered), /digest rejected/);
  await assert.rejects(bootstrapWorkerMission(tampered), /digest rejected/);
  const accepted = await acceptMission(mission);
  assert.deepEqual(accepted.candidate, candidate);
  assert.ok(Object.isFrozen(accepted.candidate));
  assert.deepEqual(JSON.parse(workerPrompt(accepted).split("\n").at(-1)!).candidate, candidate);
  let launched: any;
  const tool = createTaskTool({ role: "manager", workspace, executeWorker: async value => { launched = value; return JSON.stringify({ text: "read", candidate: { commit: "c".repeat(40) } }); } });
  const result = await tool.execute("fixture", request());
  assert.deepEqual(launched.candidate, candidate);
  assert.deepEqual(result.details.result.candidate, candidate);
  assert.equal(result.details.result.state, "succeeded");
  assert.deepEqual(launched.targets, {});
  assert.deepEqual(launched.commands, []);
  const { candidate: _candidate, ...legacy } = request();
  const legacyResult = await tool.execute("legacy", { ...legacy, goal: candidate.commit });
  assert.equal("candidate" in legacyResult.details.result, false);
  assert.equal("candidate" in JSON.parse(workerPrompt(launched).split("\n").at(-1)!), false);
});

test("all terminal paths rebind candidate to Manager values, including callback errors", async t => {
  for (const state of ["succeeded", "failed", "cancelled", "unavailable"]) {
    const terminal = workerTerminalEnvelope({ ...request(), state, report: { text: "fixture", candidate: { commit: "c".repeat(40) } } });
    assert.deepEqual(terminal.candidate, candidate);
    assert.ok(Object.isFrozen(terminal.candidate));
    assert.equal(terminal.state, state);
  }
  assert.throws(() => workerTerminalEnvelope({ ...request(), candidate: {} }), /candidate reference/);
  const pending = workerTerminalEnvelope({ ...request(), state: "succeeded", report: { recovery: { pending: true, refs: ["fixture"] } } });
  assert.deepEqual(pending.candidate, candidate); assert.equal(pending.reasonCode, "recovery_pending");
  const workspace = await realpath(await mkdtemp(join(tmpdir(), "pi-candidate-failure-")));
  t.after(() => rm(workspace, { recursive: true, force: true }));
  for (const executeWorker of [undefined, async () => { throw new Error("fixture failure"); }]) {
    const tool = createTaskTool({ role: "manager", workspace, executeWorker });
    await assert.rejects(tool.execute("fixture", request()), (error: any) => { assert.deepEqual(error.result?.candidate, candidate); return error.result?.state === (executeWorker ? "failed" : "unavailable"); });
  }
  for (const state of ["succeeded", "cancelled", "invented"]) {
    for (const matches of [true, false]) {
      const spoof = createTaskTool({ role: "manager", workspace, executeWorker: async mission => { throw { result: { nonce: matches ? mission.nonce : "wrong", role: "general", candidate: { commit: "c".repeat(40) }, state, reasonCode: state, text: "fixture" } }; } });
      await assert.rejects(spoof.execute("spoof", request()), (error: any) => { assert.deepEqual(error.result?.candidate, candidate); return error.result.role === "explore" && error.result.state === "failed" && error.result.reasonCode === "failed"; });
      const { candidate: _candidate, ...legacy } = request();
      await assert.rejects(spoof.execute("legacy-spoof", legacy), (error: any) => !("candidate" in error.result) && error.result.state === "failed");
    }
  }
  const controller = new AbortController(); controller.abort();
  const cancelled = createTaskTool({ role: "manager", workspace, executeWorker: async () => "unused" });
  await assert.rejects(cancelled.execute("cancelled", request(), controller.signal), (error: any) => error.result.state === "cancelled");
});

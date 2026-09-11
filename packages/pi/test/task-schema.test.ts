import assert from "node:assert/strict";
import test from "node:test";
import { Value } from "typebox/value";
import { taskRequestSchema } from "../src/tools/task.ts";

test("task targets distinguish file digests from candidate metadata", () => {
  const request = { goal: "Review inline design; candidate " + "a".repeat(40), nonce: "schema-fixture", role: "care-reviewer", mode: "read-only", model: "provider/model", effort: "low", criteria: ["Review supplied design"], commands: [], resultLimit: 4096, targets: {} };
  assert.equal(Value.Check(taskRequestSchema, request), true);
  for (const value of ["a".repeat(40), "{}", "inline-design", "A".repeat(64), "a".repeat(63), "ABSENT\n"]) {
    assert.equal(Value.Check(taskRequestSchema, { ...request, targets: { "file.txt": value } }), false, value);
  }
  assert.equal(Value.Check(taskRequestSchema, { ...request, targets: { "file.txt": "a".repeat(64), "new.txt": "ABSENT" } }), true);
  assert.equal(Value.Check(taskRequestSchema, { ...request, goal: "x".repeat(4097) }), false);
});

test("task requires an explicit provider without guessing a model namespace", () => {
  const request = { goal: "Read", nonce: "qualified-model", role: "explore", mode: "read-only", model: "openai-codex/gpt-5.6-luna", effort: "low", criteria: ["Read"], commands: [], resultLimit: 1024, targets: {} };
  assert.equal(Value.Check(taskRequestSchema, request), true);
  assert.equal(Value.Check(taskRequestSchema, { ...request, model: "provider/vendor/model" }), true);
  assert.equal(Value.Check(taskRequestSchema, { ...request, model: "p".repeat(128) + "/" + "m".repeat(256) }), true);
  assert.equal(Value.Check(taskRequestSchema, { ...request, model: "p".repeat(128) + "/" + "m".repeat(257) }), false);
  for (const model of ["gpt-5.6-luna", "/model", "provider/", "provider/model "]) assert.equal(Value.Check(taskRequestSchema, { ...request, model }), false, model);
});

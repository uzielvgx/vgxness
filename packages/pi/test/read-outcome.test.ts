import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { frameReadOutcome } from "../src/tools/read-outcome.ts";
import { loadPiExtensions } from "./pi-fixture.mjs";

test("read outcome separates successful execution from failed validation data", () => {
  const content = [{ type: "text", text: '{"exitCode":1,"status":"failed"}\n' }, { type: "image", data: "fixture", mimeType: "image/png" }];
  const before = structuredClone(content);
  const result = frameReadOutcome({ toolName: "read", isError: false, content })!;
  assert.match((result.content[0] as any).text, /SUCCEEDED.*isError=false/);
  assert.deepEqual(result.content.slice(1), before);
  assert.deepEqual(content, before);
  assert.equal("isError" in result, false);
});

test("read errors remain errors and unrelated or unknown outcomes are untouched", () => {
  const content = [{ type: "text", text: "ENOENT: missing file" }];
  const result = frameReadOutcome({ toolName: "read", isError: true, content })!;
  assert.match((result.content[0] as any).text, /FAILED.*isError=true/);
  assert.deepEqual(result.content.slice(1), content);
  assert.equal("isError" in result, false);
  for (const event of [{ toolName: "bash", isError: false, content }, { toolName: "read", content }, { toolName: "read", isError: false }]) assert.equal(frameReadOutcome(event), undefined);
});

test("native extension registers read result framing without backend access", async t => {
  const workspace = await realpath(await mkdtemp(join(tmpdir(), "pi-read-outcome-")));
  t.after(() => rm(workspace, { recursive: true, force: true }));
  const wrapper = join(workspace, "extension.ts");
  await writeFile(wrapper, `import { createPiExtension } from ${JSON.stringify(join(process.cwd(), "src/extension.ts"))}; export default createPiExtension({ workspace: ${JSON.stringify(workspace)}, mode: "read-only", role: "manager", backend: async () => { throw new Error("unexpected backend"); } });`);
  const { loadExtensions } = await loadPiExtensions();
  const loaded = await loadExtensions([wrapper], workspace);
  assert.deepEqual(loaded.errors, []);
  const handlers = loaded.extensions[0].handlers.get("tool_result") ?? [];
  assert.equal(handlers.length, 1);
  const event: any = { type: "tool_result", toolName: "read", toolCallId: "fixture", input: { path: "receipt.json" }, content: [{ type: "text", text: '{"exitCode":1}' }], isError: false };
  const result: any = await handlers[0](event, {} as any);
  assert.match(result.content[0].text, /SUCCEEDED/);
  assert.equal(event.isError, false);
  assert.deepEqual(result.content.slice(1), event.content);
});

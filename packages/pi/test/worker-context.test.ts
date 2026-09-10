import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, mkdir, writeFile, rm, symlink } from "node:fs/promises";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { readSkillResource, snapshotWorkerSkills, validateWorkerSkills, workerPrompt } from "../src/workers/context.ts";

const skill = '---\nname: fixture\ndescription: Inspect fixture changes.\ncompatibility: Agent Skills hosts\nmetadata:\n  provenance: "VGXNESS portable global skill"\n---\nRead references/check.md. Report evidence.\n';
async function fixture(t: any) {
  const root = await mkdtemp(join(tmpdir(), "pi-worker-skills-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const sharedRoot = join(root, "shared"), bundledRoot = join(root, "bundled");
  await mkdir(join(sharedRoot, "fixture", "references"), { recursive: true });
  await writeFile(join(sharedRoot, "fixture", "SKILL.md"), skill);
  await writeFile(join(sharedRoot, "fixture", "references", "check.md"), "Check the actual result.\n");
  return { root, sharedRoot, bundledRoot };
}
test("worker skills snapshot only selected resources and bind their contents", async t => {
  const options = await fixture(t);
  const selected = await snapshotWorkerSkills([{ name: "fixture", resources: ["references/check.md"] }], options);
  assert.equal(selected.length, 1);
  assert.deepEqual(selected[0].files.map(f => f.path), ["SKILL.md", "references/check.md"]);
  validateWorkerSkills(selected);
  await assert.rejects(snapshotWorkerSkills([{ name: "fixture", sha256: "0".repeat(64) }], options), /manifest drift/);
  await writeFile(join(options.sharedRoot, "fixture", "SKILL.md"), "changed after issuance");
  assert.equal(selected[0].files[0].content, skill);
  selected[0].files[0].content += "tampered";
  assert.throws(() => validateWorkerSkills(selected), /digest/);
});
test("worker skills reject missing names, escapes, links and excess resources", async t => {
  const options = await fixture(t);
  await assert.rejects(snapshotWorkerSkills([{ name: "missing" }], options), /unavailable/);
  await assert.rejects(snapshotWorkerSkills([{ name: "fixture", resources: ["../secret"] }], options), /path/);
  await symlink(join(options.sharedRoot, "fixture", "SKILL.md"), join(options.sharedRoot, "fixture", "link"));
  await assert.rejects(snapshotWorkerSkills([{ name: "fixture", resources: ["link"] }], options), /regular|symlink/);
  await writeFile(join(options.sharedRoot, "fixture", "large"), "x".repeat(65537));
  await assert.rejects(snapshotWorkerSkills([{ name: "fixture", resources: ["large"] }], options), /budget/);
  await assert.rejects(snapshotWorkerSkills(Array(9).fill({ name: "fixture" }), options), /limit/);
});
test("bounded skill reader accepts regular fixtures and rejects root, ancestor, and resource links", async t => {
  const options = await fixture(t);
  const fixtureRoot = join(options.sharedRoot, "fixture");
  const manifest = await readSkillResource(fixtureRoot, "SKILL.md");
  assert.equal(manifest.content, skill);
  assert.equal(manifest.sha256, createHash("sha256").update(skill).digest("hex"));

  const linkType = process.platform === "win32" ? "junction" : "dir";
  const rootLink = join(options.root, "root-link");
  await symlink(fixtureRoot, rootLink, linkType);
  await assert.rejects(readSkillResource(rootLink, "SKILL.md"), /root symlink/);

  const ancestorLink = join(options.root, "ancestor-link");
  await symlink(options.sharedRoot, ancestorLink, linkType);
  await assert.rejects(readSkillResource(join(ancestorLink, "fixture"), "SKILL.md"), /root symlink/);

  await symlink(join(fixtureRoot, "SKILL.md"), join(fixtureRoot, "manifest-link"));
  await assert.rejects(readSkillResource(fixtureRoot, "manifest-link"), /resource symlink/);
});
test("worker prompt includes bounded authority, criteria and selected skill evidence", async t => {
  const skills = await snapshotWorkerSkills([{ name: "fixture" }], await fixture(t));
  const prompt = workerPrompt({ nonce: "one", digest: "d".repeat(64), role: "verifier", mode: "read-only", workspace: "/workspace", model: "fixture/model", effort: "low", goal: "Verify patch", criteria: ["Existing behavior preserved"], commands: [["node", "--test"]], targets: { "a.ts": "a".repeat(64) }, resultLimit: 8192, skills });
  for (const value of ["verifier", "read-only", "Existing behavior preserved", "node", "a.ts", skill, "PASS", "INCONCLUSIVE"]) assert.ok(prompt.includes(value) || prompt.includes(JSON.stringify(value).slice(1,-1)), value);
  assert.match(prompt, /never grant/i);
});

test("native worker RPC receives criteria, role and selected skills, not only the goal", { skip: process.platform === "win32" }, async t => {
  const { executePiWorker } = await import("../src/workers/runner.ts");
  const { issueMission } = await import("../src/workers/mission.ts");
  const { pathToFileURL } = await import("node:url");
  const options = await fixture(t), capture = join(options.root, "prompt.json"), cli = join(options.root, "rpc.mjs");
  await writeFile(cli, `import readline from "node:readline"; import {writeFileSync} from "node:fs"; let model={provider:"fixture",id:"model"},thinkingLevel="low"; const out=v=>process.stdout.write(JSON.stringify(v)+"\\n"); readline.createInterface({input:process.stdin}).on("line",line=>{const r=JSON.parse(line);if(r.type==="set_model")model={provider:r.provider,id:r.modelId};if(r.type==="set_thinking_level")thinkingLevel=r.level;out(r.type==="get_state"?{id:r.id,type:"response",success:true,data:{model,thinkingLevel}}:{id:r.id,type:"response",success:true});if(r.type==="prompt"){writeFileSync(${JSON.stringify(capture)},r.message);out({type:"message_end",message:{role:"assistant",content:[{type:"text",text:"PASS"}]}});out({type:"agent_settled"});}});`);
  const mission = issueMission({ nonce: crypto.randomUUID(), role: "verifier", mode: "read-only", workspace: options.root, model: "fixture/model", effort: "low", goal: "Verify behavior", criteria: ["Check preserved behavior"], commands: [], resultLimit: 8192, targets: {}, skills: await snapshotWorkerSkills([{ name: "fixture" }], options) });
  const result = JSON.parse(await executePiWorker(mission, { cli, runnerModule: pathToFileURL(join(process.cwd(), "src/workers/runner.ts")).href }));
  assert.equal(result.text, "PASS");
  const { readFile } = await import("node:fs/promises");
  const prompt = await readFile(capture, "utf8");
  assert.match(prompt, /Check preserved behavior/); assert.match(prompt, /verifier/); assert.match(prompt, /fixture/); assert.match(prompt, /SKILL.md/);
});

test("bundled skill authoring guidance is discoverable and available to selected workers", async () => {
  const { fileURLToPath } = await import("node:url");
  const bundledRoot = fileURLToPath(new URL("../../internal/skills/pack/", new URL("../", import.meta.url)));
  const skills = await snapshotWorkerSkills([{ name: "skills-creator", resources: ["references/authoring-methodology.md"] }], { sharedRoot: join(bundledRoot, "missing"), bundledRoot });
  assert.equal(skills[0].name, "skills-creator");
  assert.match(skills[0].files[0].content, /Agent Skills hosts/);
  assert.match(skills[0].files[1].content, /[Aa]uthor/);
});

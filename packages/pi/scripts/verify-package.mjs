import { renderPiManagerPrompt } from "../src/orchestration/adapter.ts";
import { canonicalContract } from "../src/orchestration/contract.ts";
import fs from "node:fs";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
const pkg = JSON.parse(fs.readFileSync(new URL("../package.json", import.meta.url)));
for (const name of ["preinstall", "install", "postinstall"]) assert.equal(pkg.scripts?.[name], undefined);
for (const name of ["dependencies", "optionalDependencies", "bin", "os", "cpu"]) assert.equal(pkg[name], undefined);
assert.equal(pkg.engines.node, ">=22.19.0");
assert.deepEqual(pkg.peerDependencies, {"@earendil-works/pi-coding-agent":"^0.84.4", typebox:"1.3.7"});
const root = new URL("../resources/migrations/", import.meta.url);
const manifest = JSON.parse(fs.readFileSync(new URL("manifest.json", root)));
assert.equal(manifest.version, 23);
assert.equal(manifest.migrations.length, 23);
assert.equal(fs.readdirSync(root).filter(name => name.endsWith(".sql")).length, 23);
for (const [index, migration] of manifest.migrations.entries()) {
 assert.equal(migration.version, index + 1);
 assert.equal(createHash("sha256").update(fs.readFileSync(new URL(migration.file, root))).digest("hex"), migration.sha256);
}
const contractSource = JSON.parse(fs.readFileSync(new URL("../../../internal/orchestration/manager_contract.json", import.meta.url)));
const contractResource = fs.readFileSync(new URL("../resources/orchestration/contract.json", import.meta.url), "utf8");
const contractDigest = createHash("sha256").update(canonicalContract(contractSource)).digest("hex");
assert.equal(contractResource, JSON.stringify({...contractSource, sourceDigest: contractDigest}) + "\n", "Pi contract resource drift");
const managerPrompt = fs.readFileSync(new URL("../resources/prompts/manager.md", import.meta.url), "utf8");
const generatedPrompt = renderPiManagerPrompt({...contractSource, sourceDigest: contractDigest});
assert.equal(managerPrompt, generatedPrompt, "Pi Manager prompt drift");

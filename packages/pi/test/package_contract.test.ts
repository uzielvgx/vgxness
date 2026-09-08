import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

const root = path.resolve(import.meta.dirname, "../../..");
test("Pi package metadata is closed, offline, and portable", () => {
  const workspace = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8"));
  const lockfile = JSON.parse(fs.readFileSync(path.join(root, "package-lock.json"), "utf8"));
  const pi = JSON.parse(fs.readFileSync(path.join(root, "packages/pi/package.json"), "utf8"));
  const tsconfig = JSON.parse(fs.readFileSync(path.join(root, "packages/pi/tsconfig.json"), "utf8"));
  assert.equal(workspace.private, true);
  assert.deepEqual(workspace.workspaces, ["packages/*"]);
  assert.equal(lockfile.lockfileVersion, 3);
  assert.equal(lockfile.packages["packages/pi"].name, "@vgxness/pi");
  assert.equal(pi.engines.node, ">=22.19.0");
  assert.equal(pi.scripts["pack:check"], "npm pack --dry-run --ignore-scripts");
  assert.equal(tsconfig.compilerOptions.noEmit, true);
  assert.equal(pi.scripts.postinstall, undefined);
  assert.equal(pi.scripts.preinstall, undefined);
  assert.equal(pi.scripts.install, undefined);
  assert.equal(pi.optionalDependencies, undefined);
  assert.deepEqual(pi.peerDependencies, {"@earendil-works/pi-coding-agent":"^0.84.4",typebox:"1.3.7"});
  assert.deepEqual(lockfile.packages["packages/pi"].peerDependencies, pi.peerDependencies);
  assert.equal(lockfile.packages["packages/pi"].optionalDependencies, undefined);
  assert.equal(pi.bin, undefined);
  assert.deepEqual(pi.files, ["src", "resources", "LICENSE"]);
});

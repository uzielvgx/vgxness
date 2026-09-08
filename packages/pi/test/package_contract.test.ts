import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

const root = path.resolve(import.meta.dirname, "../../..");
const platforms = [
  ["linux", "x64"], ["linux", "arm64"],
  ["darwin", "x64"], ["darwin", "arm64"],
  ["win32", "x64"], ["win32", "arm64"],
] as const;

test("Pi package metadata is closed, offline, and platform-specific", () => {
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
  assert.equal(Object.keys(pi.optionalDependencies).length, 6);

  for (const [os, cpu] of platforms) {
    const suffix = `${os}-${cpu}`;
    const name = `@vgxness/pi-backend-${suffix}`;
    assert.equal(pi.optionalDependencies[name], pi.version);
    const dir = path.join(root, `packages/pi-backend-${suffix}`);
    const pkg = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
    const schema = JSON.parse(fs.readFileSync(path.join(dir, "manifest.schema.json"), "utf8"));
    assert.equal(pkg.name, name);
    assert.equal(pkg.version, pi.version);
    assert.deepEqual(pkg.os, [os]);
    assert.deepEqual(pkg.cpu, [cpu]);
    assert.equal(pkg.scripts, undefined);
    assert.equal(schema.additionalProperties, false);
    assert.deepEqual(schema.required, ["name", "version", "binary", "sha256"]);
  }
});

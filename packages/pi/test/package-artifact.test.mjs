import test from "node:test";
import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
const source = fileURLToPath(new URL("../", import.meta.url));

test("portable runtime runs from isolated source artifacts with only Node on PATH", () => {
 const root = mkdtempSync(join(tmpdir(), "vgxness-pi-artifact-"));
 try {
  for (const name of ["src", "resources", "package.json"]) cpSync(join(source, name), join(root, name), {recursive:true});
  const entries = readdirSync(root, {recursive:true});
  assert.ok(!entries.some(name => /(?:^|[/\\])(?:node_modules|bin)(?:[/\\]|$)|\.(?:go|exe|node)$/.test(name)));
  const result = spawnSync(process.execPath, [join(root, "src/probe.ts")], {cwd:root, encoding:"utf8", env:{PATH:dirname(process.execPath), HOME:root, TMPDIR:root, TMP:root, TEMP:root, SystemRoot:process.env.SystemRoot ?? ""}, timeout:10000});
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), {type:"health", runtime:"typescript", schemaVersion:23, foreignKeys:true, fts5:true, bigint:true, backup:true});
  assert.ok(!readdirSync(root).some(name => name.endsWith(".db") || name.startsWith("vgxness-pi-health-")), "probe must remove temporary databases");
  const pkg = JSON.parse(readFileSync(join(root,"package.json")));
  assert.equal(pkg.optionalDependencies, undefined);
 } finally { rmSync(root, {recursive:true, force:true}); }
});

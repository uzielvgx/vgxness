import { readdirSync, realpathSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
const directory = new URL("../test/", import.meta.url);
const available = readdirSync(directory).filter(name => /\.test\.(?:ts|mjs)$/.test(name)).sort();
const selected = process.argv.slice(2);
if (selected.some(name => !available.includes(name))) throw new Error("unknown test file");
const files = (selected.length ? selected : available).map(name => fileURLToPath(new URL(name, directory)));
// macOS commonly exposes its temporary directory through /var -> /private/var.
// Canonicalize test-owned scratch space without relaxing runtime symlink checks.
const temporary = realpathSync(tmpdir());
const result = spawnSync(process.execPath, ["--test", ...files], {stdio:"inherit", env:{...process.env, TMPDIR:temporary, TMP:temporary, TEMP:temporary}});
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;

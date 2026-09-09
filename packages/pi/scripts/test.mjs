import { readdirSync, realpathSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { delimiter, join } from "node:path";
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
const cwd = fileURLToPath(new URL("../", import.meta.url));
const go = spawnSync("go", ["env", "GOROOT"], { cwd, encoding: "utf8" });
if (go.error || go.status !== 0) throw go.error ?? new Error("resolve Go toolchain failed");
const result = spawnSync(process.execPath, ["--test", ...files], {cwd, stdio:"inherit", env:{...process.env, PATH:`${join(go.stdout.trim(), "bin")}${delimiter}${process.env.PATH ?? ""}`, GOTOOLCHAIN:"local", TMPDIR:temporary, TMP:temporary, TEMP:temporary}});
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;

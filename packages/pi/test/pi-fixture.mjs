import { access, realpath } from "node:fs/promises";
import { createRequire, globalPaths } from "node:module";
import { execFileSync } from "node:child_process";
import { dirname, join } from "node:path";

const require = createRequire(import.meta.url);

async function existing(path) {
  try { await access(path); return path; } catch { return undefined; }
}

export async function piPaths() {
  const explicitCli = process.env.VGXNESS_PI_CLI;
  const explicitSdk = process.env.VGXNESS_PI_SDK_ROOT;
  let packageJson;
  if (!explicitSdk) {
    try { packageJson = require.resolve("@earendil-works/pi-coding-agent/package.json", { paths: globalPaths }); }
    catch {
      const npmRoot = execFileSync("npm", ["root", "-g"], { encoding: "utf8" }).trim();
      packageJson = join(npmRoot, "@earendil-works", "pi-coding-agent", "package.json");
      if (!(await existing(packageJson))) {
        const piExecutable = execFileSync("which", ["pi"], { encoding: "utf8" }).trim();
        const cli = await realpath(piExecutable);
        packageJson = join(dirname(cli), "..", "..", "package.json");
      }
      if (!(await existing(packageJson))) throw new Error("installed Pi SDK is unavailable for this native fixture");
    }
  }
  const root = explicitSdk ?? dirname(packageJson);
  const cli = explicitCli ?? join(root, "dist", "bundle", "cli.js");
  if (!(await existing(cli))) throw new Error("installed Pi CLI is unavailable for this native fixture");
  return { root, cli };
}

export async function loadPiExtensions() {
  const { root } = await piPaths();
  return await import(join(root, "dist", "core", "extensions", "loader.js"));
}

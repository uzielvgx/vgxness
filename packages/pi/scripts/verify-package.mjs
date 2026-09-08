import fs from "node:fs";
const pkg = JSON.parse(fs.readFileSync(new URL("../package.json", import.meta.url)));
if (pkg.scripts.postinstall || pkg.scripts.install || pkg.scripts.preinstall) throw new Error("lifecycle scripts are forbidden");
if (Object.values(pkg.optionalDependencies).some(version => version !== pkg.version)) throw new Error("sidecars must be exact-versioned");

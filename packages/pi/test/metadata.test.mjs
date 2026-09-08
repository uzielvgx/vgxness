import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
test("package has six exact optional sidecars and no lifecycle scripts", () => {
 const pkg = JSON.parse(fs.readFileSync(new URL("../package.json", import.meta.url)));
 assert.equal(Object.keys(pkg.optionalDependencies).length, 6);
 assert.ok(Object.values(pkg.optionalDependencies).every(v => v === pkg.version));
 assert.equal(pkg.scripts.postinstall, undefined);
});

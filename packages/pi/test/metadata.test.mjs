import test from "node:test";
test("portable metadata and exact migration hashes match the host contract", async () => {
 await import("../scripts/verify-package.mjs");
});

import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, writeFile, rm, symlink, chmod, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { CredentialFile } from "../src/ports/credentials.ts";

test("credential reader checks private regular file and rejects symlinks, oversized and changing paths", async t => {
 const root = await mkdtemp(join(tmpdir(), "pi-credential-")); t.after(() => rm(root, { recursive: true, force: true }));
 const path = join(root, "credential"); await writeFile(path, "fixture-private-token\n", { mode: 0o600 });
 assert.equal(new CredentialFile(path).get("fixture"), "fixture-private-token");
 await symlink(path, join(root, "link")); assert.equal(new CredentialFile(join(root, "link")).get("fixture"), undefined);
 await mkdir(join(root, "nested")); await symlink(join(root, "nested"), join(root, "directory-link")); await writeFile(join(root, "nested", "secret"), "token", { mode: 0o600 });
 assert.equal(new CredentialFile(join(root, "directory-link", "secret")).get("fixture"), undefined);
 if (process.platform !== "win32") { await chmod(path, 0o644); assert.equal(new CredentialFile(path).get("fixture"), undefined); await chmod(path, 0o600); }
 await writeFile(path, "x".repeat(515)); assert.equal(new CredentialFile(path).get("fixture"), undefined);
});

test("portable credential path implementation rechecks ancestors and metadata", async t => {
 const root = await mkdtemp(join(tmpdir(), "pi-credential-portable-")); t.after(() => rm(root, { recursive: true, force: true }));
 const path = join(root, "credential"); await writeFile(path, "portable-token", { mode: 0o600 });
 // Exercise the portable algorithm on this host; this is not an OS certification.
 assert.equal((new CredentialFile(path) as any).portableRead(), "portable-token");
 await symlink(path, join(root, "link")); assert.equal((new CredentialFile(join(root, "link")) as any).portableRead(), undefined);
});

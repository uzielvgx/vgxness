import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildAndCheck, encodeTar, repository, rewriteInner, rewriteOuter, tar, validateArchive } from "../scripts/check-release-artifact.mjs";

function fixture() {
  const root = mkdtempSync(join(realpathSync(tmpdir()), "vgxness-pi-release-tamper-"));
  const head = execFileSync("git", ["-C", repository, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
  const output = join(root, "release");
  execFileSync("go", ["run", "./cmd/vgxness-release", "pi", "--output", output, "--release-version", `v0.0.0-ci.${head.slice(0, 12)}`, "--commit", head], { cwd: repository, env: { ...process.env, GOTOOLCHAIN: "local", GOPROXY: "off" } });
  return { root, head, path: join(output, `vgxness-pi_0.0.0-ci.${head.slice(0, 12)}_portable.tar.gz`) };
}

test("assembled portable archive passes isolated health gate", () => {
  const result = buildAndCheck();
  assert.equal(result.health.schemaVersion, 23);
});

test("outer archive rejects a tampered inner checksum", () => {
  const value = fixture();
  try {
    rewriteOuter(value.path, members => members.set("SHA256SUMS", Buffer.from("0".repeat(64) + "  vgxness-pi-0.1.0.tgz\n")));
    assert.throws(() => validateArchive(value.path, value.head), /inner checksum mismatch/);
  } finally { rmSync(value.root, { recursive: true, force: true }); }
});

test("archive rejects a release commit tampered independently of its version", () => {
  const value = fixture();
  try {
    rewriteOuter(value.path, members => {
      const release = JSON.parse(members.get("RELEASE.json"));
      release.commit = "0".repeat(40);
      members.set("RELEASE.json", Buffer.from(JSON.stringify(release)));
    });
    assert.throws(() => validateArchive(value.path, value.head), /invalid release identity/);
  }
  finally { rmSync(value.root, { recursive: true, force: true }); }
});

test("archive rejects tampered provenance before extraction", () => {
  const value = fixture();
  try {
    rewriteOuter(value.path, members => members.set("PROVENANCE.json", Buffer.from(JSON.stringify({ name: "@vgxness/pi", version: "0.1.0", sourceSHA256: "0".repeat(64), provenance: "local source snapshot; unpublished" }))));
    assert.throws(() => validateArchive(value.path, value.head), /invalid provenance/);
  } finally { rmSync(value.root, { recursive: true, force: true }); }
});

test("archive rejects duplicate release metadata keys", () => {
  const value = fixture();
  try {
    rewriteOuter(value.path, members => {
      const release = JSON.parse(members.get("RELEASE.json"));
      members.set("RELEASE.json", Buffer.from(JSON.stringify(release).replace('"commit":', `"commit":"${release.commit}","commit":`)));
    });
    assert.throws(() => validateArchive(value.path, value.head), /invalid metadata/);
  } finally { rmSync(value.root, { recursive: true, force: true }); }
});

test("tar rejects Windows drive-qualified package entries on every host", () => {
  assert.throws(() => tar(encodeTar([["package/C:/escape.txt", Buffer.from("escape")]])), /unsafe tar entry/);
});

test("checksum-consistent manifest tampering is rejected before extraction", () => {
  const value = fixture();
  try {
    rewriteInner(value.path, entries => {
      const manifest = JSON.parse(entries.get("package/package.json"));
      manifest.dependencies = { unsafe: "1.0.0" };
      entries.set("package/package.json", Buffer.from(JSON.stringify(manifest)));
    });
    assert.throws(() => validateArchive(value.path, value.head), /invalid package manifest/);
  } finally { rmSync(value.root, { recursive: true, force: true }); }
});

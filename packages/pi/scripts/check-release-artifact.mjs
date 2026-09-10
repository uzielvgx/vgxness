/** Validate an unpublished portable Pi archive without external archive tools. */
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { gunzipSync, gzipSync } from "node:zlib";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, relative, resolve, win32 } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const repository = resolve(here, "../../..");
const packageVersion = "0.1.0";
const innerName = `vgxness-pi-${packageVersion}.tgz`;
const health = { type: "health", runtime: "typescript", schemaVersion: 23, foreignKeys: true, fts5: true, bigint: true, backup: true };
const hex = /^[0-9a-f]{64}$/;
const commit = /^[0-9a-f]{40}$/;

function sum(value) { return createHash("sha256").update(value).digest("hex"); }
function field(buffer, start, length) { return buffer.subarray(start, start + length).toString("utf8").replace(/\0.*$/, ""); }
function size(header) {
  const raw = field(header, 124, 12).trim();
  if (!/^[0-7]*$/.test(raw)) throw new Error("invalid tar size");
  return Number.parseInt(raw || "0", 8);
}
function checkedGzip(data, maxOutputLength) {
  if (data.length > 64 << 20) throw new Error("archive exceeds compressed size limit");
  return gunzipSync(data, { maxOutputLength });
}
function validChecksum(header) {
  const expected = field(header, 148, 8).trim();
  if (!/^[0-7]+$/.test(expected)) return false;
  const copy = Buffer.from(header);
  copy.fill(32, 148, 156);
  return Number.parseInt(expected, 8) === copy.reduce((total, byte) => total + byte, 0);
}

/** Decode only regular tar entries, rejecting duplicate, unsafe, and trailing data. */
export function tar(data, allowed) {
  const entries = new Map();
  let offset = 0;
  while (offset < data.length) {
    const header = data.subarray(offset, offset + 512);
    if (header.length !== 512) throw new Error("truncated tar header");
    if (header.every(byte => byte === 0)) {
      if (data.subarray(offset).some(byte => byte !== 0)) throw new Error("trailing tar data");
      break;
    }
    const suffix = field(header, 0, 100);
    const prefix = field(header, 345, 155);
    const name = prefix ? `${prefix}/${suffix}` : suffix;
    const type = field(header, 156, 1) || "0";
    const length = size(header);
    if (!validChecksum(header) || !name || isAbsolute(name) || win32.isAbsolute(name) || name.includes(":") || name.includes("\\") || name.split("/").some(part => !part || part === "." || part === "..") || type !== "0" || (allowed && !allowed.has(name)) || entries.has(name)) throw new Error("unsafe tar entry");
    const body = offset + 512;
    const end = body + length;
    if (!Number.isSafeInteger(end) || end > data.length) throw new Error("truncated tar entry");
    entries.set(name, data.subarray(body, end));
    offset = body + Math.ceil(length / 512) * 512;
  }
  return entries;
}

export function encodeTar(entries) {
  const blocks = [];
  for (const [name, value] of entries) {
    const header = Buffer.alloc(512);
    header.write(name, 0, "utf8");
    header.write("0000644\0", 100, "ascii");
    header.write("0000000\0", 108, "ascii");
    header.write("0000000\0", 116, "ascii");
    header.write(value.length.toString(8).padStart(11, "0") + "\0", 124, "ascii");
    header.write("00000000000\0", 136, "ascii");
    header.fill(32, 148, 156);
    header.write("0", 156, "ascii");
    header.write("ustar\0", 257, "ascii");
    header.write("00", 263, "ascii");
    const checksum = header.reduce((total, byte) => total + byte, 0);
    header.write(checksum.toString(8).padStart(6, "0") + "\0 ", 148, "ascii");
    blocks.push(header, value, Buffer.alloc((512 - value.length % 512) % 512));
  }
  return Buffer.concat([...blocks, Buffer.alloc(1024)]);
}

function object(bytes, keys) {
  const source = bytes.toString("utf8");
  const compact = source.endsWith("\n") ? source.slice(0, -1) : source;
  let value;
  try { value = JSON.parse(compact); } catch { throw new Error("invalid metadata"); }
  // Go writes compact JSON. Re-serialization also rejects duplicate top-level keys.
  if ((source !== compact && source !== `${compact}\n`) || compact !== JSON.stringify(value) || !value || Array.isArray(value) || Object.keys(value).length !== keys.length || !keys.every(key => typeof value[key] === "string")) throw new Error("invalid metadata");
  return value;
}
function safeWrite(root, name, value) {
  if (isAbsolute(name) || win32.isAbsolute(name) || name.includes(":")) throw new Error("unsafe extraction path");
  const target = resolve(root, name);
  const unixRelative = relative(root, target);
  const windowsRelative = win32.relative(win32.resolve(root), win32.resolve(root, name));
  if (isAbsolute(unixRelative) || unixRelative === "" || unixRelative === ".." || unixRelative.startsWith(`..${process.platform === "win32" ? "\\" : "/"}`) || win32.isAbsolute(windowsRelative) || windowsRelative === "" || windowsRelative === ".." || windowsRelative.startsWith("..\\")) throw new Error("unsafe extraction path");
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, value, { flag: "wx", mode: 0o644 });
}

export function validateArchive(path, expectedCommit, expectedVersion = `v0.0.0-ci.${expectedCommit.slice(0, 12)}`) {
  const archive = readFileSync(path);
  const outer = tar(checkedGzip(archive, 96 << 20), new Set([innerName, "PROVENANCE.json", "SHA256SUMS", "RELEASE.json"]));
  if (outer.size !== 4) throw new Error("incomplete outer archive");
  const release = object(outer.get("RELEASE.json"), ["releaseVersion", "commit", "packageVersion", "sourceSHA256"]);
  if (release.releaseVersion !== expectedVersion || release.commit !== expectedCommit || !commit.test(release.commit) || release.packageVersion !== packageVersion || !hex.test(release.sourceSHA256)) throw new Error("invalid release identity");
  const provenance = object(outer.get("PROVENANCE.json"), ["name", "version", "sourceSHA256", "provenance"]);
  if (provenance.name !== "@vgxness/pi" || provenance.version !== packageVersion || provenance.sourceSHA256 !== release.sourceSHA256 || provenance.provenance !== "local source snapshot; unpublished") throw new Error("invalid provenance");
  if (outer.get("SHA256SUMS").toString("utf8") !== `${sum(outer.get(innerName))}  ${innerName}\n`) throw new Error("inner checksum mismatch");
  const inner = tar(checkedGzip(outer.get(innerName), 64 << 20));
  if ([...inner.keys()].some(name => !name.startsWith("package/"))) throw new Error("invalid package envelope");
  if (!inner.has("package/package.json") || !inner.has("package/src/probe.ts")) throw new Error("incomplete package envelope");
  assert.deepEqual(JSON.parse(inner.get("package/package.json").toString("utf8")), { name: "@vgxness/pi", version: packageVersion, type: "module", pi: { extensions: ["./src/extension.ts"] }, engines: { node: ">=22.19.0" }, peerDependencies: { "@earendil-works/pi-coding-agent": "^0.84.4", typebox: "1.3.7" }, files: ["src", "resources", "LICENSE"] }, "invalid package manifest");
  return { archive, outer, inner, release };
}

export function rewriteOuter(path, transform) {
  const members = tar(checkedGzip(readFileSync(path), 96 << 20), new Set([innerName, "PROVENANCE.json", "SHA256SUMS", "RELEASE.json"]));
  transform(members);
  writeFileSync(path, gzipSync(encodeTar([...members])));
}

export function rewriteInner(path, transform) {
  rewriteOuter(path, members => {
    const entries = tar(checkedGzip(members.get(innerName), 64 << 20));
    transform(entries);
    const inner = gzipSync(encodeTar([...entries]));
    members.set(innerName, inner);
    members.set("SHA256SUMS", Buffer.from(`${sum(inner)}  ${innerName}\n`));
  });
}

export function buildAndCheck() {
  const head = execFileSync("git", ["-C", repository, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
  if (!commit.test(head)) throw new Error("invalid checked-out git HEAD");
  const root = realpathSync(tmpdir());
  const work = mkdtempSync(join(root, "vgxness-pi-release-health-"));
  try {
    const output = join(work, "release");
    const version = `v0.0.0-ci.${head.slice(0, 12)}`;
    execFileSync("go", ["run", "./cmd/vgxness-release", "pi", "--output", output, "--release-version", version, "--commit", head], { cwd: repository, env: { ...process.env, GOTOOLCHAIN: "local", GOPROXY: "off" }, stdio: "pipe" });
    const path = join(output, `vgxness-pi_0.0.0-ci.${head.slice(0, 12)}_portable.tar.gz`);
    const checksums = readFileSync(join(output, "SHA256SUMS"), "utf8");
    if (checksums !== `${sum(readFileSync(path))}  ${path.split(/[\\/]/).at(-1)}\n`) throw new Error("outer checksum mismatch");
    const result = validateArchive(path, head, version);
    const extracted = join(work, "extracted");
    for (const [name, value] of result.inner) safeWrite(extracted, name.replace(/^package\//, ""), value);
    const nodeDir = dirname(process.execPath);
    const home = join(work, "home"), temporary = join(work, "tmp");
    mkdirSync(home); mkdirSync(temporary);
    const probe = spawnSync(process.execPath, [join(extracted, "src", "probe.ts")], { cwd: extracted, encoding: "utf8", timeout: 30000, env: { HOME: home, PATH: nodeDir, TMPDIR: temporary, TMP: temporary, TEMP: temporary, SystemRoot: process.env.SystemRoot ?? "" } });
    if (probe.status !== 0) throw new Error(`isolated probe failed: ${probe.stderr}`);
    assert.deepEqual(JSON.parse(probe.stdout), health);
    return { archive: path, release: result.release, health };
  } finally { rmSync(work, { recursive: true, force: true }); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const result = buildAndCheck();
  process.stdout.write(JSON.stringify({ type: "pi-release-artifact", release: result.release, health: result.health }) + "\n");
}

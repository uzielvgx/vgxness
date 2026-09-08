import { createHash } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";
import { dirname, parse, resolve, sep } from "node:path";
export type Manifest = { name: string; version: string; binary: string; sha256: string };
export async function assertNoLinks(path: string) { const absolute=resolve(path); let current=parse(absolute).root; for(const part of absolute.slice(current.length).split(sep).filter(Boolean)){current=resolve(current,part);if((await lstat(current)).isSymbolicLink())throw new Error("linked package artifact");} }
export async function hashFile(path: string) { await assertNoLinks(path); const info=await lstat(path); if(!info.isFile())throw new Error("invalid binary"); return createHash("sha256").update(await readFile(path)).digest("hex"); }
export async function readManifest(path: string, expected: Pick<Manifest,"name"|"version"> & { platform:string; arch:string }): Promise<{ manifest: Manifest; binary: string }> {
  await assertNoLinks(path); const root = dirname(path); const info = await lstat(path); if (!info.isFile()) throw new Error("invalid manifest");
  let value: unknown; try { value = JSON.parse(await readFile(path,"utf8")); } catch { throw new Error("invalid manifest"); }
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("invalid manifest"); const m = value as Manifest;
  const fixed=expected.platform==="win32"?"bin/vgxness-pi-backend.exe":"bin/vgxness-pi-backend";
  if (Object.keys(m).length !== 4 || m.name !== expected.name || m.version !== expected.version || m.binary !== fixed || !/^[a-f0-9]{64}$/.test(m.sha256)) throw new Error("manifest identity mismatch");
  const packagePath=resolve(root,"package.json"); await assertNoLinks(packagePath); let packageMetadata: any; try { packageMetadata = JSON.parse(await readFile(packagePath, "utf8")); } catch { throw new Error("invalid package metadata"); }
  if (!packageMetadata || packageMetadata.name !== expected.name || packageMetadata.version !== expected.version || JSON.stringify(packageMetadata.os)!==JSON.stringify([expected.platform]) || JSON.stringify(packageMetadata.cpu)!==JSON.stringify([expected.arch])) throw new Error("package metadata mismatch");
  const binary = resolve(root,fixed); const stat = await lstat(binary); if (!stat.isFile() || (expected.platform!=="win32" && (stat.mode&0o111)===0)) throw new Error("invalid binary");
  if (await hashFile(binary) !== m.sha256) throw new Error("binary hash mismatch"); return { manifest:m, binary };
}

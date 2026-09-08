import { lstat } from "node:fs/promises";
import { join, resolve } from "node:path";
import { assertNoLinks, readManifest } from "./manifest.ts";
export type SelectedBackend = { packageName: string; packageRoot: string; binary: string; manifest: { name:string;version:string;binary:string;sha256:string } };
export async function selectBackend(options: { root: string; version: string; platform?: NodeJS.Platform; arch?: string; resolvePackage?: (name:string)=>Promise<string> }): Promise<SelectedBackend> {
  const platform=options.platform ?? process.platform, arch=options.arch ?? process.arch; if (!(["linux","darwin","win32"].includes(platform) && ["x64","arm64"].includes(arch))) throw new Error("unsupported platform");
  const packageName=`@vgxness/pi-backend-${platform}-${arch}`; const root=resolve(options.resolvePackage ? await options.resolvePackage(packageName) : join(options.root,"node_modules",...packageName.split("/"))); await assertNoLinks(root); if(!(await lstat(root)).isDirectory())throw new Error("invalid package root");
  const {manifest,binary}=await readManifest(join(root,"manifest.json"),{name:packageName,version:options.version,platform,arch}); return {packageName,packageRoot:root,binary,manifest};
}

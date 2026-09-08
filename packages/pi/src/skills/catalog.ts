import { access, lstat, readdir, readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

type Options = { sharedRoot?: string; bundledRoot?: string; existingNames?: Iterable<string> };
async function compatible(root: string, known: Set<string>) {
  const paths: string[] = [];
  for (const entry of (await readdir(root, { withFileTypes: true })).filter((item) => item.isDirectory()).sort((a, b) => a.name.localeCompare(b.name))) {
    const path = join(root, entry.name);
    try {
      const manifest = join(path, "SKILL.md");
      const info = await lstat(manifest);
      if (!info.isFile() || info.isSymbolicLink() || info.size > 262144) continue;
      const text = await readFile(manifest, "utf8");
      const name = /^name:\s*([^\n]+)$/m.exec(text)?.[1]?.trim();
      const compatibility = /^compatibility:\s*([^\n]+)$/m.exec(text)?.[1]?.trim();
      // A bare compatibility key does not establish that this is a portable
      // VGXNESS skill. Accept only the declared Agent Skills host contract.
      if (!name || known.has(name) || !compatibility || !/agent skills hosts/i.test(compatibility) || !/(managed-by:\s*vgxness|provenance:\s*["']VGXNESS)/i.test(text)) continue;
      known.add(name);
      paths.push(path);
    } catch { /* A candidate is optional and must be fully readable. */ }
  }
  return paths;
}
/** Discovery reads shared skills first, then package-bundled fallbacks without touching user paths. */
export async function discoverSkillPaths(options: Options = {}) {
  const known = new Set(options.existingNames ?? []);
  const shared = options.sharedRoot ?? join(homedir(), ".agents", "skills");
  const bundled = options.bundledRoot ?? join(dirname(fileURLToPath(import.meta.url)), "../../resources/skills");
  const paths: string[] = [];
  try { await access(shared); paths.push(...await compatible(shared, known)); } catch { /* fall through to bundled */ }
  try { await access(bundled); paths.push(...await compatible(bundled, known)); } catch { /* release may omit optional bundled skills */ }
  return paths;
}

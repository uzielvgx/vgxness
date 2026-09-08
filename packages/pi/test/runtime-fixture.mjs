import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";
import { loadPiExtensions } from "./pi-fixture.mjs";
export async function loadNativeRuntime() {
  const root = await mkdtemp(join(tmpdir(), "pi-native-loader-")), key = `__runtime_${randomUUID()}`;
  const source = new URL("../src/service/dispatcher.ts", import.meta.url).pathname;
  try {
    const wrapper = join(root, "runtime.ts");
    await writeFile(wrapper, `import * as runtime from ${JSON.stringify(source)}; export default () => { globalThis[${JSON.stringify(key)}] = runtime; };`);
    const { loadExtensions } = await loadPiExtensions();
    const loaded = await loadExtensions([wrapper], root);
    if (loaded.errors.length) throw new Error(JSON.stringify(loaded.errors));
    return globalThis[key];
  } finally { delete globalThis[key]; await rm(root, { recursive: true, force: true }); }
}

import { Type } from "typebox";
import { Value } from "typebox/value";
import { listManagedSkills, snapshotWorkerSkills } from "../workers/context.ts";
const parameters = Type.Union([
  Type.Object({ operation: Type.Literal("list") }, { additionalProperties: false }),
  Type.Object({ operation: Type.Literal("read"), name: Type.String({ minLength: 1, maxLength: 64 }), resources: Type.Optional(Type.Array(Type.String({ minLength: 1, maxLength: 512 }), { maxItems: 7 })) }, { additionalProperties: false }),
]);
export function createSkillTool(options: Parameters<typeof snapshotWorkerSkills>[1] = {}) {
  return { name: "vgx_skill", label: "Read managed skill", description: "List or read managed skills and selected relative text resources. Returns exact manifest SHA-256 for task.skills. Reading grants no script execution or other permissions.", parameters,
    async execute(_id: string, input: any) {
      if (!Value.Check(parameters, input)) throw new Error("invalid skill input");
      const value = input.operation === "list" ? await listManagedSkills(options) : (await snapshotWorkerSkills([{ name: input.name, resources: input.resources }], options))[0];
      return { content: [{ type: "text", text: JSON.stringify(value) }] };
    }
  };
}

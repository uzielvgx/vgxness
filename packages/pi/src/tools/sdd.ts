import { Type } from "typebox";
import { validate, type ToolHost } from "./memory.ts";

const id = () => Type.String({ minLength: 1, maxLength: 256 });
const version = () => Type.Integer({ minimum: 1 });
const artifact = Type.Union(["explore", "proposal", "spec", "design", "tasks", "apply", "verify", "complete"].map((value) => Type.Literal(value)) as any);
const mode = Type.Union([Type.Literal("automatic"), Type.Literal("interactive")]);
const backend = Type.Union([Type.Literal("memory"), Type.Literal("openspec"), Type.Literal("hybrid")]);
const input = Type.Object({ artifactId: id(), revisionId: id(), digest: Type.String({ pattern: "^[a-f0-9]{64}$" }) }, { additionalProperties: false });
const operation = <T>(name: string, properties: T) => Type.Object({ operation: Type.Literal(name), ...(properties as object) }, { additionalProperties: false });

export const sddSchema = Type.Union([
  operation("create", { idempotencyKey: id(), title: id(), backend, interactionMode: mode, plan: Type.Union([Type.Literal("low"), Type.Literal("medium"), Type.Literal("high"), Type.Literal("ultra")]) }),
  operation("list", { status: Type.Optional(Type.Union([Type.Literal("active"), Type.Literal("completed"), Type.Literal("cancelled")])), limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 100 })) }),
  operation("get", { id: id() }),
  operation("set_interaction_mode", { changeId: id(), interactionMode: mode, expectedStateVersion: version() }),
  operation("save_revision", { changeId: id(), artifact, content: Type.String(), externalLocation: Type.Optional(Type.String()), digest: Type.Optional(Type.String({ pattern: "^[a-f0-9]{64}$" })), inputs: Type.Optional(Type.Array(input, { maxItems: 16 })), inputDigest: Type.Optional(Type.String({ pattern: "^[a-f0-9]{64}$" })), expectedStateVersion: version() }),
  operation("get_revision", { changeId: id(), revisionId: id() }),
  operation("list_revisions", { changeId: id(), artifact: Type.Optional(artifact), limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 100 })) }),
  operation("accept_revision", { changeId: id(), revisionId: id(), expectedStateVersion: version() }),
  operation("transition", { changeId: id(), targetPhase: artifact, expectedStateVersion: version() }),
  operation("cancel", { changeId: id(), expectedStateVersion: version() }),
  operation("projection_status", { changeId: id(), artifactId: id() }),
  operation("record_projection", { changeId: id(), artifactId: id(), revisionId: id(), status: Type.Union([Type.Literal("current"), Type.Literal("stale"), Type.Literal("drift"), Type.Literal("failed")]), digest: Type.String({ pattern: "^[a-f0-9]{64}$" }), location: id(), expectedStateVersion: version() }),
  operation("render_projection", { changeId: id(), revisionId: id() }),
  operation("compare_projection", { changeId: id(), revisionId: id(), relativePath: id(), projectionContent: Type.Optional(Type.String()), missing: Type.Optional(Type.Boolean()), symlink: Type.Optional(Type.Boolean()) }),
]);

const base64 = (value: string) => Buffer.from(value, "utf8").toString("base64");
const utf8 = (value: unknown) => typeof value === "string" ? Buffer.from(value, "base64").toString("utf8") : value;
export function createSddTool(host: ToolHost) {
  return { name: "sdd", label: "SDD", description: "Perform one closed SDD operation through the local backend.", parameters: sddSchema,
    async execute(_id: string, inputValue: Record<string, unknown>) {
      validate(sddSchema, inputValue);
      const { operation, ...payload } = inputValue;
      if (operation === "save_revision") payload.content = base64(String(payload.content));
      const result = await (await host.backend()).request(`sdd.${operation}`, payload, host);
      if ((operation === "get_revision" || operation === "render_projection") && result && typeof result === "object" && "content" in result) {
        (result as Record<string, unknown>).content = utf8((result as Record<string, unknown>).content);
      }
      return { content: [{ type: "text", text: JSON.stringify(result) }] };
    },
  };
}

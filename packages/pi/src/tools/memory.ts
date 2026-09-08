import { Type } from "typebox";
import { Value } from "typebox/value";

type Backend = { request(operation: string, payload: unknown, binding: { workspace: string; mode: "read-only" | "full"; role: string }): Promise<unknown> };
export type ToolHost = { workspace: string; mode: "read-only" | "full"; role: string; backend: () => Promise<Backend> };
const text = () => Type.String({ minLength: 1 });
const scope = Type.Optional(Type.Literal("project"));
const state = Type.Union([Type.Literal("active"), Type.Literal("needs_review")]);
const result = (value: unknown) => ({ content: [{ type: "text", text: JSON.stringify(value) }] });
export function validate(schema: unknown, value: unknown) {
  if (!Value.Check(schema as any, value)) throw new Error("invalid tool input");
}

export const memorySchemas = {
  memory_save: Type.Object({ title: Type.Optional(Type.String({ maxLength: 256 })), content: text(), type: Type.Optional(text()), topicKey: Type.Optional(text()), session: Type.Optional(text()), sourceProvider: Type.Optional(text()), sourceId: Type.Optional(text()), scope, state: Type.Optional(state), references: Type.Optional(Type.Array(text(), { maxItems: 32 })) }, { additionalProperties: false }),
  memory_search: Type.Object({ query: text(), type: Type.Optional(text()), topicKey: Type.Optional(text()), scope, states: Type.Optional(Type.Array(state, { maxItems: 2 })), limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 100 })), matchAny: Type.Optional(Type.Boolean()) }, { additionalProperties: false }),
  memory_recent: Type.Object({ scope, states: Type.Optional(Type.Array(state, { maxItems: 2 })), limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 100 })) }, { additionalProperties: false }),
  memory_get: Type.Object({ id: text(), scope }, { additionalProperties: false }),
  memory_forget: Type.Object({ id: text(), scope }, { additionalProperties: false }),
};
const operation = <T>(name: string, properties: T) => Type.Object({ operation: Type.Literal(name), ...(properties as object) }, { additionalProperties: false });
const timestamp = Type.String({ format: "date-time" });
export const memoryOperationSchema = Type.Union([
  operation("project.resolve", {}),
  operation("project.initialize", {}),
  operation("sync.configure", { endpoint: text(), deviceId: text() }),
  operation("sync.status", {}),
  operation("sync", {}),
  operation("sync.backfill", { limit: Type.Integer({ minimum: 1, maximum: 10000 }) }),
  operation("sync.repair_project", { confirmedRemoteAbsent: Type.Literal(true) }),
  operation("sync.reseed", {}),
  operation("sync.rejoin", {}),
  operation("session.start", { externalId: text() }),
  operation("session.checkpoint", { handle: text() }),
  operation("session.renew", { handle: text() }),
  operation("session.end", { handle: text(), state: Type.Union([Type.Literal("completed"), Type.Literal("cancelled"), Type.Literal("interrupted")]), summary: Type.String({ maxLength: 4096 }) }),
  operation("session.context", { handle: text() }),
  operation("session.draft_save", { handle: text(), summary: Type.String({ maxLength: 4096 }), expectedUpdatedAt: timestamp }),
]);

export function createMemoryTools(host: ToolHost) {
  const definitions = [
    ["memory_save", "memory.remember", "Save durable project memory"],
    ["memory_search", "memory.recall", "Search project memory"],
    ["memory_recent", "memory.recent", "Read recent project memory"],
    ["memory_get", "memory.get", "Read one project memory item"],
    ["memory_forget", "memory.forget", "Archive one project memory item"],
  ] as const;
  const tools = definitions.map(([name, operation, description]) => ({
    name, label: name, description, parameters: memorySchemas[name],
    async execute(_id: string, payload: unknown) {
      validate(memorySchemas[name], payload);
      const value = await (await host.backend()).request(operation, payload, host);
      return result(value);
    },
  }));
  tools.push({
    name: "memory", label: "Memory administration", description: "Perform one closed project, sync, or provider-session memory operation.", parameters: memoryOperationSchema,
    async execute(_id: string, inputValue: Record<string, unknown>) {
      validate(memoryOperationSchema, inputValue);
      const { operation, ...payload } = inputValue;
      return result(await (await host.backend()).request(`memory.${operation}`, payload, host));
    },
  } as any);
  return tools;
}

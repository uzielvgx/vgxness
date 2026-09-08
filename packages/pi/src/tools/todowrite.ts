import { Type } from "typebox";
import { Value } from "typebox/value";

export const todoWriteSchema = Type.Object({
  todos: Type.Array(Type.Object({
    content: Type.String({ minLength: 1 }),
    status: Type.Union([Type.Literal("pending"), Type.Literal("in_progress"), Type.Literal("completed")]),
  }, { additionalProperties: false }), { maxItems: 64 }),
}, { additionalProperties: false });

export function createTodoWriteTool(pi: { appendEntry(type: string, data: unknown): void }) {
  return {
    name: "todowrite",
    label: "Update todos",
    description: "Replace the current session todo list.",
    parameters: todoWriteSchema,
    async execute(_id: string, input: { todos: Array<{ content: string; status: string }> }, _signal: AbortSignal | undefined, _update: unknown, ctx: any) {
      if (!Value.Check(todoWriteSchema, input)) throw new Error("invalid tool input");
      pi.appendEntry("vgxness.todos", { todos: input.todos });
      ctx.ui?.setStatus?.("vgxness.todos", `${input.todos.filter((todo: any) => todo.status !== "completed").length} todo item(s) remaining`);
      return { content: [{ type: "text", text: `Recorded ${input.todos.length} todo item(s).` }] };
    },
  };
}

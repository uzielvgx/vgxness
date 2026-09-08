import { Type } from "typebox";
import { Value } from "typebox/value";

export const questionSchema = Type.Object({
  question: Type.String({ minLength: 1 }),
  options: Type.Array(Type.String({ minLength: 1 }), { minItems: 1, maxItems: 8 }),
}, { additionalProperties: false });

export function createQuestionTool() {
  return {
    name: "question",
    label: "Ask a question",
    description: "Ask the user to choose one of the supplied options.",
    parameters: questionSchema,
    async execute(_id: string, input: { question: string; options: string[] }, _signal: AbortSignal | undefined, _update: unknown, ctx: any) {
      if (!Value.Check(questionSchema, input)) throw new Error("invalid tool input");
      if (_signal?.aborted) return { isError: true, content: [{ type: "text", text: "Question cancelled." }] };
      const answer = await ctx.ui.select(input.question, input.options, { signal: _signal });
      return { isError: answer === undefined, content: [{ type: "text", text: answer === undefined ? "Question cancelled." : answer }] };
    },
  };
}

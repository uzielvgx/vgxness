/** Caller-supplied correlation metadata, never proof of a checked-out snapshot. */
export type CandidateReference = Readonly<{ commit?: string; snapshot?: string }>;
export function isCandidateReference(value: unknown): value is CandidateReference {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>, keys = Object.keys(record);
  if (!keys.length || keys.some(key => key !== "commit" && key !== "snapshot")) return false;
  return keys.every(key => typeof record[key] === "string" && (record[key] as string).length === (key === "commit" ? 40 : 64) && (key === "commit" ? /^[0-9a-f]{40}$/ : /^[0-9a-f]{64}$/).test(record[key] as string));
}

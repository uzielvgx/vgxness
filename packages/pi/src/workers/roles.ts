export const workerRoles = ["explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger", "sdd-research", "sdd-proposal", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply"] as const;
export type WorkerRole = typeof workerRoles[number];
export function workerCanWrite(role: WorkerRole) { return role === "general" || role === "sdd-apply"; }
export function assertWorkerRole(role: string): asserts role is WorkerRole { if (!(workerRoles as readonly string[]).includes(role)) throw new Error("unsupported worker role"); }
export function assertWorkerOperation(role: WorkerRole, operation: "read" | "patch" | "check") { if (operation === "patch" && !workerCanWrite(role)) throw new Error("worker role cannot write"); }

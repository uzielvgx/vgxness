import { loadManagerContract, resolveRole } from "../orchestration/contract.ts";
// Synchronous module initialization rejects a malformed generated resource
// before task schemas, the runner, or the dispatcher can use its authority.
const contract = loadManagerContract();
export const workerRoles = contract.roles.map(role => role.id) as readonly string[];
export type WorkerRole = string;
export function workerCanWrite(role: string): boolean { return contract.roles.find(item => item.id === role)?.writeAuthority === true; }
export function assertWorkerRole(role: string): asserts role is WorkerRole { if (!contract.roles.some(item => item.id === role)) throw new Error("unsupported worker role"); }
export function assertWorkerOperation(role: WorkerRole, operation: "read" | "patch" | "check") { if (operation === "patch" && !workerCanWrite(role)) throw new Error("worker role cannot write"); }

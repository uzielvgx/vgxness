export type Capability = "efficient" | "balanced" | "frontier";
export type Effort = "low" | "medium" | "high" | "ultra";
export type Plan = Effort;
export type Role = "manager" | "research" | "proposal" | "spec" | "design" | "tasks" | "apply" | "implementation" | "verification" | "care-reviewer" | "care-specialist" | "care-challenger";
export type Model = { provider: string; id: string; name: string; capability?: Capability; supportedEfforts: Effort[] };
export type Catalog = { provider: string; models: Model[] };
export type ResolvedAssignment = { role: Role; capability: Capability; model: Model; requestedEffort: Effort; effort: Effort; degradation: { degraded: boolean; reason?: string } };
export type ResolvedPlan = { provider: string; plan: Plan; slots: Record<Capability, Model>; roles: Record<Role, ResolvedAssignment> };

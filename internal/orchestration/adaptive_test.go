package orchestration

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPolicyForIsProportionalAndDeterministic(t *testing.T) {
	tests := []struct {
		name string
		in   Classification
		want ExecutionPolicy
	}{
		{
			"conversation answer",
			Classification{DomainConversation, OperationAnswer, ComplexityTrivial, SideEffectNone, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteDirect, Authorization: AuthorizationNone, Verification: VerificationNone}),
		},
		{
			"complex email draft avoids engineering ceremony",
			Classification{DomainWriting, OperationDraft, ComplexityComplex, SideEffectNone, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteDirect, Authorization: AuthorizationNone, Verification: VerificationNone}),
		},
		{
			"complex trip plan remains direct",
			Classification{DomainPlanning, OperationPlan, ComplexityComplex, SideEffectNone, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteDirect, Authorization: AuthorizationNone, Verification: VerificationNone}),
		},
		{
			"knowledge inspection",
			Classification{DomainKnowledge, OperationInspect, ComplexitySimple, SideEffectRead, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteAssisted, MaxTools: 3, Authorization: AuthorizationNone, Verification: VerificationTargeted}),
		},
		{
			"complex research may delegate without todo",
			Classification{DomainKnowledge, OperationInspect, ComplexityComplex, SideEffectRead, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteAssisted, MaxTools: 3, MaxDelegations: 1, Authorization: AuthorizationNone, Verification: VerificationTargeted}),
		},
		{
			"external calendar action",
			Classification{DomainPlanning, OperationExecute, ComplexitySimple, SideEffectExternal, RiskMedium},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteAction, MaxTools: 6, Authorization: AuthorizationExplicit, Verification: VerificationTargeted}),
		},
		{
			"local non-repository action",
			Classification{DomainSystem, OperationModify, ComplexitySimple, SideEffectLocalWrite, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteAction, MaxTools: 6, Authorization: AuthorizationScoped, Verification: VerificationTargeted}),
		},
		{
			"bounded repository read",
			Classification{DomainRepository, OperationInspect, ComplexitySimple, SideEffectRead, RiskLow},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteAssisted, MaxTools: 3, Authorization: AuthorizationNone, Verification: VerificationTargeted}),
		},
		{
			"repository diagnosis",
			Classification{DomainRepository, OperationInspect, ComplexityComplex, SideEffectRead, RiskMedium},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteEngineering, MaxTools: 12, MaxDelegations: 2, UseTodo: true, Authorization: AuthorizationNone, Verification: VerificationComprehensive}),
		},
		{
			"repository edit",
			Classification{DomainRepository, OperationModify, ComplexityComplex, SideEffectLocalWrite, RiskMedium},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteEngineering, MaxTools: 12, MaxDelegations: 2, UseTodo: true, Authorization: AuthorizationScoped, Verification: VerificationComprehensive}),
		},
		{
			"irreversible action escalation",
			Classification{DomainSystem, OperationExecute, ComplexitySimple, SideEffectIrreversible, RiskMedium},
			expectedExecutionPolicy(ExecutionPolicy{Route: AdaptiveRouteAssured, MaxTools: 16, MaxDelegations: 2, UseTodo: true, Authorization: AuthorizationExplicit, Verification: VerificationIndependent}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := PolicyFor(test.in)
			if err != nil || got != test.want {
				t.Fatalf("PolicyFor(%+v) = %+v, %v; want %+v", test.in, got, err, test.want)
			}
		})
	}
}

func TestPolicyForFailsClosedOnInvalidClassification(t *testing.T) {
	got, err := PolicyFor(Classification{Domain: "unknown", Operation: OperationAnswer, Complexity: ComplexityTrivial, SideEffect: SideEffectNone, Risk: RiskLow})
	if !errors.Is(err, ErrInvalidClassification) {
		t.Fatalf("PolicyFor invalid error = %v", err)
	}
	if got.Route != AdaptiveRouteAssured || got.MaxTools != 0 || got.MaxDelegations != 0 || !got.UseTodo || got.Authorization != AuthorizationExplicit || got.Verification != VerificationIndependent || got.BudgetAccounting != BudgetAllAttempts || got.OnExhaustion != ExhaustionHaltAndReport {
		t.Fatalf("invalid classification did not fail closed: %+v", got)
	}
}

func TestComplexityAloneNeverSelectsEngineering(t *testing.T) {
	for _, classification := range []Classification{
		{Domain: DomainConversation, Operation: OperationAnswer, Complexity: ComplexityComplex, SideEffect: SideEffectNone, Risk: RiskLow},
		{Domain: DomainWriting, Operation: OperationTransform, Complexity: ComplexityComplex, SideEffect: SideEffectNone, Risk: RiskLow},
		{Domain: DomainPlanning, Operation: OperationPlan, Complexity: ComplexityComplex, SideEffect: SideEffectNone, Risk: RiskLow},
	} {
		policy, err := PolicyFor(classification)
		if err != nil || policy.Route == AdaptiveRouteEngineering || policy.UseTodo {
			t.Fatalf("complex daily request gained engineering ceremony: %+v, %v", policy, err)
		}
	}
}

func TestMemoryPolicyIsOrthogonalAndConservative(t *testing.T) {
	tests := []struct {
		name      string
		intent    MemoryIntent
		candidate MemoryCandidate
		want      MemoryPolicy
	}{
		{"no candidate", MemoryIntentNone, MemoryCandidate{}, MemoryPolicy{Decision: MemoryIgnore}},
		{"intent recall", MemoryIntentRecall, MemoryCandidate{}, MemoryPolicy{Decision: MemoryRecall, MaxTools: 1}},
		{"autonomous durable save", MemoryIntentNone, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true}, MemoryPolicy{Decision: MemorySave, MaxTools: 1, Autonomous: true}},
		{"explicit durable save", MemoryIntentSave, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true}, MemoryPolicy{Decision: MemorySave, MaxTools: 1}},
		{"transient rejected", MemoryIntentSave, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true, Transient: true}, MemoryPolicy{Decision: MemoryReject}},
		{"log rejected", MemoryIntentSave, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true, Log: true}, MemoryPolicy{Decision: MemoryReject}},
		{"secret rejected", MemoryIntentSave, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true, Secret: true}, MemoryPolicy{Decision: MemoryReject}},
		{"recall does not admit secret candidate", MemoryIntentRecall, MemoryCandidate{Secret: true}, MemoryPolicy{Decision: MemoryReject}},
		{"personal data rejected", MemoryIntentSave, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true, PersonalData: true}, MemoryPolicy{Decision: MemoryReject}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MemoryPolicyFor(test.intent, test.candidate)
			if err != nil || got != test.want {
				t.Fatalf("MemoryPolicyFor() = %+v, %v; want %+v", got, err, test.want)
			}
			if got.AutomaticCloudSync || got.RequiresEngineering {
				t.Fatalf("memory policy caused unrelated ceremony or sync: %+v", got)
			}
		})
	}
}

func TestMemoryPolicyRejectsIncompleteSafetyAssessment(t *testing.T) {
	for _, candidate := range []MemoryCandidate{{Durable: true, EvidenceBacked: true}, {Durable: true, SafetyAssessed: true}, {EvidenceBacked: true, SafetyAssessed: true}, {}} {
		got, err := MemoryPolicyFor(MemoryIntentSave, candidate)
		if !errors.Is(err, ErrUnsafeMemoryCandidate) || got.Decision != MemoryReject || got.MaxTools != 0 {
			t.Fatalf("incomplete candidate %+v = %+v, %v", candidate, got, err)
		}
	}
	got, err := MemoryPolicyFor(MemoryIntentRecall, MemoryCandidate{})
	if err != nil || got.Decision != MemoryRecall || got.MaxTools != 1 {
		t.Fatalf("candidate-free recall = %+v, %v", got, err)
	}
	got, err = MemoryPolicyFor(MemoryIntentNone, MemoryCandidate{Durable: true, EvidenceBacked: true})
	if !errors.Is(err, ErrUnsafeMemoryCandidate) || got.Decision != MemoryReject {
		t.Fatalf("incomplete autonomous save = %+v, %v", got, err)
	}
	got, err = MemoryPolicyFor(MemoryIntentRecall, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true})
	if !errors.Is(err, ErrUnsafeMemoryCandidate) || got.Decision != MemoryReject {
		t.Fatalf("recall with save candidate = %+v, %v", got, err)
	}
}

func TestDirectExecutionAndMemoryBudgetsRemainOrthogonal(t *testing.T) {
	execution, err := PolicyFor(Classification{DomainConversation, OperationAnswer, ComplexityTrivial, SideEffectNone, RiskLow})
	if err != nil || execution.MaxTools != 0 || execution.MaxDelegations != 0 || execution.UseTodo {
		t.Fatalf("direct execution budget = %+v, %v", execution, err)
	}
	memory, err := MemoryPolicyFor(MemoryIntentSave, MemoryCandidate{Durable: true, EvidenceBacked: true, SafetyAssessed: true})
	if err != nil || memory.MaxTools != 1 || memory.RequiresEngineering || memory.AutomaticCloudSync {
		t.Fatalf("orthogonal memory budget = %+v, %v", memory, err)
	}
}

func TestExecutionBudgetAccountingAndExhaustion(t *testing.T) {
	policy, err := PolicyFor(Classification{DomainKnowledge, OperationInspect, ComplexityComplex, SideEffectRead, RiskLow})
	if err != nil || policy.BudgetAccounting != BudgetAllAttempts || policy.OnExhaustion != ExhaustionHaltAndReport {
		t.Fatalf("budget contract = %+v, %v", policy, err)
	}
	for _, test := range []struct {
		kind     BudgetKind
		attempts int
		want     bool
	}{{BudgetTool, 2, true}, {BudgetTool, 3, false}, {BudgetDelegation, 0, true}, {BudgetDelegation, 1, false}, {BudgetTool, -1, false}, {BudgetKind("unknown"), 0, false}} {
		if got := policy.AllowsAttempt(test.kind, test.attempts); got != test.want {
			t.Errorf("AllowsAttempt(%q, %d) = %t, want %t", test.kind, test.attempts, got, test.want)
		}
	}
}

func TestPolicyJSONPreservesDenyControls(t *testing.T) {
	execution, _ := PolicyFor(Classification{DomainConversation, OperationAnswer, ComplexityTrivial, SideEffectNone, RiskLow})
	memory, _ := MemoryPolicyFor(MemoryIntentNone, MemoryCandidate{})
	assertJSONKeys(t, execution, "route", "max_tools", "max_delegations", "use_todo", "authorization", "verification", "budget_accounting", "on_exhaustion")
	assertJSONKeys(t, memory, "decision", "max_tools", "autonomous", "automatic_cloud_sync", "requires_engineering")
}

func expectedExecutionPolicy(policy ExecutionPolicy) ExecutionPolicy {
	policy.BudgetAccounting = BudgetAllAttempts
	policy.OnExhaustion = ExhaustionHaltAndReport
	return policy
}

func assertJSONKeys(t *testing.T, value any, keys ...string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Errorf("%T JSON omits required control %q: %s", value, key, data)
		}
	}
}

func TestMemoryPolicyRejectsInvalidIntent(t *testing.T) {
	got, err := MemoryPolicyFor(MemoryIntent("invalid"), MemoryCandidate{})
	if !errors.Is(err, ErrInvalidMemoryIntent) || got.Decision != MemoryReject {
		t.Fatalf("invalid memory intent = %+v, %v", got, err)
	}
}

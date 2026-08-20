package orchestration

import "errors"

// Classification is supplied by a semantic classifier. This package does not
// infer intent from natural language.
type Classification struct {
	Domain     Domain     `json:"domain"`
	Operation  Operation  `json:"operation"`
	Complexity Complexity `json:"complexity"`
	SideEffect SideEffect `json:"side_effect"`
	Risk       Risk       `json:"risk"`
}

type Domain string

const (
	DomainConversation Domain = "conversation"
	DomainWriting      Domain = "writing"
	DomainPlanning     Domain = "planning"
	DomainKnowledge    Domain = "knowledge"
	DomainRepository   Domain = "repository-engineering"
	DomainSystem       Domain = "system-action"
)

type Operation string

const (
	OperationAnswer    Operation = "answer"
	OperationDraft     Operation = "draft"
	OperationTransform Operation = "transform"
	OperationInspect   Operation = "inspect"
	OperationPlan      Operation = "plan"
	OperationExecute   Operation = "execute"
	OperationModify    Operation = "modify"
)

type Complexity string

const (
	ComplexityTrivial Complexity = "trivial"
	ComplexitySimple  Complexity = "simple"
	ComplexityComplex Complexity = "complex"
)

type SideEffect string

const (
	SideEffectNone         SideEffect = "none"
	SideEffectRead         SideEffect = "read"
	SideEffectLocalWrite   SideEffect = "local-write"
	SideEffectExternal     SideEffect = "external"
	SideEffectIrreversible SideEffect = "irreversible"
)

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

type AdaptiveRoute string

const (
	AdaptiveRouteDirect      AdaptiveRoute = "direct"
	AdaptiveRouteAssisted    AdaptiveRoute = "assisted"
	AdaptiveRouteAction      AdaptiveRoute = "action"
	AdaptiveRouteEngineering AdaptiveRoute = "engineering"
	AdaptiveRouteAssured     AdaptiveRoute = "assured"
)

type AuthorizationPolicy string

const (
	AuthorizationNone     AuthorizationPolicy = "none"
	AuthorizationScoped   AuthorizationPolicy = "scoped"
	AuthorizationExplicit AuthorizationPolicy = "explicit"
)

type VerificationPolicy string

const (
	VerificationNone          VerificationPolicy = "none"
	VerificationTargeted      VerificationPolicy = "targeted"
	VerificationComprehensive VerificationPolicy = "comprehensive"
	VerificationIndependent   VerificationPolicy = "independent"
)

type ExecutionPolicy struct {
	Route            AdaptiveRoute       `json:"route"`
	MaxTools         int                 `json:"max_tools"`
	MaxDelegations   int                 `json:"max_delegations"`
	UseTodo          bool                `json:"use_todo"`
	Authorization    AuthorizationPolicy `json:"authorization"`
	Verification     VerificationPolicy  `json:"verification"`
	BudgetAccounting BudgetAccounting    `json:"budget_accounting"`
	OnExhaustion     ExhaustionPolicy    `json:"on_exhaustion"`
}

type BudgetAccounting string

const BudgetAllAttempts BudgetAccounting = "all-attempts-including-failures-and-retries"

type ExhaustionPolicy string

const (
	ExhaustionHaltAndReport         ExhaustionPolicy = "halt-and-report-no-new-call-no-escalation"
	ExhaustionCheckpointAndContinue ExhaustionPolicy = "checkpoint-and-continue-with-explicit-user-continuation"
)

type BudgetKind string

const (
	BudgetTool       BudgetKind = "tool"
	BudgetDelegation BudgetKind = "delegation"
)

// AllowsAttempt denies malformed accounting and any new call at exhaustion.
// Callers count every attempt, including failures and retries, before retrying.
func (policy ExecutionPolicy) AllowsAttempt(kind BudgetKind, attempts int) bool {
	if attempts < 0 || policy.BudgetAccounting != BudgetAllAttempts || (policy.OnExhaustion != ExhaustionHaltAndReport && policy.OnExhaustion != ExhaustionCheckpointAndContinue) {
		return false
	}
	switch kind {
	case BudgetTool:
		return attempts < policy.MaxTools
	case BudgetDelegation:
		return attempts < policy.MaxDelegations
	default:
		return false
	}
}

var ErrInvalidClassification = errors.New("invalid adaptive classification")

func (classification Classification) Validate() error {
	if !classification.Domain.valid() || !classification.Operation.valid() || !classification.Complexity.valid() || !classification.SideEffect.valid() || !classification.Risk.valid() {
		return ErrInvalidClassification
	}
	if (classification.Operation == OperationExecute || classification.Operation == OperationModify) && (classification.SideEffect == SideEffectNone || classification.SideEffect == SideEffectRead) {
		return ErrInvalidClassification
	}
	return nil
}

// PolicyFor maps supplied semantic facts to a bounded execution policy.
func PolicyFor(classification Classification) (ExecutionPolicy, error) {
	if err := classification.Validate(); err != nil {
		policy := newExecutionPolicy(AdaptiveRouteAssured, 0, 0, true, AuthorizationExplicit, VerificationIndependent)
		policy.OnExhaustion = ExhaustionHaltAndReport
		return policy, err
	}
	if classification.Risk == RiskHigh || classification.SideEffect == SideEffectIrreversible {
		return newExecutionPolicy(AdaptiveRouteAssured, 40, 5, true, AuthorizationExplicit, VerificationIndependent), nil
	}
	if classification.Domain == DomainRepository && (classification.Operation == OperationExecute || classification.Operation == OperationModify || classification.Operation == OperationInspect && (classification.Complexity == ComplexityComplex || classification.Risk == RiskMedium)) {
		return newExecutionPolicy(AdaptiveRouteEngineering, 30, 5, true, authorizationFor(classification.SideEffect), VerificationComprehensive), nil
	}
	if classification.SideEffect == SideEffectLocalWrite || classification.SideEffect == SideEffectExternal {
		return newExecutionPolicy(AdaptiveRouteAction, 6, 0, false, authorizationFor(classification.SideEffect), VerificationTargeted), nil
	}
	if classification.Operation == OperationInspect || classification.SideEffect == SideEffectRead {
		delegations := 0
		if classification.Complexity == ComplexityComplex {
			delegations = 1
		}
		return newExecutionPolicy(AdaptiveRouteAssisted, 3, delegations, false, AuthorizationNone, VerificationTargeted), nil
	}
	return newExecutionPolicy(AdaptiveRouteDirect, 0, 0, false, AuthorizationNone, VerificationNone), nil
}

func newExecutionPolicy(route AdaptiveRoute, tools, delegations int, todo bool, authorization AuthorizationPolicy, verification VerificationPolicy) ExecutionPolicy {
	exhaustion := ExhaustionHaltAndReport
	if route == AdaptiveRouteEngineering || route == AdaptiveRouteAssured {
		exhaustion = ExhaustionCheckpointAndContinue
	}
	return ExecutionPolicy{Route: route, MaxTools: tools, MaxDelegations: delegations, UseTodo: todo, Authorization: authorization, Verification: verification, BudgetAccounting: BudgetAllAttempts, OnExhaustion: exhaustion}
}

func authorizationFor(effect SideEffect) AuthorizationPolicy {
	if effect == SideEffectExternal || effect == SideEffectIrreversible {
		return AuthorizationExplicit
	}
	if effect == SideEffectLocalWrite {
		return AuthorizationScoped
	}
	return AuthorizationNone
}

func (value Domain) valid() bool {
	return value == DomainConversation || value == DomainWriting || value == DomainPlanning || value == DomainKnowledge || value == DomainRepository || value == DomainSystem
}

func (value Operation) valid() bool {
	return value == OperationAnswer || value == OperationDraft || value == OperationTransform || value == OperationInspect || value == OperationPlan || value == OperationExecute || value == OperationModify
}

func (value Complexity) valid() bool {
	return value == ComplexityTrivial || value == ComplexitySimple || value == ComplexityComplex
}

func (value SideEffect) valid() bool {
	return value == SideEffectNone || value == SideEffectRead || value == SideEffectLocalWrite || value == SideEffectExternal || value == SideEffectIrreversible
}

func (value Risk) valid() bool {
	return value == RiskLow || value == RiskMedium || value == RiskHigh
}

type MemoryIntent string

const (
	MemoryIntentNone   MemoryIntent = "none"
	MemoryIntentRecall MemoryIntent = "recall"
	MemoryIntentSave   MemoryIntent = "save"
)

type MemoryCandidate struct {
	Durable        bool `json:"durable,omitempty"`
	EvidenceBacked bool `json:"evidence_backed,omitempty"`
	SafetyAssessed bool `json:"safety_assessed,omitempty"`
	Transient      bool `json:"transient,omitempty"`
	Log            bool `json:"log,omitempty"`
	Secret         bool `json:"secret,omitempty"`
	PersonalData   bool `json:"personal_data,omitempty"`
}

type MemoryDecision string

const (
	MemoryIgnore MemoryDecision = "ignore"
	MemoryRecall MemoryDecision = "recall"
	MemorySave   MemoryDecision = "save"
	MemoryReject MemoryDecision = "reject"
)

type MemoryPolicy struct {
	Decision            MemoryDecision `json:"decision"`
	MaxTools            int            `json:"max_tools"`
	Autonomous          bool           `json:"autonomous"`
	AutomaticCloudSync  bool           `json:"automatic_cloud_sync"`
	RequiresEngineering bool           `json:"requires_engineering"`
}

var ErrInvalidMemoryIntent = errors.New("invalid memory intent")
var ErrUnsafeMemoryCandidate = errors.New("memory candidate rejected by safety policy")

// MemoryPolicyFor is independent of execution routing. It never requests
// engineering ceremony or cloud synchronization.
func MemoryPolicyFor(intent MemoryIntent, candidate MemoryCandidate) (MemoryPolicy, error) {
	if intent != MemoryIntentNone && intent != MemoryIntentRecall && intent != MemoryIntentSave {
		return MemoryPolicy{Decision: MemoryReject}, ErrInvalidMemoryIntent
	}
	if candidate.Transient || candidate.Log || candidate.Secret || candidate.PersonalData {
		return MemoryPolicy{Decision: MemoryReject}, nil
	}
	if intent == MemoryIntentRecall {
		if !candidate.present() {
			return MemoryPolicy{Decision: MemoryRecall, MaxTools: 1}, nil
		}
		return MemoryPolicy{Decision: MemoryReject}, ErrUnsafeMemoryCandidate
	}
	if candidate.Durable && candidate.EvidenceBacked && candidate.SafetyAssessed {
		return MemoryPolicy{Decision: MemorySave, MaxTools: 1, Autonomous: intent == MemoryIntentNone}, nil
	}
	if intent == MemoryIntentSave || candidate.present() {
		return MemoryPolicy{Decision: MemoryReject}, ErrUnsafeMemoryCandidate
	}
	return MemoryPolicy{Decision: MemoryIgnore}, nil
}

func (candidate MemoryCandidate) present() bool {
	return candidate.Durable || candidate.EvidenceBacked || candidate.SafetyAssessed || candidate.Transient || candidate.Log || candidate.Secret || candidate.PersonalData
}

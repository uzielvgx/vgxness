package gatekeeper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/registry"
)

var ErrInvalidPolicy = errors.New("invalid gatekeeper policy")

type Outcome string

const (
	Allow Outcome = "allow"
	Ask   Outcome = "ask"
	Deny  Outcome = "deny"
)

type Profile string

const (
	ProfileSafe       Profile = "safe"
	ProfileBalanced   Profile = "balanced"
	ProfileAutonomous Profile = "autonomous"
	ProfileCustom     Profile = "custom"
)

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

type OperationClass string

const (
	ReadFiles        OperationClass = "read-files"
	WriteFiles       OperationClass = "write-files"
	RunCommand       OperationClass = "run-command"
	InstallPackage   OperationClass = "install-package"
	Commit           OperationClass = "commit"
	Push             OperationClass = "push"
	Release          OperationClass = "release"
	PullRequest      OperationClass = "pull-request"
	Network          OperationClass = "network"
	UseMCP           OperationClass = "use-mcp"
	DestructiveFiles OperationClass = "destructive-files"
	Secrets          OperationClass = "secrets"
	Configuration    OperationClass = "configuration"
	PermissionExpand OperationClass = "permission-expand"
)

type AdapterHealth string

const (
	AdapterHealthy          AdapterHealth = "healthy"
	AdapterUnavailable      AdapterHealth = "unavailable"
	AdapterIncompatible     AdapterHealth = "incompatible"
	AdapterStale            AdapterHealth = "stale"
	AdapterPermissionDenied AdapterHealth = "permission-denied"
)

type AdapterEvidence struct {
	Reference    registry.ProviderReference
	Capabilities []registry.Capability
	Health       AdapterHealth
}

type Policy struct {
	Version       string
	Profile       Profile
	LeaseRequired map[OperationClass]bool
	CustomAllowed map[OperationClass]bool
}

type WorkUnit struct {
	ID                 string
	Active             bool
	AllowedRoots       []string
	DeniedRoots        []string
	AllowedTools       []string
	AllowedSkillScopes map[string]bool
	Operations         []OperationClass
	RiskCeiling        Risk
	ContextHash        string
}

type Lease struct {
	ID              string
	WorkUnitID      string
	ApprovedBy      string
	ApprovalWording string
	AllowedRoots    []string
	AllowedTools    []string
	Operations      []OperationClass
	RiskCeiling     Risk
	ExpiresAt       time.Time
	CorrelationID   string
	ContextHash     string
	Revoked         bool
}

type Approval struct {
	ID            string
	WorkUnitID    string
	Actor         string
	Wording       string
	Operation     OperationClass
	CorrelationID string
	ContextHash   string
	ApprovedAt    time.Time
	ExpiresAt     time.Time
	Human         bool
}

type TaskTransition struct {
	From chronicle.TaskStatus
	To   chronicle.TaskStatus
}

type Request struct {
	AgentID            string
	RequiredCapability registry.CapabilityNeed
	Adapter            AdapterEvidence
	WorkUnit           WorkUnit
	Operation          OperationClass
	Path               string
	Tool               string
	Risk               Risk
	CorrelationID      string
	Lease              *Lease
	Approval           *Approval
	Transition         *TaskTransition
}

type Decision struct {
	Outcome              Outcome
	Condition            string
	NextSafeAction       string
	PolicyVersion        string
	AgentID              string
	AgentRegistryVersion string
	CorrelationID        string
}

type Evaluator struct {
	registry *registry.Registry
	policy   Policy
	now      func() time.Time
}

func New(entries *registry.Registry, policy Policy) (*Evaluator, error) {
	if entries == nil || policy.Version == "" || !validProfile(policy.Profile) {
		return nil, ErrInvalidPolicy
	}
	if policy.Profile == ProfileCustom && len(policy.CustomAllowed) == 0 {
		return nil, ErrInvalidPolicy
	}
	leaseRequired := make(map[OperationClass]bool, len(policy.LeaseRequired))
	for operation, required := range policy.LeaseRequired {
		if !validOperation(operation) {
			return nil, ErrInvalidPolicy
		}
		leaseRequired[operation] = required
	}
	customAllowed := make(map[OperationClass]bool, len(policy.CustomAllowed))
	for operation, allowed := range policy.CustomAllowed {
		if !validOperation(operation) {
			return nil, ErrInvalidPolicy
		}
		customAllowed[operation] = allowed
	}
	policy.LeaseRequired = leaseRequired
	policy.CustomAllowed = customAllowed
	return &Evaluator{registry: entries, policy: policy, now: time.Now}, nil
}

func (e *Evaluator) Evaluate(ctx context.Context, request Request) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	base := Decision{PolicyVersion: e.policy.Version, AgentID: request.AgentID, CorrelationID: request.CorrelationID}
	if request.AgentID == "" || request.CorrelationID == "" || request.WorkUnit.ID == "" || request.WorkUnit.ContextHash == "" || request.RequiredCapability.Capability == "" || request.RequiredCapability.Version == "" || request.Adapter.Reference.Provider == "" || request.Adapter.Reference.ID == "" || request.Adapter.Reference.Version == "" || !validAdapterHealth(request.Adapter.Health) || !validRisk(request.Risk) || !validRisk(request.WorkUnit.RiskCeiling) || !validOperation(request.Operation) {
		return denied(base, "request.invalid", "correct the bounded operation request"), nil
	}
	agent, err := e.registry.ResolveAgent(ctx, request.AgentID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Decision{}, err
		}
		return denied(base, "registry.unresolved", "select an exact registered agent identity"), nil
	}
	base.AgentRegistryVersion = agent.RegistryVersion
	if request.Adapter.Reference != agent.Agent.Provider {
		return denied(base, "adapter.identity", "select the exact adapter declared by the registered agent"), nil
	}
	if request.Adapter.Health != AdapterHealthy {
		return denied(base, "adapter.health", "restore adapter health or select an eligible fallback"), nil
	}
	if !registry.SatisfiesCapabilities(request.Adapter.Capabilities, request.RequiredCapability) {
		return denied(base, "adapter.capability", "select an adapter that declares the required capability"), nil
	}
	if !request.WorkUnit.Active {
		return denied(base, "work_unit.inactive", "create or resume an active work unit"), nil
	}
	for _, reference := range agent.Agent.SkillRefs {
		if len(request.WorkUnit.AllowedSkillScopes) == 0 {
			return denied(base, "registry.scope", "authorize every resolved skill scope in the work unit"), nil
		}
		if _, err := e.registry.ResolveSkill(ctx, reference, request.WorkUnit.AllowedSkillScopes); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return Decision{}, err
			}
			return denied(base, "registry.scope", "authorize every resolved skill scope in the work unit"), nil
		}
	}
	if !containsOperation(request.WorkUnit.Operations, request.Operation) || riskRank(request.Risk) > riskRank(request.WorkUnit.RiskCeiling) {
		return denied(base, "work_unit.scope", "reduce the operation or authorize a new work unit"), nil
	}
	if request.Transition != nil && chronicle.ValidateTaskTransition(request.Transition.From, request.Transition.To) != nil {
		return denied(base, "transition.illegal", "keep the last legal task state"), nil
	}
	resolvedPath, pathErr := e.checkPath(request, agent.Agent.Permissions)
	if pathErr != nil {
		return denied(base, "work_unit.path", "choose a path inside the authorized roots"), nil
	}
	if !registry.Satisfies(agent.Agent, request.RequiredCapability) {
		return denied(base, "capability.missing", "select an agent with the exact required capability"), nil
	}
	if !permissionAllows(agent.Agent.Permissions, request.Operation) {
		return denied(base, "permission.denied", "select a permitted operation or agent"), nil
	}
	if err := checkTool(request, agent.Agent.Permissions); err != nil {
		return denied(base, "tool.denied", "select an explicitly allowed tool"), nil
	}
	now := e.now().UTC()
	approved := approvalValid(request, now)
	if !profileAllows(e.policy, request.Operation) && !approved {
		return asking(base, "profile.approval_required", "request fresh approval for this exact operation"), nil
	}
	if e.policy.LeaseRequired[request.Operation] && !leaseValid(request, resolvedPath, now) {
		return asking(base, "lease.required", "request a least-privilege lease for this work unit"), nil
	}
	if hardGate(request.Operation) && !approved {
		return asking(base, "hard_gate.approval_required", "request fresh human approval for this exact operation"), nil
	}
	base.Outcome = Allow
	base.Condition = "policy.allowed"
	base.NextSafeAction = "execute only the evaluated operation and record its result"
	return base, nil
}

func (e *Evaluator) checkPath(request Request, permissions registry.Permissions) (string, error) {
	if !pathOperation(request.Operation) {
		return "", nil
	}
	if request.Path == "" || len(request.WorkUnit.AllowedRoots) == 0 {
		return "", errors.New("path is not bounded")
	}
	path, err := resolvePath(request.Path)
	if err != nil || !withinAny(path, request.WorkUnit.AllowedRoots) {
		return "", errors.New("path is outside work unit")
	}
	if withinAny(path, request.WorkUnit.DeniedRoots) {
		return "", errors.New("path is excluded from work unit")
	}
	if withinAny(path, permissions.DeniedPaths) {
		return "", errors.New("path is denied")
	}
	if len(permissions.AllowedPaths) != 0 && !withinAny(path, permissions.AllowedPaths) {
		return "", errors.New("path is outside agent permissions")
	}
	return path, nil
}

func checkTool(request Request, permissions registry.Permissions) error {
	if !toolOperation(request.Operation) && request.Tool == "" {
		return nil
	}
	if request.Tool == "" || !contains(request.WorkUnit.AllowedTools, request.Tool) || contains(permissions.DeniedTools, request.Tool) {
		return errors.New("tool is not allowed")
	}
	if len(permissions.AllowedTools) != 0 && !contains(permissions.AllowedTools, request.Tool) {
		return errors.New("tool is outside agent permissions")
	}
	return nil
}

func approvalValid(request Request, now time.Time) bool {
	a := request.Approval
	return a != nil && a.Human && a.ID != "" && a.Actor != "" && a.Wording != "" && a.WorkUnitID == request.WorkUnit.ID && a.Operation == request.Operation && a.CorrelationID == request.CorrelationID && a.ContextHash == request.WorkUnit.ContextHash && !a.ApprovedAt.IsZero() && !a.ExpiresAt.IsZero() && !a.ApprovedAt.After(now) && a.ExpiresAt.After(now)
}

func leaseValid(request Request, resolvedPath string, now time.Time) bool {
	l := request.Lease
	if l == nil || l.Revoked || l.ID == "" || l.ApprovedBy == "" || l.ApprovalWording == "" || l.WorkUnitID != request.WorkUnit.ID || l.CorrelationID != request.CorrelationID || l.ContextHash != request.WorkUnit.ContextHash || l.ExpiresAt.IsZero() || !l.ExpiresAt.After(now) || !containsOperation(l.Operations, request.Operation) || !validRisk(l.RiskCeiling) || riskRank(request.Risk) > riskRank(l.RiskCeiling) {
		return false
	}
	if resolvedPath != "" && (len(l.AllowedRoots) == 0 || !withinAny(resolvedPath, l.AllowedRoots)) {
		return false
	}
	return request.Tool == "" || contains(l.AllowedTools, request.Tool)
}

func permissionAllows(p registry.Permissions, operation OperationClass) bool {
	switch operation {
	case ReadFiles:
		return p.MayReadFiles
	case WriteFiles, DestructiveFiles, Configuration:
		return p.MayWriteFiles
	case RunCommand:
		return p.MayRunCommands
	case InstallPackage:
		return p.MayInstallPackages
	case Commit:
		return p.MayCommit
	case Push, Release, PullRequest:
		return p.MayPush
	case Network:
		return p.MayUseNetwork
	case UseMCP:
		return p.MayUseMCP
	case Secrets, PermissionExpand:
		return true
	default:
		return false
	}
}

func profileAllows(policy Policy, operation OperationClass) bool {
	switch policy.Profile {
	case ProfileSafe:
		return operation == ReadFiles
	case ProfileBalanced, ProfileAutonomous:
		return true
	case ProfileCustom:
		return policy.CustomAllowed[operation]
	default:
		return false
	}
}

func hardGate(operation OperationClass) bool {
	switch operation {
	case InstallPackage, Commit, Push, Release, PullRequest, Network, DestructiveFiles, Secrets, Configuration, PermissionExpand:
		return true
	default:
		return false
	}
}

func pathOperation(operation OperationClass) bool {
	return operation == ReadFiles || operation == WriteFiles || operation == DestructiveFiles || operation == Configuration
}

func toolOperation(operation OperationClass) bool {
	switch operation {
	case RunCommand, InstallPackage, Commit, Push, Release, PullRequest, UseMCP:
		return true
	default:
		return false
	}
}

func resolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	probe := filepath.Clean(path)
	missing := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", err
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		resolved, err := resolvePath(root)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(resolved, path)
		if err == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func validProfile(profile Profile) bool {
	return profile == ProfileSafe || profile == ProfileBalanced || profile == ProfileAutonomous || profile == ProfileCustom
}

func validAdapterHealth(health AdapterHealth) bool {
	switch health {
	case AdapterHealthy, AdapterUnavailable, AdapterIncompatible, AdapterStale, AdapterPermissionDenied:
		return true
	default:
		return false
	}
}
func validRisk(risk Risk) bool { return risk == RiskLow || risk == RiskMedium || risk == RiskHigh }
func riskRank(risk Risk) int   { return map[Risk]int{RiskLow: 1, RiskMedium: 2, RiskHigh: 3}[risk] }

func validOperation(operation OperationClass) bool {
	for _, candidate := range []OperationClass{ReadFiles, WriteFiles, RunCommand, InstallPackage, Commit, Push, Release, PullRequest, Network, UseMCP, DestructiveFiles, Secrets, Configuration, PermissionExpand} {
		if operation == candidate {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsOperation(values []OperationClass, target OperationClass) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func denied(base Decision, condition, action string) Decision {
	base.Outcome, base.Condition, base.NextSafeAction = Deny, condition, action
	return base
}
func asking(base Decision, condition, action string) Decision {
	base.Outcome, base.Condition, base.NextSafeAction = Ask, condition, action
	return base
}

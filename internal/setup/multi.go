package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/selfinstall"
	"github.com/vgxness/vgxness/internal/skills"
)

// Provider identifies a setup target. The order is intentionally fixed so a
// combined apply never races provider-owned filesystem work.
type Provider string

const (
	ProviderOpenCode Provider = "opencode"
	ProviderCodex    Provider = "codex"
)

var providerOrder = [...]Provider{ProviderOpenCode, ProviderCodex}

// ProviderPlan is the provider-owned preflight result. Shared work is modeled
// by MultiPlan and must not be repeated by provider implementations.
type ProviderPlan struct {
	Provider       Provider
	Ready          bool
	Blocker        string
	Changed        bool
	Installed      bool
	ArtifactSHA256 string
	ArtifactCount  int
	State          integration.State
}

type ProviderResult struct {
	Provider Provider
	Verified bool
	Changed  bool
	Skipped  bool
	Recovery string
}

type SharedPhase struct{ Name string }

// SharedPlan is the launcher-and-skills preflight owned by Service.Shared.
type SharedPlan struct {
	Ready    bool
	Blocker  string
	Changed  bool
	Launcher selfinstall.Result
	Skills   skills.Result
}

type SharedResult struct {
	Verified bool
	Changed  bool
	Recovery string
	Launcher selfinstall.Result
}

type SharedRuntime interface {
	Plan(context.Context) (SharedPlan, error)
	Apply(context.Context, SharedPlan) (SharedResult, error)
	Finalize(context.Context, SharedPlan, SharedResult) (SharedResult, error)
}

type MultiOptions struct {
	Providers          []Provider
	ExpectedPlanDigest string
	// Verified is carried from a partial result into a retry. Only a verified
	// provider outcome can be considered for an unchanged-plan skip.
	Verified []ProviderResult
}

type MultiPlan struct {
	Digest     string
	Shared     []SharedPhase
	SharedPlan SharedPlan
	Providers  []ProviderPlan
	Ready      bool
	Changed    bool
	Blocker    string
}

type MultiResult struct {
	Plan      MultiPlan
	Shared    SharedResult
	Providers []ProviderResult
}

type ProviderRuntime interface {
	Provider() Provider
	Plan(context.Context, SharedPlan) (ProviderPlan, error)
	Apply(context.Context, ProviderPlan, SharedResult) (ProviderResult, error)
}

// IntegrationProvider adapts one provider-owned integration without exposing
// filesystem behavior to Multi. Codex receives only its root and shared plan.
type IntegrationProvider struct {
	provider Provider
	runtime  integration.Runtime
	options  integration.Options
}

func NewIntegrationProvider(provider Provider, runtime integration.Runtime, options integration.Options) *IntegrationProvider {
	if provider == ProviderCodex {
		root := integration.Options{ModelPlan: options.ModelPlan}
		if options.ConfigDir != "" {
			root.ConfigDir = options.ConfigDir
		} else {
			root.HomeDir = options.HomeDir
		}
		options = root
	}
	return &IntegrationProvider{provider: provider, runtime: runtime, options: options}
}

func (adapter *IntegrationProvider) Provider() Provider { return adapter.provider }

func (adapter *IntegrationProvider) Plan(ctx context.Context, _ SharedPlan) (ProviderPlan, error) {
	if adapter == nil || adapter.runtime == nil || (adapter.provider != ProviderOpenCode && adapter.provider != ProviderCodex) {
		return ProviderPlan{}, ErrInvalid
	}
	preview, err := adapter.runtime.Preview(ctx, adapter.options)
	if err != nil {
		return ProviderPlan{}, err
	}
	plan := ProviderPlan{Provider: adapter.provider, Installed: preview.State == integration.StateInstalled, Changed: preview.Changed || preview.State != integration.StateInstalled, ArtifactSHA256: preview.ArtifactSHA256, ArtifactCount: preview.ArtifactCount, State: preview.State}
	if preview.Provider != "" && preview.Provider != string(adapter.provider) {
		plan.Blocker = "provider preview identity does not match selection"
	} else if preview.State != integration.StateAbsent && preview.State != integration.StateInstalled && preview.State != integration.StatePartial {
		plan.Blocker = "provider integration has drift or is unsafe to overwrite"
	} else if preview.ArtifactSHA256 == "" {
		plan.Blocker = "provider preview lacks managed artifact identity"
	}
	plan.Ready = plan.Blocker == ""
	return plan, nil
}

func (adapter *IntegrationProvider) Apply(ctx context.Context, plan ProviderPlan, _ SharedResult) (ProviderResult, error) {
	if adapter == nil || adapter.runtime == nil || !plan.Ready || plan.Provider != adapter.provider {
		return ProviderResult{Provider: adapter.provider}, ErrPrerequisite
	}
	var installed integration.Result
	var err error
	if adapter.provider == ProviderCodex && plan.State == integration.StatePartial {
		managed, ok := adapter.runtime.(integration.ManagedRuntime)
		if !ok {
			return ProviderResult{Provider: adapter.provider}, fmt.Errorf("%w: codex managed runtime", ErrPrerequisite)
		}
		installed, err = managed.Reinstall(ctx, adapter.options)
	} else {
		installed, err = adapter.runtime.Install(ctx, adapter.options)
	}
	result := ProviderResult{Provider: adapter.provider, Changed: installed.Changed}
	if err != nil {
		return result, err
	}
	status, err := adapter.runtime.Status(ctx, adapter.options)
	if err != nil {
		return result, err
	}
	if status.Provider != string(adapter.provider) || status.State != integration.StateInstalled || status.ArtifactSHA256 != plan.ArtifactSHA256 || status.ArtifactCount != plan.ArtifactCount {
		return result, fmt.Errorf("%w: %s integration identity", ErrVerification, adapter.provider)
	}
	result.Verified = true
	return result, nil
}

// Multi is the UI-facing domain coordinator. Provider runtimes retain all
// provider filesystem behavior; this type only sequences typed outcomes.
type Multi struct {
	shared    SharedRuntime
	providers map[Provider]ProviderRuntime
}

func NewMulti(runtimes ...ProviderRuntime) *Multi {
	return NewMultiWithShared(nil, runtimes...)
}

func NewMultiWithShared(shared SharedRuntime, runtimes ...ProviderRuntime) *Multi {
	providers := make(map[Provider]ProviderRuntime, len(runtimes))
	for _, runtime := range runtimes {
		if runtime != nil {
			providers[runtime.Provider()] = runtime
		}
	}
	return &Multi{shared: shared, providers: providers}
}

func (m *Multi) Plan(ctx context.Context, options MultiOptions) (MultiPlan, error) {
	selected, err := selectedProviders(options.Providers)
	if err != nil || m == nil {
		return MultiPlan{}, ErrInvalid
	}
	plan := MultiPlan{Shared: []SharedPhase{{Name: "shared launcher and skills verification"}}, SharedPlan: SharedPlan{Ready: true}}
	if m.shared != nil {
		plan.SharedPlan, err = m.shared.Plan(ctx)
		if err != nil {
			return plan, err
		}
		if !plan.SharedPlan.Ready {
			plan.Blocker = plan.SharedPlan.Blocker
			if plan.Blocker == "" {
				plan.Blocker = "shared launcher and skills are not ready"
			}
		}
	}
	for _, provider := range selected {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		runtime := m.providers[provider]
		if runtime == nil {
			return plan, fmt.Errorf("%w: %s runtime", ErrPrerequisite, provider)
		}
		item, err := runtime.Plan(ctx, plan.SharedPlan)
		if err != nil {
			return plan, err
		}
		item.Provider = provider
		plan.Providers = append(plan.Providers, item)
		plan.Changed = plan.Changed || item.Changed
		if !item.Ready && plan.Blocker == "" {
			plan.Blocker = item.Blocker
			if plan.Blocker == "" {
				plan.Blocker = string(provider) + " is not ready"
			}
		}
	}
	plan.Changed = plan.Changed || plan.SharedPlan.Changed
	plan.Ready = plan.Blocker == ""
	plan.Digest = multiPlanDigest(plan)
	return plan, nil
}

func (m *Multi) Apply(ctx context.Context, options MultiOptions) (MultiResult, error) {
	plan, err := m.Plan(ctx, options)
	result := MultiResult{Plan: plan}
	if err != nil {
		return result, err
	}
	if options.ExpectedPlanDigest == "" || options.ExpectedPlanDigest != plan.Digest {
		return result, fmt.Errorf("%w: confirmed multi-provider preview no longer matches", ErrPrerequisite)
	}
	if !plan.Ready {
		return result, ErrPrerequisite
	}
	if m.shared != nil {
		shared, err := m.shared.Apply(ctx, plan.SharedPlan)
		result.Shared = shared
		if err != nil {
			return result, err
		}
		if !shared.Verified {
			return result, ErrVerification
		}
	}
	verified := make(map[Provider]bool, len(options.Verified))
	for _, outcome := range options.Verified {
		if outcome.Verified {
			verified[outcome.Provider] = true
		}
	}
	for _, item := range plan.Providers {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if verified[item.Provider] && item.Installed && !item.Changed {
			result.Providers = append(result.Providers, ProviderResult{Provider: item.Provider, Verified: true, Skipped: true})
			continue
		}
		outcome, err := m.providers[item.Provider].Apply(ctx, item, result.Shared)
		outcome.Provider = item.Provider
		result.Providers = append(result.Providers, outcome)
		if err != nil {
			return result, err
		}
		if !outcome.Verified {
			return result, fmt.Errorf("%w: %s", ErrVerification, item.Provider)
		}
	}
	if m.shared != nil {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		shared, err := m.shared.Finalize(ctx, plan.SharedPlan, result.Shared)
		result.Shared = shared
		if err != nil {
			return result, err
		}
		if !shared.Verified {
			return result, ErrVerification
		}
	}
	return result, nil
}

func selectedProviders(input []Provider) ([]Provider, error) {
	seen := make(map[Provider]bool, len(input))
	for _, provider := range input {
		if provider != ProviderOpenCode && provider != ProviderCodex || seen[provider] {
			return nil, ErrInvalid
		}
		seen[provider] = true
	}
	if len(seen) == 0 {
		return nil, ErrInvalid
	}
	selected := make([]Provider, 0, len(seen))
	for _, provider := range providerOrder {
		if seen[provider] {
			selected = append(selected, provider)
		}
	}
	return selected, nil
}

func multiPlanDigest(plan MultiPlan) string {
	plan.Digest = ""
	encoded, _ := json.Marshal(plan)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

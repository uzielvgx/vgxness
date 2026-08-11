package integration

import (
	"context"
	"errors"

	"github.com/vgxness/vgxness/internal/sdd"
)

var (
	ErrInvalid  = errors.New("invalid integration request")
	ErrConflict = errors.New("integration artifact conflicts with existing content")
	ErrDrift    = errors.New("integration artifact has drifted")
	ErrRecovery = errors.New("integration rollback or recovery failed")
)

type State string
type HandshakeStatus string

const (
	ModelAssignmentCount = 15

	StateAbsent    State = "absent"
	StatePartial   State = "partial"
	StateInstalled State = "installed"
	StateDrifted   State = "drifted"

	HandshakeHealthy      HandshakeStatus = "healthy"
	HandshakeUnavailable  HandshakeStatus = "unavailable"
	HandshakeIncompatible HandshakeStatus = "incompatible"
)

type Handshake struct {
	OK     bool
	Status HandshakeStatus
}

func (status HandshakeStatus) String() string { return string(status) }

type Options struct {
	ConfigDir              string
	HomeDir                string
	ModelAssignments       *map[string]sdd.ManagedAgentModelConfig
	ModelPlan              sdd.Plan
	ModelEfficient         string
	ModelBalanced          string
	ModelFrontier          string
	ModelEfficientEffort   sdd.Effort
	ModelEfficientVariant  sdd.OpenCodeVariant
	ModelBalancedEffort    sdd.Effort
	ModelBalancedVariant   sdd.OpenCodeVariant
	ModelFrontierEffort    sdd.Effort
	ModelFrontierVariant   sdd.OpenCodeVariant
	ModelVariantsSpecified bool
}

type Result struct {
	Provider                   string
	State                      State
	Path                       string
	ArtifactSHA256             string
	ToolPath                   string
	ToolSHA256                 string
	Changed                    bool
	BackupPath                 string
	ToolBackupPath             string
	ModelSchemaVersion         int
	ModelPlan                  sdd.Plan
	ModelProvider              string
	ModelAssignments           *[ModelAssignmentCount]sdd.OpenCodeAgentAssignmentV3
	ModelEfficient             string
	ModelBalanced              string
	ModelFrontier              string
	ModelEfficientEffort       sdd.Effort
	ModelEfficientVariant      sdd.OpenCodeVariant
	ModelBalancedEffort        sdd.Effort
	ModelBalancedVariant       sdd.OpenCodeVariant
	ModelFrontierEffort        sdd.Effort
	ModelFrontierVariant       sdd.OpenCodeVariant
	ModelVariantsSpecified     bool
	ModelEfficientSource       sdd.ModelSlotSource
	ModelBalancedSource        sdd.ModelSlotSource
	ModelFrontierSource        sdd.ModelSlotSource
	ModelEfficientAvailability sdd.ModelSlotAvailability
	ModelBalancedAvailability  sdd.ModelSlotAvailability
	ModelFrontierAvailability  sdd.ModelSlotAvailability
	ManifestPath               string
	ManifestSHA256             string
	DefaultAgent               string
	DefaultAgentPath           string
	RestartRequired            bool
	ArtifactCount              int
	RetainedPredecessorCount   int
	RetainedPredecessorPath    string
	DirectoryDurability        string
}

// ManagedArtifact identifies desired provider-owned content without exposing it.
type ManagedArtifact struct {
	RelativePath string
	SHA256       string
}

// ManagedLayout is the immutable desired inventory for one integration root.
type ManagedLayout struct {
	Root            string
	Artifacts       []ManagedArtifact
	AggregateSHA256 string
}

type Runtime interface {
	Preview(context.Context, Options) (Result, error)
	Install(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Result, error)
	Uninstall(context.Context, Options) (Result, error)
}

// ManagedRuntime adds recovery operations without widening ordinary CLI use.
type ManagedRuntime interface {
	Runtime
	ManagedLayout(context.Context, Options) (ManagedLayout, error)
	ReinstallPending(context.Context, Options) (bool, error)
	Reinstall(context.Context, Options) (Result, error)
}

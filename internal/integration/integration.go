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
type BridgeState string

const (
	StateAbsent    State = "absent"
	StatePartial   State = "partial"
	StateInstalled State = "installed"
	StateDrifted   State = "drifted"

	BridgeUnavailable BridgeState = "unavailable"
	BridgeConfigured  BridgeState = "configured"
	BridgeNotRequired BridgeState = "not-required"
)

type Options struct {
	ConfigDir      string
	HomeDir        string
	Model          string
	ModelPlan      sdd.Plan
	ModelEfficient string
	ModelBalanced  string
	ModelFrontier  string
}

type Result struct {
	Provider        string
	State           State
	Path            string
	ArtifactSHA256  string
	ToolPath        string
	ToolSHA256      string
	Model           string
	Bridge          BridgeState
	Changed         bool
	BackupPath      string
	ToolBackupPath  string
	ModelPlan       sdd.Plan
	ModelProvider   string
	ModelEfficient  string
	ModelBalanced   string
	ModelFrontier   string
	ManifestPath    string
	ManifestSHA256  string
	RestartRequired bool
	ArtifactCount   int
}

type Runtime interface {
	Preview(context.Context, Options) (Result, error)
	Install(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Result, error)
	Uninstall(context.Context, Options) (Result, error)
}

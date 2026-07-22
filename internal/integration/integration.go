package integration

import (
	"context"
	"errors"
)

var (
	ErrInvalid  = errors.New("invalid integration request")
	ErrConflict = errors.New("integration artifact conflicts with existing content")
	ErrDrift    = errors.New("integration artifact has drifted")
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
)

type Options struct {
	ConfigDir string
	HomeDir   string
	Model     string
}

type Result struct {
	Provider       string
	State          State
	Path           string
	ArtifactSHA256 string
	ToolPath       string
	ToolSHA256     string
	Model          string
	Bridge         BridgeState
	Changed        bool
	BackupPath     string
	ToolBackupPath string
}

type Runtime interface {
	Preview(context.Context, Options) (Result, error)
	Install(context.Context, Options) (Result, error)
	Status(context.Context, Options) (Result, error)
	Uninstall(context.Context, Options) (Result, error)
}

package tui

import (
	"context"
	"time"
)

// Backend is everything the console reads or does. Every field it returns is
// real product state (decision D-004); a source that failed reports its error
// in the matching field instead of failing the whole call.
type Backend interface {
	Overview(context.Context) (Overview, error)
	Diagnose(context.Context) (Diagnosis, error)

	// SearchMemories runs a full-text search, or lists the most recent
	// memories when the query text is empty.
	SearchMemories(context.Context, MemoryQuery) (MemoryResults, error)
	GetMemory(ctx context.Context, id string) (MemoryItem, error)
	// ForgetMemory archives one memory; it is the console's only memory write.
	ForgetMemory(ctx context.Context, id string) error
	Handoffs(ctx context.Context, limit int) ([]Handoff, error)

	// SetupState reads what plugin setup needs; RunSetupStep runs one step
	// through the claude CLI and returns its output for the setup log.
	SetupState(context.Context) (SetupState, error)
	RunSetupStep(context.Context, SetupStep) (string, error)

	SyncOverview(context.Context) (SyncOverview, error)
	// ConfigureSync stores the endpoint and device in the profile and the
	// bearer in the keyring, as `memory sync configure` does.
	ConfigureSync(ctx context.Context, endpoint, deviceID, bearer string) error
	// SyncNow runs one foreground project sync.
	SyncNow(context.Context) (SyncOutcome, error)
}

type SyncOverview struct {
	Configured, Enabled bool
	Endpoint, DeviceID  string
	Credential          string
	// PortableID is empty while the workspace has no project identity
	// marker; project sync needs one.
	PortableID string
	// Pending counts unsent changes of every project on this device.
	Pending  int
	LastPull time.Time
}

type SyncOutcome struct {
	Status                                                            string
	Pushed, PreviouslyAccepted, Rejected, Retried, Conflicts, Batches int
	FailureOperation, FailureClass                                    string
	HTTPStatus                                                        int
	Took                                                              time.Duration
}

type SetupState struct {
	PathBinary PathBinary
	Claude     Claude
	Plugin     Plugin
}

// PathBinary is the vgxness executable Claude Code will launch for the MCP
// server and hooks, which may differ from the one running the console.
type PathBinary struct {
	Path, Version string
	// SupportsPlugin is false for an older build without the
	// `claude-code` commands the plugin's hooks call.
	SupportsPlugin bool
}

type SetupStep uint8

const (
	StepAddMarketplace SetupStep = iota
	StepInstallPlugin
	StepUpdatePlugin
	StepVerifyPlugin
	StepVerifyMCP
)

type MemoryQuery struct {
	Text, Type string
}

type MemoryResults struct {
	Items []MemoryItem
	// Total is every active memory of the project; Types counts them by type.
	Total int
	Types []TypeCount
}

type TypeCount struct {
	Type  string
	Count int
}

type MemoryItem struct {
	ID, Title, Type, Topic, Producer string
	Preview, Content                 string
	References                       []string
	Created, Updated                 time.Time
}

// Overview is the state shown on Inicio.
type Overview struct {
	Workspace string
	Binary    Binary
	Claude    Claude
	Plugin    Plugin
	Storage   Storage
	Sync      SyncState
	// Handoffs holds the newest completed session handoffs, newest first.
	Handoffs []Handoff
	At       time.Time
}

type Binary struct {
	Version, Path string
}

type Claude struct {
	// Version is empty when the `claude` executable is unavailable.
	Version string
	Minimum string
	Err     error
}

type Plugin struct {
	Installed, Enabled, MarketplaceAdded bool
	Version                              string
	// Offered is the version the local marketplace checkout would install.
	Offered     string
	Agents      int
	InstallPath string
	Err         error
}

type Storage struct {
	Root, Database string
	// Exists is false for a database that was never created.
	Exists           bool
	Schema, Expected int
	Memories         int
	Err              error
}

type SyncState struct {
	Configured, Enabled bool
	// Credential is the keyring credential status: available, missing,
	// unavailable or invalid.
	Credential string
	Err        error
}

type Handoff struct {
	Handle, Summary    string
	Started, Completed time.Time
}

// Diagnosis is what Doctor shows: the overview plus checks that only make
// sense on demand.
type Diagnosis struct {
	Overview
	PathBinary   PathBinary
	RootWritable error
	// PolicyChars is the size of the installed plugin's Manager policy, the
	// text the SessionStart hook injects.
	PolicyChars int
	PolicyErr   error
	// MCPInstructions and MCPTools describe this binary's MCP server.
	MCPInstructions, MCPTools int
	Took                      time.Duration
	At                        time.Time
}

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

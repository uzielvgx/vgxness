package opencodebackup

import (
	"errors"
	"fmt"
	"time"
)

const (
	SchemaVersion   = "1"
	MaxEntries      = 100_000
	MaxFileSize     = int64(16 << 30)
	MaxTotalBytes   = int64(1 << 40)
	MaxManifestSize = int64(16 << 20)
	MaxPathLength   = 4096
)

var (
	ErrInvalid     = errors.New("invalid backup request")
	ErrConflict    = errors.New("backup restore conflict")
	ErrCorrupt     = errors.New("corrupt backup snapshot")
	ErrNotFound    = errors.New("backup snapshot not found")
	ErrUnsupported = errors.New("unsupported filesystem object or operation")
)

// Error identifies a branchable backup failure without embedding file content.
type Error struct {
	Kind error
	Op   string
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("opencode backup %s %q: %v", e.Op, e.Path, e.Kind)
	}
	return fmt.Sprintf("opencode backup %s: %v", e.Op, e.Kind)
}

func (e *Error) Unwrap() []error {
	if e.Err == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Err}
}

type Mode string

const (
	ModeManaged Mode = "managed"
	ModeFull    Mode = "full"
)

func (m Mode) Validate() error {
	if m != ModeManaged && m != ModeFull {
		return &Error{Kind: ErrInvalid, Op: "validate mode"}
	}
	return nil
}

type LauncherMetadata struct {
	Version        string `json:"version"`
	ManagedBy      string `json:"managedBy"`
	SHA256         string `json:"sha256,omitempty"`
	LauncherPath   string `json:"launcherPath,omitempty"`
	ManifestPath   string `json:"manifestPath,omitempty"`
	LauncherSHA256 string `json:"launcherSha256,omitempty"`
	ActiveSHA256   string `json:"activeSha256,omitempty"`
}

type Options struct {
	SourceRoot   string
	BackupRoot   string
	HomeDir      string
	ManagedPaths []string
	Launcher     *LauncherMetadata
}

type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion string            `json:"schemaVersion"`
	SnapshotID    string            `json:"snapshotId"`
	CreatedAt     time.Time         `json:"createdAt"`
	Mode          Mode              `json:"mode"`
	SourceRoot    string            `json:"sourceRoot"`
	Entries       []Entry           `json:"entries"`
	TotalBytes    int64             `json:"totalBytes"`
	Launcher      *LauncherMetadata `json:"launcher,omitempty"`
}

type Summary struct {
	SchemaVersion string
	SnapshotID    string
	CreatedAt     time.Time
	Mode          Mode
	SourceRoot    string
	EntryCount    int
	TotalBytes    int64
	Launcher      *LauncherMetadata
}

type Snapshot struct {
	Manifest  Manifest
	Summary   Summary
	Directory string
}

type RestoreRequest struct {
	SnapshotID       string
	PreviewSHA256    string
	ReplaceConflicts []string
	IncludePaths     []string
}

type RestorePreview struct {
	SnapshotID string
	Missing    []string
	Identical  []string
	Conflicts  []string
	SHA256     string
}

type RestoreResult struct {
	SnapshotID string
	Created    int
	Identical  int
	Replaced   int
	Unresolved []string
}

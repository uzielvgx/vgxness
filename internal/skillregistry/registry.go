// Package skillregistry discovers Agent Skills from authorized roots and
// publishes a bounded, regenerable metadata cache outside the versioned
// repository. It never copies skill bodies, never invents metadata, and never
// grants authority: an entry is discovery metadata only.
package skillregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/config"
)

// SchemaVersion is the only cache schema this build reads or writes. A cache
// with a different schema is treated as stale and rebuilt, never trusted.
const SchemaVersion = 1

// Root scopes. The default policy is an explicit global root plus an explicit
// project root; additional roots must be configured explicitly.
const (
	ScopeProject    = "project"
	ScopeGlobal     = "global"
	ScopeConfigured = "configured"
)

// Entry status values. StatusOK entries resolve by bare name, explicit ID, or
// absolute path. StatusDuplicate is not resolvable by a bare duplicated name
// (ErrAmbiguous), but an explicit ID or absolute path can resolve it while it
// retains the duplicate status and never gains ok authority. Every other value
// is retained with diagnostics so callers never mistake it for usable.
const (
	StatusOK         = "ok"
	StatusDuplicate  = "duplicate"
	StatusInvalid    = "invalid"
	StatusSymlink    = "symlink"
	StatusOversized  = "oversized"
	StatusUnreadable = "unreadable"
	StatusChanged    = "changed"
)

// Diagnostic kinds are stable, bounded identifiers; Detail is short and never
// contains skill bodies.
const (
	DiagMissingRoot  = "missing-root"
	DiagRootRejected = "root-rejected"
	DiagSymlink      = "symlink"
	DiagOversized    = "oversized"
	DiagInvalid      = "invalid-frontmatter"
	DiagUnreadable   = "unreadable"
	DiagDuplicate    = "duplicate"
	DiagLimit        = "limit-exceeded"
	DiagChanged      = "changed-during-scan"
)

// Errors classify failures for the CLI without leaking filesystem detail.
var (
	ErrInvalid   = errors.New("invalid skill registry request")
	ErrNotFound  = errors.New("skill registry entry not found")
	ErrBusy      = errors.New("skill registry is locked by another writer")
	ErrAmbiguous = errors.New("skill registry name is ambiguous")
	ErrStale     = errors.New("skill registry entry no longer matches its source")
)

// Bounds. They keep discovery and query output small and predictable.
const (
	DefaultMaxRoots          = 16
	DefaultMaxEntriesPerRoot = 512
	DefaultMaxEntriesTotal   = 2048
	DefaultMaxManifestBytes  = 262144
	DefaultMaxAge            = 15 * time.Minute
	DefaultSearchLimit       = 20
	MaxSearchLimit           = 100
	maxFrontmatterLines      = 256
	maxNameBytes             = 128
	maxDescriptionBytes      = 512
	maxDiagnosticDetailBytes = 256
	maxDiagnostics           = 4096
	maxEntryDiagnostics      = 4
	staleLockAge             = 30 * time.Second
)

// RootSpec is one authorized discovery root with its scope and provenance.
type RootSpec struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Scope      string `json:"scope"`
	Provenance string `json:"provenance"`
	Present    bool   `json:"present"`

	// trustedPrefix is the canonical directory a default root must stay under.
	// It is never serialized. Configured roots leave it empty because the caller
	// explicitly authorized that path.
	trustedPrefix string
}

// Entry is bounded metadata extracted from one candidate SKILL.md. Description
// is taken verbatim from frontmatter; it is never synthesized.
type Entry struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Path          string   `json:"path"`
	SHA256        string   `json:"sha256"`
	Compatibility string   `json:"compatibility,omitempty"`
	Status        string   `json:"status"`
	DuplicateOf   string   `json:"duplicateOf,omitempty"`
	Diagnostics   []string `json:"diagnostics,omitempty"`
}

// Diagnostic is a bounded problem report for a root or an entry.
type Diagnostic struct {
	Root   string `json:"root"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Registry is the schemaVersion 1 cache document. It is a derived artifact,
// never the source of truth: the authorized roots and their current bytes are.
// Complete is false when a query truncated the entry list.
type Registry struct {
	SchemaVersion int          `json:"schemaVersion"`
	GeneratedAt   string       `json:"generatedAt"`
	Workspace     string       `json:"workspace"`
	Roots         []RootSpec   `json:"roots"`
	Entries       []Entry      `json:"entries"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	Complete      bool         `json:"complete"`
}

// ContentDigest is a stable identity over the discovery result excluding the
// generation timestamp. It lets a caller detect additions, removals, hash
// changes, root changes, and diagnostics changes without trusting the cache.
func (registry Registry) ContentDigest() string {
	type canonical struct {
		SchemaVersion int          `json:"schemaVersion"`
		Workspace     string       `json:"workspace"`
		Roots         []RootSpec   `json:"roots"`
		Entries       []Entry      `json:"entries"`
		Diagnostics   []Diagnostic `json:"diagnostics"`
		Complete      bool         `json:"complete"`
	}
	value := canonical{
		SchemaVersion: registry.SchemaVersion,
		Workspace:     registry.Workspace,
		Roots:         append([]RootSpec{}, registry.Roots...),
		Entries:       append([]Entry{}, registry.Entries...),
		Diagnostics:   append([]Diagnostic{}, registry.Diagnostics...),
		Complete:      registry.Complete,
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Options configures discovery, cache location, and freshness. Roots, when set,
// replaces the default global+project policy. HomeDir exists for test isolation
// and for callers that resolve a non-default home.
type Options struct {
	Workspace         string
	HomeDir           string
	CachePath         string
	Roots             []RootSpec
	MaxAge            time.Duration
	MaxRoots          int
	MaxEntriesPerRoot int
	MaxEntriesTotal   int
	MaxManifestBytes  int64

	// now and beforePublish are test seams; they are never set by product code.
	now           func() time.Time
	beforePublish func(string) error
}

func (o Options) timestamp() time.Time {
	if o.now != nil {
		return o.now().UTC()
	}
	return time.Now().UTC()
}

func (o Options) maxAge() time.Duration {
	if o.MaxAge <= 0 {
		return DefaultMaxAge
	}
	return o.MaxAge
}

func (o Options) maxRoots() int {
	if o.MaxRoots <= 0 {
		return DefaultMaxRoots
	}
	return o.MaxRoots
}

func (o Options) maxEntriesPerRoot() int {
	if o.MaxEntriesPerRoot <= 0 {
		return DefaultMaxEntriesPerRoot
	}
	return o.MaxEntriesPerRoot
}

func (o Options) maxEntriesTotal() int {
	if o.MaxEntriesTotal <= 0 {
		return DefaultMaxEntriesTotal
	}
	return o.MaxEntriesTotal
}

func (o Options) maxManifestBytes() int64 {
	if o.MaxManifestBytes <= 0 {
		return DefaultMaxManifestBytes
	}
	return o.MaxManifestBytes
}

// Workspace returns the canonical absolute workspace bound to this registry.
func (o Options) workspacePath() (string, error) {
	ws := o.Workspace
	if ws == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", ErrInvalid
		}
		ws = wd
	}
	if !filepath.IsAbs(ws) || strings.IndexByte(ws, 0) >= 0 {
		return "", ErrInvalid
	}
	clean := filepath.Clean(ws)
	// Canonicalize only when the path exists; a missing workspace is still a
	// valid identity for cache derivation but never for discovery roots.
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved, nil
	}
	return clean, nil
}

// CachePath resolves the regenerable cache location. It intentionally lives in
// the existing per-project VGXNESS user-state directory, never inside the
// versioned repository.
func (o Options) cachePath() (string, error) {
	if o.CachePath != "" {
		if !filepath.IsAbs(o.CachePath) {
			return "", ErrInvalid
		}
		return filepath.Clean(o.CachePath), nil
	}
	workspace, err := o.workspacePath()
	if err != nil {
		return "", err
	}
	paths, err := config.PathsFor(config.Options{ProjectDir: workspace, HomeDir: o.HomeDir})
	if err != nil {
		return "", ErrInvalid
	}
	return filepath.Join(paths.Root, "skill-registry.json"), nil
}

// CachePath re-exported the resolved regenerable cache location so callers can
// report it without reaching into unexported state.
func CachePath(options Options) (string, error) { return options.cachePath() }

// rejectControlledSymlinks fails closed when a default root's path is outside
// its trusted prefix or traverses a symlink component under that prefix. Only
// the documented macOS /var compatibility symlink is tolerated; workspace- or
// home-controlled symlinks are not, so a project `.agents` link cannot redirect
// discovery outside the workspace. Configured roots have no prefix and are
// trusted by the caller because they were explicitly authorized.
func rejectControlledSymlinks(prefix, path string) error {
	if prefix == "" {
		return nil
	}
	relative, err := filepath.Rel(prefix, path)
	if err != nil {
		return ErrInvalid
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return ErrInvalid
	}
	current := prefix
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return ErrInvalid
		}
		if info.Mode()&os.ModeSymlink != 0 && !isPlatformCompatSymlink(current) {
			return ErrInvalid
		}
	}
	return nil
}

// isPlatformCompatSymlink permits only the documented macOS /var alias so the
// default temp/home layout is not misclassified as a hostile symlink.
func isPlatformCompatSymlink(path string) bool {
	if runtime.GOOS != "darwin" || path != "/var" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && resolved == "/private/var"
}

// withinRoots reports whether an absolute path is contained by one of the
// authorized roots. It is a lexical containment check: a cache document can
// never legitimize a path outside the roots a caller actually authorized.
func withinRoots(path string, roots []RootSpec) bool {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || strings.IndexByte(clean, 0) >= 0 {
		return false
	}
	for _, root := range roots {
		relative, err := filepath.Rel(root.Path, clean)
		if err != nil {
			continue
		}
		if relative == "." {
			return true
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

// DefaultRoots returns the explicit default policy: the project root takes
// precedence over the global root so a project override wins deterministically.
func DefaultRoots(workspace, home string) []RootSpec {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	roots := make([]RootSpec, 0, 2)
	if workspace != "" {
		roots = append(roots, RootSpec{
			ID: "project", Path: filepath.Join(workspace, ".agents", "skills"),
			Scope: ScopeProject, Provenance: "project-convention",
		})
	}
	if home != "" {
		roots = append(roots, RootSpec{
			ID: "global", Path: filepath.Join(home, ".agents", "skills"),
			Scope: ScopeGlobal, Provenance: "global-convention",
		})
	}
	return roots
}

// resolveRoots validates and bounds the configured root set.
func (o Options) resolveRoots(workspace string) ([]RootSpec, error) {
	roots := o.Roots
	if roots == nil {
		roots = DefaultRoots(workspace, o.HomeDir)
	}
	if len(roots) == 0 {
		return nil, ErrInvalid
	}
	if len(roots) > o.maxRoots() {
		// Reject rather than silently truncating an over-broad policy.
		return nil, ErrInvalid
	}
	home := o.homePath()
	out := make([]RootSpec, 0, len(roots))
	seen := map[string]bool{}
	for index, root := range roots {
		if root.Path == "" || !filepath.IsAbs(root.Path) || strings.IndexByte(root.Path, 0) >= 0 {
			return nil, ErrInvalid
		}
		clean := filepath.Clean(root.Path)
		id := root.ID
		if id == "" {
			id = "root-" + strconv.Itoa(index)
		}
		if seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		scope := root.Scope
		if scope == "" {
			scope = ScopeConfigured
		}
		provenance := root.Provenance
		if provenance == "" {
			provenance = "configured"
		}
		prefix := ""
		switch scope {
		case ScopeProject:
			prefix = workspace
		case ScopeGlobal:
			prefix = home
		}
		out = append(out, RootSpec{ID: id, Path: clean, Scope: scope, Provenance: provenance, trustedPrefix: prefix})
	}
	return out, nil
}

// homePath resolves the canonical home used to bound a default global root.
func (o Options) homePath() string {
	home := o.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		return resolved
	}
	return filepath.Clean(home)
}

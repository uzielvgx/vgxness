package skillregistry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Service is the registry API. It holds no state: the authorized roots and
// their current bytes are the source of truth, and the on-disk cache is a
// derived, regenerable artifact. A cache-backed selection returned to a caller
// is always revalidated against source bytes; Load only reports cache
// presence/freshness status and does not re-read skill content.
type Service struct{}

func New() *Service { return &Service{} }

// Scan performs read-only discovery without touching the cache.
func (service *Service) Scan(ctx context.Context, options Options) (Registry, error) {
	return scan(ctx, options)
}

// prepare performs a bounded discovery, honors the test seam, and rechecks
// winner identities immediately before publication.
func (service *Service) prepare(ctx context.Context, options Options, path string) (Registry, error) {
	registry, err := scan(ctx, options)
	if err != nil {
		return Registry{}, err
	}
	if options.beforePublish != nil {
		if err := options.beforePublish(path); err != nil {
			return Registry{}, err
		}
	}
	recheck(ctx, options, &registry)
	registry.Complete = true
	return registry, nil
}

// Refresh discovers all roots, rechecks winner identities, and atomically
// publishes the cache with a fresh generation timestamp.
func (service *Service) Refresh(ctx context.Context, options Options) (Registry, error) {
	path, err := options.cachePath()
	if err != nil {
		return Registry{}, err
	}
	registry, err := service.prepare(ctx, options, path)
	if err != nil {
		return Registry{}, err
	}
	if err := publish(ctx, path, registry); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

// Load reads the cache without rebuilding it and without re-reading skill
// content. The boolean is a status flag, not a content guarantee: it is false
// when the cache is absent, malformed, from another schema, bound to a
// different workspace or root set, from the future, older than the freshness
// window, or references a path outside the currently authorized roots. Its
// freshness reflects only the timestamp, normalized workspace, and configured
// roots; Ensure, Search, and Resolve rescan the roots and are authoritative for
// current content. Callers must not present the status flag as content
// freshness.
func (service *Service) Load(ctx context.Context, options Options) (Registry, bool, error) {
	if err := ctx.Err(); err != nil {
		return Registry{}, false, err
	}
	path, err := options.cachePath()
	if err != nil {
		return Registry{}, false, err
	}
	registry, usable := loadCache(path, options)
	return registry, usable, nil
}

// Unlock removes a stale lock only when it is old, the recorded owner process
// is provably gone, and the lock identity is unchanged. It never removes an
// active lock and reports false without error when there is nothing to recover.
// On Windows the owner probe uses OpenProcess/GetExitCodeProcess and fails
// closed on access-denied or unknown results; legacy PID-only lock files remain
// recoverable.
func (service *Service) Unlock(ctx context.Context, options Options) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := options.cachePath()
	if err != nil {
		return false, err
	}
	dir := filepath.Dir(path)
	if err := rejectSymlink(dir); err != nil {
		return false, err
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return false, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false, nil
	}
	defer root.Close()
	// Manual Unlock takes the same persistent kernel guard as every publisher
	// before touching the inner lock, so recovery never overlaps a guarded
	// writer. Exhausting the shared guard wait is reported as ErrBusy rather
	// than a false no-op, so a wedged guard holder is never hidden. On
	// non-Windows the guard is a no-op and nothing is created.
	o := realOps()
	o.deadline = time.Now().Add(o.maxWait)
	guard, err := acquireKernelGuardWith(ctx, root, o)
	if err != nil {
		return false, err
	}
	defer guard.releaseWith(o)
	return recoverStaleLockBounded(ctx, root, "skill-registry.lock", o)
}

// Ensure rescans the authorized roots (bounded) and reuses a cache only when it
// still matches the discovered content, workspace, and root set. It therefore
// detects additions, removals, root changes, and hash changes within the
// freshness window; registry availability never depends on memory or any other
// subsystem success.
func (service *Service) Ensure(ctx context.Context, options Options) (Registry, error) {
	path, err := options.cachePath()
	if err != nil {
		return Registry{}, err
	}
	discovered, err := service.prepare(ctx, options, path)
	if err != nil {
		return Registry{}, err
	}
	cached, usable := loadCache(path, options)
	if usable && cached.Workspace == discovered.Workspace && cached.ContentDigest() == discovered.ContentDigest() && !future(cached.GeneratedAt, options) {
		return cached, nil
	}
	if err := publish(ctx, path, discovered); err != nil {
		return Registry{}, err
	}
	return discovered, nil
}

// Search returns at most limit matching entries with bounded metadata only. It
// retains discovery diagnostics and marks truncation rather than hiding it.
func (service *Service) Search(ctx context.Context, options Options, query string, limit int) (Registry, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" || len(needle) > 256 {
		return Registry{}, ErrInvalid
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	registry, err := service.Ensure(ctx, options)
	if err != nil {
		return Registry{}, err
	}
	matches := make([]Entry, 0, limit)
	complete := true
	for _, entry := range registry.Entries {
		if entry.Status != StatusOK && entry.Status != StatusDuplicate {
			continue
		}
		if strings.Contains(strings.ToLower(entry.Name+"\n"+entry.ID+"\n"+entry.Description), needle) {
			if len(matches) >= limit {
				complete = false
				break
			}
			matches = append(matches, entry)
		}
	}
	diagnostics := append([]Diagnostic{}, registry.Diagnostics...)
	if !complete {
		diagnostics = append(diagnostics, Diagnostic{Kind: DiagLimit, Detail: "search results truncated"})
	}
	return Registry{
		SchemaVersion: registry.SchemaVersion,
		GeneratedAt:   registry.GeneratedAt,
		Workspace:     registry.Workspace,
		Roots:         registry.Roots,
		Entries:       matches,
		Diagnostics:   diagnostics,
		Complete:      complete,
	}, nil
}

// Resolve returns exactly one usable entry by explicit ID or path, or by a
// unique name. A duplicated name is ErrAmbiguous, never a silent winner pick.
// The selected entry is revalidated against its source bytes before return.
func (service *Service) Resolve(ctx context.Context, options Options, id string) (Entry, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || len(trimmed) > 512 {
		return Entry{}, ErrInvalid
	}
	registry, err := service.Ensure(ctx, options)
	if err != nil {
		return Entry{}, err
	}
	workspace, err := options.workspacePath()
	if err != nil {
		return Entry{}, err
	}
	roots, err := options.resolveRoots(workspace)
	if err != nil {
		return Entry{}, err
	}
	byPath := ""
	if filepath.IsAbs(trimmed) {
		byPath = filepath.Clean(trimmed)
	}
	// A bare duplicated name is ambiguous and is evaluated before any ID match,
	// because the winner's ID is the name itself. Explicit path resolution still
	// works for duplicates.
	if byPath == "" {
		nameCount := 0
		for _, entry := range registry.Entries {
			if entry.Name == trimmed && (entry.Status == StatusOK || entry.Status == StatusDuplicate) {
				nameCount++
			}
		}
		if nameCount > 1 {
			return Entry{}, ErrAmbiguous
		}
	}
	for _, entry := range registry.Entries {
		if entry.Status != StatusOK && entry.Status != StatusDuplicate {
			continue
		}
		if entry.ID == trimmed || (byPath != "" && entry.Path == byPath) {
			return revalidate(entry, roots, options)
		}
	}
	return Entry{}, ErrNotFound
}

// revalidate confirms a selected entry is still inside the authorized roots and
// still matches its recorded manifest digest.
func revalidate(entry Entry, roots []RootSpec, options Options) (Entry, error) {
	if !withinRoots(entry.Path, roots) {
		return Entry{}, ErrStale
	}
	data, _, err := readRegularBounded(entry.Path, manifestName, options.maxManifestBytes())
	if err != nil || digest(data) != entry.SHA256 {
		return Entry{}, ErrStale
	}
	return entry, nil
}

// loadCache validates a cache document against the caller's current authorized
// roots. A structurally valid document that references paths outside those
// roots is rejected, so an edited or foreign cache cannot legitimize paths.
func loadCache(path string, options Options) (Registry, bool) {
	registry, ok := readCache(path)
	if !ok {
		return Registry{}, false
	}
	workspace, err := options.workspacePath()
	if err != nil {
		return registry, false
	}
	roots, err := options.resolveRoots(workspace)
	if err != nil {
		return registry, false
	}
	if registry.Workspace != workspace || !sameRootSet(registry.Roots, roots) {
		return registry, false
	}
	for _, entry := range registry.Entries {
		if entry.Path == "" || !withinRoots(entry.Path, roots) {
			return registry, false
		}
	}
	if !fresh(options, registry) {
		return registry, false
	}
	return registry, true
}

// sameRootSet requires the cached root set to match the configured roots by
// identity and order, so a refresh under different roots is never reused.
func sameRootSet(cached, configured []RootSpec) bool {
	if len(cached) != len(configured) {
		return false
	}
	for index := range cached {
		if cached[index].ID != configured[index].ID ||
			cached[index].Path != configured[index].Path ||
			cached[index].Scope != configured[index].Scope {
			return false
		}
	}
	return true
}

// Fresh reports whether a cached registry matches the requested workspace, is
// not from the future, and is within the configured age. It is a reporting
// helper; Ensure additionally compares discovered content.
func Fresh(options Options, registry Registry) bool {
	workspace, err := options.workspacePath()
	if err != nil {
		return false
	}
	if registry.Workspace != workspace {
		return false
	}
	return fresh(options, registry)
}

func fresh(options Options, registry Registry) bool {
	generated, err := time.Parse(time.RFC3339Nano, registry.GeneratedAt)
	if err != nil || generated.IsZero() {
		return false
	}
	now := options.timestamp()
	if generated.After(now) {
		return false
	}
	return now.Sub(generated) <= options.maxAge()
}

func future(generatedAt string, options Options) bool {
	generated, err := time.Parse(time.RFC3339Nano, generatedAt)
	if err != nil || generated.IsZero() {
		return true
	}
	return generated.After(options.timestamp())
}

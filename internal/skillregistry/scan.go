package skillregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const manifestName = "SKILL.md"

// scan performs a read-only discovery pass. It never writes and never mutates
// the filesystem; the returned Registry is a pure function of the roots.
func scan(ctx context.Context, options Options) (Registry, error) {
	workspace, err := options.workspacePath()
	if err != nil {
		return Registry{}, err
	}
	roots, err := options.resolveRoots(workspace)
	if err != nil {
		return Registry{}, err
	}
	registry := Registry{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   options.timestamp().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Workspace:     workspace,
		Roots:         make([]RootSpec, 0, len(roots)),
		Entries:       []Entry{},
		Diagnostics:   []Diagnostic{},
		Complete:      true,
	}
	winnerByName := map[string]string{}
	usedIDs := map[string]bool{}
	total := 0
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return Registry{}, err
		}
		scanned, entries, diagnostics := scanRoot(options, root, winnerByName, usedIDs, total)
		registry.Roots = append(registry.Roots, scanned)
		registry.Entries = append(registry.Entries, entries...)
		registry.Diagnostics = append(registry.Diagnostics, diagnostics...)
		total += len(entries)
	}
	sort.SliceStable(registry.Entries, func(i, j int) bool {
		if registry.Entries[i].Name != registry.Entries[j].Name {
			return registry.Entries[i].Name < registry.Entries[j].Name
		}
		return registry.Entries[i].Path < registry.Entries[j].Path
	})
	for index := range registry.Entries {
		if len(registry.Entries[index].Diagnostics) > maxEntryDiagnostics {
			registry.Entries[index].Diagnostics = registry.Entries[index].Diagnostics[:maxEntryDiagnostics]
		}
	}
	registry.Diagnostics = boundDiagnostics(registry.Diagnostics)
	return registry, nil
}

// boundDiagnostics keeps the serialized cache bounded: scalar details are
// clamped and the list is capped with an explicit truncation marker.
func boundDiagnostics(items []Diagnostic) []Diagnostic {
	if len(items) > maxDiagnostics {
		items = items[:maxDiagnostics]
		items = append(items, Diagnostic{Kind: DiagLimit, Detail: "diagnostics truncated"})
	}
	for index := range items {
		items[index].Detail = clampRunes(items[index].Detail, maxDiagnosticDetailBytes)
	}
	return items
}

// clampRunes bounds a metadata scalar without splitting a UTF-8 rune.
func clampRunes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func scanRoot(options Options, root RootSpec, winnerByName map[string]string, usedIDs map[string]bool, total int) (RootSpec, []Entry, []Diagnostic) {
	resolved := root
	diagnostics := []Diagnostic{}
	if err := rejectControlledSymlinks(root.trustedPrefix, root.Path); err != nil {
		diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagRootRejected, Detail: "root path traverses a controlled symlink"})
		resolved.Present = false
		return resolved, nil, diagnostics
	}
	info, err := os.Lstat(root.Path)
	if err != nil {
		if os.IsNotExist(err) {
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagMissingRoot, Detail: "root directory is absent"})
		} else {
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagUnreadable, Detail: "root directory is unreadable"})
		}
		resolved.Present = false
		return resolved, nil, diagnostics
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagRootRejected, Detail: "root must be a real directory"})
		resolved.Present = false
		return resolved, nil, diagnostics
	}
	resolved.Present = true

	items, err := os.ReadDir(root.Path)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagUnreadable, Detail: "root directory cannot be listed"})
		return resolved, nil, diagnostics
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })

	entries := []Entry{}
	count := 0
	for _, item := range items {
		if count >= options.maxEntriesPerRoot() || total+count >= options.maxEntriesTotal() {
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagLimit, Detail: "entry limit reached"})
			break
		}
		skillPath := filepath.Join(root.Path, item.Name())
		linkInfo, err := os.Lstat(skillPath)
		if err != nil {
			continue
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			entry := Entry{
				ID:   uniqueID("symlink:"+root.ID+"/"+item.Name(), usedIDs),
				Name: item.Name(), Path: skillPath, Status: StatusSymlink,
				Diagnostics: []string{"skill directory is a symlink"},
			}
			entries = append(entries, entry)
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagSymlink, Detail: item.Name() + " is a symlink"})
			count++
			continue
		}
		if !linkInfo.IsDir() {
			continue
		}
		data, _, readErr := readRegularBounded(skillPath, manifestName, options.maxManifestBytes())
		switch {
		case errors.Is(readErr, fs.ErrNotExist):
			// A directory without SKILL.md is not a candidate; do not invent one.
			continue
		case errors.Is(readErr, errReadSymlink):
			entry := Entry{
				ID:   uniqueID("symlink:"+root.ID+"/"+item.Name(), usedIDs),
				Name: item.Name(), Path: skillPath, Status: StatusSymlink,
				Diagnostics: []string{"SKILL.md is a symlink"},
			}
			entries = append(entries, entry)
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagSymlink, Detail: item.Name() + "/SKILL.md is a symlink"})
			count++
			continue
		case errors.Is(readErr, errReadOversized):
			entry := Entry{
				ID:   uniqueID("oversized:"+root.ID+"/"+item.Name(), usedIDs),
				Name: item.Name(), Path: skillPath, Status: StatusOversized,
				Diagnostics: []string{"SKILL.md exceeds the manifest byte budget"},
			}
			entries = append(entries, entry)
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagOversized, Detail: item.Name() + "/SKILL.md exceeds budget"})
			count++
			continue
		case readErr != nil:
			entry := Entry{
				ID:   uniqueID("unreadable:"+root.ID+"/"+item.Name(), usedIDs),
				Name: item.Name(), Path: skillPath, Status: StatusUnreadable,
				Diagnostics: []string{"SKILL.md is unreadable"},
			}
			entries = append(entries, entry)
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagUnreadable, Detail: item.Name() + "/SKILL.md is unreadable"})
			count++
			continue
		}
		name, description, compatibility, ok := parseFrontmatter(data)
		entry := Entry{
			Name:          name,
			Description:   description,
			Path:          skillPath,
			SHA256:        digest(data),
			Compatibility: compatibility,
		}
		switch {
		case !ok || !validEntryName(name):
			entry.Name = strings.TrimSpace(name)
			entry.Status = StatusInvalid
			entry.ID = uniqueID("invalid:"+root.ID+"/"+item.Name(), usedIDs)
			entry.Diagnostics = []string{"SKILL.md frontmatter has no usable name"}
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagInvalid, Detail: item.Name() + " has no usable name"})
		case winnerByName[name] == "":
			entry.ID = uniqueID(name, usedIDs)
			entry.Status = StatusOK
			winnerByName[name] = entry.ID
		default:
			entry.ID = uniqueID(name+"@"+root.ID, usedIDs)
			entry.Status = StatusDuplicate
			entry.DuplicateOf = winnerByName[name]
			entry.Diagnostics = []string{"duplicate name; first root owns resolution"}
			diagnostics = append(diagnostics, Diagnostic{Root: root.ID, Kind: DiagDuplicate, Detail: name + " duplicates " + winnerByName[name]})
		}
		entries = append(entries, entry)
		count++
	}
	return resolved, entries, diagnostics
}

// recheck verifies winner identities immediately before publication so a
// concurrent edit cannot be published as fresh. Mismatches are downgraded.
func recheck(ctx context.Context, options Options, registry *Registry) {
	workspace, workspaceErr := options.workspacePath()
	roots, rootsErr := options.resolveRoots(workspace)
	for index := range registry.Entries {
		if err := ctx.Err(); err != nil {
			return
		}
		entry := &registry.Entries[index]
		if entry.Status != StatusOK || entry.SHA256 == "" {
			continue
		}
		if workspaceErr != nil || rootsErr != nil || !withinRoots(entry.Path, roots) {
			entry.Status = StatusChanged
			entry.Diagnostics = append(entry.Diagnostics, "entry is outside the authorized roots")
			registry.Diagnostics = append(registry.Diagnostics, Diagnostic{Root: "", Kind: DiagChanged, Detail: entry.Name + " escaped the authorized roots"})
			continue
		}
		data, _, readErr := readRegularBounded(entry.Path, manifestName, options.maxManifestBytes())
		if readErr == nil && digest(data) == entry.SHA256 {
			continue
		}
		entry.Status = StatusChanged
		entry.Diagnostics = append(entry.Diagnostics, "identity changed before publication")
		registry.Diagnostics = append(registry.Diagnostics, Diagnostic{Root: "", Kind: DiagChanged, Detail: entry.Name + " changed before publication"})
	}
}

// parseFrontmatter extracts only declared top-level metadata. Unquoted and
// simply quoted scalars are accepted; block scalars are treated as absent
// rather than guessed.
func parseFrontmatter(data []byte) (name, description, compatibility string, ok bool) {
	lines := strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", "", "", false
	}
	limit := len(lines)
	if limit > maxFrontmatterLines {
		limit = maxFrontmatterLines
	}
	for index := 1; index < limit; index++ {
		line := strings.TrimRight(lines[index], "\r")
		if line == "---" {
			return name, description, compatibility, true
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
			continue
		}
		value = clampRunes(strings.Trim(value, `"'`), maxDescriptionBytes)
		switch strings.TrimSpace(key) {
		case "name":
			if name == "" {
				name = value
			}
		case "description":
			if description == "" {
				description = value
			}
		case "compatibility":
			if compatibility == "" {
				compatibility = value
			}
		}
	}
	return "", "", "", false
}

func validEntryName(name string) bool {
	if name == "" || len(name) > maxNameBytes {
		return false
	}
	for index, character := range name {
		if character < 0x20 || character == 0x7f {
			return false
		}
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '_', character == '.':
		default:
			return false
		}
		if index == 0 && !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// uniqueID returns a stable, collision-free entry ID within one scan.
func uniqueID(base string, used map[string]bool) string {
	if base == "" {
		base = "skill"
	}
	candidate := base
	for suffix := 2; used[candidate]; suffix++ {
		candidate = base + "#" + strconv.Itoa(suffix)
	}
	used[candidate] = true
	return candidate
}

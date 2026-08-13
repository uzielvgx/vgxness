package skillregistry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const Version = 2
const maxSkillBytes = 512 << 10

var (
	ErrAmbiguous   = errors.New("skill selection is ambiguous")
	ErrNotFound    = errors.New("skill not found")
	ErrDrift       = errors.New("skill binding drifted")
	readSkillFile  = func(f *os.File) ([]byte, error) { return io.ReadAll(io.LimitReader(f, maxSkillBytes+1)) }
	afterSkillOpen = func() {}
	lstat          = os.Lstat
	cacheMu        sync.Mutex
	trustedCaches  = map[string]trustedCache{}
)

type trustedCache struct {
	bytes []byte
	cache cache
	files map[string]os.FileInfo
}
type Candidate struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	LogicalPath   string `json:"logicalPath"`
	CanonicalPath string `json:"canonicalPath"`
	BaseDir       string `json:"baseDir"`
	Scope         string `json:"scope"`
	Source        string `json:"source"`
	SHA256        string `json:"sha256"`
	Symlink       bool   `json:"symlink"`
	Rank          int    `json:"rank"`
	Size          int64  `json:"size"`
	ModTime       int64  `json:"modTime"`
	identity      os.FileInfo
}
type Binding struct {
	Schema         string `json:"schema"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	LogicalPath    string `json:"logicalPath"`
	CanonicalPath  string `json:"canonicalPath"`
	BaseDir        string `json:"baseDir"`
	Scope          string `json:"scope"`
	Source         string `json:"source"`
	SnapshotDigest string `json:"snapshotDigest"`
	LoadMode       string `json:"loadMode"`
	SHA256         string `json:"sha256"`
}
type Selection struct {
	Binding    Binding     `json:"binding"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Ambiguous  bool        `json:"ambiguous"`
}
type Snapshot struct {
	Version    int          `json:"version"`
	Host       string       `json:"host"`
	Workspace  string       `json:"workspace"`
	Roots      []RootStatus `json:"roots"`
	Status     string       `json:"status"`
	Candidates []Candidate  `json:"candidates"`
	Digest     string       `json:"digest"`
	FromCache  bool         `json:"fromCache"`
}
type RootStatus struct {
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	Source string `json:"source"`
	Status string `json:"status"`
}
type Options struct {
	CWD       string
	RepoRoot  string
	Home      string
	Host      string
	CachePath string
}
type root struct {
	path, scope, source string
	rank                int
}
type cache struct {
	Roots    []rootState `json:"roots"`
	Snapshot Snapshot    `json:"snapshot"`
}
type rootState struct {
	Path  string `json:"path"`
	Stamp string `json:"stamp"`
}

func Scan(ctx context.Context, options Options) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	options.Host = normalizedHost(options.Host)
	roots := roots(options)
	cached, valid := loadCache(options.CachePath, roots, options)
	if valid {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		cached.FromCache = true
		return cached, nil
	}
	previous := map[string]Candidate{}
	if cached.Version == Version {
		for _, c := range cached.Candidates {
			previous[c.LogicalPath] = c
		}
	}
	var candidates []Candidate
	statuses := make([]RootStatus, 0, len(roots))
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		found, status, err := scanRoot(ctx, root, previous)
		if err != nil {
			return Snapshot{}, err
		}
		statuses = append(statuses, status)
		candidates = append(candidates, found...)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		return a.LogicalPath < b.LogicalPath
	})
	snapshot := Snapshot{Version: Version, Host: options.Host, Workspace: filepath.Clean(options.CWD), Roots: statuses, Status: "ready", Candidates: candidates}
	for _, s := range statuses {
		if s.Status == "inaccessible" {
			snapshot.Status = "partial"
		}
	}
	snapshot.Digest = digest(candidates)
	if ctx.Err() == nil {
		storeCache(ctx, options.CachePath, roots, snapshot)
	}
	return snapshot, nil
}
func (snapshot Snapshot) Resolve(name string) (Selection, error) {
	var matches []Candidate
	rank := -1
	for _, c := range snapshot.Candidates {
		if c.Name == name && (rank < 0 || c.Rank == rank) {
			if rank < 0 {
				rank = c.Rank
			}
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return Selection{}, ErrNotFound
	}
	if len(matches) != 1 {
		return Selection{Candidates: matches, Ambiguous: true}, ErrAmbiguous
	}
	return snapshot.selection(matches[0]), nil
}
func (snapshot Snapshot) ResolvePath(path string) (Selection, error) {
	for _, c := range snapshot.Candidates {
		if filepath.Clean(path) == c.LogicalPath {
			return snapshot.selection(c), nil
		}
	}
	return Selection{}, ErrNotFound
}
func (snapshot Snapshot) selection(c Candidate) Selection {
	return Selection{Binding: Binding{Schema: "vgxness.skill-binding/v1", Name: c.Name, Description: c.Description, LogicalPath: c.LogicalPath, CanonicalPath: c.CanonicalPath, BaseDir: c.BaseDir, Scope: c.Scope, Source: c.Source, SnapshotDigest: snapshot.Digest, LoadMode: "exact-path", SHA256: c.SHA256}}
}
func (snapshot Snapshot) Verify(binding Binding) error {
	if binding.Schema != "vgxness.skill-binding/v1" || binding.LoadMode != "exact-path" || binding.SnapshotDigest != snapshot.Digest {
		return ErrDrift
	}
	matched := false
	for _, c := range snapshot.Candidates {
		if c.Name == binding.Name && c.Description == binding.Description && c.LogicalPath == binding.LogicalPath && c.CanonicalPath == binding.CanonicalPath && c.BaseDir == binding.BaseDir && c.Scope == binding.Scope && c.Source == binding.Source && c.SHA256 == binding.SHA256 {
			matched = true
			break
		}
	}
	if !matched {
		return ErrDrift
	}
	canonical, err := filepath.EvalSymlinks(binding.LogicalPath)
	info, statErr := os.Stat(canonical)
	if err != nil || statErr != nil || !info.Mode().IsRegular() || canonical != binding.CanonicalPath || !inside(binding.BaseDir, canonical) {
		return ErrDrift
	}
	contents, err := readSkill(binding.LogicalPath, binding.CanonicalPath)
	if err != nil || hash(contents) != binding.SHA256 {
		return ErrDrift
	}
	return nil
}
func roots(options Options) []root {
	host := normalizedHost(options.Host)
	repo := options.RepoRoot
	if repo == "" {
		repo = discoverRoot(options.CWD)
	}
	var out []root
	seen := map[string]bool{}
	add := func(path, scope, source string, rank int) {
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			out = append(out, root{path, scope, source, rank})
		}
	}
	for current, rank := filepath.Clean(options.CWD), 0; current != ""; current, rank = filepath.Dir(current), rank+1 {
		add(filepath.Join(current, ".agents", "skills"), "project", "common", rank)
		if host == "opencode" {
			add(filepath.Join(current, ".opencode", "skills"), "project", "opencode", rank)
			add(filepath.Join(current, ".claude", "skills"), "project", "opencode", rank)
		}
		if current == filepath.Clean(repo) || filepath.Dir(current) == current {
			break
		}
	}
	homeRank := 1 << 20
	add(filepath.Join(options.Home, ".agents", "skills"), "home", "common", homeRank)
	if host == "opencode" {
		add(filepath.Join(options.Home, ".config", "opencode", "skills"), "home", "opencode", homeRank)
		add(filepath.Join(options.Home, ".claude", "skills"), "home", "opencode", homeRank)
	}
	if host == "codex" {
		add(filepath.Join(string(filepath.Separator), "etc", "codex", "skills"), "system", "codex", homeRank+1)
	}
	return out
}
func normalizedHost(host string) string {
	host = strings.ToLower(host)
	if host == "codex" || host == "opencode" {
		return host
	}
	return "common"
}
func discoverRoot(cwd string) string {
	for current := filepath.Clean(cwd); current != ""; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return filepath.Clean(cwd)
}
func scanRoot(ctx context.Context, root root, previous map[string]Candidate) ([]Candidate, RootStatus, error) {
	status := RootStatus{Path: root.path, Scope: root.scope, Source: root.source, Status: "scanned"}
	base, err := filepath.EvalSymlinks(root.path)
	if err != nil {
		if os.IsNotExist(err) {
			status.Status = "missing"
		} else {
			status.Status = "inaccessible"
		}
		return nil, status, nil
	}
	var out []Candidate
	entries, err := os.ReadDir(root.path)
	if err != nil {
		status.Status = "inaccessible"
		return nil, status, nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, status, err
		}
		path := filepath.Join(root.path, entry.Name(), "SKILL.md")
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil || !inside(base, canonical) {
			continue
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSkillBytes {
			continue
		}
		skillDir := filepath.Dir(canonical)
		linfo, err := os.Lstat(path)
		if err != nil {
			continue
		}
		c, reused := previous[path]
		if !reused || c.CanonicalPath != canonical || c.Size != info.Size() || c.ModTime != info.ModTime().UnixNano() {
			if err := ctx.Err(); err != nil {
				return nil, status, err
			}
			body, identity, err := readSkillStable(path, canonical)
			if err != nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, status, err
			}
			name, description := frontmatter(body, filepath.Base(filepath.Dir(path)))
			c = Candidate{Name: name, Description: description, SHA256: hash(body), identity: identity}
		}
		c.LogicalPath, c.CanonicalPath, c.BaseDir, c.Scope, c.Source, c.Rank, c.Size, c.ModTime, c.Symlink = path, canonical, skillDir, root.scope, root.source, root.rank, info.Size(), info.ModTime().UnixNano(), linfo.Mode()&os.ModeSymlink != 0 || filepath.Clean(filepath.Dir(path)) != skillDir
		if c.identity == nil {
			c.identity = info
		}
		out = append(out, c)
	}
	return out, status, nil
}
func frontmatter(body []byte, fallback string) (string, string) {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fallback, ""
	}
	name, description := "", ""
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			break
		}
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		}
		if strings.HasPrefix(line, "description:") {
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"'")
			folded := description == ">" || description == "|"
			if folded {
				description = ""
				for i++; i < len(lines) && strings.HasPrefix(lines[i], " "); i++ {
					description += " " + strings.TrimSpace(lines[i])
				}
				i--
			}
		}
	}
	if name == "" {
		name = fallback
	}
	return name, strings.TrimSpace(description)
}

// This detects common replacement races, but cannot provide atomic cross-platform path identity.
func readSkill(logical, expected string) ([]byte, error) {
	body, _, err := readSkillStable(logical, expected)
	return body, err
}
func readSkillStable(logical, expected string) ([]byte, os.FileInfo, error) {
	f, err := os.Open(expected)
	if err != nil {
		return nil, nil, ErrDrift
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSkillBytes {
		return nil, nil, ErrDrift
	}
	afterSkillOpen()
	current, err := filepath.EvalSymlinks(logical)
	if err != nil || current != expected {
		return nil, nil, ErrDrift
	}
	now, err := os.Stat(current)
	if err != nil || !os.SameFile(info, now) {
		return nil, nil, ErrDrift
	}
	body, err := readSkillFile(f)
	if err != nil || len(body) > maxSkillBytes {
		return nil, nil, ErrDrift
	}
	end, err := f.Stat()
	if err != nil || !os.SameFile(info, end) || end.Size() != info.Size() || end.ModTime() != info.ModTime() {
		return nil, nil, ErrDrift
	}
	current, err = filepath.EvalSymlinks(logical)
	if err != nil || current != expected {
		return nil, nil, ErrDrift
	}
	currentInfo, err := os.Stat(current)
	if err != nil || !os.SameFile(info, currentInfo) {
		return nil, nil, ErrDrift
	}
	return body, info, nil
}
func inside(base, path string) bool {
	relative, err := filepath.Rel(base, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func hash(value []byte) string             { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func CacheKey(workspace string) string     { return hash([]byte(filepath.Clean(workspace)))[:16] }
func digest(candidates []Candidate) string { b, _ := json.Marshal(candidates); return hash(b) }
func loadCache(path string, roots []root, options Options) (Snapshot, bool) {
	if path == "" {
		return Snapshot{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, false
	}
	cacheMu.Lock()
	t, ok := trustedCaches[path]
	cacheMu.Unlock()
	if !ok || !bytes.Equal(b, t.bytes) {
		return Snapshot{}, false
	}
	c := t.cache
	if c.Snapshot.Version != Version || c.Snapshot.Host != options.Host || c.Snapshot.Workspace != filepath.Clean(options.CWD) || c.Snapshot.Digest != digest(c.Snapshot.Candidates) {
		return Snapshot{}, false
	}
	kept := make([]Candidate, 0, len(c.Snapshot.Candidates))
	stale := false
	for _, candidate := range c.Snapshot.Candidates {
		if !cacheCandidate(candidate, roots) {
			return Snapshot{}, false
		}
		info, err := os.Stat(candidate.CanonicalPath)
		if err != nil || t.files[candidate.CanonicalPath] == nil || !os.SameFile(t.files[candidate.CanonicalPath], info) || info.Size() != candidate.Size || info.ModTime().UnixNano() != candidate.ModTime {
			stale = true
			continue
		}
		candidate.identity = t.files[candidate.CanonicalPath]
		kept = append(kept, candidate)
	}
	c.Snapshot.Candidates = kept
	if stale {
		return c.Snapshot, false
	}
	states := rootStates(roots)
	if len(states) != len(c.Roots) {
		return c.Snapshot, false
	}
	for i := range states {
		if states[i] != c.Roots[i] {
			return c.Snapshot, false
		}
	}
	return c.Snapshot, true
}
func cacheCandidate(c Candidate, roots []root) bool {
	for _, r := range roots {
		base, err := filepath.EvalSymlinks(r.path)
		canonical, statErr := filepath.EvalSymlinks(c.LogicalPath)
		info, infoErr := os.Stat(canonical)
		if err == nil && statErr == nil && infoErr == nil && info.Mode().IsRegular() && info.Size() <= maxSkillBytes && canonical == c.CanonicalPath && c.Scope == r.scope && c.Source == r.source && c.Rank == r.rank && inside(base, canonical) && c.BaseDir == filepath.Dir(canonical) && filepath.Clean(c.LogicalPath) == filepath.Join(r.path, filepath.Base(filepath.Dir(c.LogicalPath)), "SKILL.md") {
			return true
		}
	}
	return false
}
func rootStates(roots []root) []rootState {
	states := make([]rootState, len(roots))
	for i, r := range roots {
		states[i] = rootState{Path: r.path, Stamp: rootStamp(r.path)}
	}
	return states
}
func rootStamp(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "missing"
	}
	h := sha256.New()
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		i, ie := lstat(p)
		if ie != nil {
			fmt.Fprintf(h, "%s|lstat-error|", e.Name())
			continue
		}
		target, te := filepath.EvalSymlinks(p)
		s, se := os.Stat(filepath.Join(p, "SKILL.md"))
		fmt.Fprintf(h, "%s|%t|%v|", e.Name(), i.IsDir(), i.Mode())
		if te != nil {
			fmt.Fprint(h, "target-error|")
		} else {
			fmt.Fprintf(h, "%s|", target)
		}
		if se == nil {
			fmt.Fprintf(h, "%d|%d|%v", s.Size(), s.ModTime().UnixNano(), s.Mode())
		} else {
			fmt.Fprint(h, "skill-error")
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
func storeCache(ctx context.Context, path string, roots []root, snapshot Snapshot) {
	if path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	b, err := json.Marshal(cache{Roots: rootStates(roots), Snapshot: snapshot})
	if err != nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	files := map[string]os.FileInfo{}
	for _, v := range snapshot.Candidates {
		if v.identity == nil {
			return
		}
		i, e := os.Stat(v.CanonicalPath)
		if e != nil || !os.SameFile(v.identity, i) {
			return
		}
		files[v.CanonicalPath] = v.identity
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".skill-registry-")
	if err != nil {
		return
	}
	name := temp.Name()
	if _, err = temp.Write(b); err == nil {
		err = temp.Chmod(0600)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil || ctx.Err() != nil {
		_ = os.Remove(name)
		return
	}
	if err = os.Rename(name, path); err != nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	var c cache
	_ = json.Unmarshal(b, &c)
	cacheMu.Lock()
	trustedCaches[path] = trustedCache{bytes: append([]byte(nil), b...), cache: c, files: files}
	cacheMu.Unlock()
}

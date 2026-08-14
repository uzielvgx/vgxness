package skillregistry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func skill(t *testing.T, p, b string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(b), 0644); e != nil {
		t.Fatal(e)
	}
}
func scan(t *testing.T, o Options) Snapshot {
	t.Helper()
	s, e := Scan(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestBoundedMetadataAndPrecedence(t *testing.T) {
	r := t.TempDir()
	p := filepath.Join(r, ".agents/skills/one/SKILL.md")
	skill(t, p, "---\r\nname: same\r\ndescription: >\r\n folded\r\n text\r\n---")
	skill(t, filepath.Join(r, ".agents/skills/one/nested/SKILL.md"), "nested")
	skill(t, filepath.Join(r, "home/.agents/skills/same/SKILL.md"), "home")
	s := scan(t, Options{CWD: r, Home: filepath.Join(r, "home"), CachePath: filepath.Join(r, "c")})
	x, e := s.Resolve("same")
	if e != nil || x.Binding.LogicalPath != p || s.Candidates[0].BaseDir != filepath.Dir(s.Candidates[0].CanonicalPath) || s.Candidates[0].Description != "folded text" {
		t.Fatal(s, e)
	}
}
func TestSameRankAndEscapingSymlink(t *testing.T) {
	r, out := t.TempDir(), t.TempDir()
	skill(t, filepath.Join(r, ".agents/skills/a/SKILL.md"), "---\nname: same\n---")
	skill(t, filepath.Join(r, ".opencode/skills/b/SKILL.md"), "---\nname: same\n---")
	if e := os.Symlink(out, filepath.Join(r, ".agents/skills/escape")); e != nil {
		t.Skip(e)
	}
	s := scan(t, Options{CWD: r, Host: "opencode", Home: filepath.Join(r, "h"), CachePath: filepath.Join(r, "c")})
	if _, e := s.Resolve("same"); e != ErrAmbiguous {
		t.Fatal(e)
	}
	for _, c := range s.Candidates {
		if c.Name == "escape" {
			t.Fatal("escape")
		}
	}
}
func TestCacheBodyReads(t *testing.T) {
	r := t.TempDir()
	for _, n := range []string{"one", "two"} {
		skill(t, filepath.Join(r, ".agents/skills", n, "SKILL.md"), n)
	}
	old, n := readSkillFile, 0
	readSkillFile = func(f *os.File) ([]byte, error) { n++; return old(f) }
	t.Cleanup(func() { readSkillFile = old })
	o := Options{CWD: r, Home: filepath.Join(r, "h"), CachePath: filepath.Join(r, "c")}
	scan(t, o)
	if n != 2 {
		t.Fatal(n)
	}
	n = 0
	scan(t, o)
	if n != 0 {
		t.Fatal(n)
	}
	skill(t, filepath.Join(r, ".agents/skills/one/SKILL.md"), "x")
	n = 0
	s := scan(t, o)
	if n != 1 {
		t.Fatal(n)
	}
	x, e := s.Resolve("one")
	if e != nil {
		t.Fatal(e)
	}
	f := x.Binding
	f.Name = "forged"
	if s.Verify(f) == nil {
		t.Fatal("forged")
	}
	n = 0
	if s.Verify(x.Binding) != nil || n != 1 {
		t.Fatal(n)
	}
}
func TestRejectsNonRegularAndReplacement(t *testing.T) {
	r := t.TempDir()
	p := filepath.Join(r, ".agents/skills/x/SKILL.md")
	skill(t, p, "x")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(r, p); err != nil {
		t.Skip(err)
	}
	if len(scan(t, Options{CWD: r, CachePath: filepath.Join(r, "c")}).Candidates) != 0 {
		t.Fatal("nonregular")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	skill(t, p, "x")
	replacement := p + ".new"
	skill(t, replacement, "y")
	old := statSkill
	statSkill = func(string) (os.FileInfo, error) { return os.Stat(replacement) }
	t.Cleanup(func() { statSkill = old })
	if _, err := readSkill(p, mustReal(t, p)); !errors.Is(err, ErrDrift) {
		t.Fatal("replacement")
	}
}
func mustReal(t *testing.T, p string) string {
	t.Helper()
	v, e := filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestRejectsForgedCache(t *testing.T) {
	r := t.TempDir()
	p := filepath.Join(r, ".agents/skills/x/SKILL.md")
	skill(t, p, "---\nname: real\n---")
	o := Options{CWD: r, CachePath: filepath.Join(r, "c")}
	s := scan(t, o)
	c := cache{Roots: rootStates(roots(o)), Snapshot: s}
	c.Snapshot.Candidates[0].Name = "forged"
	c.Snapshot.Digest = digest(c.Snapshot.Candidates)
	b, _ := json.Marshal(c)
	if err := os.WriteFile(o.CachePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if got := scan(t, o); got.Candidates[0].Name != "real" {
		t.Fatal(got)
	}
	c.Snapshot = s
	c.Snapshot.Candidates[0].LogicalPath = filepath.Join(r, "outside")
	c.Snapshot.Candidates[0].CanonicalPath = c.Snapshot.Candidates[0].LogicalPath
	c.Snapshot.Digest = digest(c.Snapshot.Candidates)
	b, _ = json.Marshal(c)
	_ = os.WriteFile(o.CachePath, b, 0600)
	if got := scan(t, o); len(got.Candidates) != 1 || got.Candidates[0].Name != "real" {
		t.Fatal(got)
	}
}
func TestRootStampInvalidatesAddRemove(t *testing.T) {
	r := t.TempDir()
	o := Options{CWD: r, CachePath: filepath.Join(r, "c")}
	skill(t, filepath.Join(r, ".agents/skills/a/SKILL.md"), "a")
	if !scan(t, o).FromCache {
		if !scan(t, o).FromCache {
			t.Fatal("cache")
		}
	}
	skill(t, filepath.Join(r, ".agents/skills/b/SKILL.md"), "b")
	if scan(t, o).FromCache {
		t.Fatal("add")
	}
	if err := os.RemoveAll(filepath.Join(r, ".agents/skills/b")); err != nil {
		t.Fatal(err)
	}
	if scan(t, o).FromCache {
		t.Fatal("remove")
	}
}
func TestTrustedCacheIdentityAndCancellation(t *testing.T) {
	r := t.TempDir()
	p := filepath.Join(r, ".agents/skills/a/SKILL.md")
	skill(t, p, "---\nname: old\n---")
	o := Options{CWD: r, CachePath: filepath.Join(r, "c")}
	scan(t, o)
	skill(t, p, "---\nname: new\n---")
	if scan(t, o).Candidates[0].Name != "new" {
		t.Fatal("identity")
	}
	b, _ := os.ReadFile(o.CachePath)
	_ = os.WriteFile(o.CachePath, append(b, 'x'), 0600)
	if scan(t, o).FromCache {
		t.Fatal("tamper")
	}
	old := lstat
	lstat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	_ = rootStamp(filepath.Join(r, ".agents/skills"))
	lstat = old
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Scan(ctx, Options{CWD: r, CachePath: filepath.Join(r, "cancel")}); e == nil {
		t.Fatal("cancel")
	}
	if _, e := os.Stat(filepath.Join(r, "cancel")); !os.IsNotExist(e) {
		t.Fatal("wrote")
	}
}
func TestCacheTrustReplacementAndCancelRead(t *testing.T) {
	r := t.TempDir()
	p := filepath.Join(r, ".agents/skills/a/SKILL.md")
	skill(t, p, "---\nname: aaa\n---")
	o := Options{CWD: r, CachePath: filepath.Join(r, "c")}
	scan(t, o)
	old := readSkillFile
	n := 0
	readSkillFile = func(f *os.File) ([]byte, error) { n++; return old(f) }
	t.Cleanup(func() { readSkillFile = old })
	cacheMu.Lock()
	delete(trustedCaches, o.CachePath)
	cacheMu.Unlock()
	b, _ := os.ReadFile(o.CachePath)
	var c cache
	_ = json.Unmarshal(b, &c)
	c.Snapshot.Candidates[0].Name = "bad"
	c.Snapshot.Digest = digest(c.Snapshot.Candidates)
	b, _ = json.Marshal(c)
	_ = os.WriteFile(o.CachePath, b, 0600)
	if s := scan(t, o); n != 1 || s.Candidates[0].Name != "aaa" {
		t.Fatal(n, s)
	}
	scan(t, o)
	q := p + ".new"
	skill(t, q, "---\nname: bbb\n---")
	i, _ := os.Stat(p)
	_ = os.Chtimes(q, i.ModTime(), i.ModTime())
	_ = os.Rename(q, p)
	n = 0
	if s := scan(t, o); n != 1 || s.Candidates[0].Name != "bbb" {
		t.Fatal(n, s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	readSkillFile = func(f *os.File) ([]byte, error) { cancel(); return old(f) }
	o.CachePath = filepath.Join(r, "cancel")
	if _, e := Scan(ctx, o); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := os.Stat(o.CachePath); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

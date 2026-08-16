package memory

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

func TestProjectIDMarker_StrictAndIdempotent(t *testing.T) {
	workspace := t.TempDir()
	id, created, err := InitializeProjectID(workspace)
	testutil.Require(t, err == nil && created && id != "", "initial marker=%q created=%t err=%v", id, created, err)
	got, present, err := ReadProjectID(workspace)
	testutil.Require(t, err == nil && present && got == id, "read marker=%q present=%t err=%v", got, present, err)

	malformed := t.TempDir()
	testutil.NoError(t, os.Mkdir(filepath.Join(malformed, ".vgxness"), 0o700))
	testutil.NoError(t, os.WriteFile(filepath.Join(malformed, ".vgxness", "project-id"), []byte("550e8400-e29b-41d4-a716-446655440000\n"), 0o600))
	_, _, err = ReadProjectID(malformed)
	testutil.Require(t, errors.Is(err, ErrInvalid), "raw marker err=%v", err)
	testutil.NoError(t, os.WriteFile(filepath.Join(malformed, ".vgxness", "project-id"), []byte(`{"format":"vgxness-project-id/v1","format":"vgxness-project-id/v1","kind":"project","project_id":"550e8400-e29b-41d4-a716-446655440000"}`), 0o600))
	_, _, err = ReadProjectID(malformed)
	testutil.Require(t, errors.Is(err, ErrInvalid), "duplicate marker err=%v", err)

	link := t.TempDir()
	testutil.NoError(t, os.Mkdir(filepath.Join(link, ".vgxness"), 0o700))
	if err := os.Symlink(filepath.Join(malformed, ".vgxness", "project-id"), filepath.Join(link, ".vgxness", "project-id")); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	_, _, err = ReadProjectID(link)
	testutil.Require(t, errors.Is(err, ErrInvalid), "symlink marker err=%v", err)
}
func TestInitializeProjectID_FaultsNeverAcceptOrOverwriteInvalidFinal(t *testing.T) {
	original := projectMarkerFS
	defer func() { projectMarkerFS = original }()
	for _, stage := range []string{"write", "file-sync", "close", "publish", "marker-open", "marker-sync", "marker-close", "workspace-open", "workspace-sync", "workspace-close"} {
		t.Run(stage, func(t *testing.T) {
			if runtime.GOOS == "windows" && (strings.HasPrefix(stage, "marker-") || strings.HasPrefix(stage, "workspace-")) {
				t.Skip("Windows directory sync is explicitly unsupported")
			}
			projectMarkerFS = original
			workspace := t.TempDir()
			fail := errors.New(stage)
			switch stage {
			case "write", "file-sync", "close":
				projectMarkerFS.createTemp = func(dir, pattern string) (markerTempFile, error) {
					file, err := os.CreateTemp(dir, pattern)
					return markerTestTemp{File: file, stage: stage, err: fail}, err
				}
			case "publish":
				projectMarkerFS.link = func(string, string) error { return fail }
			case "marker-open", "workspace-open":
				projectMarkerFS.openDir = func(path string) (markerDirFile, error) {
					if strings.HasPrefix(stage, "marker-") == (filepath.Base(path) == ".vgxness") {
						return nil, fail
					}
					return os.Open(path)
				}
			case "marker-sync", "marker-close", "workspace-sync", "workspace-close":
				projectMarkerFS.openDir = func(path string) (markerDirFile, error) {
					file, err := os.Open(path)
					if strings.HasPrefix(stage, "marker-") != (filepath.Base(path) == ".vgxness") {
						return file, err
					}
					return markerTestDir{File: file, stage: strings.TrimPrefix(stage, "workspace-"), err: fail}, err
				}
			}
			_, _, err := InitializeProjectID(workspace)
			testutil.Require(t, errors.Is(err, ErrInvalid), "InitializeProjectID() error=%v", err)
			id, present, readErr := ReadProjectID(workspace)
			if strings.HasPrefix(stage, "marker-") {
				testutil.Require(t, readErr == nil && present && id != "", "published final was invalid: id=%q present=%t err=%v", id, present, readErr)
			} else {
				testutil.Require(t, readErr == nil && !present, "invalid final accepted: id=%q present=%t err=%v", id, present, readErr)
			}
			if strings.HasPrefix(stage, "workspace-") {
				var syncs int
				projectMarkerFS.openDir = func(path string) (markerDirFile, error) {
					file, err := os.Open(path)
					return markerTestDir{File: file, sync: map[bool]*int{true: &syncs}[path == workspace]}, err
				}
				id, created, retryErr := InitializeProjectID(workspace)
				testutil.Require(t, retryErr == nil && created && id != "" && syncs == 1, "retry created=%t id=%q syncs=%d err=%v", created, id, syncs, retryErr)
			}
		})
	}
}
func TestInitializeProjectID_RetrySyncsPublishedFinalAndIgnoresResidue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows directory sync is explicitly unsupported")
	}
	original := projectMarkerFS
	defer func() { projectMarkerFS = original }()
	workspace := t.TempDir()
	testutil.NoError(t, os.Mkdir(filepath.Join(workspace, ".vgxness"), 0o700))
	projectMarkerFS.openDir = func(path string) (markerDirFile, error) {
		file, err := os.Open(path)
		if filepath.Base(path) != ".vgxness" {
			return file, err
		}
		return markerTestDir{File: file, stage: "parent-sync", err: errors.New("durability")}, err
	}
	_, _, err := InitializeProjectID(workspace)
	testutil.Require(t, errors.Is(err, ErrInvalid), "initial durability error=%v", err)
	published, present, err := ReadProjectID(workspace)
	testutil.Require(t, err == nil && present && published != "", "published final=%q present=%t err=%v", published, present, err)
	var syncs int
	projectMarkerFS.openDir = func(path string) (markerDirFile, error) {
		file, err := os.Open(path)
		return markerTestDir{File: file, sync: &syncs}, err
	}
	id, created, err := InitializeProjectID(workspace)
	testutil.Require(t, err == nil && !created && id == published && syncs == 2, "retry created=%t id=%q syncs=%d err=%v", created, id, syncs, err)
	testutil.NoError(t, os.WriteFile(filepath.Join(workspace, ".vgxness", ".project-id-abandoned"), []byte("residue"), 0o600))
	got, created, err := InitializeProjectID(workspace)
	testutil.Require(t, err == nil && !created && got == id, "residue retry id=%q created=%t err=%v", got, created, err)
}
func TestEnsureProjectID_PublishRaceRejectsDifferentFinal(t *testing.T) {
	original := projectMarkerFS
	defer func() { projectMarkerFS = original }()
	workspace := t.TempDir()
	const expected, other = "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"
	projectMarkerFS.link = func(_, path string) error {
		testutil.NoError(t, os.WriteFile(path, []byte(`{"format":"vgxness-project-id/v1","kind":"project","project_id":"`+other+`"}`), 0o600))
		return os.ErrExist
	}
	_, _, err := EnsureProjectID(workspace, expected)
	testutil.Require(t, errors.Is(err, ErrConflict), "race error=%v", err)
	got, present, err := ReadProjectID(workspace)
	testutil.Require(t, err == nil && present && got == other, "final=%q present=%t err=%v", got, present, err)
}
func TestInitializeProjectID_ConcurrentInitPublishesOneValidMarker(t *testing.T) {
	workspace := t.TempDir()
	results := make(chan string, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() { defer group.Done(); id, _, err := InitializeProjectID(workspace); results <- id; errs <- err }()
	}
	group.Wait()
	close(results)
	close(errs)
	var first string
	for err := range errs {
		testutil.NoError(t, err)
	}
	for id := range results {
		if first == "" {
			first = id
		}
		testutil.Require(t, id == first, "concurrent IDs differ: %q / %q", first, id)
	}
	got, present, err := ReadProjectID(workspace)
	testutil.Require(t, err == nil && present && got == first, "final id=%q present=%t err=%v want=%q", got, present, err, first)
}

type markerTestTemp struct {
	*os.File
	stage string
	err   error
}

func (f markerTestTemp) Write(p []byte) (int, error) {
	if f.stage == "write" {
		return 0, f.err
	}
	return f.File.Write(p)
}
func (f markerTestTemp) Sync() error {
	if f.stage == "file-sync" {
		return f.err
	}
	return f.File.Sync()
}
func (f markerTestTemp) Close() error {
	if f.stage == "close" {
		_ = f.File.Close()
		return f.err
	}
	return f.File.Close()
}

type markerTestDir struct {
	*os.File
	stage string
	err   error
	sync  *int
}

func (f markerTestDir) Sync() error {
	if f.sync != nil {
		*f.sync++
	}
	if strings.HasSuffix(f.stage, "sync") {
		return f.err
	}
	return f.File.Sync()
}
func (f markerTestDir) Close() error {
	if strings.HasSuffix(f.stage, "close") {
		_ = f.File.Close()
		return f.err
	}
	return f.File.Close()
}

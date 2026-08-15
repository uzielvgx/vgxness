package selfinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/launcher"
)

func TestGCPreviewAndApplyRetainActiveAndPrevious(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := New(Config{SourceExecutable: writeSource(t, root, "one", "one")})
	installed, err := first.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceExecutable: writeSource(t, root, "two", "two")})
	updated, err := second.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	third := New(Config{SourceExecutable: writeSource(t, root, "three", "three")})
	latest, err := third.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := third.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	retained := []string{latest.ActiveSHA256, updated.ActiveSHA256}
	sort.Strings(retained)
	if preview.Changed || !equalStrings(preview.Candidates, []string{installed.ActiveSHA256}) || !equalStrings(preview.Retained, retained) {
		t.Fatalf("preview=%+v", preview)
	}
	if _, err := os.Stat(filepath.Join(options.DataDir, "versions", installed.ActiveSHA256, executableName())); err != nil {
		t.Fatalf("preview removed a version: %v", err)
	}

	applied, err := third.GCApply(context.Background(), options, preview.PlanSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Changed || !equalStrings(applied.Deleted, preview.Candidates) || !equalStrings(applied.Retained, preview.Retained) {
		t.Fatalf("applied=%+v preview=%+v", applied, preview)
	}
	if _, err := os.Stat(filepath.Join(options.DataDir, "versions", installed.ActiveSHA256)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete version remains: %v", err)
	}
}

func TestGCRecoverPreservesInterruptedStage(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "source")})
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	service = New(Config{SourceExecutable: writeSource(t, root, "source-next", "source-next")})
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	service = New(Config{SourceExecutable: writeSource(t, root, "source-latest", "source-latest")})
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	stage := ".gc-stage-00112233445566778899aabb"
	if err := anchors.data.Rename(filepath.Join("versions", installed.ActiveSHA256), stage); err != nil {
		t.Fatal(err)
	}
	if err := writeGCJournal(anchors.data, gcJournal{SchemaVersion: "v1", State: gcStateStaged, Stage: stage, CandidateSHA256: installed.ActiveSHA256, PlanSHA256: installed.ActiveSHA256, Source: filepath.Join("versions", installed.ActiveSHA256)}); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.GCRecover(context.Background(), options)
	if err != nil || !equalStrings(recovered.Recovered, []string{installed.ActiveSHA256}) {
		t.Fatalf("recover result=%+v err=%v", recovered, err)
	}
	if _, err := anchors.data.Lstat(filepath.Join("versions", installed.ActiveSHA256, executableName())); err != nil {
		t.Fatalf("recovery removed staged executable: %v", err)
	}
}

func TestGCRejectsNoInstallationStalePlansAndUnsafeInventory(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "source")})
	if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrNoInstallation) {
		t.Fatalf("absent preview error=%v", err)
	}
	first, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceExecutable: writeSource(t, root, "next", "next")})
	if _, err := second.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	preview, err := second.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.GCApply(context.Background(), options, strings.Repeat("0", 64)); !errors.Is(err, ErrStaleGCPlan) {
		t.Fatalf("stale apply error=%v", err)
	}
	if err := os.Mkdir(filepath.Join(options.DataDir, "versions", "foreign"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := second.GCPreview(context.Background(), options); !errors.Is(err, ErrDrift) {
		t.Fatalf("unsafe preview error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(options.DataDir, "versions", first.ActiveSHA256, executableName())); err != nil {
		t.Fatalf("unsafe preview removed content: %v", err)
	}
	if preview.PlanSHA256 == "" {
		t.Fatal("missing canonical plan digest")
	}
}

func TestGCJournalBlocksPreviewAndApply(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "source")})
	installed, err := service.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	if err := writeGCJournal(anchors.data, gcJournal{SchemaVersion: "v1", State: gcStatePrepared, PlanSHA256: installed.ActiveSHA256, CandidateSHA256: installed.ActiveSHA256, Source: filepath.Join("versions", installed.ActiveSHA256), Stage: ".gc-stage-00112233445566778899aabb"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrGCRecovery) || !errors.Is(err, ErrRecovery) {
		t.Fatalf("journal preview error=%v", err)
	}
	if _, err := service.GCApply(context.Background(), options, strings.Repeat("0", 64)); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("journal apply error=%v", err)
	}
}

func TestGCPlanSHA256UsesLabeledCanonicalLFBytes(t *testing.T) {
	manifest := launcher.Manifest{ActiveSHA256: strings.Repeat("a", 64)}
	got := gcPlanSHA256("/tmp/data", []byte("manifest\n"), manifest, []string{strings.Repeat("b", 64), strings.Repeat("c", 64)})
	const want = "0e482534c8b552e0bbbf7aefec0164145ab2ba71215a5b31b3cb95e7078901b4"
	if got != want {
		t.Fatalf("canonical plan hash=%s want %s", got, want)
	}
	if empty := gcPlanSHA256("/tmp/data", []byte("manifest\n"), manifest, nil); empty == got {
		t.Fatal("empty plan was not distinct")
	}
}

func TestGCPreviewIsNonMutatingAndEmptyApplyIsNoop(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	service := New(Config{SourceExecutable: writeSource(t, root, "source", "source")})
	if _, err := service.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	preview, err := service.GCPreview(context.Background(), options)
	if err != nil || len(preview.Candidates) != 0 || preview.PlanSHA256 == "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	preview.Retained[0] = "mutated"
	again, err := service.GCPreview(context.Background(), options)
	if err != nil || again.Retained[0] == "mutated" {
		t.Fatalf("result was not copied: %+v err=%v", again, err)
	}
	applied, err := service.GCApply(context.Background(), options, again.PlanSHA256)
	if err != nil || applied.Changed || len(applied.Deleted) != 0 {
		t.Fatalf("apply=%+v err=%v", applied, err)
	}
}

func TestGCRejectsUnsafeInventoryMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, options Options, candidate, previous, active string)
	}{
		{"foreign", func(t *testing.T, o Options, _, _, _ string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(o.DataDir, "versions", "foreign"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"uppercase", func(t *testing.T, o Options, _, _, _ string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(o.DataDir, "versions", strings.Repeat("A", 64)), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"child-file", func(t *testing.T, o Options, c, _, _ string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(o.DataDir, "versions", c, "extra"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"digest-file", func(t *testing.T, o Options, c, _, _ string) {
			if err := os.Remove(filepath.Join(o.DataDir, "versions", c, executableName())); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(o.DataDir, "versions", c)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(o.DataDir, "versions", c), []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing-executable", func(t *testing.T, o Options, c, _, _ string) {
			t.Helper()
			if err := os.Remove(filepath.Join(o.DataDir, "versions", c, executableName())); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong-name", func(t *testing.T, o Options, c, _, _ string) {
			t.Helper()
			if err := os.Rename(filepath.Join(o.DataDir, "versions", c, executableName()), filepath.Join(o.DataDir, "versions", c, "wrong")); err != nil {
				t.Fatal(err)
			}
		}},
		{"zero", func(t *testing.T, o Options, c, _, _ string) {
			replaceGCExecutable(t, o, c, nil)
		}},
		{"hash-mismatch", func(t *testing.T, o Options, c, _, _ string) {
			replaceGCExecutable(t, o, c, []byte("different"))
		}},
		{"executable-directory", func(t *testing.T, o Options, c, _, _ string) {
			path := filepath.Join(o.DataDir, "versions", c, executableName())
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"executable-symlink", func(t *testing.T, o Options, c, _, _ string) {
			path := filepath.Join(o.DataDir, "versions", c, executableName())
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(o.DataDir, "versions", c, "missing"), path); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}},
		{"oversize", func(t *testing.T, o Options, c, _, _ string) {
			path := filepath.Join(o.DataDir, "versions", c, executableName())
			if err := os.Chmod(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, launcher.MaxBinarySize+1); err != nil {
				t.Skipf("sparse truncate unavailable: %v", err)
			}
		}},
		{"tampered-active", func(t *testing.T, o Options, _, _, a string) {
			replaceGCExecutable(t, o, a, []byte("different"))
		}},
		{"tampered-previous", func(t *testing.T, o Options, _, p, _ string) {
			replaceGCExecutable(t, o, p, []byte("different"))
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, options, candidate, previous, active := gcThreeVersions(t)
			test.mutate(t, options, candidate, previous, active)
			if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrDrift) {
				t.Fatalf("error=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("journal=%v", err)
			}
			for _, digest := range []string{previous, active} {
				if _, err := os.Stat(filepath.Join(options.DataDir, "versions", digest, executableName())); err != nil {
					t.Fatalf("protected version changed: %v", err)
				}
			}
		})
	}
}

func TestGCRejectsInventoryOverLimit(t *testing.T) {
	service, options, _, _, _ := gcThreeVersions(t)
	for index := 0; index < 1025; index++ {
		if err := os.Mkdir(filepath.Join(options.DataDir, "versions", "foreign-"+strings.Repeat("x", 4)+fmt.Sprintf("%04d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrDrift) {
		t.Fatalf("error=%v", err)
	}
}

func TestGCJournalCodecRejectsMalformedEvidence(t *testing.T) {
	bad := []string{
		`{"schemaVersion":"v1","schemaVersion":"v1"}`,
		`{"unknown":true}`,
		`{"schemaVersion":"v1"} trailing`,
		`{"schemaVersion":"v1","state":"prepared","planSha256":"` + strings.Repeat("a", 64) + `","candidateSha256":"` + strings.Repeat("b", 64) + `","source":"../x","stage":".gc-stage-00112233445566778899aabb"}`,
		`{"schemaVersion":"v1","state":"prepared","planSha256":"` + strings.Repeat("a", 64) + `","candidateSha256":"` + strings.Repeat("b", 64) + `","source":"versions/` + strings.Repeat("b", 64) + `","stage":"bad","unknown":true}`,
		`{"schemaVersion":"v1","state":"prepared","planSha256":"` + strings.Repeat("a", 64) + `","candidateSha256":"` + strings.Repeat("b", 64) + `","source":"versions/` + strings.Repeat("b", 64) + `"}`,
		`{"schemaVersion":"v1","state":"prepared","planSha256":"bad","candidateSha256":"` + strings.Repeat("b", 64) + `","source":"versions/` + strings.Repeat("b", 64) + `","stage":".gc-stage-00112233445566778899aabb"}`,
		`{"schemaVersion":"v1","state":"prepared","planSha256":"` + strings.Repeat("a", 64) + `","candidateSha256":"bad","source":"versions/bad","stage":".gc-stage-00112233445566778899aabb"}`,
		`{"schemaVersion":"v1","state":"prepared","planSha256":"` + strings.Repeat("a", 64) + `","candidateSha256":"` + strings.Repeat("b", 64) + `","source":"versions/` + strings.Repeat("b", 64) + `","stage":".gc-stage-00112233445566778899aabb","padding":"` + strings.Repeat("x", gcJournalMaximum) + `"}`,
	}
	for index, evidence := range bad {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			service, options, _, _, _ := gcThreeVersions(t)
			path := filepath.Join(options.DataDir, gcJournalName)
			if err := os.WriteFile(path, []byte(evidence), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
				t.Fatalf("preview error=%v", err)
			}
			if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
				t.Fatalf("error=%v", err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != evidence {
				t.Fatalf("evidence=%q err=%v", actual, err)
			}
		})
	}
}

func TestGCJournalCodecRejectsSymlinkEvidence(t *testing.T) {
	service, options, _, _, _ := gcThreeVersions(t)
	target := filepath.Join(options.DataDir, "evidence")
	if err := os.WriteFile(target, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(options.DataDir, gcJournalName)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("preview error=%v", err)
	}
	if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("recover error=%v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "foreign" {
		t.Fatalf("target=%q err=%v", data, err)
	}
}

func TestGCRecoveryStateMatrix(t *testing.T) {
	cases := []struct {
		name                                string
		state                               gcState
		moved, empty, both, tampered, extra bool
		wantErr                             bool
	}{
		{name: "prepared", state: gcStatePrepared},
		{name: "moving-unmoved", state: gcStateMoving},
		{name: "moving-moved", state: gcStateMoving, moved: true},
		{name: "staged", state: gcStateStaged, moved: true},
		{name: "deleting-exact", state: gcStateDeleting, moved: true},
		{name: "deleting-empty", state: gcStateDeleting, moved: true, empty: true},
		{name: "deleted-empty", state: gcStateDeleted, moved: true, empty: true},
		{name: "deleted-absent", state: gcStateDeleted},
		{name: "source-collision", state: gcStateStaged, moved: true, both: true, wantErr: true},
		{name: "neither", state: gcStateStaged, wantErr: true},
		{name: "tampered-staged-executable", state: gcStateStaged, moved: true, tampered: true, wantErr: true},
		{name: "extra-stage-entry", state: gcStateStaged, moved: true, extra: true, wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, options, candidate, _, _ := gcThreeVersions(t)
			anchors, err := openAnchors(mustResolvePaths(t, options))
			if err != nil {
				t.Fatal(err)
			}
			defer anchors.close()
			stage := ".gc-stage-00112233445566778899aabb"
			source := filepath.Join("versions", candidate)
			if test.moved {
				if err := anchors.data.Rename(source, stage); err != nil {
					t.Fatal(err)
				}
			}
			if test.both {
				if err := os.Mkdir(filepath.Join(options.DataDir, "versions", candidate), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(options.DataDir, "versions", candidate, executableName()), []byte("foreign"), 0o555); err != nil {
					t.Fatal(err)
				}
			}
			if test.empty {
				if err := anchors.data.Remove(filepath.Join(stage, executableName())); err != nil {
					t.Fatal(err)
				}
			}
			if test.tampered {
				replaceGCStageExecutable(t, anchors, stage, []byte("tampered"))
			}
			if test.extra {
				if err := os.WriteFile(filepath.Join(options.DataDir, stage, "extra"), []byte("extra"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "neither" {
				if err := anchors.data.Remove(filepath.Join(source, executableName())); err != nil {
					t.Fatal(err)
				}
				if err := anchors.data.Remove(source); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "deleted-absent" {
				test.moved = true
				if err := anchors.data.Rename(source, stage); err != nil {
					t.Fatal(err)
				}
				if err := anchors.data.Remove(filepath.Join(stage, executableName())); err != nil {
					t.Fatal(err)
				}
				if err := anchors.data.Remove(stage); err != nil {
					t.Fatal(err)
				}
			}
			journal := gcJournal{SchemaVersion: "v1", State: test.state, PlanSHA256: strings.Repeat("a", 64), CandidateSHA256: candidate, Source: source, Stage: stage}
			if err := writeGCJournal(anchors.data, journal); err != nil {
				t.Fatal(err)
			}
			_, err = service.GCRecover(context.Background(), options)
			if test.wantErr != errors.Is(err, ErrGCRecovery) {
				t.Fatalf("error=%v", err)
			}
			journalPath := filepath.Join(options.DataDir, gcJournalName)
			if test.wantErr {
				if _, statErr := os.Lstat(journalPath); statErr != nil {
					t.Fatalf("journal was not retained: %v", statErr)
				}
				return
			}
			if _, statErr := os.Lstat(journalPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("journal=%v", statErr)
			}
			if !test.empty && test.state != gcStateDeleted {
				path := filepath.Join(options.DataDir, "versions", candidate, executableName())
				if digest, hashErr := launcher.FileSHA256(path); hashErr != nil || digest != candidate {
					t.Fatalf("restored=%s err=%v", digest, hashErr)
				}
			}
		})
	}
}

func TestGCRecoveryNeverUnlinksExecutable(t *testing.T) {
	cases := []struct {
		name      string
		state     gcState
		move      bool
		wantError bool
	}{
		{name: "prepared", state: gcStatePrepared},
		{name: "moving-source", state: gcStateMoving},
		{name: "moving-stage", state: gcStateMoving, move: true},
		{name: "staged", state: gcStateStaged, move: true},
		{name: "deleting", state: gcStateDeleting, move: true},
		{name: "deleted-exact-stage-invalid", state: gcStateDeleted, move: true, wantError: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, options, candidate, _, _ := gcThreeVersions(t)
			anchors, err := openAnchors(mustResolvePaths(t, options))
			if err != nil {
				t.Fatal(err)
			}
			defer anchors.close()
			source, stage := filepath.Join("versions", candidate), ".gc-stage-00112233445566778899aabb"
			if test.move {
				if err := anchors.data.Rename(source, stage); err != nil {
					t.Fatal(err)
				}
			}
			location := source
			if test.move {
				location = stage
			}
			before, err := launcher.FileSHA256(filepath.Join(options.DataDir, location, executableName()))
			if err != nil || before != candidate {
				t.Fatalf("before=%s err=%v", before, err)
			}
			journal := gcJournal{SchemaVersion: "v1", State: test.state, PlanSHA256: strings.Repeat("a", 64), CandidateSHA256: candidate, Source: source, Stage: stage}
			if err := writeGCJournal(anchors.data, journal); err != nil {
				t.Fatal(err)
			}
			_, err = service.GCRecover(context.Background(), options)
			if test.wantError != errors.Is(err, ErrGCRecovery) {
				t.Fatalf("error=%v", err)
			}
			if digest, hashErr := launcher.FileSHA256(filepath.Join(options.DataDir, source, executableName())); !test.wantError && (hashErr != nil || digest != before) {
				t.Fatalf("restored=%s err=%v", digest, hashErr)
			}
			if test.wantError {
				if digest, hashErr := launcher.FileSHA256(filepath.Join(options.DataDir, stage, executableName())); hashErr != nil || digest != before {
					t.Fatalf("retained=%s err=%v", digest, hashErr)
				}
			}
		})
	}
}

func TestGCApplyPreflightSeamSupportsDeterministicCancellation(t *testing.T) {
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := New(Config{SourceExecutable: writeSource(t, root, "one", "one")})
	if _, err := first.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceExecutable: writeSource(t, root, "two", "two")})
	if _, err := second.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	third := New(Config{SourceExecutable: writeSource(t, root, "three", "three")})
	if _, err := third.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	plan, err := third.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	third.afterGCPreflight = func() error { close(entered); <-release; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := third.GCApply(ctx, options, plan.PlanSHA256); done <- err }()
	<-entered
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal=%v", err)
	}
}

func TestGCApplyRequiresExistingRegularLock(t *testing.T) {
	for _, mode := range []string{"missing", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			service, options, candidate, _, _ := gcThreeVersions(t)
			lock := filepath.Join(options.DataDir, ".install.lock")
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			if mode == "symlink" {
				if err := os.Symlink(filepath.Join(options.DataDir, "target"), lock); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			preview, err := service.GCPreview(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.GCApply(context.Background(), options, preview.PlanSHA256); !errors.Is(err, ErrDrift) {
				t.Fatalf("error=%v", err)
			}
			if _, err := os.Stat(filepath.Join(options.DataDir, "versions", candidate, executableName())); err != nil {
				t.Fatalf("candidate deleted: %v", err)
			}
			if mode == "missing" {
				if _, err := os.Lstat(lock); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("lock created: %v", err)
				}
			}
		})
	}
}

func TestGCApplyRevalidatesAfterPreflight(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	plan, err := service.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	service.afterGCPreflight = func() error { replaceGCExecutable(t, options, candidate, []byte("changed")); return nil }
	if _, err := service.GCApply(context.Background(), options, plan.PlanSHA256); !errors.Is(err, ErrDrift) && !errors.Is(err, ErrStaleGCPlan) {
		t.Fatalf("error=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(options.DataDir, "versions", candidate, executableName())); err != nil || string(data) != "changed" {
		t.Fatalf("candidate=%q err=%v", data, err)
	}
}

func TestGCApplyFaultRecoveryByDurableState(t *testing.T) {
	states := []gcState{gcStatePrepared, gcStateMoving, gcStateStaged, gcStateDeleting, gcStateDeleted}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			root := t.TempDir()
			options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
			var service *Service
			for _, value := range []string{"one", "two", "three", "four"} {
				service = New(Config{SourceExecutable: writeSource(t, root, value, value)})
				if _, err := service.Install(context.Background(), options); err != nil {
					t.Fatal(err)
				}
			}
			preview, err := service.GCPreview(context.Background(), options)
			if err != nil || len(preview.Candidates) < 2 {
				t.Fatalf("preview=%+v err=%v", preview, err)
			}
			sentinel := errors.New("fault-" + string(state))
			service.afterGCJournal = func(actual gcState) error {
				if actual == state {
					return sentinel
				}
				return nil
			}
			_, err = service.GCApply(context.Background(), options, preview.PlanSHA256)
			if !errors.Is(err, ErrGCRecovery) || !errors.Is(err, sentinel) {
				t.Fatalf("apply error=%v", err)
			}
			anchors, openErr := openAnchors(mustResolvePaths(t, options))
			if openErr != nil {
				t.Fatal(openErr)
			}
			journal, readErr := readGCJournal(anchors.data)
			anchors.close()
			if readErr != nil || journal.State != state {
				t.Fatalf("journal=%+v err=%v", journal, readErr)
			}
			later := preview.Candidates[1]
			if _, statErr := os.Lstat(filepath.Join(options.DataDir, "versions", later)); statErr != nil {
				t.Fatalf("later candidate changed: %v", statErr)
			}
			recovered, recoverErr := service.GCRecover(context.Background(), options)
			if recoverErr != nil {
				t.Fatalf("recover=%+v err=%v", recovered, recoverErr)
			}
			if _, statErr := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("journal=%v", statErr)
			}
		})
	}
}

func TestGCApplyLockWaitIsCancellable(t *testing.T) {
	service, options, _, _, _ := gcThreeVersions(t)
	preview, err := service.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	file, err := anchors.data.OpenFile(".install.lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	held, err := acquireFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	service.afterGCPreflight = func() error { close(entered); return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.GCApply(ctx, options, preview.PlanSHA256); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("preflight did not signal")
	}
	cancel()
	var applyErr error
	select {
	case applyErr = <-done:
	case <-time.After(time.Second):
		t.Fatal("apply did not cancel")
	}
	if !errors.Is(applyErr, context.Canceled) {
		t.Fatalf("error=%v", applyErr)
	}
	if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal=%v", err)
	}
	held.release()
	service.afterGCPreflight = nil
	if _, err := service.GCApply(context.Background(), options, preview.PlanSHA256); err != nil {
		t.Fatalf("fresh apply=%v", err)
	}
}

func TestGCJournalNextEvidenceFailsClosed(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	journal := gcJournal{SchemaVersion: "v1", State: gcStatePrepared, PlanSHA256: strings.Repeat("a", 64), CandidateSHA256: candidate, Source: filepath.Join("versions", candidate), Stage: ".gc-stage-00112233445566778899aabb"}
	if err := writeGCJournal(anchors.data, journal); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("foreign-next")
	if err := writeRootFile(anchors.data, gcJournalNextName, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := advanceGCJournal(anchors.data, &journal, gcStateMoving); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("advance=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(options.DataDir, gcJournalNextName)); err != nil || string(data) != string(foreign) {
		t.Fatalf("next=%q err=%v", data, err)
	}
	if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("preview=%v", err)
	}
}

func TestGCJournalCanonicalNextInconsistencyPreservesEvidence(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	base := gcJournal{SchemaVersion: "v1", State: gcStatePrepared, PlanSHA256: strings.Repeat("a", 64), CandidateSHA256: candidate, Source: filepath.Join("versions", candidate), Stage: ".gc-stage-00112233445566778899aabb"}
	if err := writeGCJournal(anchors.data, base); err != nil {
		t.Fatal(err)
	}
	next := base
	next.State = gcStateDeleted
	if err := writeGCJournalNamed(anchors.data, gcJournalNextName, next); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GCPreview(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("preview=%v", err)
	}
	if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("recover=%v", err)
	}
	for _, name := range []string{gcJournalName, gcJournalNextName} {
		if _, err := os.Lstat(filepath.Join(options.DataDir, name)); err != nil {
			t.Fatalf("%s=%v", name, err)
		}
	}
}

func TestGCJournalCanonicalSuccessorNextRecovers(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	base := gcJournal{SchemaVersion: "v1", State: gcStatePrepared, PlanSHA256: strings.Repeat("a", 64), CandidateSHA256: candidate, Source: filepath.Join("versions", candidate), Stage: ".gc-stage-00112233445566778899aabb"}
	if err := writeGCJournal(anchors.data, base); err != nil {
		t.Fatal(err)
	}
	next := base
	next.State = gcStateMoving
	if err := writeGCJournalNamed(anchors.data, gcJournalNextName, next); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GCRecover(context.Background(), options); err != nil {
		t.Fatalf("recover=%v", err)
	}
	for _, name := range []string{gcJournalName, gcJournalNextName} {
		if _, err := os.Lstat(filepath.Join(options.DataDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s=%v", name, err)
		}
	}
}

func TestGCJournalCanonicalNextHardlinkRecovers(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	journal := gcJournal{
		SchemaVersion:   "v1",
		State:           gcStatePrepared,
		PlanSHA256:      strings.Repeat("a", 64),
		CandidateSHA256: candidate,
		Source:          filepath.Join("versions", candidate),
		Stage:           ".gc-stage-00112233445566778899aabb",
	}
	if err := writeGCJournal(anchors.data, journal); err != nil {
		t.Fatal(err)
	}
	if err := anchors.data.Link(gcJournalName, gcJournalNextName); err != nil {
		t.Fatal(err)
	}
	canonical, err := anchors.data.Lstat(gcJournalName)
	if err != nil {
		t.Fatal(err)
	}
	next, err := anchors.data.Lstat(gcJournalNextName)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(canonical, next) {
		t.Fatal("canonical and next journal evidence are not hardlinks")
	}
	if _, err := service.GCRecover(context.Background(), options); err != nil {
		t.Fatalf("recover=%v", err)
	}
	for _, name := range []string{gcJournalName, gcJournalNextName} {
		if _, err := os.Lstat(filepath.Join(options.DataDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s=%v", name, err)
		}
	}
}

func TestGCApplyPreparedLockReplacementStopsBeforeMove(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	plan, err := service.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	service.afterGCJournal = func(state gcState) error {
		if state == gcStatePrepared {
			lock := filepath.Join(options.DataDir, ".install.lock")
			if err := os.Remove(lock); err != nil {
				return err
			}
			return os.WriteFile(lock, []byte("replacement"), 0o600)
		}
		return nil
	}
	if _, err := service.GCApply(context.Background(), options, plan.PlanSHA256); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("apply=%v", err)
	}
	if _, err := os.Stat(filepath.Join(options.DataDir, "versions", candidate, executableName())); err != nil {
		t.Fatalf("candidate moved/deleted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); err != nil {
		t.Fatalf("journal not retained: %v", err)
	}
}

func TestGCApplyRejectsRawManifestDriftBeforeMoveAndDelete(t *testing.T) {
	for _, state := range []gcState{gcStateMoving, gcStateDeleting} {
		t.Run(string(state), func(t *testing.T) {
			service, options, candidate, _, _ := gcThreeVersions(t)
			plan, err := service.GCPreview(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			service.afterGCJournal = func(actual gcState) error {
				if actual != state {
					return nil
				}
				path := mustResolvePaths(t, options).manifest
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				return os.WriteFile(path, append(raw, '\n'), 0o600)
			}
			if _, err := service.GCApply(context.Background(), options, plan.PlanSHA256); !errors.Is(err, ErrGCRecovery) {
				t.Fatalf("apply error=%v", err)
			}
			locations := []string{filepath.Join("versions", candidate)}
			anchors, openErr := openAnchors(mustResolvePaths(t, options))
			if openErr != nil {
				t.Fatal(openErr)
			}
			journal, journalErr := readGCJournal(anchors.data)
			anchors.close()
			if journalErr != nil {
				t.Fatal(journalErr)
			}
			locations = append(locations, journal.Stage)
			found := false
			for _, location := range locations {
				if _, err := os.Stat(filepath.Join(options.DataDir, location, executableName())); err == nil {
					found = true
				}
			}
			if !found {
				t.Fatal("candidate executable was removed after raw manifest drift")
			}
			if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); err != nil {
				t.Fatalf("journal not retained: %v", err)
			}
		})
	}
}

func TestGCApplyRejectsRawManifestDriftImmediatelyBeforeExecutableRemoval(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	plan, err := service.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	service.afterGCDeleteOpened = func() error {
		path := mustResolvePaths(t, options).manifest
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(raw, '\n'), 0o600)
	}
	if _, err := service.GCApply(context.Background(), options, plan.PlanSHA256); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("apply error=%v", err)
	}
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := readGCJournal(anchors.data)
	anchors.close()
	if err != nil {
		t.Fatal(err)
	}
	if digest, err := launcher.FileSHA256(filepath.Join(options.DataDir, journal.Stage, executableName())); err != nil || digest != candidate {
		t.Fatalf("staged executable=%s err=%v", digest, err)
	}
}

func TestGCApplyReadsPlanManifestBytesAfterSemanticValidation(t *testing.T) {
	service, options, candidate, _, _ := gcThreeVersions(t)
	plan, err := service.GCPreview(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	service.afterGCSemanticValidation = func() error {
		path := mustResolvePaths(t, options).manifest
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(raw, '\n'), 0o600)
	}
	if _, err := service.GCApply(context.Background(), options, plan.PlanSHA256); !errors.Is(err, ErrDrift) {
		t.Fatalf("apply error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(options.DataDir, "versions", candidate, executableName())); err != nil {
		t.Fatalf("candidate moved after final manifest drift: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal=%v", err)
	}
}

func TestGCApplyReturnsPartialResultAfterCompletedDeletion(t *testing.T) {
	service, options := gcFourVersions(t)
	plan, err := service.GCPreview(context.Background(), options)
	if err != nil || len(plan.Candidates) != 2 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	prepared := 0
	service.afterGCJournal = func(state gcState) error {
		if state == gcStatePrepared {
			prepared++
			if prepared == 2 {
				return errors.New("second deletion fault")
			}
		}
		return nil
	}
	partial, err := service.GCApply(context.Background(), options, plan.PlanSHA256)
	if !errors.Is(err, ErrGCRecovery) || !equalStrings(partial.Deleted, plan.Candidates[:1]) || !partial.Changed {
		t.Fatalf("partial=%+v err=%v", partial, err)
	}
	remaining := plan.Candidates[1]
	if _, err := os.Stat(filepath.Join(options.DataDir, "versions", remaining, executableName())); err != nil {
		t.Fatalf("incomplete candidate changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); err != nil {
		t.Fatalf("incomplete recovery evidence missing: %v", err)
	}
}

func TestGCApplyReportsUnlinkedCandidateWhenFinalizationFails(t *testing.T) {
	for _, test := range []struct {
		point     string
		journal   gcState
		stage     bool
		recovered bool
	}{
		{gcSyncPostUnlinkStage, gcStateDeleting, true, true},
		{gcSyncAfterStageRemoval, gcStateDeleted, false, true},
		{gcSyncAfterJournalRemoval, "", false, false},
	} {
		t.Run(test.point, func(t *testing.T) {
			service, options, candidate, _, _ := gcThreeVersions(t)
			plan, err := service.GCPreview(context.Background(), options)
			if err != nil || !equalStrings(plan.Candidates, []string{candidate}) {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
			service.gcSync = func(actual string, _ *os.Root) error {
				if actual == test.point {
					return errors.New("sync fault")
				}
				return nil
			}
			result, err := service.GCApply(context.Background(), options, plan.PlanSHA256)
			if !errors.Is(err, ErrGCRecovery) || !equalStrings(result.Deleted, []string{candidate}) || !result.Changed {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if _, err := os.Lstat(filepath.Join(options.DataDir, "versions", candidate)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate remains after unlink: %v", err)
			}
			anchors, err := openAnchors(mustResolvePaths(t, options))
			if err != nil {
				t.Fatal(err)
			}
			defer anchors.close()
			journal, journalErr := readGCJournal(anchors.data)
			if test.journal == "" {
				if !errors.Is(journalErr, os.ErrNotExist) {
					t.Fatalf("journal after final sync failure=%+v err=%v", journal, journalErr)
				}
				entries, err := readRootDir(anchors.data, ".", gcInventoryMax)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".gc-stage-") {
						t.Errorf("stage remains after final sync failure: %s", entry.Name())
					}
				}
			} else if journalErr != nil || journal.State != test.journal {
				t.Errorf("journal=%+v err=%v want state=%s", journal, journalErr, test.journal)
			}
			if test.journal != "" {
				_, stageErr := anchors.data.Lstat(journal.Stage)
				if test.stage && stageErr != nil {
					t.Errorf("stage missing after %s: %v", test.point, stageErr)
				}
				if !test.stage && !errors.Is(stageErr, os.ErrNotExist) {
					t.Errorf("stage after %s: %v", test.point, stageErr)
				}
			}
			recovered, recoverErr := service.GCRecover(context.Background(), options)
			if test.recovered {
				if recoverErr != nil || !equalStrings(recovered.Recovered, []string{candidate}) || !recovered.Changed {
					t.Errorf("recovered=%+v err=%v", recovered, recoverErr)
				}
			} else if recoverErr != nil || recovered.Changed || len(recovered.Recovered) != 0 {
				t.Errorf("final recovery=%+v err=%v", recovered, recoverErr)
			}
		})
	}
}

func TestGCRecoverFinalizesStagedOrDeletingJournalAfterRestorePublish(t *testing.T) {
	for _, state := range []gcState{gcStateStaged, gcStateDeleting} {
		t.Run(string(state), func(t *testing.T) {
			service, options, candidate, _ := gcRecoveryFixture(t, state, false, false)
			recovered, err := service.GCRecover(context.Background(), options)
			if err != nil || !equalStrings(recovered.Recovered, []string{candidate}) || !recovered.Changed {
				t.Fatalf("recovered=%+v err=%v", recovered, err)
			}
			if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("journal=%v", err)
			}
		})
	}
}

func TestGCRecoverRetriesAfterRestorePublishBeforeJournalCleanup(t *testing.T) {
	service, options, candidate, _ := gcRecoveryFixture(t, gcStateStaged, true, false)
	mutations := 0
	service.beforeGCRecoveryMutation = func() error {
		mutations++
		if mutations == 2 {
			return errors.New("interrupt after restore publish")
		}
		return nil
	}
	if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("first recovery error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(options.DataDir, "versions", candidate, executableName())); err != nil {
		t.Fatalf("restored executable=%v", err)
	}
	service.beforeGCRecoveryMutation = nil
	recovered, err := service.GCRecover(context.Background(), options)
	if err != nil || !equalStrings(recovered.Recovered, []string{candidate}) || !recovered.Changed {
		t.Fatalf("second recovery=%+v err=%v", recovered, err)
	}
}

func TestReadRootDirRejectsBoundedInventoryOverflow(t *testing.T) {
	root := t.TempDir()
	anchored, err := openInstallRoot(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer anchored.Close()
	for index := 0; index <= gcInventoryMax; index++ {
		if err := anchored.Mkdir(fmt.Sprintf("version-%04d", index), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readRootDir(anchored, ".", gcInventoryMax); !errors.Is(err, ErrDrift) {
		t.Fatalf("versions overflow error=%v", err)
	}
	stageRoot, err := openInstallRoot(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer stageRoot.Close()
	for _, name := range []string{"stage-a", "stage-b"} {
		if err := stageRoot.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readRootDir(stageRoot, ".", 1); !errors.Is(err, ErrDrift) {
		t.Fatalf("stage overflow error=%v", err)
	}
}

func TestReadRootDirAcceptsInventoryAtMaximum(t *testing.T) {
	anchored, err := openInstallRoot(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer anchored.Close()
	for index := 0; index < gcInventoryMax; index++ {
		if err := anchored.Mkdir(fmt.Sprintf("version-%04d", index), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := readRootDir(anchored, ".", gcInventoryMax)
	if err != nil || len(entries) != gcInventoryMax {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
}

func TestGCRecoverRejectsInvalidLockAndPreservesEvidence(t *testing.T) {
	for _, mode := range []string{"missing", "directory", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			service, options, candidate, stage := gcStagedRecovery(t)
			lock := filepath.Join(options.DataDir, ".install.lock")
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "directory":
				if err := os.Mkdir(lock, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(options.DataDir, "foreign-lock"), lock); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrDrift) {
				t.Fatalf("recover error=%v", err)
			}
			assertGCRecoveryEvidence(t, options.DataDir, stage, candidate)
			info, err := os.Lstat(lock)
			if mode == "missing" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing lock replaced: %v", err)
				}
			} else if err != nil || (mode == "directory" && !info.IsDir()) || (mode == "symlink" && info.Mode()&os.ModeSymlink == 0) {
				t.Fatalf("nonregular lock replaced: info=%v err=%v", info, err)
			}
		})
	}
}

func TestGCRecoverRejectsReplacedLockBeforeEveryMutationClass(t *testing.T) {
	cases := []struct {
		name         string
		state        gcState
		moved, empty bool
	}{
		{"journal-removal", gcStatePrepared, false, false},
		{"journal-removal-staged", gcStateStaged, false, false},
		{"journal-removal-deleting", gcStateDeleting, false, false},
		{"stage-publication", gcStateStaged, true, false},
		{"journal-advancement", gcStateDeleting, true, true},
		{"stage-removal", gcStateDeleted, true, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, options, candidate, stage := gcRecoveryFixture(t, test.state, test.moved, test.empty)
			var once sync.Once
			service.beforeGCRecoveryMutation = func() error {
				var replaceErr error
				once.Do(func() {
					lock := filepath.Join(options.DataDir, ".install.lock")
					if err := os.Remove(lock); err != nil {
						replaceErr = err
						return
					}
					replaceErr = os.WriteFile(lock, []byte("replacement"), 0o600)
				})
				return replaceErr
			}
			if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
				t.Fatalf("recover error=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(options.DataDir, gcJournalName)); err != nil {
				t.Fatalf("journal not retained: %v", err)
			}
			if test.moved {
				if _, err := os.Lstat(filepath.Join(options.DataDir, stage)); err != nil {
					t.Fatalf("stage changed: %v", err)
				}
				if !test.empty {
					assertGCRecoveryEvidence(t, options.DataDir, stage, candidate)
				}
			} else if digest, err := launcher.FileSHA256(filepath.Join(options.DataDir, "versions", candidate, executableName())); err != nil || digest != candidate {
				t.Fatalf("source executable=%s err=%v", digest, err)
			}
		})
	}
}

func TestGCRecoverRejectsReplacedRootBeforeMutation(t *testing.T) {
	service, options, candidate, stage := gcStagedRecovery(t)
	evidenceDir := options.DataDir + ".replaced"
	service.beforeGCRecoveryMutation = func() error {
		if err := os.Rename(options.DataDir, evidenceDir); err != nil {
			return err
		}
		return os.Mkdir(options.DataDir, 0o700)
	}
	if _, err := service.GCRecover(context.Background(), options); !errors.Is(err, ErrGCRecovery) {
		t.Fatalf("recover error=%v", err)
	}
	assertGCRecoveryEvidence(t, evidenceDir, stage, candidate)
}

func TestGCPlanUsesDarwinAnchoredCanonicalName(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin alias only")
	}
	path := t.TempDir()
	alternate := "/private" + path
	first, err := openInstallRoot(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := openInstallRoot(alternate, false)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	manifest := launcher.Manifest{ActiveSHA256: strings.Repeat("a", 64)}
	if got, want := gcPlanSHA256(first.Name(), []byte("m"), manifest, nil), gcPlanSHA256(second.Name(), []byte("m"), manifest, nil); got != want {
		t.Fatalf("plans %s %s", got, want)
	}
}

func gcThreeVersions(t *testing.T) (*Service, Options, string, string, string) {
	t.Helper()
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	first := New(Config{SourceExecutable: writeSource(t, root, "one", "one")})
	one, err := first.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second := New(Config{SourceExecutable: writeSource(t, root, "two", "two")})
	two, err := second.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	third := New(Config{SourceExecutable: writeSource(t, root, "three", "three")})
	three, err := third.Install(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return third, options, one.ActiveSHA256, two.ActiveSHA256, three.ActiveSHA256
}

func gcFourVersions(t *testing.T) (*Service, Options) {
	t.Helper()
	root := t.TempDir()
	options := Options{BinDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "data")}
	var service *Service
	for _, value := range []string{"one", "two", "three", "four"} {
		service = New(Config{SourceExecutable: writeSource(t, root, value, value)})
		if _, err := service.Install(context.Background(), options); err != nil {
			t.Fatal(err)
		}
	}
	return service, options
}

func replaceGCExecutable(t *testing.T, options Options, digest string, data []byte) {
	t.Helper()
	path := filepath.Join(options.DataDir, "versions", digest, executableName())
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o555); err != nil {
		t.Fatal(err)
	}
}

func replaceGCStageExecutable(t *testing.T, anchors installAnchors, stage string, data []byte) {
	t.Helper()
	root, err := anchors.data.OpenRoot(stage)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Chmod(executableName(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Remove(executableName()); err != nil {
		t.Fatal(err)
	}
	if err := writeRootFile(root, executableName(), data, 0o555); err != nil {
		t.Fatal(err)
	}
}

func gcStagedRecovery(t *testing.T) (*Service, Options, string, string) {
	return gcRecoveryFixture(t, gcStateStaged, true, false)
}

func gcRecoveryFixture(t *testing.T, state gcState, moved, empty bool) (*Service, Options, string, string) {
	t.Helper()
	service, options, candidate, _, _ := gcThreeVersions(t)
	anchors, err := openAnchors(mustResolvePaths(t, options))
	if err != nil {
		t.Fatal(err)
	}
	defer anchors.close()
	stage := ".gc-stage-00112233445566778899aabb"
	source := filepath.Join("versions", candidate)
	if moved {
		if err := anchors.data.Rename(source, stage); err != nil {
			t.Fatal(err)
		}
	}
	if empty {
		if err := anchors.data.Remove(filepath.Join(stage, executableName())); err != nil {
			t.Fatal(err)
		}
	}
	journal := gcJournal{SchemaVersion: "v1", State: state, PlanSHA256: strings.Repeat("a", 64), CandidateSHA256: candidate, Source: source, Stage: stage}
	if err := writeGCJournal(anchors.data, journal); err != nil {
		t.Fatal(err)
	}
	return service, options, candidate, stage
}

func assertGCRecoveryEvidence(t *testing.T, dataDir, stage, candidate string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dataDir, gcJournalName)); err != nil {
		t.Fatalf("journal not retained: %v", err)
	}
	if digest, err := launcher.FileSHA256(filepath.Join(dataDir, stage, executableName())); err != nil || digest != candidate {
		t.Fatalf("staged executable=%s err=%v", digest, err)
	}
}

func mustResolvePaths(t *testing.T, options Options) paths {
	t.Helper()
	value, err := resolvePaths(options)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

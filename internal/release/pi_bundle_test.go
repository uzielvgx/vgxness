package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/piartifact"
)

func bundleRepository(t *testing.T) string {
	t.Helper()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestPackagePiBundleRoundTrip(t *testing.T) {
	repository := bundleRepository(t)
	commit, err := repositoryHEAD(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "portable")
	if err := PackagePiBundle(context.Background(), repository, output, "v1.2.3", commit); err != nil {
		t.Fatal(err)
	}
	filename, err := piartifact.Filename("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := os.ReadFile(filepath.Join(output, filename))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := piartifact.Decode(asset)
	if err != nil {
		t.Fatal(err)
	}
	source, err := piSourceIdentity(repository)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Release.ReleaseVersion != "v1.2.3" || bundle.Release.Commit != commit || bundle.Release.PackageVersion != piartifact.PackageVersion || bundle.Release.SourceSHA256 != source {
		t.Fatalf("unexpected release: %#v", bundle.Release)
	}
	sum := sha256.Sum256(asset)
	checksums, err := os.ReadFile(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if string(checksums) != hex.EncodeToString(sum[:])+"  "+filename+"\n" {
		t.Fatalf("outer checksum = %q", checksums)
	}
}

func TestPackagePiBundleRejectsInvalidOrExistingOutput(t *testing.T) {
	repository := bundleRepository(t)
	commit, err := repositoryHEAD(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string][2]string{
		"invalid tag":     {"1.2.3", commit},
		"invalid commit":  {"v1.2.3", strings.Repeat("a", 39)},
		"mismatched head": {"v1.2.3", strings.Repeat("a", 40)},
	} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "portable")
			err := PackagePiBundle(context.Background(), repository, output, request[0], request[1])
			if err == nil {
				t.Fatal("PackagePiBundle accepted invalid request")
			}
			if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("output exists after rejected request: %v", statErr)
			}
		})
	}
	output := filepath.Join(t.TempDir(), "portable")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PackagePiBundle(context.Background(), repository, output, "v1.2.3", commit); err == nil {
		t.Fatal("PackagePiBundle overwrote existing output")
	}
}

func TestPackagePiBundleCancelledLeavesNoOutput(t *testing.T) {
	repository := bundleRepository(t)
	commit, err := repositoryHEAD(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := filepath.Join(t.TempDir(), "portable")
	err = PackagePiBundle(ctx, repository, output, "v1.2.3", commit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PackagePiBundle error = %v, want cancellation", err)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output exists after cancellation: %v", statErr)
	}
}

func TestRunPiBundleFlagPairing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunPi(context.Background(), []string{"--output", filepath.Join(t.TempDir(), "portable"), "--release-version", "v1.2.3"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unpaired release flags exit = %d, stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := RunPi(context.Background(), []string{"--output", filepath.Join(t.TempDir(), "portable"), "--release-version", "v1.2.3", "--commit", "bad"}, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid paired bundle flags exit = %d, stderr=%s", code, stderr.String())
	}
}

func TestPackagePiBundleCanonicalTemporaryParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink alias fixture is not portable on Windows")
	}
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "tmp-alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	repository := bundleRepository(t)
	commit, err := repositoryHEAD(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "portable")
	if err := PackagePiBundle(context.Background(), repository, output, "v1.2.3", commit); err != nil {
		t.Fatal(err)
	}
}

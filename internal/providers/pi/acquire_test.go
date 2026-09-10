package pi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/vgxness/vgxness/internal/piartifact"
	"github.com/vgxness/vgxness/internal/release"
)

type acquireTransport func(*http.Request) (*http.Response, error)

func (fn acquireTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func acquireFixture(t *testing.T, version string) ([]byte, []byte) {
	t.Helper()
	inner := []byte("tiny Pi package")
	source := strings.Repeat("a", 64)
	sum := sha256.Sum256(inner)
	bundle := piartifact.Bundle{Inner: inner, Provenance: []byte(`{"name":"@vgxness/pi","version":"0.1.0","sourceSHA256":"` + source + `","provenance":"local source snapshot; unpublished"}`), Checksums: []byte(hex.EncodeToString(sum[:]) + "  vgxness-pi-0.1.0.tgz\n"), Release: piartifact.Release{ReleaseVersion: version, Commit: strings.Repeat("b", 40), PackageVersion: piartifact.PackageVersion, SourceSHA256: source}}
	archive, err := piartifact.Encode(bundle)
	if err != nil {
		t.Fatal(err)
	}
	outer := sha256.Sum256(archive)
	asset, err := piartifact.Filename(version)
	if err != nil {
		t.Fatal(err)
	}
	return archive, []byte(hex.EncodeToString(outer[:]) + "  " + asset + "\n")
}

func fixtureClient(checksums, archive []byte) *http.Client {
	return &http.Client{Transport: acquireTransport(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch {
		case strings.HasSuffix(request.URL.Path, "/SHA256SUMS"):
			body = checksums
		case strings.HasSuffix(request.URL.Path, ".tar.gz"):
			body = archive
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})}
}

func TestAcquireReleaseSuccessAndCleanup(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(sums, archive))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(directory) == "." || cleanup == nil {
		t.Fatal("missing acquired release ownership")
	}
	if info, err := os.Stat(directory); err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("private directory = %v, %v", info, err)
	}
	if _, _, err := validateRelease(directory, ""); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 3 {
		t.Fatalf("release entries = %v, %v", entries, err)
	}
	for _, entry := range entries {
		if info, err := entry.Info(); err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatalf("private entry %s = %v, %v", entry.Name(), info, err)
		}
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left directory: %v", err)
	}
}

func TestAcquireReleaseRejectsRemoteFailures(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	for name, client := range map[string]*http.Client{
		"checksum mismatch":  fixtureClient([]byte(strings.Repeat("0", 64)+"  vgxness-pi_1.2.3_portable.tar.gz\n"), archive),
		"duplicate checksum": fixtureClient(append(sums, sums...), archive),
		"version mismatch": func() *http.Client {
			wrong, _ := acquireFixture(t, "v1.2.4")
			digest := sha256.Sum256(wrong)
			return fixtureClient([]byte(hex.EncodeToString(digest[:])+"  vgxness-pi_1.2.3_portable.tar.gz\n"), wrong)
		}(),
		"checksum limit": fixtureClient(bytes.Repeat([]byte("x"), acquireChecksumLimit+1), archive),
	} {
		t.Run(name, func(t *testing.T) {
			directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", client)
			if err == nil || directory != "" || cleanup != nil {
				t.Fatalf("acquire result = %q, cleanup-present=%t, %v", directory, cleanup != nil, err)
			}
		})
	}
	missing := &http.Client{Transport: acquireTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header), Request: request}, nil
	})}
	if directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", missing); err == nil || directory != "" || cleanup != nil {
		t.Fatalf("404 result = %q, cleanup-present=%t, %v", directory, cleanup != nil, err)
	}
}

func TestAcquireReleaseRejectsTruncatedArchiveCancellationAndRedirect(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	truncated := archive[:len(archive)-1]
	sum := sha256.Sum256(truncated)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  vgxness-pi_1.2.3_portable.tar.gz\n")
	if directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(checksums, truncated)); err == nil || directory != "" || cleanup != nil {
		t.Fatalf("truncated result = %q, cleanup-present=%t, %v", directory, cleanup != nil, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if directory, cleanup, err := acquireRelease(ctx, "v1.2.3", fixtureClient(sums, archive)); !errors.Is(err, context.Canceled) || directory != "" || cleanup != nil {
		t.Fatalf("cancelled result = %q, cleanup-present=%t, %v", directory, cleanup != nil, err)
	}
	redirect := &http.Client{Transport: acquireTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://example.invalid/asset"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	if directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", redirect); err == nil || directory != "" || cleanup != nil {
		t.Fatalf("redirect result = %q, cleanup-present=%t, %v", directory, cleanup != nil, err)
	}
}

func TestAcquireReleaseAllowsTrustedRedirectWithoutCredentials(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	var redirected int
	client := &http.Client{Transport: acquireTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "github.com" {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://release-assets.githubusercontent.com" + request.URL.Path}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		redirected++
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" || request.Header.Get("Proxy-Authorization") != "" {
			return nil, errors.New("credential forwarded")
		}
		if strings.HasSuffix(request.URL.Path, "/SHA256SUMS") {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(sums)), Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archive)), Request: request}, nil
	})}
	directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", client)
	if err != nil {
		t.Fatal(err)
	}
	if redirected != 2 {
		t.Fatalf("trusted redirects = %d", redirected)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup = %v", err)
	}
}

func TestAcquireReleaseRejectsArchiveLimitAndSanitizesTransportError(t *testing.T) {
	_, sums := acquireFixture(t, "v1.2.3")
	tooLarge := bytes.Repeat([]byte("x"), acquireArchiveLimit+1)
	directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(sums, tooLarge))
	if err == nil || directory != "" || cleanup != nil {
		t.Fatalf("archive limit result = %q cleanup-present=%t err=%v", directory, cleanup != nil, err)
	}
	secret := "signed-secret-token"
	broken := &http.Client{Transport: acquireTransport(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: "https://release-assets.githubusercontent.com/file?token=" + secret, Err: errors.New("network")}
	})}
	_, _, err = acquireRelease(context.Background(), "v1.2.3", broken)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("transport error exposed URL secret: %v", err)
	}
}

func TestAcquireReleaseCleanupPreservesReplacement(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(sums, archive))
	if err != nil {
		t.Fatal(err)
	}
	// Rename without deleting owned contents. Windows may protect the held
	// directory from replacement until cleanup closes its root handle.
	identity, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	original := make(map[string][32]byte)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		original[entry.Name()] = sha256.Sum256(data)
	}
	moved := filepath.Join(t.TempDir(), "original")
	t.Cleanup(func() { _ = cleanup(); _ = os.RemoveAll(directory) })
	if err := os.Rename(directory, moved); err != nil {
		if runtime.GOOS != "windows" || (!os.IsPermission(err) && !errors.Is(err, syscall.Errno(32))) {
			t.Fatal(err)
		}
		current, statErr := os.Stat(directory)
		if statErr != nil || !os.SameFile(identity, current) {
			t.Fatalf("protected root identity changed: %v", statErr)
		}
		for name, digest := range original {
			data, readErr := os.ReadFile(filepath.Join(directory, name))
			if readErr != nil || sha256.Sum256(data) != digest {
				t.Fatalf("protected root content changed: %s: %v", name, readErr)
			}
		}
		if err := cleanup(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("protected root cleanup: %v", err)
		}
		return
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(directory, "foreign")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err == nil || !strings.Contains(err.Error(), "retained") {
		t.Fatalf("cleanup error = %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "keep" {
		t.Fatalf("foreign replacement changed: %q, %v", data, err)
	}
}

func TestAcquireReleaseWriteFailureCleansOwnedDirectory(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	var directory string
	writer := func(root, name string, data []byte) error {
		directory = root
		return errors.New("injected write failure")
	}
	got, cleanup, err := acquireReleaseWithWriter(context.Background(), "v1.2.3", fixtureClient(sums, archive), writer)
	if err == nil || got != "" || cleanup != nil {
		t.Fatalf("failure result = %q cleanup-present=%t err=%v", got, cleanup != nil, err)
	}
	if _, statErr := os.Stat(directory); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("owned temporary directory remained: %v", statErr)
	}
}

func TestAcquireReleaseCleanupIsIdempotent(t *testing.T) {
	archive, sums := acquireFixture(t, "v1.2.3")
	directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(sums, archive))
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left directory: %v", err)
	}
}

func TestAcquireReleaseReportsVersionMismatchAfterChecksum(t *testing.T) {
	wrong, _ := acquireFixture(t, "v1.2.4")
	digest := sha256.Sum256(wrong)
	checksums := []byte(hex.EncodeToString(digest[:]) + "  vgxness-pi_1.2.3_portable.tar.gz\n")
	directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(checksums, wrong))
	if directory != "" || cleanup != nil || err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("version mismatch = %q cleanup-present=%t err=%v", directory, cleanup != nil, err)
	}
}

func TestAcquireReleaseInstallJourney(t *testing.T) {
	repository := filepath.Join("..", "..", "..")
	head, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	bundleRoot := t.TempDir()
	if err := release.PackagePiBundle(context.Background(), repository, filepath.Join(bundleRoot, "bundle"), "v1.2.3", strings.TrimSpace(string(head))); err != nil {
		t.Fatal(err)
	}
	asset, err := os.ReadFile(filepath.Join(bundleRoot, "bundle", "vgxness-pi_1.2.3_portable.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(bundleRoot, "bundle", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	acquired, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(sums, asset))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	if err := os.MkdirAll(agent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"foreign":true,"packages":["/foreign"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	userData := filepath.Join(agent, "user-data.txt")
	if err := os.WriteFile(userData, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	options := Options{ReleaseDir: acquired, AgentDir: agent, InstallRoot: filepath.Join(root, "managed"), GOOS: "linux", GOARCH: "amd64"}
	first, err := Install(context.Background(), options)
	if err != nil || !first.Changed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := Install(context.Background(), options)
	if err != nil || second.Changed {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	data, err := os.ReadFile(filepath.Join(agent, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil || string(settings["foreign"]) != "true" || !bytes.Contains(settings["packages"], []byte(`/foreign`)) {
		t.Fatalf("foreign settings lost: %s %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(first.PackagePath, "src", "probe.ts")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(userData); err != nil || string(got) != "keep" {
		t.Fatalf("user data=%q %v", got, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(acquired); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("acquired cleanup=%v", err)
	}
}

func TestAcquireCleanupReplacementAfterIdentityCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows prohibits renaming this open-root fixture")
	}
	parent := t.TempDir()
	directory := filepath.Join(parent, "acquired")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SHA256SUMS"), []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := os.Lstat(directory)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := root.Lstat("SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(parent, "moved")
	cleanup := ownedCleanup(directory, identity, root, map[string]acquiredIdentity{"SHA256SUMS": {info: fileInfo, digest: sha256.Sum256([]byte("owned"))}}, func() {
		if err := os.Rename(directory, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SHA256SUMS"), []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := cleanup(); err == nil || !strings.Contains(err.Error(), "ownership changed") {
		t.Fatalf("cleanup = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS")); err != nil || string(data) != "foreign" {
		t.Fatalf("replacement changed: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(moved, "SHA256SUMS")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("held-root cleanup did not remove owned file: %v", err)
	}
}

func TestAcquireCleanupPreservesUnknownAndChangedFiles(t *testing.T) {
	for _, change := range []string{"unknown", "changed", "replaced"} {
		t.Run(change, func(t *testing.T) {
			archive, sums := acquireFixture(t, "v1.2.3")
			directory, cleanup, err := acquireRelease(context.Background(), "v1.2.3", fixtureClient(sums, archive))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(directory) })
			name := "SHA256SUMS"
			if change == "unknown" {
				name = "foreign"
			}
			path := filepath.Join(directory, name)
			if change == "replaced" {
				if err := os.Rename(path, filepath.Join(directory, "retained-original")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := cleanup(); err == nil {
				t.Fatal("cleanup must report retained foreign/changed file")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
				t.Fatalf("foreign file changed: %q %v", data, err)
			}
		})
	}
}

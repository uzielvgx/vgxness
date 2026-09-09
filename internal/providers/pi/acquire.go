package pi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vgxness/vgxness/internal/piartifact"
)

const (
	acquireChecksumLimit = 64 << 10
	acquireArchiveLimit  = 64 << 20
)

// AcquireRelease downloads and validates one pinned portable Pi release. The
// returned directory is private temporary storage and must be released with cleanup.
func AcquireRelease(ctx context.Context, version string) (directory string, cleanup func() error, err error) {
	client := &http.Client{Timeout: 45 * time.Second}
	return acquireRelease(ctx, version, client)
}

func acquireRelease(ctx context.Context, version string, client *http.Client) (directory string, cleanup func() error, err error) {
	return acquireReleaseWithWriter(ctx, version, client, writeAcquiredFile)
}

func acquireReleaseWithWriter(ctx context.Context, version string, client *http.Client, writer func(string, string, []byte) error) (directory string, cleanup func() error, err error) {
	asset, err := piartifact.Filename(version)
	if err != nil {
		return "", nil, err
	}
	if client == nil {
		return "", nil, errors.New("acquisition client required")
	}
	clientCopy := *client
	clientCopy.Timeout = 45 * time.Second
	clientCopy.Jar = nil
	clientCopy.CheckRedirect = safeReleaseRedirect
	base := "https://github.com/uzielvgx/vgxness/releases/download/" + url.PathEscape(version) + "/"
	checksums, err := downloadBounded(ctx, &clientCopy, base+"SHA256SUMS", acquireChecksumLimit)
	if err != nil {
		return "", nil, err
	}
	expected, err := acquireChecksum(checksums, asset)
	if err != nil {
		return "", nil, err
	}
	archive, err := downloadBounded(ctx, &clientCopy, base+asset, acquireArchiveLimit)
	if err != nil {
		return "", nil, err
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expected {
		return "", nil, errors.New("Pi release archive checksum mismatch")
	}
	bundle, err := piartifact.Decode(archive)
	if err != nil {
		return "", nil, fmt.Errorf("invalid Pi release bundle: %w", err)
	}
	if bundle.Release.ReleaseVersion != version || bundle.Release.PackageVersion != piartifact.PackageVersion {
		return "", nil, errors.New("Pi release bundle version mismatch")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	tempParent, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", nil, fmt.Errorf("resolve Pi acquisition temporary directory: %w", err)
	}
	directory, err = os.MkdirTemp(tempParent, ".vgxness-pi-acquire-")
	if err != nil {
		return "", nil, fmt.Errorf("create Pi acquisition directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.Remove(directory)
		return "", nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(directory)
		return "", nil, errors.New("unsafe Pi acquisition directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		_ = os.Remove(directory)
		return "", nil, err
	}
	files := make(map[string]acquiredIdentity)
	owned := ownedCleanup(directory, info, root, files, nil)
	cleanup = owned
	failed := true
	defer func() {
		if failed {
			if cleanErr := owned(); cleanErr != nil {
				err = errors.Join(err, cleanErr)
			}
			directory, cleanup = "", nil
		}
	}()
	for _, file := range []struct {
		name string
		data []byte
	}{{"PROVENANCE.json", bundle.Provenance}, {"SHA256SUMS", bundle.Checksums}, {"vgxness-pi-" + piartifact.PackageVersion + ".tgz", bundle.Inner}} {
		if err := writer(directory, file.name, file.data); err != nil {
			return "", nil, err
		}
		fileInfo, statErr := root.Lstat(file.name)
		data, readErr := root.ReadFile(file.name)
		if statErr != nil || readErr != nil || !fileInfo.Mode().IsRegular() || !bytes.Equal(data, file.data) {
			return "", nil, errors.New("Pi acquisition ownership readback mismatch")
		}
		files[file.name] = acquiredIdentity{info: fileInfo, digest: sha256.Sum256(data)}
	}
	if _, _, err := validateRelease(directory, ""); err != nil {
		return "", nil, fmt.Errorf("validate acquired Pi release: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	failed = false
	return directory, cleanup, nil
}

func downloadBounded(ctx context.Context, client *http.Client, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, errors.New("Pi release transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Pi release download status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("Pi release download read failed")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("Pi release download exceeds size limit")
	}
	return data, nil
}

func safeReleaseRedirect(request *http.Request, via []*http.Request) error {
	request.Header.Del("Authorization")
	request.Header.Del("Cookie")
	request.Header.Del("Proxy-Authorization")
	if len(via) >= 5 {
		return errors.New("too many Pi release redirects")
	}
	if request.URL == nil || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Port() != "" {
		return errors.New("unsafe Pi release redirect")
	}
	if request.URL.Hostname() != "github.com" && request.URL.Hostname() != "release-assets.githubusercontent.com" {
		return errors.New("unsafe Pi release redirect")
	}
	return nil
}

func acquireChecksum(data []byte, asset string) (string, error) {
	if !bytes.HasSuffix(data, []byte("\n")) {
		return "", errors.New("invalid Pi release checksums")
	}
	var result string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 || !validHex(parts[0]) || parts[1] == "" {
			return "", errors.New("invalid Pi release checksums")
		}
		if parts[1] == asset {
			if result != "" {
				return "", errors.New("duplicate Pi release checksum")
			}
			result = parts[0]
		}
	}
	if result == "" {
		return "", errors.New("missing Pi release checksum")
	}
	return result, nil
}

func writeAcquiredFile(directory, name string, data []byte) error {
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	readback, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(readback, data) {
		return errors.New("Pi acquisition file readback mismatch")
	}
	return nil
}

// acquiredIdentity binds cleanup to the files observed after successful writes.
// A partial failed write is deliberately retained if it cannot be identified.
type acquiredIdentity struct {
	info   os.FileInfo
	digest [sha256.Size]byte
}

func ownedCleanup(directory string, identity os.FileInfo, root *os.Root, files map[string]acquiredIdentity, afterCheck func()) func() error {
	var once sync.Once
	var result error
	return func() error {
		once.Do(func() {
			closed := false
			defer func() {
				if !closed {
					_ = root.Close()
				}
			}()
			checkDirectory := func() bool {
				held, err := root.Stat(".")
				info, statErr := os.Lstat(directory)
				return err == nil && statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && os.SameFile(info, identity) && os.SameFile(info, held)
			}
			if !checkDirectory() {
				result = fmt.Errorf("Pi acquisition retained at %s: ownership changed", directory)
				return
			}
			if afterCheck != nil {
				afterCheck()
			}
			// Never recurse through the mutable directory pathname. Held-root
			// operations remain in the acquired tree if it is renamed mid-cleanup.
			for name, owned := range files {
				info, err := root.Lstat(name)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, owned.info) {
					result = errors.Join(result, fmt.Errorf("Pi acquisition retained at %s: file identity changed: %s", directory, name))
					continue
				}
				data, err := root.ReadFile(name)
				if err != nil || sha256.Sum256(data) != owned.digest {
					result = errors.Join(result, fmt.Errorf("Pi acquisition retained at %s: file content changed: %s", directory, name))
					continue
				}
				if err := root.Remove(name); err != nil {
					result = errors.Join(result, err)
				}
			}
			if !checkDirectory() {
				result = errors.Join(result, fmt.Errorf("Pi acquisition retained at %s: ownership changed", directory))
				return
			}
			// Windows does not permit deleting a directory while this root
			// handle is open. Identity was checked above; release it before
			// the final nonrecursive removal on every platform.
			closed = true
			if err := root.Close(); err != nil {
				result = errors.Join(result, fmt.Errorf("Pi acquisition retained at %s: close root: %w", directory, err))
				return
			}
			// Nonrecursive removal preserves unrecognized entries. Portable Go
			// offers no atomic compare-identity-and-unlink against same-UID races.
			if err := os.Remove(directory); err != nil {
				result = errors.Join(result, fmt.Errorf("Pi acquisition retained at %s: %w", directory, err))
			}
		})
		return result
	}
}

// Package pi provisions the published Pi extension without invoking npm or a
// VGXNESS runtime. Its only input is a locally assembled release directory.
package pi

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vgxness/vgxness/internal/agentmodels"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const version = "0.1.0"

const (
	maxArchiveBytes = int64(64 << 20)
	maxMemberBytes  = int64(32 << 20)
	maxTreeBytes    = int64(96 << 20)
)

type Options struct {
	Models              *agentmodels.Config
	expectedSettingsSHA string
	ReleaseDir          string
	AgentDir            string
	InstallRoot         string
	// GOOS and GOARCH are retained for caller compatibility; portable artifacts do not select a target.
	GOOS   string
	GOARCH string
}

type Result struct {
	PackagePath string
	Changed     bool
	State       string
}

type recoveryError struct {
	state, path string
	err         error
}

func (e recoveryError) Error() string {
	return fmt.Sprintf("Pi recovery %s retained at %s: %v", e.state, e.path, e.err)
}
func (e recoveryError) Unwrap() error { return e.err }
func retained(state, path string, err error) error {
	return recoveryError{state: state, path: path, err: err}
}
func recoveryDetail(err error) (string, bool) {
	var value recoveryError
	if errors.As(err, &value) {
		return value.state + " retained at " + value.path, true
	}
	return "", false
}

type managedManifest struct {
	Schema      int               `json:"schema"`
	ManagedBy   string            `json:"managedBy"`
	SourceSHA   string            `json:"sourceSHA256"`
	PackagePath string            `json:"packagePath"`
	Files       map[string]string `json:"files"`
}

type rootManifest struct {
	Schema    int    `json:"schema"`
	ManagedBy string `json:"managedBy"`
}

type reservationManifest struct {
	Schema      int    `json:"schema"`
	ManagedBy   string `json:"managedBy"`
	SourceSHA   string `json:"sourceSHA256"`
	PackagePath string `json:"packagePath"`
}

const rootManifestName = ".vgxness-pi-root.json"

var installBoundaryHook func(string) error
var reservationOperationHook func(string) error

func installBoundary(name string) error {
	if installBoundaryHook != nil {
		return installBoundaryHook(name)
	}
	return nil
}
func reservationOperation(name string) error {
	if reservationOperationHook != nil {
		return reservationOperationHook(name)
	}
	return nil
}

func normalize(options Options) (Options, string, error) {
	if options.Models != nil {
		if err := options.Models.Validate(); err != nil {
			return Options{}, "", err
		}
	}
	if options.ReleaseDir == "" || options.AgentDir == "" || options.InstallRoot == "" || !filepath.IsAbs(options.ReleaseDir) || !filepath.IsAbs(options.AgentDir) || !filepath.IsAbs(options.InstallRoot) {
		return Options{}, "", errors.New("Pi release, agent, and managed roots must be absolute")
	}
	for _, path := range []string{options.ReleaseDir, options.AgentDir, options.InstallRoot} {
		if err := noLinkAncestors(path); err != nil {
			return Options{}, "", errors.New("symlinked Pi path")
		}
	}
	return options, "portable-typescript", nil
}

func Install(ctx context.Context, options Options) (Result, error) {
	return install(ctx, options, "")
}

// install binds an accepted preview source before it creates any installer
// artifact.  The exported entry point remains useful to package tests.
func install(ctx context.Context, options Options, expectedSource string) (result Result, resultErr error) {
	options, target, err := normalize(options)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	source, sums, err := validateRelease(options.ReleaseDir, target)
	if err != nil {
		return Result{}, err
	}
	if expectedSource != "" && source != expectedSource {
		return Result{}, errors.New("Pi release no longer matches accepted plan")
	}
	if _, err := preflightSettings(options.AgentDir, options.InstallRoot, ""); err != nil {
		return Result{}, err
	}
	digest, _, modelsChanged, modelErr := modelSettings(options.AgentDir, options.Models)
	if modelErr != nil {
		return Result{}, modelErr
	}
	if options.expectedSettingsSHA != "" && options.expectedSettingsSHA != digest {
		return Result{}, errors.New("Pi settings changed since preview")
	}
	if err := claimInstallRoot(options.InstallRoot); err != nil {
		return Result{}, err
	}
	key := source[:16]
	destination := filepath.Join(options.InstallRoot, "packages", "pi-"+version+"-"+key)
	packagePath := filepath.Join(destination, "package")
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Result{}, errors.New("managed Pi destination is unsafe")
		}
		if err := verifyManaged(packagePath, source); err != nil {
			if !recoverReservation(destination, packagePath, source) {
				return Result{}, fmt.Errorf("managed Pi destination is occupied: %w", err)
			}
		} else {
			if err := activate(ctx, options.AgentDir, options.InstallRoot, packagePath, activationModels{options.Models, options.expectedSettingsSHA}); err != nil {
				return Result{PackagePath: packagePath, State: "published-inactive"}, retained("published-inactive", packagePath, err)
			}
			if err := finalizeReservation(destination, packagePath, source); err != nil {
				return Result{PackagePath: packagePath, State: "activated-finalization-pending"}, retained("activated-finalization-pending", filepath.Join(destination, ".vgxness-pi-reservation"), err)
			}
			return Result{PackagePath: packagePath, State: "installed", Changed: modelsChanged}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Join(options.InstallRoot, "packages"), 0o700); err != nil {
		return Result{}, err
	}
	stage, err := os.MkdirTemp(filepath.Join(options.InstallRoot, "packages"), ".pi-stage-")
	if err != nil {
		return Result{}, err
	}
	keep := true
	recoveryState, recoveryPath := "staged", stage
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
		if resultErr != nil && recoveryState != "" {
			if _, known := recoveryDetail(resultErr); !known {
				resultErr = retained(recoveryState, recoveryPath, resultErr)
			}
		}
	}()
	if err := installBoundary("staged"); err != nil {
		return Result{}, err
	}
	for _, file := range []string{"vgxness-pi-" + version + ".tgz"} {
		if err := extract(filepath.Join(options.ReleaseDir, file), stage, sums[file]); err != nil {
			return Result{}, err
		}
		if sums[file] == "" {
			return Result{}, errors.New("release checksum missing")
		}
	}
	if err := verifyPackage(stage, target); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if reread, _, err := validateRelease(options.ReleaseDir, target); err != nil || reread != source {
		if err != nil {
			return Result{}, err
		}
		return Result{}, errors.New("Pi release changed during staging")
	}
	files, err := treeHashes(stage)
	if err != nil {
		return Result{}, err
	}
	manifest, _ := json.Marshal(managedManifest{Schema: 1, ManagedBy: "vgxness", SourceSHA: source, PackagePath: packagePath, Files: files})
	if err := os.WriteFile(filepath.Join(stage, ".vgxness-pi.json"), append(manifest, '\n'), 0o600); err != nil {
		return Result{}, err
	}
	// Reserving the destination first is intentionally separate from stage
	// publication: Rename may replace an empty directory on Unix.  Once the
	// exclusive reservation exists, only this transaction can publish below it.
	if err := os.Mkdir(destination, 0o700); err != nil {
		return Result{}, fmt.Errorf("reserve Pi package destination: %w", err)
	}
	reservation, _ := json.Marshal(reservationManifest{Schema: 1, ManagedBy: "vgxness", SourceSHA: source, PackagePath: packagePath})
	reservation = append(reservation, '\n')
	reservationPath := filepath.Join(destination, ".vgxness-pi-reservation")
	if err := reservationOperation("write"); err != nil {
		return Result{}, retained("reservation-unproven", stage+" and "+destination, err)
	}
	if err := os.WriteFile(reservationPath, reservation, 0o600); err != nil {
		return Result{}, retained("reservation-unproven", stage+" and "+destination, err)
	}
	recoveryState, recoveryPath = "reserved", destination
	if err := installBoundary("reserved"); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{PackagePath: packagePath, State: "reserved"}, retained("reserved", destination, err)
	}
	if err := os.Rename(stage, packagePath); err != nil {
		return Result{}, fmt.Errorf("publish Pi package without overwrite: %w", err)
	}
	keep = false
	recoveryState, recoveryPath = "published-inactive", packagePath
	if err := installBoundary("published-inactive"); err != nil {
		return Result{}, err
	}
	if err := verifyManaged(packagePath, source); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{PackagePath: packagePath, State: "published-inactive"}, retained("published-inactive", packagePath, err)
	}
	if err := activate(ctx, options.AgentDir, options.InstallRoot, packagePath, activationModels{options.Models, options.expectedSettingsSHA}); err != nil {
		return Result{}, err
	}
	if err := finalizeReservation(destination, packagePath, source); err != nil {
		return Result{}, retained("activated-finalization-pending", reservationPath, err)
	}
	return Result{PackagePath: packagePath, Changed: true, State: "installed"}, nil
}

func finalizeReservation(destination, packagePath, source string) error {
	path := filepath.Join(destination, ".vgxness-pi-reservation")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("invalid Pi reservation marker")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var marker reservationManifest
	if strictJSON(data, &marker) != nil || marker.Schema != 1 || marker.ManagedBy != "vgxness" || marker.SourceSHA != source || marker.PackagePath != packagePath {
		return errors.New("invalid Pi reservation marker")
	}
	if err := reservationOperation("remove"); err != nil {
		return err
	}
	return os.Remove(path)
}

// recoverReservation removes only our exact, incomplete reservation. A retry
// never deletes a foreign directory or a publication whose identity differs.
func recoverReservation(destination, packagePath, source string) bool {
	if _, err := os.Lstat(packagePath); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	markerPath := filepath.Join(destination, ".vgxness-pi-reservation")
	info, err := os.Lstat(markerPath)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	var reservation reservationManifest
	if strictJSON(data, &reservation) != nil || reservation.Schema != 1 || reservation.ManagedBy != "vgxness" || reservation.SourceSHA != source || reservation.PackagePath != packagePath {
		return false
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".vgxness-pi-reservation" {
		return false
	}
	if err := reservationOperation("recover"); err != nil {
		return false
	}
	if err := os.Remove(markerPath); err != nil {
		return false
	}
	return os.Remove(destination) == nil
}

func validateRelease(dir, target string) (string, map[string]string, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("Pi release directory is unsafe")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	expected := map[string]bool{"PROVENANCE.json": true, "SHA256SUMS": true, "vgxness-pi-" + version + ".tgz": true}
	if len(entries) != len(expected) {
		return "", nil, errors.New("Pi release has unexpected assets")
	}
	for _, entry := range entries {
		if !expected[entry.Name()] || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return "", nil, errors.New("Pi release contains unsafe member")
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return "", nil, err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || !validHex(parts[0]) || sums[parts[1]] != "" {
			return "", nil, errors.New("invalid Pi checksums")
		}
		sums[parts[1]] = parts[0]
	}
	if len(sums) != len(expected)-2 {
		return "", nil, errors.New("unexpected Pi checksum asset")
	}
	for name := range expected {
		if name != "PROVENANCE.json" && name != "SHA256SUMS" && sums[name] == "" {
			return "", nil, fmt.Errorf("missing release asset %s", name)
		}
	}
	for name, digest := range sums {
		asset, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || checksum(asset) != digest {
			return "", nil, fmt.Errorf("Pi checksum mismatch: %s", name)
		}
	}
	var provenance struct{ Name, Version, SourceSHA256, Provenance string }
	data, err = os.ReadFile(filepath.Join(dir, "PROVENANCE.json"))
	if err != nil || strictObjectFields(data, map[string]bool{"name": true, "version": true, "sourceSHA256": true, "provenance": true}) != nil || strictJSON(data, &provenance) != nil || provenance.Name != "@vgxness/pi" || provenance.Version != version || !validHex(provenance.SourceSHA256) || provenance.Provenance != "local source snapshot; unpublished" {
		return "", nil, errors.New("invalid Pi provenance")
	}
	main := "vgxness-pi-" + version + ".tgz"
	// Bind the single portable package bytes as well as source provenance.
	return checksum([]byte(provenance.SourceSHA256 + "\nportable-typescript\n" + sums[main])), sums, nil
}

func extract(archive, root string, expected string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	limited := &io.LimitedReader{R: f, N: maxArchiveBytes + 1}
	gz, err := gzip.NewReader(io.TeeReader(limited, hash))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	total := int64(0)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || h.Mode != 0o644 || h.Size < 0 || h.Size > 32<<20 || !strings.HasPrefix(h.Name, "package/") || strings.Contains(h.Name, "..") || filepath.IsAbs(h.Name) || seen[h.Name] {
			return errors.New("unsafe Pi archive")
		}
		seen[h.Name] = true
		total += h.Size
		if total > maxTreeBytes {
			return errors.New("Pi archive exceeds size limit")
		}
		rel := strings.TrimPrefix(h.Name, "package/")
		path := filepath.Join(root, rel)
		if !within(root, path) {
			return errors.New("unsafe Pi archive path")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(h.Mode)&0o777)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(tr, h.Size+1))
		closeErr := out.Close()
		if written != h.Size || copyErr != nil || closeErr != nil {
			return fmt.Errorf("extract Pi archive")
		}
	}
	if _, err := io.Copy(io.Discard, gz); err != nil || limited.N == 0 || hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("Pi archive changed during extraction")
	}
	return nil
}

func verifyPackage(root, target string) error {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return err
	}
	var main struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Type    string `json:"type"`
		Pi      struct {
			Extensions []string `json:"extensions"`
		} `json:"pi"`
		Engines map[string]string `json:"engines"`
		Peers   map[string]string `json:"peerDependencies"`
		Files   []string          `json:"files"`
	}
	if strictObjectFields(data, map[string]bool{"name": true, "version": true, "type": true, "pi": true, "engines": true, "peerDependencies": true, "files": true}) != nil || strictJSON(data, &main) != nil || main.Name != "@vgxness/pi" || main.Version != version || main.Type != "module" || len(main.Pi.Extensions) != 1 || main.Pi.Extensions[0] != "./src/extension.ts" || main.Engines["node"] != ">=22.19.0" || len(main.Peers) != 2 || main.Peers["typebox"] != "1.3.7" || main.Peers["@earendil-works/pi-coding-agent"] != "^0.84.4" {
		return errors.New("invalid portable Pi package metadata")
	}
	for _, name := range []string{"src/extension.ts", "src/probe.ts", "resources/migrations/manifest.json", "resources/prompts/manager.md", "resources/skills/memory-sync/SKILL.md", "LICENSE"} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("Pi package missing runtime resource")
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "resources", "migrations"))
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".sql") {
			count++
		}
	}
	if count != 23 {
		return errors.New("Pi package must contain exactly 23 SQL migrations")
	}
	data, err = os.ReadFile(filepath.Join(root, "resources", "migrations", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest struct {
		Version    int
		Migrations []struct {
			Version      int
			File, SHA256 string
		}
	}
	if strictJSON(data, &manifest) != nil || manifest.Version != 23 || len(manifest.Migrations) != 23 {
		return errors.New("invalid Pi migration manifest")
	}
	seen := map[string]bool{}
	for index, migration := range manifest.Migrations {
		if migration.Version != index+1 || filepath.Base(migration.File) != migration.File || !strings.HasSuffix(migration.File, ".sql") || seen[migration.File] {
			return errors.New("invalid Pi migration manifest")
		}
		seen[migration.File] = true
		data, err := os.ReadFile(filepath.Join(root, "resources", "migrations", migration.File))
		if err != nil || checksum(data) != migration.SHA256 {
			return errors.New("Pi migration hash mismatch")
		}
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.Name() == "node_modules" || entry.Name() == "bin" || strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".exe") {
			return errors.New("non-portable Pi package member")
		}
		return nil
	})
}

type activationModels struct {
	models   *agentmodels.Config
	expected string
}

func activate(ctx context.Context, agent, installRoot, packagePath string, selection ...activationModels) error {
	var models *agentmodels.Config
	var expected string
	if len(selection) > 0 {
		models, expected = selection[0].models, selection[0].expected
	}

	if err := noLinkAncestors(agent); err != nil {
		return err
	}
	if err := os.MkdirAll(agent, 0o700); err != nil {
		return err
	}
	checkLock, release, err := lockSettings(ctx, filepath.Join(agent, "settings.json"))
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	value, err := preflightSettings(agent, installRoot, packagePath)
	if err != nil {
		return err
	}
	settings := filepath.Join(agent, "settings.json")
	before := []byte(nil)
	if info, err := os.Lstat(settings); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlinked Pi settings")
	} else if data, err := os.ReadFile(settings); err == nil {
		before = data
		if json.Unmarshal(data, &value) != nil || value == nil {
			return errors.New("malformed Pi settings")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var packages []json.RawMessage
	if raw := value["packages"]; raw != nil && json.Unmarshal(raw, &packages) != nil {
		return errors.New("malformed Pi packages settings")
	}
	count := 0
	for _, existing := range packages {
		if path, ok := packageEntryPath(existing); ok && path == packagePath {
			count++
		}
	}
	if count > 1 {
		return errors.New("ambiguous Pi managed package entries")
	}
	if expected != "" && settingsDigest(before) != expected {
		return errors.New("Pi settings changed since preview")
	}
	if models != nil {
		if err := models.Validate(); err != nil {
			return err
		}
		applyModelSettings(value, *models)
	}
	if count == 1 && models == nil {
		return nil
	}
	for index, existing := range packages {
		if path, ok := packageEntryPath(existing); ok && path != packagePath && isManagedPackagePath(path, installRoot) {
			if err := verifyManaged(path, ""); err != nil {
				return fmt.Errorf("managed Pi predecessor drift: %w", err)
			}
			replaced, err := replacePackageEntry(existing, packagePath)
			if err != nil {
				return err
			}
			packages[index] = replaced
			count++
		}
	}
	if count == 0 {
		packages = append(packages, jsonString(packagePath))
	}
	encoded, _ := json.Marshal(packages)
	value["packages"] = encoded
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if string(before) == string(data) {
		return nil
	}
	tmp, err := os.CreateTemp(agent, ".settings-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0o600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return err
	}
	current, readErr := os.ReadFile(settings)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		_ = os.Remove(name)
		return readErr
	}
	if string(current) != string(before) {
		_ = os.Remove(name)
		return errors.New("Pi settings changed before activation; recovery retained")
	}
	if err := checkLock(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, settings); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("publish Pi settings: %w", err)
	}
	return nil
}

// Pi itself uses proper-lockfile on settings.json. Its portable lock is the
// sibling .lock directory, so cooperate with it and refuse a live lock rather
// than replacing another writer's settings. This protects cooperative writers
// during the short final write; it cannot make arbitrary filesystem actors or
// host scheduling atomic. The recorded mtime prevents removing a replaced lock.
func lockSettings(ctx context.Context, settings string) (func() error, func() error, error) {
	lock := settings + ".lock"
	for attempt := 0; attempt < 10; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := os.Mkdir(lock, 0o700); err == nil {
			info, err := os.Stat(lock)
			if err != nil {
				_ = os.Remove(lock)
				return nil, nil, err
			}
			mtime := info.ModTime()
			var mutex sync.Mutex
			compromised := false
			stop, done := make(chan struct{}), make(chan struct{})
			go func() {
				ticker := time.NewTicker(250 * time.Millisecond)
				defer ticker.Stop()
				defer close(done)
				for {
					select {
					case <-stop:
						return
					case <-ticker.C:
						mutex.Lock()
						current, statErr := os.Stat(lock)
						if statErr != nil || !current.ModTime().Equal(mtime) {
							compromised = true
						} else {
							now := time.Now()
							if os.Chtimes(lock, now, now) != nil {
								compromised = true
							} else {
								mtime = now
							}
						}
						mutex.Unlock()
					}
				}
			}()
			check := func() error {
				mutex.Lock()
				defer mutex.Unlock()
				if compromised {
					return errors.New("Pi settings lock was compromised; recovery retained")
				}
				current, err := os.Stat(lock)
				if err != nil || !current.ModTime().Equal(mtime) {
					return errors.New("Pi settings lock was compromised; recovery retained")
				}
				return nil
			}
			release := func() error {
				close(stop)
				<-done
				if err := check(); err != nil {
					return err
				}
				return os.Remove(lock)
			}
			return check, release, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, nil, errors.New("Pi settings are locked; retry after Pi finishes writing")
}

// preflightSettings rejects malformed, duplicate, and drifted predecessor
// state before the installer creates a staging directory or reservation.
func preflightSettings(agent, installRoot, packagePath string) (map[string]json.RawMessage, error) {
	settings := filepath.Join(agent, "settings.json")
	if info, err := os.Lstat(settings); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("symlinked Pi settings")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	value := map[string]json.RawMessage{}
	if data, err := os.ReadFile(settings); err == nil {
		if json.Unmarshal(data, &value) != nil || value == nil {
			return nil, errors.New("malformed Pi settings")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var packages []json.RawMessage
	if raw := value["packages"]; raw != nil && json.Unmarshal(raw, &packages) != nil {
		return nil, errors.New("malformed Pi packages settings")
	}
	managed, exact := 0, 0
	for _, existing := range packages {
		path, ok := packageEntryPath(existing)
		if !ok {
			continue
		}
		if path == packagePath {
			exact++
		}
		if isManagedPackagePath(path, installRoot) {
			managed++
			if err := verifyManaged(path, ""); err != nil {
				return nil, fmt.Errorf("managed Pi predecessor drift: %w", err)
			}
		}
	}
	if exact > 1 || managed > 1 {
		return nil, errors.New("ambiguous Pi managed package entries")
	}
	return value, nil
}

func isManagedPackagePath(path, installRoot string) bool {
	base := filepath.Join(installRoot, "packages")
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) == 2 && strings.HasPrefix(parts[0], "pi-"+version+"-") && parts[1] == "package"
}

func replacePackageEntry(raw json.RawMessage, path string) (json.RawMessage, error) {
	var stringPath string
	if json.Unmarshal(raw, &stringPath) == nil {
		return jsonString(path), nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, errors.New("malformed Pi package entry")
	}
	for _, key := range []string{"path", "source"} {
		if object[key] != nil {
			object[key] = jsonString(path)
			return json.Marshal(object)
		}
	}
	return nil, errors.New("malformed Pi package entry")
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 64 && err == nil && strings.ToLower(value) == value
}

// strictJSON rejects duplicate keys before decoding. JSON's ordinary decoder
// accepts duplicates and silently lets the final value win, which is unsafe for
// release provenance and selected package identity.
func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := rejectDuplicateJSON(decoder); err != nil {
		return err
	}
	decoder = json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing JSON")
	}
	return nil
}

func strictObjectFields(data []byte, allowed map[string]bool) error {
	var object map[string]json.RawMessage
	if err := strictJSON(data, &object); err != nil || object == nil {
		return errors.New("invalid JSON object")
	}
	for key := range object {
		if !allowed[key] {
			return errors.New("unexpected JSON field")
		}
	}
	return nil
}

func rejectDuplicateJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate JSON key")
				}
				seen[name] = true
				if err := rejectDuplicateJSON(decoder); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := rejectDuplicateJSON(decoder); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		}
	}
	return nil
}

func claimInstallRoot(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
			return err
		}
		if err := noLinkAncestors(root); err != nil {
			return errors.New("symlinked Pi path")
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			return err
		}
		data, _ := json.Marshal(rootManifest{Schema: 1, ManagedBy: "vgxness"})
		return os.WriteFile(filepath.Join(root, rootManifestName), append(data, '\n'), 0o600)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("Pi install root is unsafe")
	}
	data, err := os.ReadFile(filepath.Join(root, rootManifestName))
	if errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(root)
		if readErr != nil || len(entries) != 0 {
			return errors.New("Pi install root is occupied and not owned by vgxness")
		}
		encoded, _ := json.Marshal(rootManifest{Schema: 1, ManagedBy: "vgxness"})
		return os.WriteFile(filepath.Join(root, rootManifestName), append(encoded, '\n'), 0o600)
	}
	if err != nil {
		return err
	}
	var manifest rootManifest
	if strictJSON(data, &manifest) != nil || manifest.Schema != 1 || manifest.ManagedBy != "vgxness" {
		return errors.New("invalid Pi install root ownership")
	}
	return nil
}

func verifyManaged(root, source string) error {
	data, err := os.ReadFile(filepath.Join(root, ".vgxness-pi.json"))
	if err != nil {
		return err
	}
	var manifest managedManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != 1 || manifest.ManagedBy != "vgxness" || (source != "" && manifest.SourceSHA != source) || manifest.PackagePath != root {
		return errors.New("invalid Pi managed manifest")
	}
	files, err := treeHashes(root)
	if err != nil {
		return err
	}
	delete(files, ".vgxness-pi.json")
	if len(files) != len(manifest.Files) {
		return errors.New("Pi managed files drifted")
	}
	for path, digest := range manifest.Files {
		if files[path] != digest {
			return errors.New("Pi managed files drifted")
		}
	}
	return nil
}
func treeHashes(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in Pi managed root")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("non-regular Pi managed file")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = checksum(data)
		return nil
	})
	return files, err
}
func checksum(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func noLinkAncestors(path string) error {
	for path = filepath.Clean(path); ; path = filepath.Dir(path) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinked path ancestor")
		}
		if filepath.Dir(path) == path {
			return nil
		}
	}
}
func packageEntryPath(raw json.RawMessage) (string, bool) {
	var path string
	if json.Unmarshal(raw, &path) == nil {
		return path, true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for _, key := range []string{"path", "source"} {
			if raw := object[key]; raw != nil && json.Unmarshal(raw, &path) == nil {
				return path, true
			}
		}
	}
	return "", false
}
func jsonString(value string) json.RawMessage { data, _ := json.Marshal(value); return data }

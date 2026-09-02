package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/secrets"
	"github.com/vgxness/vgxness/internal/syncclient"
	"github.com/vgxness/vgxness/internal/syncservice"
)

func TestMemoryReadOnlyResolveProjectDoesNotCreateAbsentStore(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "absent-store")
	_, err := NewMemory("cli", true).ResolveProject(context.Background(), config.Options{StorageRoot: storageRoot}, t.TempDir())
	if !errors.Is(err, memory.ErrCorrupt) {
		t.Fatalf("resolve project error = %v, want corrupt absent-store error", err)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("read-only resolve created storage root: %v", err)
	}
}

func TestMemoryInitializeProjectBindsMarkerWithoutRekeyingLocalProject(t *testing.T) {
	workspace, storage := t.TempDir(), t.TempDir()
	runtime := NewMemory("cli", false)
	store, err := openStore(context.Background(), config.Options{StorageRoot: storage})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.ResolveProject(context.Background(), workspace)
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	id, err := runtime.InitializeProject(context.Background(), config.Options{StorageRoot: storage}, workspace)
	if err != nil {
		t.Fatalf("initialize project: %v", err)
	}
	marker, present, err := memory.ReadProjectID(workspace)
	if err != nil || !present || marker != id {
		t.Fatalf("marker=%q present=%t err=%v want=%q", marker, present, err, id)
	}
	store, err = openStore(context.Background(), config.Options{StorageRoot: storage})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resolved, err := store.ResolveProject(context.Background(), workspace)
	if err != nil || resolved != legacy {
		t.Fatalf("normal resolution=%q err=%v; want legacy local project %q", resolved, err, legacy)
	}
	repeated, err := runtime.InitializeProject(context.Background(), config.Options{StorageRoot: storage}, workspace)
	if err != nil || repeated != id {
		t.Fatalf("repeat=%q err=%v want=%q", repeated, err, id)
	}
	if err := os.Remove(filepath.Join(workspace, ".vgxness", "project-id")); err != nil {
		t.Fatal(err)
	}
	recovered, err := runtime.InitializeProject(context.Background(), config.Options{StorageRoot: storage}, workspace)
	if err != nil || recovered != id {
		t.Fatalf("recovered=%q err=%v want=%q", recovered, err, id)
	}
}

func TestMemorySyncFailsClosedBeforeSecretsOrTransport(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "project-local")
	credentials, requests := 0, 0
	runtime := NewMemory("cli", false)
	runtime.credential = func(string) (string, error) { credentials++; return "", nil }
	runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("unexpected request") })
	result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: storageRoot, ProjectDir: t.TempDir(), ProjectLocal: true})
	if err != nil || result.Status != memory.SyncStatusUnavailable || credentials != 0 || requests != 0 {
		t.Fatalf("project-local sync = %+v, %v; credentials=%d requests=%d", result, err, credentials, requests)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("project-local sync opened storage root: %v", err)
	}
}

func TestMemoryBackfillSyncProjectRejectsRelativeWorkspaceBeforeStoreOpen(t *testing.T) {
	storageRoot := filepath.Join(t.TempDir(), "absent-store")
	result, err := NewMemory("cli", false).BackfillSyncProject(context.Background(), config.Options{StorageRoot: storageRoot}, "relative", 1)
	if !errors.Is(err, memory.ErrInvalid) || result != (memory.SyncBackfillResult{}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("relative backfill opened storage root: %v", err)
	}
}

func TestMemorySyncRejectsUnboundOrMalformedWorkspaceBeforeRemote(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, err := openStore(context.Background(), config.Options{StorageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ConfigureSyncProfile(context.Background(), memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	requests := 0
	runtime := NewMemory("cli", false)
	runtime.credential = func(string) (string, error) { return "vgx1.550e8400-e29b-41d4-a716-446655440000.secret", nil }
	runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected remote call")
	})
	for _, malformed := range []bool{false, true} {
		if malformed {
			if err := os.Mkdir(filepath.Join(workspace, ".vgxness"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, ".vgxness", "project-id"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: root, ProjectDir: workspace})
		if err != nil || result.Status != memory.SyncStatusUnavailable || requests != 0 {
			t.Fatalf("malformed=%t result=%+v err=%v requests=%d", malformed, result, err, requests)
		}
		if malformed {
			break
		}
	}
}

func TestMemorySyncProfileAndCredentialStatesAvoidNetwork(t *testing.T) {
	root := t.TempDir()
	setup := func(profile *memory.SyncProfile) {
		t.Helper()
		store, err := openStore(context.Background(), config.Options{StorageRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if profile != nil {
			if _, err := store.ConfigureSyncProfile(context.Background(), *profile); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func(credential string, credentialErr error) (memory.SyncResult, int, int) {
		credentials, requests := 0, 0
		runtime := NewMemory("cli", false)
		runtime.credential = func(string) (string, error) { credentials++; return credential, credentialErr }
		runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("unexpected request") })
		result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: root, ProjectDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		return result, credentials, requests
	}

	setup(nil)
	result, credentials, requests := run("", nil)
	if result.Status != memory.SyncStatusAbsent || credentials != 0 || requests != 0 {
		t.Fatalf("absent = %+v credentials=%d requests=%d", result, credentials, requests)
	}
	profile := memory.SyncProfile{Enabled: false, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"}
	setup(&profile)
	result, credentials, requests = run("", nil)
	if result.Status != memory.SyncStatusDisabled || credentials != 0 || requests != 0 {
		t.Fatalf("disabled = %+v credentials=%d requests=%d", result, credentials, requests)
	}
	profile.Enabled = true
	setup(&profile)
	for _, credential := range []string{"malformed", "vgx1.550e8400-e29b-41d4-a716-446655440001.secret"} {
		result, credentials, requests = run(credential, nil)
		if result.Status != memory.SyncStatusInvalid || credentials != 1 || requests != 0 {
			t.Fatalf("credential %q = %+v credentials=%d requests=%d", credential, result, credentials, requests)
		}
	}
	for credentialErr, want := range map[error]memory.SyncStatus{secrets.ErrMissing: memory.SyncStatusCredentialMissing, secrets.ErrUnavailable: memory.SyncStatusCredentialUnavailable} {
		result, credentials, requests = run("vgx1.550e8400-e29b-41d4-a716-446655440000.secret", credentialErr)
		if result.Status != want || credentials != 1 || requests != 0 {
			t.Fatalf("credential error %v = %+v credentials=%d requests=%d", credentialErr, result, credentials, requests)
		}
	}
}

func TestMemoryConfigureSyncStoresCredentialBeforeActivatingProfileAndStatusIsLocal(t *testing.T) {
	root := t.TempDir()
	bearer := "vgx1.550e8400-e29b-41d4-a716-446655440000.secret"
	values := map[string]string{}
	runtime := NewMemory("cli", false)
	runtime.putSecret = func(reference, value string) error {
		values[reference] = value
		return nil
	}
	runtime.deleteSecret = func(reference string) error {
		delete(values, reference)
		return nil
	}
	runtime.credential = func(reference string) (string, error) {
		value, found := values[reference]
		if !found {
			return "", secrets.ErrMissing
		}
		return value, nil
	}
	runtime.transport = roundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("configure/status made a network request")
		return nil, nil
	})

	status, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "HTTPS://Sync.Example.Test:443", "550E8400-E29B-41D4-A716-446655440000", bearer)
	if err != nil || !status.Configured || !status.Enabled || status.Credential != memory.SyncCredentialAvailable || len(values) != 1 {
		t.Fatalf("configure status=%+v err=%v values=%v", status, err, values)
	}
	store, err := openStoreRead(context.Background(), config.Options{StorageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	profile, found, err := store.GetSyncProfile(context.Background())
	store.Close()
	if err != nil || !found || !profile.Enabled || values[profile.CredentialRef] != bearer || profile.CredentialRef == bearer {
		t.Fatalf("profile=%+v found=%t err=%v", profile, found, err)
	}
	status, err = runtime.SyncStatus(context.Background(), config.Options{StorageRoot: root})
	if err != nil || !status.Configured || !status.Enabled || status.Credential != memory.SyncCredentialAvailable {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	updated := "vgx1.550e8400-e29b-41d4-a716-446655440000.updated"
	status, err = runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://other.example.test", profile.DeviceID, updated)
	if err != nil || !status.Configured || len(values) != 1 || values[profile.CredentialRef] == updated {
		t.Fatalf("reconfigure status=%+v err=%v values=%v", status, err, values)
	}
	store, err = openStoreRead(context.Background(), config.Options{StorageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	updatedProfile, found, err := store.GetSyncProfile(context.Background())
	store.Close()
	if err != nil || !found || updatedProfile.CredentialRef == profile.CredentialRef || values[updatedProfile.CredentialRef] != updated || updatedProfile.Endpoint != "https://other.example.test" {
		t.Fatalf("updated profile=%+v found=%t err=%v", updatedProfile, found, err)
	}
}

func TestMemoryConfigureSyncCredentialFileDoesNotUseOrPersistKeyring(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("credential files are unsupported on Windows")
	}
	root := t.TempDir()
	bearer := "vgx1.550e8400-e29b-41d4-a716-446655440000.secret"
	credentialFile := filepath.Join(canonicalRuntimeTestDir(t), "credential")
	if err := os.WriteFile(credentialFile, []byte(bearer+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewMemory("cli", false)
	runtime.putSecret = func(string, string) error { t.Fatal("put keyring"); return nil }
	runtime.deleteSecret = func(string) error { t.Fatal("delete keyring"); return nil }
	status, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root, CredentialFile: credentialFile}, "https://sync.example.test", "550e8400-e29b-41d4-a716-446655440000", "")
	if err != nil || !status.Configured || status.Credential != memory.SyncCredentialAvailable {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	store, err := openStoreRead(context.Background(), config.Options{StorageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	profile, found, err := store.GetSyncProfile(context.Background())
	store.Close()
	if err != nil || !found || profile.CredentialRef != "secret://keychain/sync/file" || strings.Contains(profile.CredentialRef, credentialFile) || strings.Contains(profile.CredentialRef, bearer) {
		t.Fatalf("profile=%+v found=%t err=%v", profile, found, err)
	}
	status, err = runtime.SyncStatus(context.Background(), config.Options{StorageRoot: root, CredentialFile: credentialFile})
	if err != nil || status.Credential != memory.SyncCredentialAvailable {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestMemoryConfigureSyncCredentialFileRejectsKeyringProfileWithoutSideEffects(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("credential files are unsupported on Windows")
	}
	root := t.TempDir()
	device := "550e8400-e29b-41d4-a716-446655440000"
	runtime := enrollmentRuntime(map[string]string{})
	if _, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", device, "vgx1."+device+".keyring"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(canonicalRuntimeTestDir(t), "credential")
	if err := os.WriteFile(file, []byte("vgx1."+device+".file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.putSecret = func(string, string) error { t.Fatal("put keyring"); return nil }
	runtime.deleteSecret = func(string) error { t.Fatal("delete keyring"); return nil }
	_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root, CredentialFile: file}, "https://sync.example.test", device, "")
	if !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("error=%v", err)
	}
	if profile := syncEnrollmentProfile(t, root); profile.CredentialRef == "secret://keychain/sync/file" {
		t.Fatalf("profile mutated=%+v", profile)
	}
}

func TestMemorySyncStatusForAbsentProfileDoesNotReadCredential(t *testing.T) {
	reads := 0
	runtime := NewMemory("cli", false)
	runtime.credential = func(string) (string, error) {
		reads++
		return "", nil
	}
	status, err := runtime.SyncStatus(context.Background(), config.Options{StorageRoot: t.TempDir()})
	if err != nil || status.Configured || status.Enabled || status.Credential != memory.SyncCredentialNotConfigured || reads != 0 {
		t.Fatalf("status=%+v err=%v reads=%d", status, err, reads)
	}
}

func TestMemoryConfigureSyncDoesNotStoreCredentialWhenProfileCannotBeOpened(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	deleted := 0
	runtime := NewMemory("cli", false)
	runtime.putSecret = func(string, string) error { return nil }
	runtime.deleteSecret = func(string) error { deleted++; return nil }
	_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: blocked}, "https://sync.example.test", "550e8400-e29b-41d4-a716-446655440000", "vgx1.550e8400-e29b-41d4-a716-446655440000.secret")
	if err == nil || deleted != 0 {
		t.Fatalf("configure error=%v deleted=%d", err, deleted)
	}
}

func TestMemoryConfigureSyncCompensatesByMutationOutcome(t *testing.T) {
	const deviceID = "550e8400-e29b-41d4-a716-446655440000"
	const first = "vgx1.550e8400-e29b-41d4-a716-446655440000.first"
	const second = "vgx1.550e8400-e29b-41d4-a716-446655440000.second"
	newRuntime := func(values map[string]string) Memory {
		runtime := NewMemory("cli", false)
		runtime.putSecret = func(reference, value string) error { values[reference] = value; return nil }
		runtime.deleteSecret = func(reference string) error { delete(values, reference); return nil }
		runtime.credential = func(reference string) (string, error) {
			value, found := values[reference]
			if !found {
				return "", secrets.ErrMissing
			}
			return value, nil
		}
		return runtime
	}
	t.Run("first enrollment deletes credential", func(t *testing.T) {
		values := map[string]string{}
		runtime := newRuntime(values)
		runtime.configureProfile = func(context.Context, *memory.Store, memory.SyncProfile) (memory.SyncProfile, error) {
			return memory.SyncProfile{}, errors.New("mutation failed")
		}
		_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: t.TempDir()}, "https://sync.example.test", deviceID, first)
		if err == nil || len(values) != 0 {
			t.Fatalf("err=%v values=%v", err, values)
		}
	})
	t.Run("reconfigure restores existing credential", func(t *testing.T) {
		root, values := t.TempDir(), map[string]string{}
		runtime := newRuntime(values)
		if _, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", deviceID, first); err != nil {
			t.Fatal(err)
		}
		store, err := openStoreRead(context.Background(), config.Options{StorageRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		profile, found, err := store.GetSyncProfile(context.Background())
		store.Close()
		if err != nil || !found {
			t.Fatalf("profile=%+v found=%t err=%v", profile, found, err)
		}
		runtime.configureProfile = func(context.Context, *memory.Store, memory.SyncProfile) (memory.SyncProfile, error) {
			return memory.SyncProfile{}, errors.New("mutation failed")
		}
		_, err = runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://other.example.test", deviceID, second)
		if err == nil || len(values) != 1 || values[profile.CredentialRef] != first {
			t.Fatalf("err=%v values=%v", err, values)
		}
	})
	t.Run("existing credential is not read before slot switch", func(t *testing.T) {
		root, values := t.TempDir(), map[string]string{}
		runtime := newRuntime(values)
		if _, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", deviceID, first); err != nil {
			t.Fatal(err)
		}
		reads := 0
		runtime.credential = func(string) (string, error) { return "", secrets.ErrUnavailable }
		runtime.credential = func(string) (string, error) { reads++; return "", secrets.ErrUnavailable }
		_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://other.example.test", deviceID, second)
		if err != nil || reads != 0 {
			t.Fatalf("err=%v reads=%d", err, reads)
		}
	})
	t.Run("compensation failure is redacted", func(t *testing.T) {
		values := map[string]string{}
		runtime := newRuntime(values)
		mutationErr := errors.New("mutation failed")
		compensationErr := errors.New("delete failure")
		runtime.configureProfile = func(context.Context, *memory.Store, memory.SyncProfile) (memory.SyncProfile, error) {
			return memory.SyncProfile{}, mutationErr
		}
		deletes := 0
		runtime.deleteSecret = func(string) error {
			deletes++
			if deletes == 2 {
				return compensationErr
			}
			return nil
		}
		_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: t.TempDir()}, "https://sync.example.test", deviceID, first)
		if err == nil || !errors.Is(err, mutationErr) || !errors.Is(err, compensationErr) || !strings.Contains(err.Error(), "compensation failed") || strings.Contains(err.Error(), first) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("post-commit close retains new credential", func(t *testing.T) {
		root, values := t.TempDir(), map[string]string{}
		runtime := newRuntime(values)
		runtime.closeStore = func(store *memory.Store) error {
			if err := store.Close(); err != nil {
				return err
			}
			return errors.New("close failed")
		}
		status, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", deviceID, first)
		if err == nil || !status.Configured || len(values) != 1 || strings.Contains(err.Error(), first) {
			t.Fatalf("status=%+v err=%v values=%v", status, err, values)
		}
		store, openErr := openStoreRead(context.Background(), config.Options{StorageRoot: root})
		if openErr != nil {
			t.Fatal(openErr)
		}
		profile, found, profileErr := store.GetSyncProfile(context.Background())
		store.Close()
		if profileErr != nil || !found || values[profile.CredentialRef] != first {
			t.Fatalf("profile=%+v found=%t err=%v values=%v", profile, found, profileErr, values)
		}
	})
	t.Run("put failure closes without mutation", func(t *testing.T) {
		root, values := t.TempDir(), map[string]string{}
		runtime := newRuntime(values)
		putErr := errors.New("put failure")
		closed := 0
		runtime.putSecret = func(string, string) error { return putErr }
		runtime.closeStore = func(store *memory.Store) error { closed++; return store.Close() }
		_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", deviceID, first)
		if !errors.Is(err, putErr) || closed != 1 || strings.Contains(err.Error(), first) || len(values) != 0 {
			t.Fatalf("err=%v closed=%d values=%v", err, closed, values)
		}
		store, openErr := openStoreRead(context.Background(), config.Options{StorageRoot: root})
		if openErr != nil {
			t.Fatal(openErr)
		}
		_, found, profileErr := store.GetSyncProfile(context.Background())
		store.Close()
		if profileErr != nil || found {
			t.Fatalf("found=%t err=%v", found, profileErr)
		}
	})
}

func TestMemoryConfigureSyncPreservesLegacyAndRejectsForgedRecoveryMarker(t *testing.T) {
	const device = "550e8400-e29b-41d4-a716-446655440000"
	token := "vgx1." + device + ".token"
	t.Run("legacy retained", func(t *testing.T) {
		root, values := t.TempDir(), map[string]string{"secret://keychain/legacy": "legacy"}
		paths, err := config.Prepare(context.Background(), config.Options{StorageRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		store, err := memory.Open(context.Background(), paths.Database, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.ConfigureSyncProfile(context.Background(), memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: device, CredentialRef: "secret://keychain/legacy"})
		store.Close()
		if err != nil {
			t.Fatal(err)
		}
		runtime := NewMemory("cli", false)
		runtime.putSecret = func(ref, value string) error { values[ref] = value; return nil }
		runtime.deleteSecret = func(ref string) error { delete(values, ref); return nil }
		_, err = runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", device, token)
		profile := syncProfileForTest(t, root)
		if err != nil || values["secret://keychain/legacy"] != "legacy" || profile.PreviousCredentialRef != "" || profile.CredentialRef == "secret://keychain/legacy" {
			t.Fatalf("err=%v profile=%+v values=%v", err, profile, values)
		}
	})
	t.Run("forged marker fails before delete", func(t *testing.T) {
		root, values := t.TempDir(), map[string]string{}
		runtime := NewMemory("cli", false)
		runtime.putSecret = func(ref, value string) error { values[ref] = value; return nil }
		runtime.deleteSecret = func(ref string) error { delete(values, ref); return nil }
		if _, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", device, token); err != nil {
			t.Fatal(err)
		}
		profile := syncProfileForTest(t, root)
		store, err := memory.Open(context.Background(), filepath.Join(root, "memory.db"), nil)
		if err != nil {
			t.Fatal(err)
		}
		profile.PreviousCredentialRef = "secret://keychain/forged"
		_, err = store.ConfigureSyncProfile(context.Background(), profile)
		store.Close()
		if err != nil {
			t.Fatal(err)
		}
		deletes := 0
		runtime.deleteSecret = func(string) error { deletes++; return nil }
		_, err = runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", device, token)
		if !errors.Is(err, memory.ErrCorrupt) || deletes != 0 {
			t.Fatalf("err=%v deletes=%d", err, deletes)
		}
	})
}

func syncProfileForTest(t *testing.T, root string) memory.SyncProfile {
	t.Helper()
	store, err := openStoreRead(context.Background(), config.Options{StorageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile, found, err := store.GetSyncProfile(context.Background())
	if err != nil || !found {
		t.Fatalf("profile=%+v found=%t err=%v", profile, found, err)
	}
	return profile
}

type roundTripper func(*http.Request) (*http.Response, error)

func canonicalRuntimeTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func (round roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return round(request)
}

func TestRunForegroundSyncPreflightsAndPersistsOutcomes(t *testing.T) {
	store := newForegroundStore(t)
	remote := &testForegroundRemote{disposition: syncservice.DispositionPreviouslyAccepted}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusSynced || result.Pushed != 2 || result.PreviouslyAccepted != 2 || remote.capabilities != 1 || remote.pushes != 1 || remote.discovers != 1 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}

	store = newForegroundStore(t)
	remote = &testForegroundRemote{disposition: syncservice.DispositionRejected, retryable: true}
	result, err = runForegroundSync(context.Background(), store, remote)
	entries, entriesErr := store.DueSyncOutbox(context.Background(), time.Now().Add(time.Hour))
	if err != nil || entriesErr != nil || result.Status != memory.SyncStatusPartial || result.Pushed != 0 || result.Retried != 2 || len(entries) != 2 || remote.discovers != 0 {
		t.Fatalf("retry result=%+v err=%v entries=%+v entriesErr=%v remote=%+v", result, err, entries, entriesErr, remote)
	}
}

func TestRunForegroundSyncCapabilityAndTransportFailuresDoNotBootstrap(t *testing.T) {
	store := newForegroundStore(t)
	remote := &testForegroundRemote{capabilityErr: syncclient.ErrUnauthorized}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusUnauthorized || remote.pushes != 0 || remote.discovers != 0 {
		t.Fatalf("capability result=%+v err=%v remote=%+v", result, err, remote)
	}
	store = newForegroundStore(t)
	remote = &testForegroundRemote{capabilityErr: syncclient.NewDiagnosticError(syncclient.OperationCapabilities, syncclient.ErrorClassHTTPStatus, 503, syncclient.ErrUnavailable)}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusUnreachable || result.FailureOperation != string(syncclient.OperationCapabilities) || result.FailureClass != string(syncclient.ErrorClassHTTPStatus) || result.FailureHTTPStatus != 503 || remote.pushes != 0 {
		t.Fatalf("legacy capability result=%+v err=%v remote=%+v", result, err, remote)
	}

	store = newForegroundStore(t)
	remote = &testForegroundRemote{pushErr: syncclient.ErrUnavailable}
	result, err = runForegroundSync(context.Background(), store, remote)
	entries, entriesErr := store.DueSyncOutbox(context.Background(), time.Now().Add(time.Hour))
	if err != nil || entriesErr != nil || result.Status != memory.SyncStatusUnreachable || result.Retried != 2 || len(entries) != 2 || remote.discovers != 0 {
		t.Fatalf("transport result=%+v err=%v entries=%+v entriesErr=%v remote=%+v", result, err, entries, entriesErr, remote)
	}
}

func TestRunForegroundSyncRejectedAndCapBoundaries(t *testing.T) {
	store := newForegroundStore(t)
	remote := &testForegroundRemote{disposition: syncservice.DispositionRejected}
	result, err := runForegroundSync(context.Background(), store, remote)
	entries, entriesErr := store.DueSyncOutbox(context.Background(), time.Now().Add(time.Hour))
	if err != nil || entriesErr != nil || result.Status != memory.SyncStatusRejected || result.Pushed != 0 || result.Rejected != 2 || len(entries) != 0 || remote.pushes != 1 || remote.discovers != 0 {
		t.Fatalf("rejected result=%+v err=%v entries=%+v entriesErr=%v remote=%+v", result, err, entries, entriesErr, remote)
	}

	store = batchStore(t, 256)
	remote = &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusSynced || result.Pushed != 256 || result.Batches != 16 || remote.pushes != 16 || remote.discovers != 1 {
		t.Fatalf("exact cap result=%+v err=%v remote=%+v", result, err, remote)
	}

	store = batchStore(t, 257)
	remote = &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusPartial || result.Pushed != 256 || result.Batches != 16 || remote.pushes != 16 || remote.discovers != 0 {
		t.Fatalf("over cap result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundSyncConflictOrderingAndCancellation(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440099"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440098"}
	store := &orderedStore{claims: [][]memory.SyncOutboxClaim{{claim}, nil}}
	remote := &testForegroundRemote{disposition: syncservice.DispositionConflict}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusConflict || result.Conflicts != 1 || result.Pushed != 0 || store.ownID != claim.Mutation.MutationID || fmt.Sprint(store.events) != "[claim apply own]" {
		t.Fatalf("conflict result=%+v err=%v events=%v", result, err, store.events)
	}

	ctx, cancel := context.WithCancel(context.Background())
	store = &orderedStore{claims: [][]memory.SyncOutboxClaim{{claim}}}
	remote = &testForegroundRemote{disposition: syncservice.DispositionAccepted, cancelPush: cancel}
	result, err = runForegroundSync(ctx, store, remote)
	if !errors.Is(err, context.Canceled) || result.Status == memory.SyncStatusSynced || fmt.Sprint(store.events) != "[claim]" {
		t.Fatalf("cancel result=%+v err=%v events=%v", result, err, store.events)
	}
	for _, claimErr := range []error{context.Canceled, context.DeadlineExceeded, errors.New("durable")} {
		result, err = runForegroundSync(context.Background(), &orderedStore{claimErr: claimErr}, remote)
		if !errors.Is(err, claimErr) || result.Status != memory.SyncStatusPartial {
			t.Fatalf("claim error %v: result=%+v err=%v", claimErr, result, err)
		}
	}

	store = &orderedStore{ids: []string{claim.Mutation.MutationID}, ownErr: errors.New("temporary")}
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusPartial || fmt.Sprint(store.events) != "[own]" {
		t.Fatalf("recovery failure result=%+v err=%v events=%v", result, err, store.events)
	}
	store.ownErr, store.events = nil, nil
	result, err = runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusConflict || result.Conflicts != 1 || fmt.Sprint(store.events) != "[own]" {
		t.Fatalf("recovery result=%+v err=%v events=%v", result, err, store.events)
	}
}

func TestRunForegroundSyncPullsResolutionsBeforeBlockedReturn(t *testing.T) {
	store := &orderedStore{claims: [][]memory.SyncOutboxClaim{nil}, queue: memory.SyncQueueSummary{Conflict: true}, resolutionErr: memory.ErrConflict}
	result, err := runForegroundSync(context.Background(), store, &testForegroundRemote{})
	if err != nil || result.Status != memory.SyncStatusConflict || fmt.Sprint(store.events) != "[claim resolutions]" {
		t.Fatalf("result=%+v err=%v events=%v", result, err, store.events)
	}
}

func TestRunForegroundSyncPushesAfterResolutionPull(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440097"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440098"}
	store := &orderedStore{claims: [][]memory.SyncOutboxClaim{nil, {claim}, nil}, queues: []memory.SyncQueueSummary{{Conflict: true}, {}}}
	remote := &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err := runForegroundSync(context.Background(), store, remote)
	if err != nil || result.Status != memory.SyncStatusSynced || remote.pushes != 1 || fmt.Sprint(store.events) != "[claim resolutions claim apply claim bootstrap]" {
		t.Fatalf("result=%+v err=%v events=%v remote=%+v", result, err, store.events, remote)
	}
}

func TestRunForegroundProjectSyncPullsSelectedProjectAfterEmptyPush(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	history := "550e8400-e29b-41d4-a716-446655440010"
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pullCursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 4}}
	remote := &testForegroundRemote{pages: []syncservice.PullPage{
		{Cursor: syncservice.Cursor{HistoryID: history, Position: 4, Watermark: 4}, Changes: []syncservice.Change{}},
	}}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", project)
	if err != nil || result.Status != memory.SyncStatusSynced || remote.discovers != 1 || remote.projectPulls != 1 || remote.projectIDs[0] != project || remote.cursors[0].Position != 2 || len(store.pages) != 1 || store.pages[0].Cursor.Position != 4 || remote.pulls != 0 {
		t.Fatalf("result=%+v err=%v store=%+v remote=%+v", result, err, store, remote)
	}
}

func TestRunForegroundProjectSyncPullsPagesAndRetriesFromCommittedCursor(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	history := "550e8400-e29b-41d4-a716-446655440010"
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pullCursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 4}}
	remote := &testForegroundRemote{pages: []syncservice.PullPage{
		{Cursor: syncservice.Cursor{HistoryID: history, Position: 3, Watermark: 4}, HasMore: true},
		{Cursor: syncservice.Cursor{HistoryID: history, Position: 4, Watermark: 4}},
	}}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", project)
	if err != nil || result.Status != memory.SyncStatusSynced || remote.projectPulls != 2 || remote.cursors[0].Position != 2 || remote.cursors[1].Position != 3 || store.pullCursor.Position != 4 {
		t.Fatalf("result=%+v err=%v store=%+v remote=%+v", result, err, store, remote)
	}

	store.applyPageErr = memory.ErrConflict
	store.claims = [][]memory.SyncOutboxClaim{nil}
	remote = &testForegroundRemote{pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 4, Watermark: 4}}}}
	result, err = runForegroundProjectSync(context.Background(), store, remote, "project-a", project)
	if !errors.Is(err, memory.ErrConflict) || result.Status != memory.SyncStatusPartial || store.pullCursor.Position != 4 || remote.projectPulls != 1 {
		t.Fatalf("apply result=%+v err=%v store=%+v remote=%+v", result, err, store, remote)
	}
	store.applyPageErr = nil
	store.claims = [][]memory.SyncOutboxClaim{nil}
	remote = &testForegroundRemote{pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 4, Watermark: 4}}}}
	result, err = runForegroundProjectSync(context.Background(), store, remote, "project-a", project)
	if err != nil || result.Status != memory.SyncStatusSynced || remote.cursors[0].Position != 4 {
		t.Fatalf("retry result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncDoesNotPullAfterBlockingPush(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440411"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440412"}
	for _, disposition := range []syncservice.Disposition{syncservice.DispositionRejected, syncservice.DispositionConflict} {
		store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}}}
		remote := &testForegroundRemote{disposition: disposition}
		result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
		if err != nil || remote.discovers != 0 || remote.projectPulls != 0 {
			t.Fatalf("disposition=%q result=%+v err=%v remote=%+v", disposition, result, err, remote)
		}
	}
}

func TestRunForegroundProjectSyncFailsClosedBeforeRemoteWhenRepairPending(t *testing.T) {
	store := &projectForegroundStore{pendingErr: memory.ErrSyncProjectRepairPending}
	remote := &testForegroundRemote{}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, memory.ErrSyncProjectRepairPending) || result.Status != memory.SyncStatusPartial || remote.discovers != 0 || remote.projectPulls != 0 || remote.pushes != 0 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestProbeEmptyProjectRequiresExactEmptyPage(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	remote := &testForegroundRemote{}
	if err := probeEmptyProject(context.Background(), remote, project); err != nil || remote.discovers != 1 || remote.projectPulls != 1 || len(remote.cursors) != 1 || remote.cursors[0].Position != 0 || remote.cursors[0].Watermark != 0 {
		t.Fatalf("err=%v remote=%+v", err, remote)
	}
	remote = &testForegroundRemote{pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: "550e8400-e29b-41d4-a716-446655440010", Position: 1, Watermark: 1}}}}
	if err := probeEmptyProject(context.Background(), remote, project); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("nonempty probe err=%v", err)
	}
}

func TestRunSyncProjectTransitionReusesBackupIntentAfterPrepareFailure(t *testing.T) {
	root := t.TempDir()
	database, backup := filepath.Join(root, "memory.db"), filepath.Join(root, "backup.sqlite")
	creates, verifies := 0, 0
	backupOps := syncProjectBackupOps{create: func(context.Context, string, string) error {
		creates++
		return os.WriteFile(backup, []byte("backup"), 0o600)
	}, verify: func(context.Context, string, string) error { verifies++; return nil }, digest: func(context.Context, string) ([]byte, error) { return make([]byte, 32), nil }}
	prepareFailure := errors.New("prepare failure")
	store := &transitionTestStore{intent: memory.SyncProjectBackupIntent{IntentID: "intent", BackupPath: backup}, prepareErr: prepareFailure}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := runSyncProjectTransition(context.Background(), store, &testForegroundRemote{}, database, "550e8400-e29b-41d4-a716-446655440001", "project", memory.SyncProjectTransitionRejoinMerge, backupOps)
		if !errors.Is(err, prepareFailure) {
			t.Fatalf("attempt=%d err=%v", attempt, err)
		}
	}
	if creates != 1 || verifies != 1 || len(store.backupPaths) != 2 || store.backupPaths[0] == "" || store.backupPaths[0] != store.backupPaths[1] {
		t.Fatalf("creates=%d verifies=%d paths=%q", creates, verifies, store.backupPaths)
	}
}

func TestRunSyncProjectTransitionRecoversOnlyMatchingUnsealedBackup(t *testing.T) {
	root := t.TempDir()
	database, backup := filepath.Join(root, "memory.db"), filepath.Join(root, "backup.sqlite")
	if err := os.WriteFile(backup, []byte("backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := "550e8400-e29b-41d4-a716-446655440001"
	t.Run("matching embedded intent seals and reuses without a second backup", func(t *testing.T) {
		creates, embeddedChecks := 0, 0
		store := &transitionTestStore{intent: memory.SyncProjectBackupIntent{IntentID: "intent", BackupPath: backup}, prepareErr: errors.New("prepare failure")}
		ops := syncProjectBackupOps{
			create: func(context.Context, string, string) error { creates++; return nil },
			verify: func(context.Context, string, string) error { return nil },
			verifyIntent: func(context.Context, string, string, string, string, memory.SyncProjectTransitionMode, memory.SyncProjectBackupIntent) error {
				embeddedChecks++
				return nil
			},
			digest: func(context.Context, string) ([]byte, error) { return make([]byte, 32), nil },
		}
		_, err := runSyncProjectTransition(context.Background(), store, &testForegroundRemote{}, database, project, "project", memory.SyncProjectTransitionRejoinMerge, ops)
		if err == nil || creates != 0 || embeddedChecks != 1 || store.seals != 1 || store.prepareCalls != 1 {
			t.Fatalf("err=%v creates=%d embedded=%d seals=%d prepares=%d", err, creates, embeddedChecks, store.seals, store.prepareCalls)
		}
	})
	t.Run("healthy different database fails closed without replacement", func(t *testing.T) {
		creates := 0
		store := &transitionTestStore{intent: memory.SyncProjectBackupIntent{IntentID: "intent", BackupPath: backup}}
		ops := syncProjectBackupOps{
			create: func(context.Context, string, string) error { creates++; return nil },
			verify: func(context.Context, string, string) error { return nil },
			verifyIntent: func(context.Context, string, string, string, string, memory.SyncProjectTransitionMode, memory.SyncProjectBackupIntent) error {
				return memory.ErrConflict
			},
			digest: func(context.Context, string) ([]byte, error) { return make([]byte, 32), nil },
		}
		_, err := runSyncProjectTransition(context.Background(), store, &testForegroundRemote{}, database, project, "project", memory.SyncProjectTransitionRejoinMerge, ops)
		if !errors.Is(err, memory.ErrConflict) || creates != 0 || store.seals != 0 || store.prepareCalls != 0 {
			t.Fatalf("err=%v creates=%d seals=%d prepares=%d", err, creates, store.seals, store.prepareCalls)
		}
	})
}

func TestRunSyncProjectTransitionFailsClosedForInvalidIntentAndIncompleteSync(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	t.Run("invalid existing backup creates no replacement backup", func(t *testing.T) {
		creates := 0
		root := t.TempDir()
		backup := filepath.Join(root, "backup.sqlite")
		if err := os.WriteFile(backup, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		backupOps := syncProjectBackupOps{create: func(context.Context, string, string) error { creates++; return nil }, verify: func(context.Context, string, string) error { return memory.ErrCorrupt }, digest: func(context.Context, string) ([]byte, error) { return nil, memory.ErrCorrupt }}
		store := &transitionTestStore{intent: memory.SyncProjectBackupIntent{IntentID: "intent", BackupPath: backup, BackupSHA256: make([]byte, 32)}}
		_, err := runSyncProjectTransition(context.Background(), store, &testForegroundRemote{}, filepath.Join(root, "memory.db"), project, "project", memory.SyncProjectTransitionRejoinMerge, backupOps)
		if !errors.Is(err, memory.ErrCorrupt) || creates != 0 {
			t.Fatalf("err=%v creates=%d", err, creates)
		}
	})
	t.Run("completed resumes without remote work and mode conflict fails", func(t *testing.T) {
		store := &transitionTestStore{active: true, transition: memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionCompleted}}
		_, err := runSyncProjectTransition(context.Background(), store, &testForegroundRemote{}, "", project, "project", memory.SyncProjectTransitionRejoinMerge, testBackupOps())
		if err != nil || store.finalizes != 0 {
			t.Fatalf("err=%v finalizes=%d", err, store.finalizes)
		}
		_, err = runSyncProjectTransition(context.Background(), store, &testForegroundRemote{}, "", project, "project", memory.SyncProjectTransitionReseedSource, testBackupOps())
		if !errors.Is(err, memory.ErrConflict) {
			t.Fatalf("mode err=%v", err)
		}
	})
	for name, test := range map[string]struct {
		transition memory.SyncProjectTransitionResult
		store      projectForegroundStore
		remote     *testForegroundRemote
	}{
		"pull": {memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPulling}, projectForegroundStore{}, &testForegroundRemote{discoverErr: errors.New("offline")}},
		"push": {memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionReseedSource, Status: memory.SyncProjectTransitionPublishing}, projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440002"}}, ClaimToken: "claim"}}}}, &testForegroundRemote{capabilityErr: errors.New("offline")}},
	} {
		t.Run("incomplete "+name+" never finalizes", func(t *testing.T) {
			store := &transitionTestStore{active: true, transition: test.transition, projectForegroundStore: test.store}
			expectedFinalizes := 0
			if test.transition.Status == memory.SyncProjectTransitionPublishing && test.transition.Mode == memory.SyncProjectTransitionRejoinMerge {
				store.finalizeResults = []memory.SyncProjectTransitionResult{test.transition}
				expectedFinalizes = 1
			}
			_, err := runSyncProjectTransition(context.Background(), store, test.remote, "", project, "project", test.transition.Mode, testBackupOps())
			if !errors.Is(err, memory.ErrConflict) || store.finalizes != expectedFinalizes {
				t.Fatalf("err=%v finalizes=%d", err, store.finalizes)
			}
		})
	}
	t.Run("pre-cancel does no work", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		store := &transitionTestStore{}
		_, err := runSyncProjectTransition(ctx, store, &testForegroundRemote{}, "", project, "project", memory.SyncProjectTransitionRejoinMerge, testBackupOps())
		if !errors.Is(err, context.Canceled) || store.ensureCalls != 0 {
			t.Fatalf("err=%v ensures=%d", err, store.ensureCalls)
		}
	})
}

func TestRunSyncProjectTransitionRecoversBeforePublishingResume(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440002", RecordID: "observation", RecordKind: syncservice.RecordKindObservation, Kind: syncservice.MutationCreate, Observation: &syncservice.Observation{ID: "observation", ProjectID: "project"}}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440003"}
	publishing := memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPublishing}
	store := &transitionTestStore{
		active:          true,
		transition:      publishing,
		finalizeResults: []memory.SyncProjectTransitionResult{publishing, {Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionCompleted}},
		recoveredClaims: [][]memory.SyncOutboxClaim{{claim}, {}},
	}
	remote := &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err := runSyncProjectTransition(context.Background(), store, remote, "", project, "project", memory.SyncProjectTransitionRejoinMerge, testBackupOps())
	if err != nil || result.Status != memory.SyncProjectTransitionCompleted || store.finalizes != 2 || remote.pushes != 1 || store.applied != 1 {
		t.Fatalf("result=%+v err=%v finalizes=%d remote=%+v applied=%d", result, err, store.finalizes, remote, store.applied)
	}
}

func TestRunSyncProjectTransitionFreshReseedPublishesWithoutPreFinalize(t *testing.T) {
	root := t.TempDir()
	database, backup := filepath.Join(root, "memory.db"), filepath.Join(root, "backup.sqlite")
	project := "550e8400-e29b-41d4-a716-446655440001"
	history := "550e8400-e29b-41d4-a716-446655440010"
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440002", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440003"}
	store := &transitionTestStore{
		intent:        memory.SyncProjectBackupIntent{IntentID: "intent", BackupPath: backup},
		prepareResult: memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionReseedSource, Status: memory.SyncProjectTransitionPublishing},
		projectForegroundStore: projectForegroundStore{
			claims:     [][]memory.SyncOutboxClaim{{claim}, {}},
			pullCursor: syncservice.Cursor{HistoryID: history},
		},
	}
	ops := syncProjectBackupOps{
		create: func(_ context.Context, _ string, path string) error {
			return os.WriteFile(path, []byte("backup"), 0o600)
		},
		verify: func(context.Context, string, string) error { return nil },
		digest: func(context.Context, string) ([]byte, error) { return make([]byte, 32), nil },
	}
	remote := &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err := runSyncProjectTransition(context.Background(), store, remote, database, project, "project", memory.SyncProjectTransitionReseedSource, ops)
	if err != nil || result.Status != memory.SyncProjectTransitionCompleted || store.prepareCalls != 1 || store.finalizes != 1 || remote.pushes != 1 {
		t.Fatalf("result=%+v err=%v prepares=%d finalizes=%d pushes=%d", result, err, store.prepareCalls, store.finalizes, remote.pushes)
	}
}

func TestRunSyncProjectTransitionPublishingRejoinPullsPostWatermarkEchoes(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	history := "550e8400-e29b-41d4-a716-446655440010"
	publishing := memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPublishing}
	echo := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440011", RecordID: "550e8400-e29b-41d4-a716-446655440012", RecordKind: syncservice.RecordKindSession, Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: "550e8400-e29b-41d4-a716-446655440012", ProjectID: project}}
	store := &transitionTestStore{
		active:          true,
		transition:      publishing,
		finalizeResults: []memory.SyncProjectTransitionResult{publishing, {Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionCompleted}},
		projectForegroundStore: projectForegroundStore{
			claims:     [][]memory.SyncOutboxClaim{{}},
			pullCursor: syncservice.Cursor{HistoryID: history, Position: 5},
		},
	}
	remote := &testForegroundRemote{pages: []syncservice.PullPage{{Cursor: syncservice.Cursor{HistoryID: history, Position: 6, Watermark: 6}, Changes: []syncservice.Change{{Sequence: 6, CanonicalVersion: 1, Mutation: echo}}}}}
	result, err := runSyncProjectTransition(context.Background(), store, remote, "", project, "project", memory.SyncProjectTransitionRejoinMerge, testBackupOps())
	if err != nil || result.Status != memory.SyncProjectTransitionCompleted || store.finalizes != 2 || remote.projectPulls != 1 || len(remote.cursors) != 1 || remote.cursors[0] != (syncservice.Cursor{HistoryID: history, Position: 5}) || len(store.pages) != 1 || store.pages[0].Cursor.Position != 6 {
		t.Fatalf("result=%+v err=%v finalizes=%d cursors=%+v pages=%+v", result, err, store.finalizes, remote.cursors, store.pages)
	}
}

func TestRunSyncProjectTransitionPublishingRejoinContinuesIncompleteEchoPages(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	history := "550e8400-e29b-41d4-a716-446655440010"
	publishing := memory.SyncProjectTransitionResult{Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionPublishing}
	store := &transitionTestStore{
		active:          true,
		transition:      publishing,
		finalizeResults: []memory.SyncProjectTransitionResult{publishing, {Mode: memory.SyncProjectTransitionRejoinMerge, Status: memory.SyncProjectTransitionCompleted}},
		projectForegroundStore: projectForegroundStore{
			claims:     [][]memory.SyncOutboxClaim{{}},
			pullCursor: syncservice.Cursor{HistoryID: history, Position: 174, Watermark: 364},
		},
	}
	remote := &testForegroundRemote{pages: []syncservice.PullPage{
		{Cursor: syncservice.Cursor{HistoryID: history, Position: 300, Watermark: 364}, HasMore: true},
		{Cursor: syncservice.Cursor{HistoryID: history, Position: 364, Watermark: 364}},
	}}
	result, err := runSyncProjectTransition(context.Background(), store, remote, "", project, "project", memory.SyncProjectTransitionRejoinMerge, testBackupOps())
	expectedCursors := []syncservice.Cursor{{HistoryID: history, Position: 174, Watermark: 364}, {HistoryID: history, Position: 300, Watermark: 364}}
	if err != nil || result.Status != memory.SyncProjectTransitionCompleted || store.finalizes != 2 || remote.projectPulls != 2 || len(remote.cursors) != 2 || remote.cursors[0] != expectedCursors[0] || remote.cursors[1] != expectedCursors[1] || len(store.pages) != 2 || store.pages[1].Cursor.Position != 364 {
		t.Fatalf("result=%+v err=%v finalizes=%d cursors=%+v pages=%+v", result, err, store.finalizes, remote.cursors, store.pages)
	}
}

func TestRunForegroundProjectPullRechecksPendingRepairBeforeDiscoverAndPull(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pendingErrs: []error{nil, nil, memory.ErrSyncProjectRepairPending}}
	remote := &testForegroundRemote{}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", project)
	if !errors.Is(err, memory.ErrSyncProjectRepairPending) || result.Status != memory.SyncStatusPartial || remote.discovers != 0 || remote.projectPulls != 0 {
		t.Fatalf("before discover result=%+v err=%v remote=%+v", result, err, remote)
	}
	store = &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pendingErrs: []error{nil, nil, nil, memory.ErrSyncProjectRepairPending}}
	remote = &testForegroundRemote{}
	result, err = runForegroundProjectSync(context.Background(), store, remote, "project-a", project)
	if !errors.Is(err, memory.ErrSyncProjectRepairPending) || result.Status != memory.SyncStatusPartial || remote.discovers != 1 || remote.projectPulls != 0 {
		t.Fatalf("before pull result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncSendsOnlyPendingRepairBeforePull(t *testing.T) {
	repair := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440419"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440420"}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{repair}, nil}, repairMutationIDs: []string{repair.Mutation.MutationID, repair.Mutation.MutationID, repair.Mutation.MutationID, repair.Mutation.MutationID, ""}}
	remote := &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusSynced || remote.pushes != 1 || len(remote.sent) != 1 || remote.sent[0].MutationID != repair.Mutation.MutationID || remote.discovers != 1 || remote.projectPulls != 1 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncDoesNotCallRemoteWhenRepairChangesAfterClaim(t *testing.T) {
	repair := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440422"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440423"}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{repair}}, repairMutationIDs: []string{repair.Mutation.MutationID, ""}}
	remote := &testForegroundRemote{}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, memory.ErrSyncProjectRepairPending) || result.Status != memory.SyncStatusPartial || remote.capabilities != 0 || remote.pushes != 0 || remote.discovers != 0 || remote.projectPulls != 0 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncDoesNotCallRemoteWhenPendingRepairIsUnclaimable(t *testing.T) {
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, repairMutationIDs: []string{"550e8400-e29b-41d4-a716-446655440421"}}
	remote := &testForegroundRemote{}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, memory.ErrSyncProjectRepairPending) || result.Status != memory.SyncStatusPartial || remote.pushes != 0 || remote.discovers != 0 || remote.projectPulls != 0 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncPullCancellationReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pullCursor: syncservice.Cursor{HistoryID: "550e8400-e29b-41d4-a716-446655440010"}}
	remote := &testForegroundRemote{cancelPull: cancel}
	result, err := runForegroundProjectSync(ctx, store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, context.Canceled) || result.Status != memory.SyncStatusPartial || remote.projectPulls != 1 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncPullPreflightAndRemoteFailures(t *testing.T) {
	project := "550e8400-e29b-41d4-a716-446655440001"
	history := "550e8400-e29b-41d4-a716-446655440010"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pullCursor: syncservice.Cursor{HistoryID: history}}
	remote := &testForegroundRemote{}
	result, err := runForegroundProjectSync(ctx, store, remote, "project-a", project)
	if !errors.Is(err, context.Canceled) || result.Status != memory.SyncStatusPartial || remote.discovers != 0 || remote.projectPulls != 0 {
		t.Fatalf("cancel result=%+v err=%v remote=%+v", result, err, remote)
	}
	for _, tc := range []struct {
		name   string
		remote *testForegroundRemote
		status memory.SyncStatus
	}{
		{"discover unavailable", &testForegroundRemote{discoverErr: syncclient.ErrUnavailable}, memory.SyncStatusUnreachable},
		{"discover invalid", &testForegroundRemote{invalidDiscovery: true}, memory.SyncStatusIncompatible},
		{"pull unavailable", &testForegroundRemote{projectPullErr: syncclient.ErrUnavailable}, memory.SyncStatusUnreachable},
		{"pull remote", &testForegroundRemote{projectPullErr: syncclient.ErrRemote}, memory.SyncStatusIncompatible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pullCursor: syncservice.Cursor{HistoryID: history}}
			result, err := runForegroundProjectSync(context.Background(), store, tc.remote, "project-a", project)
			if err != nil || result.Status != tc.status || len(store.pages) != 0 {
				t.Fatalf("result=%+v err=%v store=%+v", result, err, store)
			}
		})
	}
}

func TestRunForegroundProjectSyncRejectsNonProgressingPullPage(t *testing.T) {
	history := "550e8400-e29b-41d4-a716-446655440010"
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}, pullCursor: syncservice.Cursor{HistoryID: history, Position: 2, Watermark: 4}}
	remote := &testForegroundRemote{pages: []syncservice.PullPage{{Cursor: store.pullCursor, HasMore: true}}}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusPartial || len(store.pages) != 0 || store.pullCursor.Position != 2 {
		t.Fatalf("result=%+v err=%v store=%+v", result, err, store)
	}
}

func TestRunForegroundProjectSyncUsesOnlySelectedProject(t *testing.T) {
	claims := []memory.SyncOutboxClaim{
		{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440401", RecordID: "a", RecordKind: syncservice.RecordKindProject}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440402"},
		{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440403", RecordID: "a-observation", RecordKind: syncservice.RecordKindObservation}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440404"},
	}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{claims, nil}}
	remote := &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Mode != memory.SyncModeProjectBidirectional || result.Status != memory.SyncStatusSynced || result.Pushed != 2 || result.Batches != 1 || store.project != "project-a" || remote.pushes != 1 || remote.discovers != 1 || remote.projectPulls != 1 || remote.pulls != 0 || len(remote.sent) != 2 || remote.sent[0].RecordID == "b" {
		t.Fatalf("result=%+v err=%v project=%q remote=%+v", result, err, store.project, remote)
	}
}

func TestRunForegroundProjectSyncPushesWireCopyAndAppliesOriginalClaim(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440451", RecordID: "local", RecordKind: syncservice.RecordKindProject, Project: &syncservice.Project{ID: "local"}}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440452"}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}, nil}, translate: func(_ context.Context, _ string, _ string, mutations []syncservice.Mutation) ([]syncservice.Mutation, error) {
		mutations[0].RecordID, mutations[0].Project.ID = "portable", "portable"
		return mutations, nil
	}}
	remote := &testForegroundRemote{disposition: syncservice.DispositionAccepted}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "local", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Pushed != 1 || len(remote.sent) != 1 || remote.sent[0].RecordID != "portable" || store.appliedID != "550e8400-e29b-41d4-a716-446655440451" || claim.Mutation.RecordID != "local" {
		t.Fatalf("result=%+v err=%v sent=%+v applied=%q claim=%+v", result, err, remote.sent, store.appliedID, claim)
	}
}

func TestRunForegroundProjectSyncTranslationFailureDoesNotContactRemote(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440453"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440454"}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}}, translate: func(context.Context, string, string, []syncservice.Mutation) ([]syncservice.Mutation, error) {
		return nil, memory.ErrInvalid
	}}
	remote := &testForegroundRemote{}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, memory.ErrInvalid) || result.Status != memory.SyncStatusPartial || remote.capabilities != 0 || remote.pushes != 0 || remote.discovers != 0 || remote.pulls != 0 {
		t.Fatalf("result=%+v err=%v remote=%+v", result, err, remote)
	}
}

func TestRunForegroundProjectSyncCapabilityFailureRetriesClaim(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440455"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440456"}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}}, translate: func(_ context.Context, _ string, _ string, mutations []syncservice.Mutation) ([]syncservice.Mutation, error) {
		return mutations, nil
	}}
	remote := &testForegroundRemote{capabilityErr: syncclient.ErrUnavailable}
	result, err := runForegroundProjectSync(context.Background(), store, remote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusUnreachable || store.retries != 1 || remote.pushes != 0 {
		t.Fatalf("result=%+v err=%v retries=%d remote=%+v", result, err, store.retries, remote)
	}
}

func TestRunForegroundProjectSyncPropagatesSanitizedDiagnostics(t *testing.T) {
	claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440455"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440456"}
	capabilityErr := syncclient.NewDiagnosticError(syncclient.OperationCapabilities, syncclient.ErrorClassHTTPStatus, 503, syncclient.ErrUnavailable)
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}}}
	result, err := runForegroundProjectSync(context.Background(), store, &testForegroundRemote{capabilityErr: capabilityErr}, "project-secret", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.FailureOperation != string(syncclient.OperationCapabilities) || result.FailureClass != string(syncclient.ErrorClassHTTPStatus) || result.FailureHTTPStatus != 503 {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	pullErr := syncclient.NewDiagnosticError(syncclient.OperationProjectPull, syncclient.ErrorClassResponseInvalid, 200, syncclient.ErrRemote)
	result, err = runForegroundProjectSync(context.Background(), &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{nil}}, &testForegroundRemote{projectPullErr: pullErr}, "project-secret", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.FailureOperation != string(syncclient.OperationProjectPull) || result.FailureClass != string(syncclient.ErrorClassResponseInvalid) || result.FailureHTTPStatus != 200 {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	remoteErr := syncclient.NewDiagnosticError(syncclient.OperationCapabilities, syncclient.ErrorClassTransport, 200, syncclient.ErrRemote)
	store = &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}}}
	result, err = runForegroundProjectSync(context.Background(), store, &testForegroundRemote{capabilityErr: remoteErr}, "project-secret", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusIncompatible || result.Retried != 0 || store.retries != 0 || result.FailureClass != string(syncclient.ErrorClassTransport) || result.FailureHTTPStatus != 200 {
		t.Fatalf("result=%+v err=%v retries=%d", result, err, store.retries)
	}
}

func TestRunForegroundProjectSyncCapabilityFailureRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		remote   error
		retryErr error
		status   memory.SyncStatus
		retries  int
	}{
		{"unavailable", syncclient.ErrUnavailable, nil, memory.SyncStatusUnreachable, 1},
		{"retry persistence", syncclient.ErrUnavailable, errors.New("store"), memory.SyncStatusPartial, 1},
		{"unauthorized", syncclient.ErrUnauthorized, nil, memory.SyncStatusUnauthorized, 0},
		{"remote", syncclient.ErrRemote, nil, memory.SyncStatusIncompatible, 0},
		{"discovery", syncclient.ErrDiscoveryUnsupported, nil, memory.SyncStatusIncompatible, 0},
		{"generic", errors.New("generic"), nil, memory.SyncStatusUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := memory.SyncOutboxClaim{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440457"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440458"}
			store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{{claim}}, retryErr: tc.retryErr}
			result, err := runForegroundProjectSync(context.Background(), store, &testForegroundRemote{capabilityErr: tc.remote}, "project-a", "550e8400-e29b-41d4-a716-446655440001")
			if err != nil || result.Status != tc.status || store.retries != tc.retries {
				t.Fatalf("result=%+v err=%v retries=%d", result, err, store.retries)
			}
		})
	}
}

func TestRunForegroundProjectSyncCancellationAndRetryableBatchSemantics(t *testing.T) {
	claims := []memory.SyncOutboxClaim{
		{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440411"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440412"},
		{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440413"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440414"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelStore := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{claims}}
	cancelRemote := &testForegroundRemote{disposition: syncservice.DispositionAccepted, cancelPush: cancel}
	result, err := runForegroundProjectSync(ctx, cancelStore, cancelRemote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, context.Canceled) || result.Status != memory.SyncStatusPartial || result.Mode != memory.SyncModeProjectBidirectional || cancelStore.retries != 0 {
		t.Fatalf("cancel result=%+v err=%v retries=%d", result, err, cancelStore.retries)
	}
	applyCtx, cancelApply := context.WithCancel(context.Background())
	applyStore := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{claims}, applyErr: func() error { cancelApply(); return context.Canceled }}
	result, err = runForegroundProjectSync(applyCtx, applyStore, &testForegroundRemote{disposition: syncservice.DispositionAccepted}, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if !errors.Is(err, context.Canceled) || result.Status != memory.SyncStatusPartial || applyStore.retries != 0 {
		t.Fatalf("apply cancel result=%+v err=%v retries=%d", result, err, applyStore.retries)
	}

	retryStore := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{claims, nil}}
	retryRemote := &testForegroundRemote{retryable: true}
	result, err = runForegroundProjectSync(context.Background(), retryStore, retryRemote, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusPartial || result.Retried != 2 || result.Rejected != 0 || retryStore.applied != 2 {
		t.Fatalf("retry result=%+v err=%v applied=%d", result, err, retryStore.applied)
	}
}

func TestRunForegroundProjectSyncTransportRetryPersistence(t *testing.T) {
	claims := []memory.SyncOutboxClaim{{SyncOutboxEntry: memory.SyncOutboxEntry{Mutation: syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440421"}}, ClaimToken: "550e8400-e29b-41d4-a716-446655440422"}}
	store := &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{claims}, retryErr: errors.New("storage")}
	result, err := runForegroundProjectSync(context.Background(), store, &testForegroundRemote{pushErr: syncclient.ErrUnavailable}, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusPartial || store.retries != 1 {
		t.Fatalf("retry persistence result=%+v err=%v retries=%d", result, err, store.retries)
	}
	store = &projectForegroundStore{claims: [][]memory.SyncOutboxClaim{claims}}
	result, err = runForegroundProjectSync(context.Background(), store, &testForegroundRemote{pushErr: syncclient.ErrUnauthorized}, "project-a", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil || result.Status != memory.SyncStatusUnauthorized || store.retries != 0 {
		t.Fatalf("unauthorized result=%+v err=%v retries=%d", result, err, store.retries)
	}
}

func TestMemorySyncPreflightResultIsProjectBidirectional(t *testing.T) {
	result, err := NewMemory("cli", false).Sync(context.Background(), config.Options{StorageRoot: t.TempDir(), ProjectDir: t.TempDir(), ProjectLocal: true})
	if err != nil || result.Mode != memory.SyncModeProjectBidirectional || result.Status != memory.SyncStatusUnavailable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMemorySyncAndRepairWaitForEnrollmentLock(t *testing.T) {
	storage, workspace := t.TempDir(), t.TempDir()
	opts := config.Options{StorageRoot: storage, ProjectDir: workspace}
	paths, err := config.Prepare(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireSyncEnrollmentLock(context.Background(), paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	runtime := NewMemory("cli", false)
	for name, call := range map[string]func(context.Context) error{
		"sync": func(ctx context.Context) error { _, err := runtime.Sync(ctx, opts); return err },
		"repair": func(ctx context.Context) error {
			_, err := runtime.RepairSyncProject(ctx, opts, workspace, true)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := call(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func batchStore(t *testing.T, count int) *memory.Store {
	t.Helper()
	store := newForegroundStore(t)
	for index := 0; index < (count-2)/2; index++ {
		if _, err := memory.NewMemoryService(store, "cli", nil).Remember(context.Background(), memory.Remember{Content: fmt.Sprintf("sync-%d", index), Project: fmt.Sprintf("project-%d", index), Scope: memory.ScopeProject}); err != nil {
			t.Fatal(err)
		}
	}
	if count%2 != 0 {
		if _, err := memory.NewMemoryService(store, "cli", nil).Remember(context.Background(), memory.Remember{Content: "odd", Project: "default", Scope: memory.ScopeProject}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

type orderedStore struct {
	claims           [][]memory.SyncOutboxClaim
	events           []string
	ownID            string
	ids              []string
	ownErr, claimErr error
	queue            memory.SyncQueueSummary
	queues           []memory.SyncQueueSummary
	resolutionErr    error
}

type projectForegroundStore struct {
	claims            [][]memory.SyncOutboxClaim
	project           string
	applied           int
	retries           int
	applyErr          func() error
	retryErr          error
	translate         func(context.Context, string, string, []syncservice.Mutation) ([]syncservice.Mutation, error)
	appliedID         string
	pullCursor        syncservice.Cursor
	pullErr           error
	applyPageErr      error
	pages             []syncservice.PullPage
	pendingErr        error
	pendingErrs       []error
	repairMutationIDs []string
}

func (store *projectForegroundStore) PendingProjectRepair(context.Context, string, string) error {
	if len(store.pendingErrs) != 0 {
		err := store.pendingErrs[0]
		store.pendingErrs = store.pendingErrs[1:]
		return err
	}
	return store.pendingErr
}

func (store *projectForegroundStore) PendingProjectRepairMutation(context.Context, string, string) (string, error) {
	if len(store.repairMutationIDs) != 0 {
		id := store.repairMutationIDs[0]
		store.repairMutationIDs = store.repairMutationIDs[1:]
		return id, nil
	}
	if len(store.pendingErrs) != 0 {
		err := store.pendingErrs[0]
		store.pendingErrs = store.pendingErrs[1:]
		return "", err
	}
	return "", store.pendingErr
}

func (store *projectForegroundStore) ProjectPullCursor(context.Context, string, string) (syncservice.Cursor, error) {
	return store.pullCursor, store.pullErr
}
func (store *projectForegroundStore) ApplyProjectPulledPage(_ context.Context, _ string, _ string, page syncservice.PullPage) error {
	store.pages = append(store.pages, page)
	if store.applyPageErr != nil {
		return store.applyPageErr
	}
	store.pullCursor = page.Cursor
	return nil
}

func (store *projectForegroundStore) ClaimDueSyncOutboxForProject(_ context.Context, _ time.Duration, _ int, project string) ([]memory.SyncOutboxClaim, error) {
	store.project = project
	claims := store.claims[0]
	store.claims = store.claims[1:]
	return claims, nil
}
func (store *projectForegroundStore) ApplySyncPushResult(_ context.Context, id string, _ string, _ syncservice.Result) error {
	store.applied++
	store.appliedID = id
	if store.applyErr != nil {
		return store.applyErr()
	}
	return nil
}
func (store *projectForegroundStore) TranslateSyncMutations(ctx context.Context, project, localProject string, mutations []syncservice.Mutation) ([]syncservice.Mutation, error) {
	if store.translate == nil {
		return mutations, nil
	}
	return store.translate(ctx, project, localProject, mutations)
}
func (store *projectForegroundStore) MarkSyncOutboxRetry(context.Context, string, string, time.Time, string) error {
	store.retries++
	return store.retryErr
}

type transitionTestStore struct {
	projectForegroundStore
	transition      memory.SyncProjectTransitionResult
	active          bool
	intent          memory.SyncProjectBackupIntent
	prepareErr      error
	prepareResult   memory.SyncProjectTransitionResult
	ensureCalls     int
	backupPaths     []string
	finalizes       int
	finalizeResults []memory.SyncProjectTransitionResult
	recoveredClaims [][]memory.SyncOutboxClaim
	seals           int
	prepareCalls    int
}

func testBackupOps() syncProjectBackupOps {
	return syncProjectBackupOps{
		create: func(context.Context, string, string) error { return memory.ErrCorrupt },
		verify: func(context.Context, string, string) error { return memory.ErrCorrupt },
		digest: func(context.Context, string) ([]byte, error) { return nil, memory.ErrCorrupt },
	}
}

func (store *transitionTestStore) SyncProjectTransition(context.Context, string, string) (memory.SyncProjectTransitionResult, bool, error) {
	return store.transition, store.active, nil
}

func (store *transitionTestStore) EnsureSyncProjectBackupIntent(_ context.Context, _ string, _ string, _ memory.SyncProjectTransitionMode, _ string) (memory.SyncProjectBackupIntent, error) {
	store.ensureCalls++
	if store.intent.BackupPath == "" {
		return store.intent, nil
	}
	store.backupPaths = append(store.backupPaths, store.intent.BackupPath)
	return store.intent, nil
}

func (store *transitionTestStore) PrepareSyncProjectTransitionWithBackupIntent(context.Context, string, string, memory.SyncProjectTransitionMode, bool, memory.SyncProjectBackupIntent) (memory.SyncProjectTransitionResult, error) {
	store.prepareCalls++
	return store.prepareResult, store.prepareErr
}

func (store *transitionTestStore) SealSyncProjectBackupIntent(_ context.Context, _ string, _ string, _ memory.SyncProjectTransitionMode, intent memory.SyncProjectBackupIntent, digest []byte) (memory.SyncProjectBackupIntent, error) {
	store.seals++
	intent.BackupSHA256 = append([]byte(nil), digest...)
	store.intent = intent
	return intent, nil
}

func (store *transitionTestStore) FinalizeSyncProjectTransition(context.Context, string, string) (memory.SyncProjectTransitionResult, error) {
	store.finalizes++
	if store.finalizes == 1 && store.recoveredClaims != nil {
		store.claims = store.recoveredClaims
	}
	if len(store.finalizeResults) != 0 {
		result := store.finalizeResults[0]
		store.finalizeResults = store.finalizeResults[1:]
		return result, nil
	}
	return memory.SyncProjectTransitionResult{Status: memory.SyncProjectTransitionCompleted}, nil
}

func (store *orderedStore) ClaimDueSyncOutbox(context.Context, time.Duration, int) ([]memory.SyncOutboxClaim, error) {
	store.events = append(store.events, "claim")
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	claims := store.claims[0]
	store.claims = store.claims[1:]
	return claims, nil
}
func (store *orderedStore) ApplySyncPushResult(context.Context, string, string, syncservice.Result) error {
	store.events = append(store.events, "apply")
	return nil
}
func (store *orderedStore) MarkSyncOutboxRetry(context.Context, string, string, time.Time, string) error {
	store.events = append(store.events, "retry")
	return nil
}
func (store *orderedStore) BootstrapSync(context.Context, memory.BootstrapRemote) error {
	store.events = append(store.events, "bootstrap")
	return nil
}
func (store *orderedStore) PullConflictResolutions(context.Context, memory.BootstrapRemote) error {
	store.events = append(store.events, "resolutions")
	return store.resolutionErr
}
func (store *orderedStore) BootstrapOwnConflict(_ context.Context, _ memory.BootstrapRemote, mutationID string) error {
	store.events = append(store.events, "own")
	store.ownID = mutationID
	return store.ownErr
}
func (store *orderedStore) PendingOwnConflictReceipts(context.Context) ([]string, error) {
	return store.ids, nil
}
func (store *orderedStore) SyncQueueSummary(context.Context) (memory.SyncQueueSummary, error) {
	if len(store.queues) != 0 {
		queue := store.queues[0]
		store.queues = store.queues[1:]
		return queue, nil
	}
	return store.queue, nil
}

func newForegroundStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := openStore(context.Background(), config.Options{StorageRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.ConfigureSyncProfile(context.Background(), memory.SyncProfile{Enabled: true, Endpoint: "https://sync.example.test", DeviceID: "550e8400-e29b-41d4-a716-446655440000", CredentialRef: "secret://keychain/sync"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = memory.NewMemoryService(store, "cli", nil).Remember(context.Background(), memory.Remember{Content: "sync me"}); err != nil {
		t.Fatal(err)
	}
	return store
}

type testForegroundRemote struct {
	disposition      syncservice.Disposition
	retryable        bool
	capabilityErr    error
	discoverErr      error
	projectPullErr   error
	invalidDiscovery bool
	pushErr          error
	cancelPush       func()
	cancelPull       func()
	capabilities     int
	pushes           int
	discovers        int
	pulls            int
	projectPulls     int
	projectIDs       []string
	cursors          []syncservice.Cursor
	pages            []syncservice.PullPage
	sent             []syncservice.Mutation
}

func (remote *testForegroundRemote) Capabilities(context.Context) error {
	remote.capabilities++
	return remote.capabilityErr
}
func (remote *testForegroundRemote) Discover(context.Context) (syncservice.Discovery, error) {
	remote.discovers++
	if remote.discoverErr != nil {
		return syncservice.Discovery{}, remote.discoverErr
	}
	if remote.invalidDiscovery {
		return syncservice.Discovery{}, nil
	}
	return syncservice.Discovery{ProtocolVersion: 1, HistoryID: "550e8400-e29b-41d4-a716-446655440010", Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, nil
}
func (remote *testForegroundRemote) Pull(_ context.Context, cursor syncservice.Cursor, _ int) (syncservice.PullPage, error) {
	remote.pulls++
	return syncservice.PullPage{Cursor: cursor}, nil
}
func (remote *testForegroundRemote) PullProject(_ context.Context, cursor syncservice.Cursor, project string, _ int) (syncservice.PullPage, error) {
	remote.projectPulls++
	remote.projectIDs = append(remote.projectIDs, project)
	remote.cursors = append(remote.cursors, cursor)
	if remote.cancelPull != nil {
		remote.cancelPull()
		return syncservice.PullPage{}, context.Canceled
	}
	if remote.projectPullErr != nil {
		return syncservice.PullPage{}, remote.projectPullErr
	}
	if len(remote.pages) == 0 {
		return syncservice.PullPage{Cursor: cursor}, nil
	}
	page := remote.pages[0]
	remote.pages = remote.pages[1:]
	return page, nil
}
func (remote *testForegroundRemote) Push(_ context.Context, mutations []syncservice.Mutation) ([]syncservice.Result, error) {
	remote.pushes++
	remote.sent = append(remote.sent, mutations...)
	if remote.cancelPush != nil {
		remote.cancelPush()
		return nil, context.Canceled
	}
	if remote.pushErr != nil {
		return nil, remote.pushErr
	}
	results := make([]syncservice.Result, len(mutations))
	for index, mutation := range mutations {
		if remote.retryable {
			results[index] = syncservice.Result{MutationID: mutation.MutationID, Disposition: syncservice.DispositionRejected, Retryable: true, Code: "temporary"}
			continue
		}
		if remote.disposition == syncservice.DispositionRejected {
			results[index] = syncservice.Result{MutationID: mutation.MutationID, Disposition: remote.disposition, Code: "invalid_input"}
			continue
		}
		sequence := int64((remote.pushes-1)*16 + index + 1)
		results[index] = syncservice.Result{MutationID: mutation.MutationID, Disposition: remote.disposition, Sequence: &sequence, Version: mutation.BaseVersion + 1}
	}
	return results, nil
}

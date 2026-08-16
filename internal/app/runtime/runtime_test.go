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
	result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: storageRoot, ProjectLocal: true})
	if err != nil || result.Status != memory.SyncStatusUnavailable || credentials != 0 || requests != 0 {
		t.Fatalf("project-local sync = %+v, %v; credentials=%d requests=%d", result, err, credentials, requests)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("project-local sync opened storage root: %v", err)
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
		result, err := runtime.Sync(context.Background(), config.Options{StorageRoot: root})
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
	credentialFile := filepath.Join(t.TempDir(), "credential")
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
	file := filepath.Join(t.TempDir(), "credential")
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
	disposition   syncservice.Disposition
	retryable     bool
	capabilityErr error
	pushErr       error
	cancelPush    func()
	capabilities  int
	pushes        int
	discovers     int
}

func (remote *testForegroundRemote) Capabilities(context.Context) error {
	remote.capabilities++
	return remote.capabilityErr
}
func (remote *testForegroundRemote) Discover(context.Context) (syncservice.Discovery, error) {
	remote.discovers++
	return syncservice.Discovery{ProtocolVersion: 1, HistoryID: "550e8400-e29b-41d4-a716-446655440010", Capabilities: []syncservice.Capability{syncservice.CapabilityBootstrapDiscovery}}, nil
}
func (remote *testForegroundRemote) Pull(_ context.Context, cursor syncservice.Cursor, _ int) (syncservice.PullPage, error) {
	return syncservice.PullPage{Cursor: cursor}, nil
}
func (remote *testForegroundRemote) Push(_ context.Context, mutations []syncservice.Mutation) ([]syncservice.Result, error) {
	remote.pushes++
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

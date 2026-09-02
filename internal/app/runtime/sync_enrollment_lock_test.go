package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/secrets"
)

func TestSyncEnrollmentCredentialRefsAreStableSlots(t *testing.T) {
	first, second := syncEnrollmentCredentialRefs("/private/tmp/example/memory.db")
	if first == second || first == "" || second == "" {
		t.Fatalf("slots=%q/%q", first, second)
	}
	if again, _ := syncEnrollmentCredentialRefs("/private/tmp/example/memory.db"); again != first {
		t.Fatalf("unstable slot=%q want %q", again, first)
	}
}

func TestMemoryConfigureSyncRecoversCredentialSlots(t *testing.T) {
	const device = "550e8400-e29b-41d4-a716-446655440000"
	root, values := t.TempDir(), map[string]string{}
	runtime := enrollmentRuntime(values)
	configure := func(token string) error {
		_, err := runtime.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", device, token)
		return err
	}
	if err := configure(testBearerWithSecret(device, 1)); err != nil {
		t.Fatal(err)
	}
	runtime.afterSyncCredentialPut = func() error { return errors.New("crash before profile") }
	if err := configure(testBearerWithSecret(device, 2)); err == nil {
		t.Fatal("pre-commit crash succeeded")
	}
	runtime.afterSyncCredentialPut = nil
	if err := configure(testBearerWithSecret(device, 3)); err != nil || len(values) != 1 {
		t.Fatalf("pre-commit recovery err=%v values=%v", err, values)
	}
	runtime.afterSyncProfileCommit = func() error { return errors.New("crash after profile") }
	if err := configure(testBearerWithSecret(device, 4)); err == nil {
		t.Fatal("post-commit crash succeeded")
	}
	profile := syncEnrollmentProfile(t, root)
	if profile.PreviousCredentialRef == "" || len(values) != 2 {
		t.Fatalf("pending profile=%+v values=%v", profile, values)
	}
	runtime.deleteSecret = func(string) error { return secrets.ErrUnavailable }
	runtime.afterSyncProfileCommit = nil
	if err := configure(testBearerWithSecret(device, 5)); !errors.Is(err, secrets.ErrUnavailable) || syncEnrollmentProfile(t, root).PreviousCredentialRef == "" {
		t.Fatalf("failed recovery err=%v profile=%+v", err, syncEnrollmentProfile(t, root))
	}
	runtime.deleteSecret = func(reference string) error { delete(values, reference); return nil }
	if err := configure(testBearerWithSecret(device, 6)); err != nil {
		t.Fatal(err)
	}
	profile = syncEnrollmentProfile(t, root)
	if profile.PreviousCredentialRef != "" || len(values) != 1 {
		t.Fatalf("recovered profile=%+v values=%v", profile, values)
	}
}

func TestMemoryConfigureSyncSerializesConcurrentEnrollment(t *testing.T) {
	const device = "550e8400-e29b-41d4-a716-446655440000"
	root, values := t.TempDir(), map[string]string{}
	first, second := enrollmentRuntime(values), enrollmentRuntime(values)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	first.configureProfile = func(ctx context.Context, store *memory.Store, profile memory.SyncProfile) (memory.SyncProfile, error) {
		close(entered)
		<-release
		return store.ConfigureSyncProfile(ctx, profile)
	}
	go func() {
		_, err := first.ConfigureSync(context.Background(), config.Options{StorageRoot: root}, "https://sync.example.test", device, testBearerWithSecret(device, 1))
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	secondCalls := 0
	second.configureProfile = func(ctx context.Context, store *memory.Store, profile memory.SyncProfile) (memory.SyncProfile, error) {
		secondCalls++
		return store.ConfigureSyncProfile(ctx, profile)
	}
	_, err := second.ConfigureSync(ctx, config.Options{StorageRoot: root}, "https://sync.example.test", device, testBearerWithSecret(device, 2))
	if !errors.Is(err, context.Canceled) || secondCalls != 0 {
		t.Fatalf("second err=%v calls=%d", err, secondCalls)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func enrollmentRuntime(values map[string]string) Memory {
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

func syncEnrollmentProfile(t *testing.T, root string) memory.SyncProfile {
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

func TestSyncEnrollmentLockHonorsCancellation(t *testing.T) {
	database := filepath.Join(t.TempDir(), "memory.db")
	release, err := acquireSyncEnrollmentLock(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = acquireSyncEnrollmentLock(ctx, database)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lock error=%v", err)
	}
}

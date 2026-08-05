package secrets

import (
	"errors"
	"strings"
	"testing"
)

type fakeBackend struct {
	value string
	err   error
	got   [2]string
	set   [3]string
	dead  [2]string
}

func (f *fakeBackend) Get(service, user string) (string, error) {
	f.got = [2]string{service, user}
	return f.value, f.err
}
func (f *fakeBackend) Set(service, user, value string) error {
	f.set = [3]string{service, user, value}
	return f.err
}
func (f *fakeBackend) Delete(service, user string) error {
	f.dead = [2]string{service, user}
	return f.err
}

func TestStoreUsesInjectedBackendForPutGetDelete(t *testing.T) {
	backend := &fakeBackend{value: "value"}
	store := New(backend)
	if err := store.Put("secret://keychain/sync", "value"); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Get("secret://keychain/sync"); err != nil || value != "value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if err := store.Delete("secret://keychain/sync"); err != nil {
		t.Fatal(err)
	}
	if backend.set != [3]string{"vgxness", "sync", "value"} || backend.got != [2]string{"vgxness", "sync"} || backend.dead != [2]string{"vgxness", "sync"} {
		t.Fatalf("backend=%+v", backend)
	}
}

func TestStoreDistinguishesMissingAndUnavailableWithoutTokenLeakage(t *testing.T) {
	const token = "token-that-must-not-leak"
	for name, test := range map[string]struct {
		err  error
		want error
	}{
		"missing":     {ErrMissing, ErrMissing},
		"unavailable": {errors.New("backend " + token), ErrUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			store := New(&fakeBackend{err: test.err})
			_, err := store.Get("secret://keychain/sync")
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), token) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

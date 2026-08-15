// Package secrets provides the narrow credential boundary used by sync.
package secrets

import (
	"errors"
	"net/url"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

var (
	ErrMissing     = errors.New("secret missing")
	ErrUnavailable = errors.New("secret store unavailable")
	ErrInvalid     = errors.New("invalid secret reference")
	ErrUnsupported = errors.New("secret operation unsupported")
)

// Backend is injectable so callers never depend directly on a platform keyring.
type Backend interface {
	Get(service, user string) (string, error)
	Set(service, user, value string) error
	Delete(service, user string) error
}

// Store stores only in its backend; it has no plaintext fallback.
type Store struct{ backend Backend }

func New(backend Backend) Store { return Store{backend: backend} }

// System returns the production keyring-backed store.
func System() Store { return New(keyringBackend{}) }

func (s Store) Get(reference string) (string, error) {
	service, user, err := secretName(reference)
	if err != nil || s.backend == nil {
		return "", ErrInvalid
	}
	value, err := s.backend.Get(service, user)
	if err != nil || value == "" {
		return "", classify(err, value == "")
	}
	return value, nil
}

func (s Store) Put(reference, value string) error {
	service, user, err := secretName(reference)
	if err != nil || s.backend == nil || value == "" {
		return ErrInvalid
	}
	return classify(s.backend.Set(service, user, value), false)
}

func (s Store) Delete(reference string) error {
	service, user, err := secretName(reference)
	if err != nil || s.backend == nil {
		return ErrInvalid
	}
	return classify(s.backend.Delete(service, user), false)
}

type keyringBackend struct{}

func (keyringBackend) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (keyringBackend) Set(service, user, value string) error {
	return keyring.Set(service, user, value)
}
func (keyringBackend) Delete(service, user string) error { return keyring.Delete(service, user) }

func classify(err error, empty bool) error {
	if err == nil {
		if empty {
			return ErrMissing
		}
		return nil
	}
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrMissing) {
		return ErrMissing
	}
	return ErrUnavailable
}

func secretName(reference string) (string, string, error) {
	u, err := url.Parse(reference)
	if err != nil || u.Scheme != "secret" || u.Host != "keychain" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, "/") {
		return "", "", ErrInvalid
	}
	user := strings.TrimPrefix(u.Path, "/")
	if user == "" || len(user) > 512 || strings.ContainsAny(user, "\x00\r\n\t") {
		return "", "", ErrInvalid
	}
	return "vgxness", user, nil
}

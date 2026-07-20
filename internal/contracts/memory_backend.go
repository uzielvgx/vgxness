package contracts

import (
	"errors"
	"fmt"
)

var ErrInvalid = errors.New("invalid")

func ValidateOwnedMemoryBackend(backend string) error {
	if backend == "memory" {
		return nil
	}
	if backend == "engram" {
		return fmt.Errorf("%w: memory.backend_legacy: backend %q is invalid for owned memory; use %q; external Engram references use provider %q", ErrInvalid, backend, "memory", "engram")
	}
	return fmt.Errorf("%w: unsupported owned memory backend %q", ErrInvalid, backend)
}

func ValidateExternalReference(provider, id string) error {
	if provider == "" || id == "" {
		return fmt.Errorf("%w: external reference requires provider and id", ErrInvalid)
	}
	return nil
}

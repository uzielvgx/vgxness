package app

import (
	"errors"
	"testing"

	"github.com/vgxness/vgxness/internal/memory"
)

func TestWithStoreJoinsWritableCloseFailure(t *testing.T) {
	closeErr := errors.New("close failed")
	_, err := withStore(
		func() (*memory.Store, error) { return &memory.Store{}, nil },
		func(*memory.Store) (string, error) { return "value", nil },
		func(*memory.Store) error { return closeErr },
	)
	if !errors.Is(err, closeErr) {
		t.Fatalf("missing close error: %v", err)
	}
}

func TestWithStoreClosesWhenOperationPanics(t *testing.T) {
	closed := false
	defer func() {
		if recover() == nil {
			t.Fatal("withStore did not propagate panic")
		}
		if !closed {
			t.Fatal("withStore did not close after panic")
		}
	}()
	_, _ = withStore(
		func() (*memory.Store, error) { return &memory.Store{}, nil },
		func(*memory.Store) (string, error) { panic("operation panic") },
		func(*memory.Store) error { closed = true; return nil },
	)
}

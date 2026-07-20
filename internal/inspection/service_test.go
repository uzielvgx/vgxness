package inspection

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestInspection_DiagnosesCorruptDBMalformedOrUnknownChronicleAndCancellation(t *testing.T) {
	t.Run("storage", func(t *testing.T) {
		svc := Service{Health: func(context.Context, string) (int, error) { return 0, errors.New("corrupt database") }}
		_, err := svc.Doctor(context.Background(), config.Options{StorageRoot: t.TempDir()})
		testutil.Require(t, err != nil && errors.Is(err, ErrCorrupt), "expected categorized corruption, got %v", err)
	})
	t.Run("chronicle", func(t *testing.T) {
		root := t.TempDir()
		testutil.NoError(t, os.WriteFile(filepath.Join(root, "current-run.json"), []byte(`{"schemaVersion":"2"}`), 0o600))
		_, err := (Service{Health: healthy}).Status(context.Background(), config.Options{StorageRoot: root})
		testutil.Require(t, err != nil && errors.Is(err, ErrCorrupt), "expected Chronicle corruption, got %v", err)
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := (Service{Health: healthy}).Status(ctx, config.Options{StorageRoot: t.TempDir()})
		testutil.Require(t, errors.Is(err, context.Canceled), "expected cancellation, got %v", err)
	})
}

func TestInspection_DoesNotCreateOrMigrateStorage(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "absent")
		_, err := (Service{Health: healthFile}).Status(context.Background(), config.Options{StorageRoot: root})
		testutil.NoError(t, err)
		_, err = os.Stat(root)
		testutil.Require(t, os.IsNotExist(err), "inspection created storage: %v", err)
	})
	t.Run("version zero", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		db, err := sql.Open("sqlite", path)
		testutil.NoError(t, err)
		testutil.NoError(t, db.Close())
		_, _ = (Service{Health: healthFile}).Doctor(context.Background(), config.Options{StorageRoot: filepath.Dir(path)})
		db, err = sql.Open("sqlite", path)
		testutil.NoError(t, err)
		defer db.Close()
		var version int
		testutil.NoError(t, db.QueryRow(`PRAGMA user_version`).Scan(&version))
		testutil.Require(t, version == 0, "inspection migrated database to version %d", version)
	})
}

func healthFile(ctx context.Context, path string) (int, error) {
	return memory.HealthFile(ctx, path)
}

func healthy(context.Context, string) (int, error) { return 1, nil }

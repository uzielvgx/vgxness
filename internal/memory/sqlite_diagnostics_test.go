package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"modernc.org/sqlite"
)

func TestDiagnosticMigrationErrorIsTypedAndSanitized(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const secret = "diagnostic_secret_identifier"
	_, cause := db.Exec("SELECT * FROM " + secret)
	if cause == nil {
		t.Fatal("invalid SQL succeeded")
	}
	err = migrationError(context.Background(), "read ledger", cause)
	if !errors.Is(err, ErrMigration) || !strings.Contains(err.Error(), "sqlite=other code=1") || strings.Contains(err.Error(), secret) {
		t.Fatalf("error=%v", err)
	}
	var typed *sqlite.Error
	if errors.Is(err, cause) || errors.As(err, &typed) {
		t.Fatalf("migration exposed raw cause: %v", err)
	}
	if diagnostic := sqliteDiagnostic(fmt.Errorf("wrapped: %w", cause)); diagnostic != " sqlite=other code=1" {
		t.Fatalf("wrapped diagnostic=%q", diagnostic)
	}
	plain := migrationError(context.Background(), "read ledger", errors.New(secret))
	if !errors.Is(plain, ErrMigration) || strings.Contains(plain.Error(), secret) || strings.Contains(plain.Error(), "sqlite=") {
		t.Fatalf("generic error=%v", plain)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := migrationError(ctx, "read ledger", cause); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost precedence: %v", err)
	}
}

func TestDiagnosticReportsTypedBusyWithoutSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.Exec("CREATE TABLE entries (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	second, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.Exec("PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	tx, err := first.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO entries VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	_, err = second.Exec("INSERT INTO entries VALUES (2)")
	if err == nil {
		t.Fatal("concurrent write succeeded")
	}
	diagnostic := sqliteDiagnostic(err)
	if diagnostic != " sqlite=busy code=5" && diagnostic != " sqlite=locked code=6" {
		t.Fatalf("diagnostic=%q error=%v", diagnostic, err)
	}
}

func TestDiagnosticConfigurationPreservesSentinelAndCancellation(t *testing.T) {
	for _, stage := range []string{"busy-timeout", "foreign-keys", "wal", "final-timeout"} {
		t.Run(stage, func(t *testing.T) {
			cause := errors.New("secret SQL /tmp/private")
			err := configurationError(context.Background(), stage, cause)
			if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "stage="+stage) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v", err)
			}
			var typed *sqlite.Error
			if errors.Is(err, cause) || errors.As(err, &typed) {
				t.Fatalf("configuration exposed raw cause: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := configurationError(ctx, "wal", errors.New("secret")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost precedence: %v", err)
	}
}

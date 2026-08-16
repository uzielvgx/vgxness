package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

func TestSchemaV10CumulativeIdentity(t *testing.T) {
	const schemaV10CumulativeHash = "541e918b41360b307e5473c37b6b785c63d09aef21c325674c93440378e84a3b"
	// Raw direct schemaV1...schemaV10 concatenation: no separator; excludes schemaV11, seeds, PRAGMA, and test SQL.
	cumulative := schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5 + schemaV6 + schemaV7 + schemaV8 + schemaV9 + schemaV10
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(cumulative)))
	testutil.Require(t, got == schemaV10CumulativeHash, "historical migration bytes are immutable; changing the V1-V10 cumulative schema requires an explicit compatibility decision: got %s want %s", got, schemaV10CumulativeHash)
}
func TestHealthRejectsWeakenedPortableBindingSchema(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.Exec(`DROP TABLE portable_project_identities; CREATE TABLE portable_project_identities (portable_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, workspace_hash TEXT NOT NULL UNIQUE, source TEXT NOT NULL, bound_at TEXT NOT NULL)`)
	testutil.NoError(t, err)
	_, err = store.Health(context.Background())
	testutil.Require(t, errors.Is(err, ErrCorrupt), "Health() error=%v", err)
}
func TestProjectIdentityMigrationFromV12Fixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	testutil.NoError(t, store.Close())
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`DROP TABLE sync_portable_identities; DROP TABLE portable_project_identities; PRAGMA user_version=12`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	store = openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 14, "version=%d err=%v", version, err)
}

func TestHealthRejectsWeakenedSyncPortableIdentitySchema(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.Exec(`DROP INDEX sync_portable_identities_inverse_idx`)
	testutil.NoError(t, err)
	_, err = store.Health(context.Background())
	testutil.Require(t, errors.Is(err, ErrCorrupt), "Health() error=%v", err)
}

package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/vgxness/vgxness/internal/syncservice"
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
	_, err = db.Exec(`DROP TABLE sync_portable_identity_adoptions; DROP TABLE sync_portable_identities; DROP TABLE portable_project_identities; PRAGMA user_version=12`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	store = openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	testutil.Require(t, err == nil && version == 15, "version=%d err=%v", version, err)
}

func TestHealthRejectsWeakenedSyncPortableIdentitySchema(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.Exec(`DROP INDEX sync_portable_identities_inverse_idx`)
	testutil.NoError(t, err)
	_, err = store.Health(context.Background())
	testutil.Require(t, errors.Is(err, ErrCorrupt), "Health() error=%v", err)
}

func TestHealthRejectsWeakenedSyncPortableIdentityAdoptionSchema(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	_, err := store.db.Exec(`DROP TABLE sync_portable_identity_adoptions`)
	testutil.NoError(t, err)
	_, err = store.Health(context.Background())
	testutil.Require(t, errors.Is(err, ErrCorrupt), "Health() error=%v", err)
}

func TestSyncPortableIdentityAdoptionMigrationFromV14PreservesMappings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store := openPath(t, path)
	project, local := "550e8400-e29b-41d4-a716-446655440001", "existing"
	wire := portableSyncUUID(project, string("session"), local)
	_, err := store.db.Exec(`INSERT INTO projects(id,sync_version) VALUES('local',0); INSERT INTO sessions(id,project_id,sync_version) VALUES('existing','local',0); INSERT INTO portable_project_identities(portable_id,project_id,workspace_hash,source,bound_at) VALUES('550e8400-e29b-41d4-a716-446655440001','local','hash','test','now'); INSERT INTO sync_profiles(singleton,enabled,endpoint,device_id,credential_ref,created_at,updated_at) VALUES(1,1,'https://example.test','550e8400-e29b-41d4-a716-446655440099','secret://keychain/sync/test',1,1)`)
	testutil.NoError(t, err)
	_, err = store.db.Exec(`INSERT INTO sync_portable_identities(portable_project_id,record_kind,local_id,portable_id,origin_device_id,created_at) VALUES(?,'session',?,?,?,1)`, project, local, wire, "550e8400-e29b-41d4-a716-446655440099")
	testutil.NoError(t, err)
	testutil.NoError(t, store.Close())
	db, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	_, err = db.Exec(`DROP TABLE sync_portable_identity_adoptions; PRAGMA user_version=14`)
	testutil.NoError(t, err)
	testutil.NoError(t, db.Close())
	store = openPath(t, path)
	defer store.Close()
	version, err := store.Health(context.Background())
	var mappings, adoptions int
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identities`).Scan(&mappings))
	testutil.NoError(t, store.db.QueryRow(`SELECT count(*) FROM sync_portable_identity_adoptions`).Scan(&adoptions))
	inverse, found, inverseErr := store.LocalSyncPortableIdentity(context.Background(), project, "session", wire)
	mutation := syncservice.Mutation{MutationID: "550e8400-e29b-41d4-a716-446655440303", RecordID: local, RecordKind: "session", Kind: syncservice.MutationCreate, Session: &syncservice.Session{ID: local, ProjectID: "local"}}
	translated, translateErr := store.TranslateSyncMutations(context.Background(), project, "local", []syncservice.Mutation{mutation})
	testutil.Require(t, err == nil && version == 15 && mappings == 1 && adoptions == 0 && inverseErr == nil && found && inverse == local && translateErr == nil && len(translated) == 1 && translated[0].RecordID == wire, "version=%d mappings=%d adoptions=%d inverse=%q/%t/%v translated=%+v err=%v", version, mappings, adoptions, inverse, found, inverseErr, translated, err)
}

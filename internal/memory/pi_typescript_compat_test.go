package memory

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

// The Pi migration resources are copied byte-for-byte so a database created by
// either runtime has the same SQLite schema ledger and constraints.
func TestPiTypeScriptMigrationResourcesMatchGo(t *testing.T) {
	root := filepath.Join("..", "..", "packages", "pi", "resources", "migrations")
	for version, step := range migrations {
		name := fmt.Sprintf("%03d_", step.version)
		entries, err := os.ReadDir(root)
		testutil.NoError(t, err)
		var path string
		for _, entry := range entries {
			if len(entry.Name()) > len(name) && entry.Name()[:len(name)] == name {
				path = filepath.Join(root, entry.Name())
				break
			}
		}
		testutil.Require(t, path != "", "missing Pi migration %d", version)
		bytes, err := os.ReadFile(path)
		testutil.NoError(t, err)
		testutil.Require(t, fmt.Sprintf("%x", sha256.Sum256(bytes)) == fmt.Sprintf("%x", sha256.Sum256([]byte(step.sql))), "Pi migration %d differs from Go", version)
	}
}

func TestPiTypeScriptBidirectionalSQLite(t *testing.T) {
	root, rootErr := filepath.EvalSymlinks(t.TempDir())
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	wd, err := os.Getwd()
	testutil.NoError(t, err)
	source := piTypeScriptFileURI(filepath.Join(wd, "..", "..", "packages", "pi", "src"))
	runNode := func(db, body string) {
		script := filepath.Join(root, "fixture.mts")
		testutil.NoError(t, os.WriteFile(script, []byte(body), 0600))
		cmd := exec.Command("node", "--experimental-strip-types", script, db)
		out, err := cmd.CombinedOutput()
		testutil.Require(t, err == nil, "node fixture: %s: %v", out, err)
	}
	goPath := filepath.Join(root, "go.sqlite")
	goDB, err := sql.Open("sqlite", goPath)
	testutil.NoError(t, err)
	testutil.NoError(t, applyMigrations(t.Context(), goDB, migrations))
	testutil.NoError(t, goDB.Close())
	runNode(goPath, fmt.Sprintf("import {DatabaseSync} from 'node:sqlite'; const d=new DatabaseSync(process.argv[2],{readBigInts:true}); d.prepare(\"INSERT INTO projects(id) VALUES(?)\").run('node-project'); d.close();"))
	goDB, err = sql.Open("sqlite", goPath)
	testutil.NoError(t, err)
	var id string
	testutil.NoError(t, goDB.QueryRow("SELECT id FROM projects").Scan(&id))
	testutil.Require(t, id == "node-project", "Go did not read Node write")
	testutil.NoError(t, goDB.Close())
	nodePath := filepath.Join(root, "node.sqlite")
	runNode(nodePath, fmt.Sprintf("import {SQLiteDatabase} from '%s/sqlite/node-sqlite.ts'; import {applyMigrations} from '%s/sqlite/migrations.ts'; const d=new SQLiteDatabase(process.argv[2]); applyMigrations(d); d.execute(\"INSERT INTO projects(id) VALUES(?)\",'node-init'); d.close();", source, source))
	goDB, err = sql.Open("sqlite", nodePath)
	testutil.NoError(t, err)
	testutil.NoError(t, goDB.QueryRow("SELECT id FROM projects").Scan(&id))
	testutil.Require(t, id == "node-init", "Go did not read Node initialized database")
	testutil.NoError(t, goDB.Close())
}

func piTypeScriptFileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}).String()
}

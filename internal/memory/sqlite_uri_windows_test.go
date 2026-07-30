//go:build windows

package memory

import "testing"

func TestSQLiteReadURIWindows(t *testing.T) {
	if got, want := sqliteReadURI(`C:\memory\memory.db`), "file:///C:/memory/memory.db?mode=ro"; got != want {
		t.Fatalf("sqliteReadURI() = %q, want %q", got, want)
	}
}

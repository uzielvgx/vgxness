package buildinfo

import (
	"runtime"
	"testing"
)

func TestCurrentDefaultsAndRendering(t *testing.T) {
	info := Current()
	if info.Version != "dev" || info.Commit != "unknown" || info.Date != "unknown" {
		t.Fatalf("unexpected development metadata: %#v", info)
	}
	if info.GoVersion != runtime.Version() || info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Fatalf("unexpected runtime metadata: %#v", info)
	}
	want := "version=dev\ncommit=unknown\ndate=unknown\ngo_version=" + runtime.Version() + "\nos=" + runtime.GOOS + "\narch=" + runtime.GOARCH + "\n"
	if got := Render(info); got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestCurrentSnapshotsInjectedMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })
	Version = "v0.1.0-alpha.1"
	Commit = "0123456789abcdef0123456789abcdef01234567"
	Date = "2026-07-29T00:00:00Z"

	info := Current()
	Version, Commit, Date = "changed", "changed", "changed"
	if info.Version != "v0.1.0-alpha.1" || info.Commit != "0123456789abcdef0123456789abcdef01234567" || info.Date != "2026-07-29T00:00:00Z" {
		t.Fatalf("Current() did not return an immutable snapshot: %#v", info)
	}
}

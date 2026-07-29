// Package opencodebackup creates and merge-restores verified OpenCode snapshots.
//
// Unix permission bits are enforced for backup-owned objects and newly restored
// objects. Windows does not expose equivalent private ACL management through the
// standard library, so mode-bit verification is unavailable there. Conflict
// replacement is unsupported on every platform; merge restore creates missing
// files exclusively and reports conflicts for manual repair.
package opencodebackup

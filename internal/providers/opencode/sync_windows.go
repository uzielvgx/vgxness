//go:build windows

package opencode

func directoryDurability() string { return "file-sync-namespace-best-effort" }

// syncDirectory is a best-effort namespace boundary on Windows. Regular files
// are flushed before publication; Windows has no supported directory fsync
// equivalent. Namespace operations are atomic/journaled, but power-loss
// directory-entry durability is not claimed.
func syncDirectory(string) error { return nil }

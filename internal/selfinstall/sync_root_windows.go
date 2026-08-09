//go:build windows

package selfinstall

import "os"

// Windows does not provide POSIX directory fsync semantics; FlushFileBuffers
// on the directory handle returned by os.Root fails. File contents are synced
// before publication and the rename itself requests write-through.
func syncRoot(*os.Root) error { return nil }

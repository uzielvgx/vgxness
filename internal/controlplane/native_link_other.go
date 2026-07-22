//go:build !unix

package controlplane

import "os"

// Native file reads remain fail-closed on platforms without the Unix
// link-count boundary used by the first supported broker implementation.
func nativeSingleLink(os.FileInfo) bool { return false }

// The native broker is initially supported only where a stable device/inode
// identity can bind the authorized workspace root to later reads.
func nativeFileIdentity(os.FileInfo) (string, bool) { return "", false }

//go:build unix

package skillregistry

import "syscall"

// openReadFlags is read-only (O_RDONLY == 0) and rejects symlinks and
// non-blocking so an attacker-swapped FIFO cannot block the reader.
const openReadFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

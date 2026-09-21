//go:build !unix

package skillregistry

// openReadFlags is plain read-only on platforms without O_NOFOLLOW/O_NONBLOCK;
// the regular-file and identity checks still apply before and after open.
const openReadFlags = 0

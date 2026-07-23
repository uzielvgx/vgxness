//go:build !windows

package delivery

import (
	"os"
	"syscall"
)

func openDeliveryFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

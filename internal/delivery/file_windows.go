//go:build windows

package delivery

import "os"

func openDeliveryFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrCorrupt
	}
	return os.Open(path)
}

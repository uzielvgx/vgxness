//go:build windows

package pi

import (
	"context"
	"io"
)

type platformProcessLauncher struct{}

var newPlatformProcessLauncher processLauncher = platformProcessLauncher{}

func (platformProcessLauncher) Start(ctx context.Context, name string, args []string, in io.Reader, out, errOut io.Writer) (processHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errWindowsProcessUnsupported
}

var errWindowsProcessUnsupported = unsupportedWindowsProcessError{}

type unsupportedWindowsProcessError struct{}

func (unsupportedWindowsProcessError) Error() string {
	return "pi: Windows process launching is unsupported"
}

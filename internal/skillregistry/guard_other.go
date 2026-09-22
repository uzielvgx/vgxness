//go:build !windows

package skillregistry

import "context"

// kernelGuard is the no-op guard used on platforms without the Windows kernel
// guard. It never creates a file and never blocks, so the existing Unix lock,
// nonce, descriptor, and stale-owner recovery protocol is unchanged.
type kernelGuard struct{}

func acquireKernelGuardWith(ctx context.Context, root rootedFS, o ops) (*kernelGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &kernelGuard{}, nil
}

func (guard *kernelGuard) releaseWith(o ops) {}

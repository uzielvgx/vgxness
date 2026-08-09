package inspection

import (
	"context"
	"errors"
	"fmt"

	"github.com/vgxness/vgxness/internal/config"
)

type Service struct {
	Health func(context.Context, string) (int, error)
}

var (
	ErrCorrupt = errors.New("corrupt")
)

type Result struct {
	Root, Database string
	Migration      int
}

func (s Service) Status(ctx context.Context, opts config.Options) (Result, error) {
	return s.inspect(ctx, opts)
}
func (s Service) Doctor(ctx context.Context, opts config.Options) (Result, error) {
	return s.inspect(ctx, opts)
}

func (s Service) inspect(ctx context.Context, opts config.Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	paths, err := config.PathsFor(opts)
	if err != nil {
		return Result{}, err
	}
	version := 0
	if s.Health != nil {
		version, err = s.Health(ctx, paths.Database)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return Result{}, err
			}
			return Result{}, fmt.Errorf("%w: memory health check failed", ErrCorrupt)
		}
	}
	return Result{Root: paths.Root, Database: paths.Database, Migration: version}, nil
}

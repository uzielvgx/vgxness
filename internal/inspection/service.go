package inspection

import (
	"context"
	"errors"
	"fmt"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/config"
)

type Service struct {
	Health func(context.Context, string) (int, error)
}

var (
	ErrCorrupt = errors.New("corrupt")
	ErrIO      = errors.New("io")
)

type Result struct {
	Root, Database   string
	Migration        int
	ChroniclePresent bool
	RunID            string
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
			if errors.Is(err, context.Canceled) {
				return Result{}, err
			}
			return Result{}, fmt.Errorf("%w: memory health check failed", ErrCorrupt)
		}
	}
	run, present, err := chronicle.ReadCurrent(ctx, paths.CurrentRun)
	if err != nil {
		if errors.Is(err, chronicle.ErrCorrupt) {
			return Result{}, fmt.Errorf("%w: Chronicle current run is malformed", ErrCorrupt)
		}
		return Result{}, fmt.Errorf("%w: Chronicle inspection failed", ErrIO)
	}
	return Result{Root: paths.Root, Database: paths.Database, Migration: version, ChroniclePresent: present, RunID: run.ID}, nil
}

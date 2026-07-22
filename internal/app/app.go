package app

import (
	"context"
	"io"

	"github.com/vgxness/vgxness/internal/cli"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/controlplane"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	installer := selfinstall.New(selfinstall.Config{})
	integrationRuntime := opencode.NewIntegration()
	cliMemory := memoryRuntime{producer: "cli"}
	controlPlane := controlplane.New(controlplane.Options{Memory: memoryRuntime{producer: "vgxness-controlplane"}})
	setupRuntime := setupflow.New(installer, integrationRuntime, func(executable string) (integration.Runtime, error) {
		return opencode.NewManagedIntegration(executable)
	}, controlPlane)
	return cli.RunProductRuntime(ctx, args, stdin, stdout, stderr, inspection.Service{Health: memory.HealthFile}, cliMemory, integrationRuntime, controlPlane, installer, setupRuntime)
}

type memoryRuntime struct{ producer string }

func (runtime memoryRuntime) Save(ctx context.Context, opts config.Options, request memory.SaveRequest) (memory.MemoryResult, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Save(ctx, request)
}

func (memoryRuntime) Search(ctx context.Context, opts config.Options, request memory.SearchRequest) ([]memory.MemoryResult, error) {
	return memory.NewMemoryService(storeRuntime{opts}, "cli", nil).Search(ctx, request)
}

func (memoryRuntime) Get(ctx context.Context, opts config.Options, request memory.GetRequest) (memory.MemoryResult, error) {
	return memory.NewMemoryService(storeRuntime{opts}, "cli", nil).Get(ctx, request)
}

func (runtime memoryRuntime) producerName() string {
	if runtime.producer == "" {
		return "cli"
	}
	return runtime.producer
}

type storeRuntime struct{ opts config.Options }

func (runtime storeRuntime) Save(ctx context.Context, item memory.Observation) (memory.Observation, error) {
	paths, err := config.Prepare(ctx, runtime.opts)
	if err != nil {
		return memory.Observation{}, err
	}
	store, err := memory.Open(ctx, paths.Database, nil)
	if err != nil {
		return memory.Observation{}, err
	}
	defer store.Close()
	return store.Save(ctx, item)
}

func (runtime storeRuntime) Search(ctx context.Context, query memory.Search) ([]memory.Observation, error) {
	store, err := openRead(ctx, runtime.opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Search(ctx, query)
}

func (runtime storeRuntime) Get(ctx context.Context, id, project string, scope memory.Scope) (memory.Observation, error) {
	store, err := openRead(ctx, runtime.opts)
	if err != nil {
		return memory.Observation{}, err
	}
	defer store.Close()
	return store.Get(ctx, id, project, scope)
}

func openRead(ctx context.Context, opts config.Options) (*memory.Store, error) {
	paths, err := config.PathsFor(opts)
	if err != nil {
		return nil, err
	}
	return memory.OpenRead(ctx, paths.Database)
}

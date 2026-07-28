package app

import (
	"context"
	"io"
	"os"

	"github.com/vgxness/vgxness/internal/cli"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/controlplane"
	"github.com/vgxness/vgxness/internal/delivery"
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
	deliveryRuntime, err := delivery.New(mustWorkspace())
	if err != nil {
		return 1
	}
	return cli.RunProductRuntime(ctx, args, stdin, stdout, stderr, inspection.Service{Health: memory.HealthFile}, cliMemory, integrationRuntime, controlPlane, installer, setupRuntime, deliveryRuntime)
}

func mustWorkspace() string {
	workspace, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workspace
}

type memoryRuntime struct{ producer string }

func (runtime memoryRuntime) Remember(ctx context.Context, opts config.Options, request memory.Remember) (memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Remember(ctx, request)
}

func (runtime memoryRuntime) Recall(ctx context.Context, opts config.Options, request memory.Recall) ([]memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Recall(ctx, request)
}

func (runtime memoryRuntime) Recent(ctx context.Context, opts config.Options, request memory.Recent) ([]memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Recent(ctx, request)
}

func (runtime memoryRuntime) Get(ctx context.Context, opts config.Options, request memory.Lookup) (memory.Entry, error) {
	return memory.NewMemoryService(storeRuntime{opts}, runtime.producerName(), nil).Get(ctx, request)
}

func (runtime memoryRuntime) Forget(ctx context.Context, opts config.Options, request memory.Forget) (memory.Entry, error) {
	store, err := openStore(ctx, opts)
	if err != nil {
		return memory.Entry{}, err
	}
	defer store.Close()
	return memory.NewMemoryService(store, runtime.producerName(), nil).Forget(ctx, request)
}

// Save and Search are temporary control-plane adapter spellings. Both route
// immediately into the native core; application and CLI code use the native API.
func (runtime memoryRuntime) Save(ctx context.Context, opts config.Options, request memory.SaveRequest) (memory.MemoryResult, error) {
	return runtime.Remember(ctx, opts, request)
}

func (runtime memoryRuntime) Search(ctx context.Context, opts config.Options, request memory.SearchRequest) ([]memory.MemoryResult, error) {
	return runtime.Recall(ctx, opts, request)
}

func (memoryRuntime) ResolveProject(ctx context.Context, opts config.Options, workspace string) (string, error) {
	store, err := openStore(ctx, opts)
	if err != nil {
		return "", err
	}
	defer store.Close()
	return store.ResolveProject(ctx, workspace)
}

func (runtime memoryRuntime) producerName() string {
	if runtime.producer == "" {
		return "cli"
	}
	return runtime.producer
}

type storeRuntime struct{ opts config.Options }

func (runtime storeRuntime) Save(ctx context.Context, item memory.Observation) (memory.Observation, error) {
	store, err := openStore(ctx, runtime.opts)
	if err != nil {
		return memory.Observation{}, err
	}
	defer store.Close()
	return store.Save(ctx, item)
}

func (runtime storeRuntime) Search(ctx context.Context, query memory.Search) ([]memory.Observation, error) {
	store, err := openStoreRead(ctx, runtime.opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Search(ctx, query)
}

func (runtime storeRuntime) Recent(ctx context.Context, request memory.Recent) ([]memory.Observation, error) {
	store, err := openStoreRead(ctx, runtime.opts)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Recent(ctx, request)
}

func (runtime storeRuntime) Get(ctx context.Context, id, project string, scope memory.Scope) (memory.Observation, error) {
	store, err := openStoreRead(ctx, runtime.opts)
	if err != nil {
		return memory.Observation{}, err
	}
	defer store.Close()
	return store.Get(ctx, id, project, scope)
}

func openStore(ctx context.Context, opts config.Options) (*memory.Store, error) {
	paths, err := config.Prepare(ctx, opts)
	if err != nil {
		return nil, err
	}
	return memory.Open(ctx, paths.Database, nil)
}

func openStoreRead(ctx context.Context, opts config.Options) (*memory.Store, error) {
	paths, err := config.PathsFor(opts)
	if err != nil {
		return nil, err
	}
	return memory.OpenRead(ctx, paths.Database)
}

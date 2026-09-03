package runtime

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/syncclient"
	"github.com/vgxness/vgxness/internal/syncservice"
)

// PlanSyncProject reads local and remote sync state and returns one recommendation.
// It never executes the recommendation or changes local state.
func (runtime Memory) PlanSyncProject(ctx context.Context, opts config.Options) (SyncProjectPlan, error) {
	if ctx == nil || opts.ProjectLocal || opts.ProjectDir == "" || !filepath.IsAbs(opts.ProjectDir) {
		return SyncProjectPlan{}, memory.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return SyncProjectPlan{}, err
	}
	workspace, err := canonicalInvocationWorkspace(opts.ProjectDir)
	if err != nil {
		return SyncProjectPlan{}, memory.ErrInvalid
	}
	portableID, present, err := memory.ReadProjectID(workspace)
	if err != nil {
		return SyncProjectPlan{}, err
	}
	if !present {
		return SyncProjectPlan{}, memory.ErrInvalid
	}
	topology, profile, err := readSyncPlanLocal(ctx, opts, workspace, portableID)
	if err != nil {
		return SyncProjectPlan{}, err
	}
	if topology.PortableProjectID != portableID {
		return SyncProjectPlan{}, memory.ErrCorrupt
	}
	if topology.BoundProjectID != "" && (topology.WorkspaceProjectID != topology.BoundProjectID || topology.WorkspacePortableProjectID != portableID) || topology.BoundProjectID == "" && topology.WorkspacePortableProjectID != "" {
		plan := PlanSyncProject(topology, syncservice.ProjectState{Status: syncservice.ProjectStateAbsent})
		if plan.Action != SyncPlanActionBlockedManual || plan.Reason != SyncPlanReasonBindingMismatch {
			return SyncProjectPlan{}, memory.ErrCorrupt
		}
		return plan, nil
	}
	credential, err := runtime.syncCredential(opts, profile.CredentialRef)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return SyncProjectPlan{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return SyncProjectPlan{}, context.DeadlineExceeded
		}
		return SyncProjectPlan{}, memory.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return SyncProjectPlan{}, err
	}
	if !validBearer(credential, profile.DeviceID) {
		return SyncProjectPlan{}, memory.ErrInvalid
	}
	transport := runtime.transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client, err := syncclient.New(profile.Endpoint, transport)
	if err != nil {
		return SyncProjectPlan{}, memory.ErrInvalid
	}
	remote, err := client.ProjectState(ctx, credential, portableID)
	if err != nil {
		return SyncProjectPlan{}, err
	}
	return PlanSyncProject(topology, remote), nil
}

func readSyncPlanLocal(ctx context.Context, opts config.Options, workspace, portableID string) (topology memory.SyncProjectTopology, profile memory.SyncProfile, err error) {
	store, err := openStoreRead(ctx, opts)
	if err != nil {
		return topology, profile, err
	}
	topology, err = store.ReadSyncProjectTopology(ctx, workspace, portableID)
	if err == nil {
		var found bool
		profile, found, err = store.GetSyncProfile(ctx)
		if err == nil && (!found || !profile.Enabled) {
			err = memory.ErrInvalid
		}
	}
	closeErr := store.Close()
	if err != nil {
		return memory.SyncProjectTopology{}, memory.SyncProfile{}, err
	}
	if closeErr != nil {
		return memory.SyncProjectTopology{}, memory.SyncProfile{}, closeErr
	}
	return topology, profile, nil
}

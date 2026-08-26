package opencode

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestOpenCodeCAREV2Manager58PackageIsExactImmediatePredecessorAndRejectsDrift(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor, err := immediatePredecessor(current)
	testutil.NoError(t, err)
	testutil.Require(t, bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 58")), "CARE-v2/Manager58 predecessor marker changed")

	configDirectory := t.TempDir()
	writeModelPlanBundleFixture(t, configDirectory, predecessor)
	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.Require(t, err == nil && status.State == integration.StatePartial, "CARE-v2/Manager58 status=%+v err=%v", status, err)
	managerPath := configDirectory + "/agents/" + managerAgentName
	testutil.NoError(t, os.WriteFile(managerPath, append(predecessor.agents[managerAgentName], '\n'), 0o600))
	status, err = service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.Require(t, err == nil && status.State == integration.StateDrifted, "one-byte CARE-v2/Manager58 drift status=%+v err=%v", status, err)
}

func TestOpenCodeV54PackageIsExactUpgradeablePredecessor(t *testing.T) {
	current, err := buildModelPlanBundle(sdd.DefaultModelPlanConfig())
	testutil.NoError(t, err)
	predecessor, err := previousV54ModelPlanBundle(current)
	testutil.NoError(t, err)
	testutil.Require(t,
		bytes.Contains(predecessor.agents[managerAgentName], []byte("version: 54")) &&
			!bytes.Contains(predecessor.agents[managerAgentName], []byte(currentManagerCandidateCapsuleContract)),
		"v54 predecessor is not exact: %s", predecessor.agents[managerAgentName])
	fixedLens, err := sdd.NewModelPlanConfig(sdd.PlanMedium, "openai/gpt-5.6-luna", "openai/gpt-5.6-terra", "openai/gpt-5.6-sol")
	testutil.NoError(t, err)
	v53, err := fixedLensV53ModelPlanBundle(fixedLens)
	testutil.NoError(t, err)
	testutil.Require(t, bytes.Contains(v53.agents[managerAgentName], []byte("version: 53")), "v53 predecessor is not exact")

	configDirectory := t.TempDir()
	writeModelPlanBundleFixture(t, configDirectory, predecessor)
	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.Require(t, err == nil && status.State == integration.StatePartial, "status=%+v err=%v", status, err)
	installed, err := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.Require(t, err == nil && installed.State == integration.StateInstalled, "install=%+v err=%v", installed, err)

	testutil.Require(t, artifactSHA256(v53.manifest) == "35f0e54376007532d95c5f4f8aeb4da75ae4775007acde831de8a8e0dee16b82", "schema-v1 v53 manifest=%s", artifactSHA256(v53.manifest))
	v53Directory := t.TempDir()
	writeModelPlanBundleFixture(t, v53Directory, v53)
	managerPath := v53Directory + "/agents/" + managerAgentName
	partial, err := service.Status(context.Background(), integration.Options{ConfigDir: v53Directory})
	testutil.Require(t, err == nil && partial.State == integration.StatePartial, "exact v53 status=%+v err=%v", partial, err)
	testutil.NoError(t, os.WriteFile(managerPath, predecessor.agents[managerAgentName], 0o600))
	mixed, err := service.Status(context.Background(), integration.Options{ConfigDir: v53Directory})
	testutil.Require(t, err == nil && mixed.State == integration.StateDrifted, "mixed v53 status=%+v err=%v", mixed, err)
	testutil.NoError(t, os.WriteFile(managerPath, v53.agents[managerAgentName], 0o600))
	testutil.NoError(t, os.WriteFile(managerPath, append(v53.agents[managerAgentName], '\n'), 0o600))
	drifted, err := service.Status(context.Background(), integration.Options{ConfigDir: v53Directory})
	testutil.Require(t, err == nil && drifted.State == integration.StateDrifted, "one-byte v53 drift status=%+v err=%v", drifted, err)
}

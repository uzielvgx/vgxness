package opencode

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/orchestration"
)

func receiptArtifactPath(root string) string {
	return filepath.Join(root, "vgxness", integration.ReceiptName)
}
func readInstallationReceipt(root string) (integration.InstallationReceipt, []byte, error) {
	data, err := readRegularFile(receiptArtifactPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return integration.InstallationReceipt{}, nil, nil
	}
	if err != nil {
		return integration.InstallationReceipt{}, nil, err
	}
	r, err := integration.DecodeReceipt(data, "opencode")
	if err != nil {
		return integration.InstallationReceipt{}, nil, integration.ErrDrift
	}
	return r, data, nil
}
func currentReceipt(plan modelPlanBundle, plugin []byte) ([]byte, error) {
	files := map[string][]byte{"vgxness/model-plan.json": plan.manifest, "plugins/vgxness-memory-lifecycle.ts": plugin}
	for name, data := range plan.agents {
		files["agents/"+name] = data
	}
	return integration.EncodeReceipt("opencode", orchestration.ManagerContractDigest(), "", files)
}
func parseManagedModelPlan(root string, data []byte) (modelPlanManifest, modelPlanBundle, error) {
	r, receipt, err := readInstallationReceipt(root)
	if err != nil {
		return modelPlanManifest{}, modelPlanBundle{}, err
	}
	if receipt == nil {
		return parseInstalledModelPlanManifest(data)
	}
	if !r.Matches("vgxness/model-plan.json", data) || len(r.Files) != 9 {
		return modelPlanManifest{}, modelPlanBundle{}, integration.ErrDrift
	}
	m, err := decodeModelPlanManifest(data)
	if err != nil {
		return m, modelPlanBundle{}, err
	}
	b, err := currentBundleForManifestConfig(m)
	return m, b, err
}
func currentBundleForManifestConfig(m modelPlanManifest) (modelPlanBundle, error) {
	if m.ConfigV3 != nil {
		return buildModelPlanBundleV3(*m.ConfigV3)
	}
	if m.ConfigV2 != nil {
		return buildModelPlanBundleV2(*m.ConfigV2)
	}
	if m.Config != nil {
		return buildModelPlanBundle(*m.Config)
	}
	return modelPlanBundle{}, integration.ErrInvalid
}

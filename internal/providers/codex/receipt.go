package codex

import (
	"errors"
	"os"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

const receiptPath = "vgxness/" + integration.ReceiptName

func appendReceipt(pkg *Package) error {
	files := map[string][]byte{}
	for _, a := range pkg.Artifacts {
		files[a.Path] = a.Bytes
	}
	data, err := integration.EncodeReceipt("codex", orchestration.ManagerContractDigest(), string(pkg.plan), files)
	if err != nil {
		return err
	}
	pkg.Artifacts = append(pkg.Artifacts, Artifact{Path: receiptPath, Bytes: data})
	return nil
}
func packageReceiptMatches(pkg Package) bool {
	if len(pkg.Artifacts) == 0 {
		return false
	}
	last := pkg.Artifacts[len(pkg.Artifacts)-1]
	if last.Path != receiptPath {
		return false
	}
	r, err := integration.DecodeReceipt(last.Bytes, "codex")
	if err != nil || r.Plan != string(pkg.plan) || len(r.Files) != len(pkg.Artifacts)-1 {
		return false
	}
	for _, a := range pkg.Artifacts[:len(pkg.Artifacts)-1] {
		if !r.Matches(a.Path, a.Bytes) {
			return false
		}
	}
	return true
}
func readReceiptPackage(root *Root) (Package, bool, error) {
	return readReceiptPackageFrom(root, false)
}

func readReceiptPackageFrom(root *Root, recoveryMode bool) (Package, bool, error) {
	data, _, err := root.Read(receiptPath, 16384)
	if errors.Is(err, os.ErrNotExist) && recoveryMode {
		for _, suffix := range []string{".vgxness-remove", ".vgxness-stage"} {
			data, _, err = root.Read(receiptPath+suffix, 16384)
			if !errors.Is(err, os.ErrNotExist) {
				break
			}
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return Package{}, false, nil
	}
	if err != nil {
		return Package{}, true, err
	}
	r, err := integration.DecodeReceipt(data, "codex")
	if err != nil {
		return Package{}, true, integration.ErrDrift
	}
	pkg, err := RenderPlan("v0.0.0", modelplan.Plan(r.Plan))
	if err != nil || len(r.Files) != len(pkg.Artifacts)-1 {
		return Package{}, true, integration.ErrDrift
	}
	for i, a := range pkg.Artifacts {
		if a.Path == receiptPath {
			pkg.Artifacts[i].Bytes = data
			continue
		}
		body, _, err := root.Read(a.Path, maxArtifactBytes)
		if errors.Is(err, os.ErrNotExist) && recoveryMode {
			for _, suffix := range []string{".vgxness-remove", ".vgxness-stage"} {
				body, _, err = root.Read(a.Path+suffix, maxArtifactBytes)
				if !errors.Is(err, os.ErrNotExist) {
					break
				}
			}
		}
		// Missing current bytes can be regenerated only if the receipt matches.
		if errors.Is(err, os.ErrNotExist) && r.Matches(a.Path, a.Bytes) {
			body, err = a.Bytes, nil
		}
		if err != nil || !r.Matches(a.Path, body) {
			return Package{}, true, integration.ErrDrift
		}
		pkg.Artifacts[i].Bytes = body
	}
	pkg.fromReceipt = true
	pkg.SHA256 = aggregateSHA256(pkg.Artifacts)
	return pkg, true, pkg.Validate()
}

package codex

import (
	"crypto/sha256"
	"fmt"
	"github.com/vgxness/vgxness/internal/modelplan"
	"testing"
)

func TestCurrentRendererPreservesNativeBytes(t *testing.T) {
	const marketplace = "217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821"
	const explore = "0154aaf99a32e727c6c115c22315bc7e95d399d01ab301c9fb0295637e4ffcae"
	const plugin = "30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"
	golden := map[string]map[string]string{
		"high": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "1b6fa8716f0c79527b3e92754b9aba0b33361dc2eae0ec6cf22923ee2faa8120",
			"agents/care-challenger.toml": "fa81a751506dc5c1b969dc74014fc8f4d31ebb45f11b70587d0b50796cbfaeab", "agents/care-reviewer.toml": "1d3e13d272fe297a6b1667751442a2281d3d435d99ed2047313dda2655b5c279",
			"agents/care-specialist.toml": "e6c5736a2e30b8d0c593d910073ad4472098f93c34f82b7d4af4c34119f63c0b", "agents/explore.toml": explore,
			"agents/general.toml": "95e57b64da2510402c20c7bfa3b388425366e83043038dd47b6c317fd251b9e0", "agents/verifier.toml": "f80a224b595fba72783f272a241daa1430e7a2a15f6ef646f5c7dbbb7854c508",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
		"low": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "1b6fa8716f0c79527b3e92754b9aba0b33361dc2eae0ec6cf22923ee2faa8120",
			"agents/care-challenger.toml": "ffdd43116bd175f3b66fc1ecc2e69035dee1738e781e5c81c29b75a4a592a754", "agents/care-reviewer.toml": "3fdc1377f7f0794c47800b63e297660c65b5c12ac4c90a777f4377e25f6a79c7",
			"agents/care-specialist.toml": "2a02d6db7e818d53754cb69188ecf0453d34c6ffecae5e76521a4b6d7ec652ed", "agents/explore.toml": "86d2317577dba6098da84224d35ca0b4ee3dbd7c460d4c583f7127e74f11f1db",
			"agents/general.toml": "92b130574f1e94c14f726331f70338a15946d8fc344fe9198ffab3a4d9d5f9a6", "agents/verifier.toml": "3e0131631dcf737a6b1d3b145c3ba14d53cffe0d2ba02db8734556f310e074b0",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
		"medium": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "1b6fa8716f0c79527b3e92754b9aba0b33361dc2eae0ec6cf22923ee2faa8120",
			"agents/care-challenger.toml": "93fd6d95150d131c1875619a59044e294a08b32a8833d2ef92c8c271b3980376", "agents/care-reviewer.toml": "55f6dfad9893417e2b8474dd8eaa3a27a3cbab3b144eed6697253fa54ea557fe",
			"agents/care-specialist.toml": "9d9cf611f13982fab7e2da0aef72f0d9e5149d5a6ef3affc05bfecfcf45d2664", "agents/explore.toml": "4901081f458e810094a93b05288bec5c070ef38b88e5b1f2182517c8c11d89e5",
			"agents/general.toml": "7e673a1e53fa1dcc2dbb960298896a24d7c9b342307db7fae3d351d793a51336", "agents/verifier.toml": "70c8f6543e4b7f534876183a89d4c97591bc8bb8cc6e027879991d002d4b6406",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
		"ultra": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "1b6fa8716f0c79527b3e92754b9aba0b33361dc2eae0ec6cf22923ee2faa8120",
			"agents/care-challenger.toml": "fa81a751506dc5c1b969dc74014fc8f4d31ebb45f11b70587d0b50796cbfaeab", "agents/care-reviewer.toml": "1d3e13d272fe297a6b1667751442a2281d3d435d99ed2047313dda2655b5c279",
			"agents/care-specialist.toml": "e6c5736a2e30b8d0c593d910073ad4472098f93c34f82b7d4af4c34119f63c0b", "agents/explore.toml": "d951208c4e96e1d2df83aa1bce405fd894ec065a12de8b4f3d504a3b61da6f6a",
			"agents/general.toml": "95e57b64da2510402c20c7bfa3b388425366e83043038dd47b6c317fd251b9e0", "agents/verifier.toml": "1cf72f42dc4cae55157c66fda70e5f9448e4d8f6c0217248dda42c308c9c9d34",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
	}
	for plan, files := range golden {
		pkg, err := RenderPlan("v0.0.0", modelplan.Plan(plan))
		if err != nil {
			t.Fatal(err)
		}
		if len(pkg.Artifacts) != len(files)+1 {
			t.Fatal("unexpected artifact inventory")
		}
		for _, a := range pkg.Artifacts {
			if a.Path == receiptPath {
				continue
			}
			got := fmt.Sprintf("%x", sha256.Sum256(a.Bytes))
			if got != files[a.Path] {
				t.Errorf("current bytes changed: %s/%s: want %s, got %s", plan, a.Path, files[a.Path], got)
			}
		}
	}
}

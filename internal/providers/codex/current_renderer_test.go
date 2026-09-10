package codex

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/vgxness/vgxness/internal/modelplan"
	"testing"
)

func TestCurrentRendererPreservesNativeBytes(t *testing.T) {
	var golden map[string]map[string]string
	if err := json.Unmarshal([]byte(`{"high":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"529b4ab990530a29a4c3ff584b6ebd83686c98e5fc4ad30c6619cb9a165378b5","agents/care-challenger.toml":"12b0cfc45eebc426160bfbc8c62020a411764954ae565cf4f8996fd96aadb42c","agents/care-reviewer.toml":"76c32a3bbbc88aa2ea6ffbbd6aace7d7245824bc007e347416cf57a44ad45655","agents/care-specialist.toml":"4be9c752f314d2881917dfa3afce87b5873347a80fbc4414e60d4a68d8fba00f","agents/explore.toml":"73b293e5e100af0da32c16e2161647b228d20572b8791be36a04d8340b221127","agents/general.toml":"c0a780fb4b367d0ee08d6282c3f88a5ebdf4aea66b50b692c18760b5f3306049","agents/verifier.toml":"dd8dd2255c5d7afed980f44fb1d8a53fb513d7d6f6c3b3b860d8ccb8ac1e04a0","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"},"low":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"529b4ab990530a29a4c3ff584b6ebd83686c98e5fc4ad30c6619cb9a165378b5","agents/care-challenger.toml":"1db17e474e0a3148c7bca64f55e8ca15ce3e71c6013f6d642ec38d4dab394208","agents/care-reviewer.toml":"e6821d6bfb72f72deeefd784e7c409ea3bf2b3bd374a936e7bf0863f8139bbe1","agents/care-specialist.toml":"357f15613b17a269f6db10e8a9b09cf4b56c79ab0e0c26181b48689b25f0ad3d","agents/explore.toml":"46cd8a02522f4cfc095f88c0f0565402a7174c341e5a29a41a5dba291b8da9f6","agents/general.toml":"7e86b4ea9d79cb4bb6abebb1226b7313416926ea511e09ac2e41e99501fef417","agents/verifier.toml":"894f4de77e55c984a72514d46afe2232bd0b22637083f9b2d028a0f9cef89e7a","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"},"medium":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"529b4ab990530a29a4c3ff584b6ebd83686c98e5fc4ad30c6619cb9a165378b5","agents/care-challenger.toml":"15d1ab0f3b09dce10a7b6bfb3d634a2512a8be6803763ec9d7359ee713a9c830","agents/care-reviewer.toml":"b336bc137d069dd4790776f6de49d3876aad6f1db9db213f4cae8d63f9f3eedb","agents/care-specialist.toml":"d9862406b7d5935ec3daac7fa5a209fe9b0f4337eae24d5f32191525d18ce1bb","agents/explore.toml":"198cd4e0f2bcac58eda397b0bda138c54920c259c510a08a7b49d75f0f28a7a6","agents/general.toml":"195f752313109ab7d3dd4a12d51f32a3a89c7785791f661d0e3cb5be99f40343","agents/verifier.toml":"955f36c1e5e278bc1579095f7c664ce72de06ea312ff542cc7449d0aa48413eb","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"},"ultra":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"529b4ab990530a29a4c3ff584b6ebd83686c98e5fc4ad30c6619cb9a165378b5","agents/care-challenger.toml":"12b0cfc45eebc426160bfbc8c62020a411764954ae565cf4f8996fd96aadb42c","agents/care-reviewer.toml":"76c32a3bbbc88aa2ea6ffbbd6aace7d7245824bc007e347416cf57a44ad45655","agents/care-specialist.toml":"4be9c752f314d2881917dfa3afce87b5873347a80fbc4414e60d4a68d8fba00f","agents/explore.toml":"17f6e99fc6bc702c91f8eca78e0405f9514d0337b2018a819b1e50dc818a6d32","agents/general.toml":"c0a780fb4b367d0ee08d6282c3f88a5ebdf4aea66b50b692c18760b5f3306049","agents/verifier.toml":"d0a8beb8c6d55f7a45608e648d05b9e915dcc8b717aabfb8d59d64d80be2a4f2","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"}}`), &golden); err != nil {
		t.Fatal(err)
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
			if fmt.Sprintf("%x", sha256.Sum256(a.Bytes)) != files[a.Path] {
				t.Errorf("current bytes changed: %s/%s", plan, a.Path)
			}
		}
	}
}

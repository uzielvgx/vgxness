package codex

import (
	"crypto/sha256"
	"fmt"
	"github.com/vgxness/vgxness/internal/modelplan"
	"testing"
)

func TestCurrentRendererPreservesNativeBytes(t *testing.T) {
	const marketplace = "217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821"
	const explore = "73b293e5e100af0da32c16e2161647b228d20572b8791be36a04d8340b221127"
	const plugin = "30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"
	golden := map[string]map[string]string{
		"high": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "2274aa65c0d138282074712b3fdda79362e1a0820e42ece5469a2e745bd9a649",
			"agents/care-challenger.toml": "9738dc81d6f89ec943b9d3fb0d2b8f787784e7a03ddf77e2e7c0952140aad53b", "agents/care-reviewer.toml": "ea36f476313d2305c2c1f4b1ad774f36a38d80ac35d028956887dc52c6010cde",
			"agents/care-specialist.toml": "b6cbe4ee0822f76e0ce575a7b36b8120cf3e7fa5ef3163c8b4336d4c5f46abb8", "agents/explore.toml": explore,
			"agents/general.toml": "2afc7bdb6fa47b89cc8f734cde476310209f3b4ab96eafd44f071b68637c0e74", "agents/verifier.toml": "8d9e7b0e3c77f352118f5860eae5809e9cab1172a2e5e4562a2dedfd420df143",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
		"low": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "2274aa65c0d138282074712b3fdda79362e1a0820e42ece5469a2e745bd9a649",
			"agents/care-challenger.toml": "794825567962bd8495c850a952bfa731a20b014c7f46c244e259a46eea9fb759", "agents/care-reviewer.toml": "620110bd1c9ce280b3ac5860a9c406f37d7ad9ad7f2340382fff615865173ec8",
			"agents/care-specialist.toml": "c7c27cb77668cd19b8b2f9b927e88e6baa26f33bc2885803f5113989df9fd4a8", "agents/explore.toml": "46cd8a02522f4cfc095f88c0f0565402a7174c341e5a29a41a5dba291b8da9f6",
			"agents/general.toml": "4f70081d2e2661501555e3c6e6ede93cee5dc713ff3e2ae539bb56d25a95042b", "agents/verifier.toml": "434f2c2cb05f5537f4c652e3d2ebc4b15cae47b4f63a63834abeedc7f2af175c",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
		"medium": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "2274aa65c0d138282074712b3fdda79362e1a0820e42ece5469a2e745bd9a649",
			"agents/care-challenger.toml": "d7923436fe9527420f7bc90a513a343552e961191634e6b1ba6ae673e92b2608", "agents/care-reviewer.toml": "f0632eb161110fada1d0035baa847d76d321e447cf42d74f6d09746c3c507819",
			"agents/care-specialist.toml": "a660f3fe3ff69453e027f66ef0d349d9cb5b8c3e9a0c6b7180a25032ee8d54ff", "agents/explore.toml": "198cd4e0f2bcac58eda397b0bda138c54920c259c510a08a7b49d75f0f28a7a6",
			"agents/general.toml": "911a55bf24621cd711af8bfe08afb9da8c0d0ee791db3c91ae7f2e6565eeba3f", "agents/verifier.toml": "6d5d0e23f9958aada5e61454ad6b4b8e74ee6ea8b7999654029c43a26b40ac4c",
			"plugins/vgxness/.codex-plugin/plugin.json": plugin,
		},
		"ultra": {
			".agents/plugins/marketplace.json": marketplace, "AGENTS.md": "2274aa65c0d138282074712b3fdda79362e1a0820e42ece5469a2e745bd9a649",
			"agents/care-challenger.toml": "9738dc81d6f89ec943b9d3fb0d2b8f787784e7a03ddf77e2e7c0952140aad53b", "agents/care-reviewer.toml": "ea36f476313d2305c2c1f4b1ad774f36a38d80ac35d028956887dc52c6010cde",
			"agents/care-specialist.toml": "b6cbe4ee0822f76e0ce575a7b36b8120cf3e7fa5ef3163c8b4336d4c5f46abb8", "agents/explore.toml": "17f6e99fc6bc702c91f8eca78e0405f9514d0337b2018a819b1e50dc818a6d32",
			"agents/general.toml": "2afc7bdb6fa47b89cc8f734cde476310209f3b4ab96eafd44f071b68637c0e74", "agents/verifier.toml": "35940f330bb199a0209ca0cdf4b498becc75e48b3f1177ad405754210705f3f2",
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

package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/vgxness/vgxness/internal/modelplan"
	"testing"
)

func TestCurrentRendererPreservesNativeArtifacts(t *testing.T) {
	expected := map[modelplan.Plan]map[string]string{
		"high": {
			"explore.md":                 "275597ab04c80f7ebb056c2859962aec17ba6d53d752eb1e6becccb9f97f0210",
			"general.md":                 "57be6f7f0fae411eca8d0008404f052b6b12a653c35846342c325231500c42f1",
			"vgxness-care-challenger.md": "9d482fe430687b890c84c7742142b551166a390bd2eef16b8623220a85fc64c9",
			"vgxness-care-reviewer.md":   "bf225f49a6705fc9943f03429fe006353bb777e89a9b93edc94e7eca3b98eb69",
			"vgxness-care-specialist.md": "4db40adef1818933da9bd59c77aa76d799118e99be84268504a344c1b9d9aa2f",
			"vgxness-manager.md":         "5d284a34a1fb8f0b52dcb30909230ed762493cfe3dff5b33f36019a58f15fc3d",
			"vgxness-verifier.md":        "31b49fb33fb8526395b4fc2eb2b4d3d79ba8f34d08f51d515a16299a580f4505",
		},
		"low": {
			"explore.md":                 "afda6dc7c5c99f1490f964f4690d4aa6da7ad5ed805344623748d1a78cdb5e6b",
			"general.md":                 "908d2589197d667a9b1b3bb5e7360b3d93700c9e2eea741cfeadbaac9d9e4015",
			"vgxness-care-challenger.md": "94059a6e04110ddb44b1eefad1b486aaef0bcc3265444f56eb216b41906606c6",
			"vgxness-care-reviewer.md":   "df87b66d88f59b1a80c4f5a92c4c4f9137f8a0983ebadf227e86cc97f4d540c0",
			"vgxness-care-specialist.md": "8e9eb45f7f48bfba93bd3d0cfcc7c1b8573a9967d67c1fa0d940bf95453500dc",
			"vgxness-manager.md":         "280e1b1a14871f12f474ce396f29e75d53eb1d57896b41d0030e0612d43e6e93",
			"vgxness-verifier.md":        "2cad621d3db29eac0eef722aa25c047bf9e063899f226d57fd3c4c10fa0636c4",
		},
		"medium": {
			"explore.md":                 "275fb1c998574e39591f3dffb5e33ddb9384fb34c354b17e216529525a45d470",
			"general.md":                 "7d1c90bc2c9b25b1d5d19a090af201879b9e2eb9d3e2ad681da78c37045240e9",
			"vgxness-care-challenger.md": "d82b89a623657b60211cbba83dd9239eaaae6b82e77558bf148324a48b3d0a06",
			"vgxness-care-reviewer.md":   "212b40afff636ad0aa382b49bb898d9f972ab5686e3b13d33853fcff587156fb",
			"vgxness-care-specialist.md": "c90178a3eab4839714a87dbb206b6dd5dd228fec586fc845af5d66d248d63863",
			"vgxness-manager.md":         "2da20af9b96dd8a7352260ac5c6eb4f42db70cf80d920e49e24fc35988789f9c",
			"vgxness-verifier.md":        "eb7eecf60f8903c2a681aa405110e52a19ee3e8bfb0d149320aa8896bd44b6e3",
		},
		"ultra": {
			"explore.md":                 "a365d14a4d3459e325943ebc0bb0c6f565bcdd6dac45b94bd058e97dd530b36d",
			"general.md":                 "57be6f7f0fae411eca8d0008404f052b6b12a653c35846342c325231500c42f1",
			"vgxness-care-challenger.md": "9d482fe430687b890c84c7742142b551166a390bd2eef16b8623220a85fc64c9",
			"vgxness-care-reviewer.md":   "bf225f49a6705fc9943f03429fe006353bb777e89a9b93edc94e7eca3b98eb69",
			"vgxness-care-specialist.md": "4db40adef1818933da9bd59c77aa76d799118e99be84268504a344c1b9d9aa2f",
			"vgxness-manager.md":         "5d284a34a1fb8f0b52dcb30909230ed762493cfe3dff5b33f36019a58f15fc3d",
			"vgxness-verifier.md":        "4793bf3d7ac5d0770d9633217e51411af74754b32300ecae874f957b4dfcb7fc",
		},
	}
	for plan, want := range expected {
		c := modelplan.DefaultModelPlanConfig()
		c.ActivePlan = plan
		b, err := buildModelPlanBundle(c)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.agents) != 7 {
			t.Fatal("unexpected agents")
		}
		for name, digest := range want {
			sum := sha256.Sum256(b.agents[name])
			got := hex.EncodeToString(sum[:])
			if got != digest {
				t.Errorf("%s/%s changed native artifact: want %s, got %s", plan, name, digest, got)
			}
		}
	}
}

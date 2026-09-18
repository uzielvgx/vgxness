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
			"explore.md":                 "653373e73c96171dff3d054252037acc09989ab5222750e511272ed69e816589",
			"general.md":                 "50be29567a60459f1dc94f8c226a00ff13af8559b06d304a71bb6255a2ffd039",
			"vgxness-care-challenger.md": "2db95af4fa399e44362195cea4b3b3ebaf5116dc7169ae7f58d4683cfac3c0c0",
			"vgxness-care-reviewer.md":   "e0a2aa218e19e0b656e40649283bd6be2c24087ef81d81167538aacefdbf3a32",
			"vgxness-care-specialist.md": "2e0a0fda6940b1ad7a718db53e59dd230352fbc9f772cef1bb2922c371308666",
			"vgxness-manager.md":         "2dab2328b2137aa29205e957b14f939796c41c8a1092099e7c3f129aa1c145f0",
			"vgxness-verifier.md":        "68105189fa48eb2d4f98632bfe1c625c94f8f0a4a2ae0205a7d8e6435eac9b3e",
		},
		"low": {
			"explore.md":                 "cd781db31a207dcdd1c094cd74e91b43a75287747ee90c8b02b7fb6d903fa8af",
			"general.md":                 "d274e6a3e8073db7c9dca5365b927b3059889fabf23e4c5c7ba1dd5d1f101c02",
			"vgxness-care-challenger.md": "e91f8e929869ce565deda101941f162555db1fc7d5f36e8c08e2edf25363c33b",
			"vgxness-care-reviewer.md":   "69ca73dba11e1bc0061f683585293047cbde27ea0f8cac1ba383389a571332bc",
			"vgxness-care-specialist.md": "a28600c4bdc912110a2eb2fd7730c33d1940878e46981bf35c93a99d5c70edbd",
			"vgxness-manager.md":         "d5dad7a03f738d597978ec998a6d92335172dabe19e905afdedd1de56d9f94ee",
			"vgxness-verifier.md":        "342caddd012351171584bcd86874860702fd2ede07061c903da9ada3712768db",
		},
		"medium": {
			"explore.md":                 "54786b44473fc06e9b9eb6a179fd6b7f3dbe592420460742b295cd28b0de0769",
			"general.md":                 "c76906ae5d95709b6781c1a2283db65fee87028cbcb27e814b2b088f26b58cec",
			"vgxness-care-challenger.md": "2c570c56c0ef8b87b778d929c96bfe999c9d3a9d161a8e2f6c9cb90132686a26",
			"vgxness-care-reviewer.md":   "e12724ed8b5f83389b50bbc53e0eb494ff491311f8691ca4a7addc6433a7d2dc",
			"vgxness-care-specialist.md": "5275782dd00ff9685e6e56b308b7a9533db39de770622e15e7e0683e4bb22f30",
			"vgxness-manager.md":         "61cf363b2190d271930da132918d6554b814fc057f5ba62eaa2c85851225e411",
			"vgxness-verifier.md":        "95e87996fd2eca0f70e60fd12c2d3c8ef3efd45d41198646fa58c4cc222ed645",
		},
		"ultra": {
			"explore.md":                 "318432c7df43ebaa1e9e3fa83d3e61bab7aa9c236c4e92f74ada31c36d28d3d1",
			"general.md":                 "50be29567a60459f1dc94f8c226a00ff13af8559b06d304a71bb6255a2ffd039",
			"vgxness-care-challenger.md": "2db95af4fa399e44362195cea4b3b3ebaf5116dc7169ae7f58d4683cfac3c0c0",
			"vgxness-care-reviewer.md":   "e0a2aa218e19e0b656e40649283bd6be2c24087ef81d81167538aacefdbf3a32",
			"vgxness-care-specialist.md": "2e0a0fda6940b1ad7a718db53e59dd230352fbc9f772cef1bb2922c371308666",
			"vgxness-manager.md":         "2dab2328b2137aa29205e957b14f939796c41c8a1092099e7c3f129aa1c145f0",
			"vgxness-verifier.md":        "99e3eb217fced90cd0a49c4d3711d2ea3d5b67d071191f31e431d328e0833be7",
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

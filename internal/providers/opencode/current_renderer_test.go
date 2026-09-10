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
			"explore.md":                 "36749ba7f3c605059d7e75d15b9431da5ef424a526d091daa4e1bdaccb8b81ee",
			"general.md":                 "ba08704659c4fae697b5843d899269c8864cc637b0190abcaeaae1cd1c7d8be2",
			"vgxness-care-challenger.md": "5d844fac71a57d439ac2d8e4379457448ffdcbaa0112d1c41805c2f8ed423bf6",
			"vgxness-care-reviewer.md":   "79e6a1c5486e2166371112b4c8fecc58a76dd858f8deb7c3bba8b5d5229d01c1",
			"vgxness-care-specialist.md": "cd90396b1ee53236e29caf85b1e3a0556056268256fc3f1a747ec598987efa49",
			"vgxness-manager.md":         "fa63e9ca21f8f73bf9e831c5db18d1e3377bd390aef516d27975fec68afc218f",
			"vgxness-verifier.md":        "db7017c7bbffb2311b68970ad2156bd1b03f1cfd5e461d5cbaaa272abf3a53ee",
		},
		"low": {
			"explore.md":                 "d0ec97b255631e652cec6a36044b036c9be497dd229828ded484c1e40acac07a",
			"general.md":                 "b364cc5491d5292c9f285bc27277bb7f27c77b89e25757b8abe0e732ff319a94",
			"vgxness-care-challenger.md": "0b7f2d8dd57a063c429226754267f513fa260909f8bfcbfbbdf32cd903a44372",
			"vgxness-care-reviewer.md":   "075b87d835cbc9c30bff160412b0ea7fcd6a282318ac8a1cf7b56299fafee127",
			"vgxness-care-specialist.md": "76b04295104ad053dc097bb481c0e1c7958c48d5cbd554b511f07782023ea09c",
			"vgxness-manager.md":         "e7e57cbcf62a3d8b1e26f0449f436d609fdb0702365e78215ba50bd97a9d951e",
			"vgxness-verifier.md":        "f325b1cba081efee961c02925340719cd6bbbff7f400c5fc4ae15272a2a277d9",
		},
		"medium": {
			"explore.md":                 "9455bcfd23515593824c1dda4addffa2b7a31bb195fd0dfca7c9218ed9a67774",
			"general.md":                 "2a43621736f557df5dff346bf2087c5c2f3f024a14eee1edfc00e1476312a900",
			"vgxness-care-challenger.md": "ca825c336a2c1d739d3bfad23647f5b8135a45adec86b8728b52e4dd06ca9c3b",
			"vgxness-care-reviewer.md":   "51e77b1f597d22adc845f4564e5ad43aaffa57da22faf5bf6133ca43f254cb03",
			"vgxness-care-specialist.md": "80fa426d5e56d0d2700b0726d284d67ec3e6879ab468e4566193446f0ad04766",
			"vgxness-manager.md":         "abd67a990b63bb25e576d16f6d7aaa738eb582aca3a39bacfda2513b01f59a4a",
			"vgxness-verifier.md":        "42e01498480359b1b0fc975995fde0e0ff3a1175d2cb283fec4bda7f64dbc484",
		},
		"ultra": {
			"explore.md":                 "1640b50ff491283f56555eb7a0b42e0d769495829c544cfe8b05a1f588bd5cad",
			"general.md":                 "ba08704659c4fae697b5843d899269c8864cc637b0190abcaeaae1cd1c7d8be2",
			"vgxness-care-challenger.md": "5d844fac71a57d439ac2d8e4379457448ffdcbaa0112d1c41805c2f8ed423bf6",
			"vgxness-care-reviewer.md":   "79e6a1c5486e2166371112b4c8fecc58a76dd858f8deb7c3bba8b5d5229d01c1",
			"vgxness-care-specialist.md": "cd90396b1ee53236e29caf85b1e3a0556056268256fc3f1a747ec598987efa49",
			"vgxness-manager.md":         "fa63e9ca21f8f73bf9e831c5db18d1e3377bd390aef516d27975fec68afc218f",
			"vgxness-verifier.md":        "82edcaa78e6fc667c3401c81ef90cd104d484f914ff47be8c576d55f6e353c51",
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
			if hex.EncodeToString(sum[:]) != digest {
				t.Fatalf("%s/%s changed native artifact", plan, name)
			}
		}
	}
}

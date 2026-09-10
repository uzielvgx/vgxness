package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/vgxness/vgxness/internal/orchestration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestSharedCurrentCodexProjection(t *testing.T) {
	c, e := orchestration.LoadManagerContract()
	if e != nil {
		t.Fatal(e)
	}
	p, e := Render("v0.0.0")
	if e != nil {
		t.Fatal(e)
	}
	files := map[string]string{}
	for _, a := range p.Artifacts {
		files[a.Path] = string(a.Bytes)
	}
	if !strings.Contains(files["AGENTS.md"], c.RenderManagerSections()) {
		t.Fatal("current Manager does not use shared policy")
	}
	for _, r := range c.Roles {
		value := files["agents/"+r.ID+".toml"]
		var instructions string
		for _, line := range strings.Split(value, "\n") {
			if strings.HasPrefix(line, "developer_instructions = ") {
				instructions, e = strconv.Unquote(strings.TrimPrefix(line, "developer_instructions = "))
				if e != nil {
					t.Fatal(e)
				}
			}
		}
		if !strings.Contains(instructions, r.Instructions) {
			t.Errorf("role %s not shared", r.ID)
		}
	}
}
func TestExactManager19Predecessors(t *testing.T) {
	var golden map[string]map[string]string
	if e := json.Unmarshal([]byte(`{"high":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"11625aa8f821c1f3a115a49b620ac83ab7b1f0823d9917896b8d45900b46f993","agents/care-challenger.toml":"c8a59bba361bd72e9f5e614f7e84c3aef78d85bf683a5126e21079ce42c8d39b","agents/care-reviewer.toml":"c64d907e7dcb752c77f76003b049b1dab180c56aaf017ad92a1f41e733f89e7e","agents/care-specialist.toml":"542e80baab01c22031709a2a193f7ae689b20a2be6d8704403b3d2a7db33eeb1","agents/explore.toml":"af98f87519a98b81f8d56b65f4c852bd91dd352a9a7c8cef5f24fd1a36f1aada","agents/general.toml":"1a220f7f5a7b682213daad3dd8982208f6c6ea7ec73ed4035c1be184b111df4c","agents/sdd-apply.toml":"22ee69a332219dc5e57456f80a70aa9ef3a73bd392d0b4b34a1af4cef7259a53","agents/sdd-design.toml":"73f8b80266f73d1b658224a4c5a4ea8fe282fbe8ddefdd8a42aa961e4f797e4d","agents/sdd-proposal.toml":"b9aab2eebbf482ac008f47f9e2f1e7ff2f25e698e3d6442f92582ec9e2cc587e","agents/sdd-research.toml":"97d7579a0ffebd9aae450a8adc47fb387f2bf5f424f9c88ffe08167a88e739cb","agents/sdd-spec.toml":"cbb4c94c499811fe880cfc3d49dc65955e86e40d0fc37cac16413ac00f46b7fd","agents/sdd-tasks.toml":"d9df27a31e916eacf0635a4c93dc5e926b0003efcf0551e2590002cb7cdfe2fb","agents/verifier.toml":"99c2e21ae340c28c728702ca3486be1e184fea30fc00fcc8c66dba327742976d","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"},"low":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"11625aa8f821c1f3a115a49b620ac83ab7b1f0823d9917896b8d45900b46f993","agents/care-challenger.toml":"e287ddf6fb7212b9bd5eda0aafa33e23b1fe5fd1dcb63317e03b96f28ee316ef","agents/care-reviewer.toml":"f6512e877621da94539784d6bf1e051f6a2c3abe7d1341897f6bd40344738e7d","agents/care-specialist.toml":"f4f6c86de7aa8b9e9ecb7a4ea7a89f82b5b79665f5c917f3c139494a0556a25d","agents/explore.toml":"df95d2693e62f32cfa808d344943a125f853dad923f66ca5e426515a665dcc80","agents/general.toml":"9dbd98ec7fa70389f5679a9e7318b08ac32d94d4492fc0b761a4450306c373f1","agents/sdd-apply.toml":"dc87306675c44fc8d47dd46311931c389249f6ef396b6875511b45c5f8174ac1","agents/sdd-design.toml":"c33051b86540d27c6f8def9426432593bb43def57b0f14a95b25f6167249a4b0","agents/sdd-proposal.toml":"88b7dbb3c8985c3b9a4e5d559d96b90d89e121f4253b7885462c382f33cb0646","agents/sdd-research.toml":"3dc4ad5d6d04d895f9cc8b990d7425342c906582b344286ee30578acf0d03a94","agents/sdd-spec.toml":"527df0a01ae97bc702b55830a7ea72f08d293208eaf2483e5524d9608a03f617","agents/sdd-tasks.toml":"947f9ba10413cd4021ee9f3b0b5b779a784656a19e94308eabc327b2b8c645d3","agents/verifier.toml":"88f31ae1e4e3c567a5628da02e88449f1959299ddeaeee72b3b32103eb8d8cf4","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"},"medium":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"11625aa8f821c1f3a115a49b620ac83ab7b1f0823d9917896b8d45900b46f993","agents/care-challenger.toml":"e4ea875cb67280e8925446459544d34a31dba563b632034f4c4ce8476281fdaf","agents/care-reviewer.toml":"48e2d2f0d3bed69a387c967dc0a0dcc05c9952243c0f94a722b6ca6448767534","agents/care-specialist.toml":"cca26ecba85b44cbdf316505ae0d168bf8541036d55869812a87b6011208e54f","agents/explore.toml":"a8a01bc756492f60a7e4d12dffcaae496dced8c9fde1139396e1e1008bc8c456","agents/general.toml":"afe89d41b60b5b4c6ccae10217329aead95fd8997d6cb42798bf7907c8496006","agents/sdd-apply.toml":"c14ffc0eb562c3e4d2b8558f178f76351f9114113500755ebd14d871ca58144e","agents/sdd-design.toml":"709288a685535a7b49751235551c42504cbc2c38638cd078c836348edd044e7f","agents/sdd-proposal.toml":"265f732cb510c84aba55f6135baf5e854970d873b43e8fcd8a33bdc6f2c60e9e","agents/sdd-research.toml":"6a742443c781db400f9bd4bfffa181b90082f72188f3debe654511777989b08c","agents/sdd-spec.toml":"dfccae05c08f55bc6b90323cc21bd2572a7811ade23576fa2165ae0f8490269a","agents/sdd-tasks.toml":"d7ce9f58e31b3e6eead94f4a183e0460d4ce5abfb734fff004eec1d9a187fe65","agents/verifier.toml":"8c369a8cb13b3b4970771ba7f9b807ae97591c5213e39cd0c390b8620f874b97","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"},"ultra":{".agents/plugins/marketplace.json":"217bb8e1a5c3e5199f1f14dda13ea1e132bf4140903c155452e4e2b31e241821","AGENTS.md":"11625aa8f821c1f3a115a49b620ac83ab7b1f0823d9917896b8d45900b46f993","agents/care-challenger.toml":"c8a59bba361bd72e9f5e614f7e84c3aef78d85bf683a5126e21079ce42c8d39b","agents/care-reviewer.toml":"c64d907e7dcb752c77f76003b049b1dab180c56aaf017ad92a1f41e733f89e7e","agents/care-specialist.toml":"542e80baab01c22031709a2a193f7ae689b20a2be6d8704403b3d2a7db33eeb1","agents/explore.toml":"8f9152db2310dd128c39f1f65366d2b86ac25b9c71ae9b99ed88df99b538f713","agents/general.toml":"1a220f7f5a7b682213daad3dd8982208f6c6ea7ec73ed4035c1be184b111df4c","agents/sdd-apply.toml":"268531efe9cb69ee884913f7a2f538dfc538cbc012253db31ac7717c978cd0a6","agents/sdd-design.toml":"73f8b80266f73d1b658224a4c5a4ea8fe282fbe8ddefdd8a42aa961e4f797e4d","agents/sdd-proposal.toml":"1662832e9c25c135a84c32bfa47dc48140abef82f7a878913a57696b3b42ca23","agents/sdd-research.toml":"32d454560aca0ab55fbbfceb94842a0b6ff302eb4eeeee0e161ab78850e31017","agents/sdd-spec.toml":"cbb4c94c499811fe880cfc3d49dc65955e86e40d0fc37cac16413ac00f46b7fd","agents/sdd-tasks.toml":"920f126c0eecc64ad4fdb46781636dbe93822e3b08eb2987ffea23f5392c0f07","agents/verifier.toml":"d1da94d12913e21424cbaef2cb683897662dedd0b15d5e7285fbb0bb9363aa5b","plugins/vgxness/.codex-plugin/plugin.json":"30602a0173f09c465f3741fd04ad6990688114038eeaada9c35592f24eaaa42b"}}`), &golden); e != nil {
		t.Fatal(e)
	}
	known, e := knownPackages()
	if e != nil {
		t.Fatal(e)
	}
	for name, want := range golden {
		p, e := renderActiveV19("v0.0.0", modelplan.Plan(name))
		if e != nil {
			t.Fatal(e)
		}
		if e = p.Validate(); e != nil {
			t.Fatal(e)
		}
		if len(p.Artifacts) != len(want) {
			t.Fatal("predecessor shape")
		}
		for _, a := range p.Artifacts {
			h := sha256.Sum256(a.Bytes)
			if hex.EncodeToString(h[:]) != want[a.Path] {
				t.Errorf("historical bytes changed: %s/%s", name, a.Path)
			}
		}
		found := false
		for _, k := range known {
			if k.SHA256 == p.SHA256 {
				found = true
			}
		}
		if !found {
			t.Error("complete predecessor unrecognized")
		}
		current, e := RenderPlan("v0.0.0", modelplan.Plan(name))
		if e != nil {
			t.Fatal(e)
		}
		mixed := clonePackage(p)
		for i, a := range mixed.Artifacts {
			if a.Path == "agents/general.toml" {
				for _, newA := range current.Artifacts {
					if newA.Path == a.Path {
						mixed.Artifacts[i].Bytes = newA.Bytes
					}
				}
			}
		}
		mixed.SHA256 = aggregateSHA256(mixed.Artifacts)
		if mixed.Validate() == nil {
			t.Error("mixed predecessor accepted")
		}
	}
}

func TestNativeSharedDevelopmentScenarios(t *testing.T) {
	raw, e := os.ReadFile("../../orchestration/testdata/manager-scenarios.json")
	if e != nil {
		t.Fatal(e)
	}
	var corpus struct {
		Cases []struct{ ID, Fragment string }
	}
	if e = json.Unmarshal(raw, &corpus); e != nil {
		t.Fatal(e)
	}
	p, e := Render("v0.0.0")
	if e != nil {
		t.Fatal(e)
	}
	text := ""
	for _, a := range p.Artifacts {
		if a.Path == "AGENTS.md" {
			text = string(a.Bytes)
		}
	}
	if len(corpus.Cases) != 11 {
		t.Fatal("missing scenarios")
	}
	for _, scenario := range corpus.Cases {
		if !strings.Contains(text, scenario.Fragment) {
			t.Errorf("native projection lacks scenario %s", scenario.ID)
		}
	}
}

func TestCurrentCodexKeepsNativeProfileBindings(t *testing.T) {
	strip := func(value []byte) string {
		var out []string
		for _, line := range strings.Split(string(value), "\n") {
			if !strings.HasPrefix(line, "developer_instructions = ") {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	}
	for _, plan := range []modelplan.Plan{modelplan.PlanLow, modelplan.PlanMedium, modelplan.PlanHigh, modelplan.PlanUltra} {
		current, e := RenderPlan("v0.0.0", plan)
		if e != nil {
			t.Fatal(e)
		}
		old, e := renderActiveV19("v0.0.0", plan)
		if e != nil {
			t.Fatal(e)
		}
		previous := map[string][]byte{}
		for _, a := range old.Artifacts {
			previous[a.Path] = a.Bytes
		}
		for _, a := range current.Artifacts {
			if strings.HasPrefix(a.Path, "agents/") && strip(a.Bytes) != strip(previous[a.Path]) {
				t.Errorf("native profile fields changed: %s/%s", plan, a.Path)
			}
		}
	}
}

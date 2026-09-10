package opencode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
	"os"
	"strings"
	"testing"
)

// frozenManagerV60 is used by predecessor-policy tests. Current artifacts are
// deliberately rendered from the shared contract, while v60 remains a byte
// exact recognized installation predecessor.
func frozenManagerV60(t *testing.T, current modelPlanBundle) modelPlanBundle {
	t.Helper()
	frozen, err := managerV60Bundle(current)
	if err != nil {
		t.Fatal(err)
	}
	return frozen
}

func TestSharedOpenCodeProjectionAndPredecessors(t *testing.T) {
	var golden map[string]map[string]string
	if e := json.Unmarshal([]byte(`{"v1/high":{"explore.md":"e138255d283edba2dfbbe595be79d9531d8c8c20ec8b4509e020da5b17a17d00","general.md":"7ed0fc1d3afdbf27b100dbf525c941cfb9859b67443a63f119ea7e03e8798870","model-plan.json":"6f1dba2a77bcfdc1642b1463293fbd8774212e01547024946d82ca04fcc61df1","vgxness-care-challenger.md":"66f1b6461ecbd85c4b567947064c8a6587322aa7ae9c5bac0dba01ccbddf9792","vgxness-care-reviewer.md":"6719623258ad3ba78219582dc14f60151d0e5d9e72948d26bf76a859e67846af","vgxness-care-specialist.md":"86cc4da90b669ce6dec3746c31f2ca3326195034d667ae89e4eddb0207a6748f","vgxness-manager.md":"81f7423ba3c89dfed3016638464b37a25d448e40085a477c6edef94d027cbf5f","vgxness-sdd-apply.md":"6e561e8ae7fc2b4f7e8b3c9916c0234f38f322c2eb7b78cdf2bc1be4be96e2be","vgxness-sdd-design.md":"964c1af55d3ed99449ba5b5ec8632f16938471d60ea63f5d1e490ac9ace20e69","vgxness-sdd-proposal.md":"4ebf93aae857e78b86eb57a8031c54c5032b38c272d5eaff53308521d415ba51","vgxness-sdd-research.md":"d43ff948257b7a8c0eaf1569e6d114ef83e67b15b69da130e6d87bd917bb5440","vgxness-sdd-spec.md":"c861c37d72910d3332e80009a388e4e240985171cc84c7285e957a4f8d58f8d5","vgxness-sdd-tasks.md":"fce932762242e061df210aac2db5a515bae40475f20b98a8b8f01ff949451648","vgxness-verifier.md":"9fef5e292b9478487721318972f2cfbdc084e572fed270918a7df394d180b6d4"},"v1/low":{"explore.md":"aefd8d367507bf6168bb4182b36c63061ced5c938b1234f3f865caa474f52d28","general.md":"fdb3306ac2e756fad49ded4c6a6643fdfe43fd419a227840ecf0c9cf044c4bf0","model-plan.json":"be4370f88332ee5fee40228fad2ae33122ec3d158a18e04b3cef741292e0c6fc","vgxness-care-challenger.md":"cdd2ef1ba034fcd79b96aa63dba144d8951e995c45437dd41ad8ea5ea3c9520d","vgxness-care-reviewer.md":"e50253ee50bf96d5474bb14bc029c7c1865cc4937b3abb2158fcfeb87532e1d4","vgxness-care-specialist.md":"3625f36f8f7778367266b56721bd7fa02a40d07097619a0fc80f90ea0c4a1836","vgxness-manager.md":"5c183e8a502b7ae44a1332b79ebc763c0613e2c2fe1d8afd4a3a14dc61e8e132","vgxness-sdd-apply.md":"d078e4cf0401a1fbc415a45579c997407c2feae80cf16d367a3503f3da195606","vgxness-sdd-design.md":"791406e90243b2eb286e64dd1d71c2a81625729c85f414d41fa0785aec3f94c9","vgxness-sdd-proposal.md":"f7dac963f2007bf6534144c057d1d242e33617d54bc5c282c576400a80968c5b","vgxness-sdd-research.md":"326bc2856980fd8b442a894171c63aa99e5b247ab2eee29cb3d3c41c93bad8c7","vgxness-sdd-spec.md":"36133de698480207cbe9382713f219215bddf3477d36d8de1a1c87b2993c49e9","vgxness-sdd-tasks.md":"66e9d0a79f1420ef2f41fc5e34307fda2bd628929881513bbcdbb16743c2954d","vgxness-verifier.md":"b4da17c783ffab891ab135751fbc47f42f9a983f2b9476850d2b3f4cf5da7477"},"v1/medium":{"explore.md":"9db5edabb4c8af4faac601a01952bac15a161b54b96a30bbf5d24bae500ff96d","general.md":"168b7597d67020bdf7fc7c5f0af1db246415f3c2dbf4420813bafd22ccbf0d1e","model-plan.json":"edc9917e22d1a7ae4398a083d38110f598046e8603f68ab0a46e5f87b51c8a79","vgxness-care-challenger.md":"3750b1d74ea4e2455eee4a971e31a098f72f0f3239a185042ff60f3bfa8c006f","vgxness-care-reviewer.md":"be0daf21abbaedd884cf7f76ab4c02bcc8c6c41b74ec3a7d0c5e28cf207772d0","vgxness-care-specialist.md":"608f3c65cf983826dee2590f4a121e3a905f7bb33db1b55b10275f0e9a4f9cd8","vgxness-manager.md":"cd618260cdce1c46087a419641e11d8494786ed7eb8032e3a3265fff9812232a","vgxness-sdd-apply.md":"3683f63cb16ad8ecb5197e58d873103a6c6c2d11b387e16bcb3198068227c818","vgxness-sdd-design.md":"c7dc18b833eed8a6b40e5745444be0768865b137b3d9d437f8a7757d2c1a9316","vgxness-sdd-proposal.md":"17e2032bc59992b81a8ae04a944ac00480c6fe6f6af87d46f7078ff2242bfa3b","vgxness-sdd-research.md":"0d5246581454fdc55009cbbdb5410e668ba1cd8a4dfd3f39ebeb1f41185ae036","vgxness-sdd-spec.md":"91cd90473e0e958fd196f0f735cfbede6ff4ae038cb10bec435a13c3b1aebf0a","vgxness-sdd-tasks.md":"1ce6e27a8e54fbf182485edc3fa97cd07607a05a63b241b5bdb3d65e5c73ffc8","vgxness-verifier.md":"2b7acca1c530a863b2f4fbf7f89b4430d6d195d80a05c6f84725962804c9a6ed"},"v1/ultra":{"explore.md":"c8730469667fe0c828912ccda5647a9692b1715a2202e477e0d5cfff3405ce1f","general.md":"7ed0fc1d3afdbf27b100dbf525c941cfb9859b67443a63f119ea7e03e8798870","model-plan.json":"6a2065062a9792fe1e21760b753b7718a68bbe08dfb29079f8606d460f8e0464","vgxness-care-challenger.md":"66f1b6461ecbd85c4b567947064c8a6587322aa7ae9c5bac0dba01ccbddf9792","vgxness-care-reviewer.md":"6719623258ad3ba78219582dc14f60151d0e5d9e72948d26bf76a859e67846af","vgxness-care-specialist.md":"86cc4da90b669ce6dec3746c31f2ca3326195034d667ae89e4eddb0207a6748f","vgxness-manager.md":"81f7423ba3c89dfed3016638464b37a25d448e40085a477c6edef94d027cbf5f","vgxness-sdd-apply.md":"d99a4bec6fe0635ce6ee4f316f594430bf12540a188b9caf93de181005c65827","vgxness-sdd-design.md":"964c1af55d3ed99449ba5b5ec8632f16938471d60ea63f5d1e490ac9ace20e69","vgxness-sdd-proposal.md":"66e0524da3239526b35eb38bb5d33f2fa3b7139b137a333b1a7abac86963796b","vgxness-sdd-research.md":"a10efc1b94f1c7d998084e83d6fd9297169c5633ece9393fd8a293a276ce2bb7","vgxness-sdd-spec.md":"c861c37d72910d3332e80009a388e4e240985171cc84c7285e957a4f8d58f8d5","vgxness-sdd-tasks.md":"89674fa975a8f3a774d3a8b6ac8ab6e19b9c09ec43e5b953a36830a4cd7ed424","vgxness-verifier.md":"16dd8f0e4fb36a88801ff745b2656a51cdab08a5a2e23c64a0ddde786cbe8677"},"v2/high":{"explore.md":"048411335fba45f7abedf4a219acc9f3806f33cc457c9ed8f1c86c3d44b49844","general.md":"b724377ff98c9674029a85673b273b8089019a91acc5d3b97685191a2acfd588","model-plan.json":"fb30866c606f3d53b29dbcb54f674a09e15b386eead99fe3ffbe1d64298d5bb9","vgxness-care-challenger.md":"3750b1d74ea4e2455eee4a971e31a098f72f0f3239a185042ff60f3bfa8c006f","vgxness-care-reviewer.md":"be0daf21abbaedd884cf7f76ab4c02bcc8c6c41b74ec3a7d0c5e28cf207772d0","vgxness-care-specialist.md":"479016c85dda80d8cf6cd47b8ee9566d71dd426d7ebb70f25612349558858fe2","vgxness-manager.md":"caa4da2d8ade09f565a16905da61b09ab8fc65fe0eedf17dac23572c3ae3da4d","vgxness-sdd-apply.md":"3683f63cb16ad8ecb5197e58d873103a6c6c2d11b387e16bcb3198068227c818","vgxness-sdd-design.md":"c7dc18b833eed8a6b40e5745444be0768865b137b3d9d437f8a7757d2c1a9316","vgxness-sdd-proposal.md":"17e2032bc59992b81a8ae04a944ac00480c6fe6f6af87d46f7078ff2242bfa3b","vgxness-sdd-research.md":"5804488948ebba56d31bcd549da6a3e9c8db15f53fb4c0a9dabc9d7d5d6cb674","vgxness-sdd-spec.md":"dd52310adfd0f23dc8a27be3e3130cb0ea6240cfbbca99d6595b87917bfc8057","vgxness-sdd-tasks.md":"1ce6e27a8e54fbf182485edc3fa97cd07607a05a63b241b5bdb3d65e5c73ffc8","vgxness-verifier.md":"95d69a49e865f0f810a7fb9280fdbf5d52cd0955053b65b5e7ebbbff9abb273e"},"v2/low":{"explore.md":"9db5edabb4c8af4faac601a01952bac15a161b54b96a30bbf5d24bae500ff96d","general.md":"168b7597d67020bdf7fc7c5f0af1db246415f3c2dbf4420813bafd22ccbf0d1e","model-plan.json":"2fa9a2eb74a0c72ad8e767e62ef28d5af3f2a804b60d7ba540458a423654c14c","vgxness-care-challenger.md":"cdd2ef1ba034fcd79b96aa63dba144d8951e995c45437dd41ad8ea5ea3c9520d","vgxness-care-reviewer.md":"e50253ee50bf96d5474bb14bc029c7c1865cc4937b3abb2158fcfeb87532e1d4","vgxness-care-specialist.md":"3625f36f8f7778367266b56721bd7fa02a40d07097619a0fc80f90ea0c4a1836","vgxness-manager.md":"ea9297233b1be634a3748e7f74fe07906029e9fcb06709d321491f7bde9424e3","vgxness-sdd-apply.md":"3683f63cb16ad8ecb5197e58d873103a6c6c2d11b387e16bcb3198068227c818","vgxness-sdd-design.md":"791406e90243b2eb286e64dd1d71c2a81625729c85f414d41fa0785aec3f94c9","vgxness-sdd-proposal.md":"f7dac963f2007bf6534144c057d1d242e33617d54bc5c282c576400a80968c5b","vgxness-sdd-research.md":"0d5246581454fdc55009cbbdb5410e668ba1cd8a4dfd3f39ebeb1f41185ae036","vgxness-sdd-spec.md":"36133de698480207cbe9382713f219215bddf3477d36d8de1a1c87b2993c49e9","vgxness-sdd-tasks.md":"e061eb747a5e98b89d19916f777ef3ee2e3ece7d1819a1b2da68ed41499d3729","vgxness-verifier.md":"2b7acca1c530a863b2f4fbf7f89b4430d6d195d80a05c6f84725962804c9a6ed"},"v2/medium":{"explore.md":"9db5edabb4c8af4faac601a01952bac15a161b54b96a30bbf5d24bae500ff96d","general.md":"168b7597d67020bdf7fc7c5f0af1db246415f3c2dbf4420813bafd22ccbf0d1e","model-plan.json":"25f97e93cd15b30e4097a73d9bd0087522a47315fe1d36ab6fddc734879a305c","vgxness-care-challenger.md":"3750b1d74ea4e2455eee4a971e31a098f72f0f3239a185042ff60f3bfa8c006f","vgxness-care-reviewer.md":"be0daf21abbaedd884cf7f76ab4c02bcc8c6c41b74ec3a7d0c5e28cf207772d0","vgxness-care-specialist.md":"3625f36f8f7778367266b56721bd7fa02a40d07097619a0fc80f90ea0c4a1836","vgxness-manager.md":"caa4da2d8ade09f565a16905da61b09ab8fc65fe0eedf17dac23572c3ae3da4d","vgxness-sdd-apply.md":"3683f63cb16ad8ecb5197e58d873103a6c6c2d11b387e16bcb3198068227c818","vgxness-sdd-design.md":"c7dc18b833eed8a6b40e5745444be0768865b137b3d9d437f8a7757d2c1a9316","vgxness-sdd-proposal.md":"17e2032bc59992b81a8ae04a944ac00480c6fe6f6af87d46f7078ff2242bfa3b","vgxness-sdd-research.md":"0d5246581454fdc55009cbbdb5410e668ba1cd8a4dfd3f39ebeb1f41185ae036","vgxness-sdd-spec.md":"2b24b378c4b857f87244760c7a0617ca7c65c16fcfe90d6beda21ea415ee6f5a","vgxness-sdd-tasks.md":"1ce6e27a8e54fbf182485edc3fa97cd07607a05a63b241b5bdb3d65e5c73ffc8","vgxness-verifier.md":"2b7acca1c530a863b2f4fbf7f89b4430d6d195d80a05c6f84725962804c9a6ed"},"v2/ultra":{"explore.md":"20bf372da498efeeb2b90731409f4e924a2014c4532ac37b9399e118a36bb168","general.md":"b724377ff98c9674029a85673b273b8089019a91acc5d3b97685191a2acfd588","model-plan.json":"fc094989d2cd1a85bfd168ba20dabc7b1609324d92d5a5e6d5f63a23f05c94fa","vgxness-care-challenger.md":"3750b1d74ea4e2455eee4a971e31a098f72f0f3239a185042ff60f3bfa8c006f","vgxness-care-reviewer.md":"be0daf21abbaedd884cf7f76ab4c02bcc8c6c41b74ec3a7d0c5e28cf207772d0","vgxness-care-specialist.md":"479016c85dda80d8cf6cd47b8ee9566d71dd426d7ebb70f25612349558858fe2","vgxness-manager.md":"caa4da2d8ade09f565a16905da61b09ab8fc65fe0eedf17dac23572c3ae3da4d","vgxness-sdd-apply.md":"351bcdadd73fc312bd99b9d3d77f8de5467b23c3965e36b0b852e70663ab13f5","vgxness-sdd-design.md":"c7dc18b833eed8a6b40e5745444be0768865b137b3d9d437f8a7757d2c1a9316","vgxness-sdd-proposal.md":"bbaadfb283fb52330b0007d9b5a92f02a656dd1068bcbcacc99cb41813ce516b","vgxness-sdd-research.md":"a9fb3b1ba3c4bbd5381a9cab6f5bbff894e07501d1f98044ab572ec89c32818d","vgxness-sdd-spec.md":"dd52310adfd0f23dc8a27be3e3130cb0ea6240cfbbca99d6595b87917bfc8057","vgxness-sdd-tasks.md":"a7f0329a9f96214a92313aed6e299c300b8c300b389009e6f0df2689d15054e0","vgxness-verifier.md":"b5dbab9ab6cd7d1d16317673bafca7c9fb3ee57afa147acfdfc3a598a6a96907"},"v3/high":{"explore.md":"e138255d283edba2dfbbe595be79d9531d8c8c20ec8b4509e020da5b17a17d00","general.md":"7ed0fc1d3afdbf27b100dbf525c941cfb9859b67443a63f119ea7e03e8798870","model-plan.json":"af0d2d1b965f5d542705c4ad7794eb59d6a57d15e6045ef378f0451978b679d2","vgxness-care-challenger.md":"66f1b6461ecbd85c4b567947064c8a6587322aa7ae9c5bac0dba01ccbddf9792","vgxness-care-reviewer.md":"6719623258ad3ba78219582dc14f60151d0e5d9e72948d26bf76a859e67846af","vgxness-care-specialist.md":"86cc4da90b669ce6dec3746c31f2ca3326195034d667ae89e4eddb0207a6748f","vgxness-manager.md":"81f7423ba3c89dfed3016638464b37a25d448e40085a477c6edef94d027cbf5f","vgxness-sdd-apply.md":"6e561e8ae7fc2b4f7e8b3c9916c0234f38f322c2eb7b78cdf2bc1be4be96e2be","vgxness-sdd-design.md":"964c1af55d3ed99449ba5b5ec8632f16938471d60ea63f5d1e490ac9ace20e69","vgxness-sdd-proposal.md":"4ebf93aae857e78b86eb57a8031c54c5032b38c272d5eaff53308521d415ba51","vgxness-sdd-research.md":"d43ff948257b7a8c0eaf1569e6d114ef83e67b15b69da130e6d87bd917bb5440","vgxness-sdd-spec.md":"c861c37d72910d3332e80009a388e4e240985171cc84c7285e957a4f8d58f8d5","vgxness-sdd-tasks.md":"fce932762242e061df210aac2db5a515bae40475f20b98a8b8f01ff949451648","vgxness-verifier.md":"9fef5e292b9478487721318972f2cfbdc084e572fed270918a7df394d180b6d4"},"v3/low":{"explore.md":"aefd8d367507bf6168bb4182b36c63061ced5c938b1234f3f865caa474f52d28","general.md":"fdb3306ac2e756fad49ded4c6a6643fdfe43fd419a227840ecf0c9cf044c4bf0","model-plan.json":"d034733d40fa42d595a6a322e3baad5954b74aa2d12c80442ce31f5c225de7cc","vgxness-care-challenger.md":"cdd2ef1ba034fcd79b96aa63dba144d8951e995c45437dd41ad8ea5ea3c9520d","vgxness-care-reviewer.md":"e50253ee50bf96d5474bb14bc029c7c1865cc4937b3abb2158fcfeb87532e1d4","vgxness-care-specialist.md":"3625f36f8f7778367266b56721bd7fa02a40d07097619a0fc80f90ea0c4a1836","vgxness-manager.md":"5c183e8a502b7ae44a1332b79ebc763c0613e2c2fe1d8afd4a3a14dc61e8e132","vgxness-sdd-apply.md":"d078e4cf0401a1fbc415a45579c997407c2feae80cf16d367a3503f3da195606","vgxness-sdd-design.md":"791406e90243b2eb286e64dd1d71c2a81625729c85f414d41fa0785aec3f94c9","vgxness-sdd-proposal.md":"f7dac963f2007bf6534144c057d1d242e33617d54bc5c282c576400a80968c5b","vgxness-sdd-research.md":"326bc2856980fd8b442a894171c63aa99e5b247ab2eee29cb3d3c41c93bad8c7","vgxness-sdd-spec.md":"36133de698480207cbe9382713f219215bddf3477d36d8de1a1c87b2993c49e9","vgxness-sdd-tasks.md":"66e9d0a79f1420ef2f41fc5e34307fda2bd628929881513bbcdbb16743c2954d","vgxness-verifier.md":"b4da17c783ffab891ab135751fbc47f42f9a983f2b9476850d2b3f4cf5da7477"},"v3/medium":{"explore.md":"9db5edabb4c8af4faac601a01952bac15a161b54b96a30bbf5d24bae500ff96d","general.md":"168b7597d67020bdf7fc7c5f0af1db246415f3c2dbf4420813bafd22ccbf0d1e","model-plan.json":"03f45b6a263ea68b5fa54ce77cf274fa409b649756607f7292f8c0cac62c489e","vgxness-care-challenger.md":"3750b1d74ea4e2455eee4a971e31a098f72f0f3239a185042ff60f3bfa8c006f","vgxness-care-reviewer.md":"be0daf21abbaedd884cf7f76ab4c02bcc8c6c41b74ec3a7d0c5e28cf207772d0","vgxness-care-specialist.md":"608f3c65cf983826dee2590f4a121e3a905f7bb33db1b55b10275f0e9a4f9cd8","vgxness-manager.md":"cd618260cdce1c46087a419641e11d8494786ed7eb8032e3a3265fff9812232a","vgxness-sdd-apply.md":"3683f63cb16ad8ecb5197e58d873103a6c6c2d11b387e16bcb3198068227c818","vgxness-sdd-design.md":"c7dc18b833eed8a6b40e5745444be0768865b137b3d9d437f8a7757d2c1a9316","vgxness-sdd-proposal.md":"17e2032bc59992b81a8ae04a944ac00480c6fe6f6af87d46f7078ff2242bfa3b","vgxness-sdd-research.md":"0d5246581454fdc55009cbbdb5410e668ba1cd8a4dfd3f39ebeb1f41185ae036","vgxness-sdd-spec.md":"91cd90473e0e958fd196f0f735cfbede6ff4ae038cb10bec435a13c3b1aebf0a","vgxness-sdd-tasks.md":"1ce6e27a8e54fbf182485edc3fa97cd07607a05a63b241b5bdb3d65e5c73ffc8","vgxness-verifier.md":"2b7acca1c530a863b2f4fbf7f89b4430d6d195d80a05c6f84725962804c9a6ed"},"v3/ultra":{"explore.md":"c8730469667fe0c828912ccda5647a9692b1715a2202e477e0d5cfff3405ce1f","general.md":"7ed0fc1d3afdbf27b100dbf525c941cfb9859b67443a63f119ea7e03e8798870","model-plan.json":"6686a9e0186d217fdf52e265b937d7ce08bcaf0ff8216c180d13bc26547a337f","vgxness-care-challenger.md":"66f1b6461ecbd85c4b567947064c8a6587322aa7ae9c5bac0dba01ccbddf9792","vgxness-care-reviewer.md":"6719623258ad3ba78219582dc14f60151d0e5d9e72948d26bf76a859e67846af","vgxness-care-specialist.md":"86cc4da90b669ce6dec3746c31f2ca3326195034d667ae89e4eddb0207a6748f","vgxness-manager.md":"81f7423ba3c89dfed3016638464b37a25d448e40085a477c6edef94d027cbf5f","vgxness-sdd-apply.md":"d99a4bec6fe0635ce6ee4f316f594430bf12540a188b9caf93de181005c65827","vgxness-sdd-design.md":"964c1af55d3ed99449ba5b5ec8632f16938471d60ea63f5d1e490ac9ace20e69","vgxness-sdd-proposal.md":"66e0524da3239526b35eb38bb5d33f2fa3b7139b137a333b1a7abac86963796b","vgxness-sdd-research.md":"a10efc1b94f1c7d998084e83d6fd9297169c5633ece9393fd8a293a276ce2bb7","vgxness-sdd-spec.md":"c861c37d72910d3332e80009a388e4e240985171cc84c7285e957a4f8d58f8d5","vgxness-sdd-tasks.md":"89674fa975a8f3a774d3a8b6ac8ab6e19b9c09ec43e5b953a36830a4cd7ed424","vgxness-verifier.md":"16dd8f0e4fb36a88801ff745b2656a51cdab08a5a2e23c64a0ddde786cbe8677"}}`), &golden); e != nil {
		t.Fatal(e)
	}
	contract, e := orchestration.LoadManagerContract()
	if e != nil {
		t.Fatal(e)
	}
	for key, want := range golden {
		parts := strings.Split(key, "/")
		config := modelplan.DefaultModelPlanConfig()
		config.ActivePlan = modelplan.Plan(parts[1])
		config2 := modelplan.DefaultModelPlanConfigV2()
		config2.ActivePlan = config.ActivePlan
		config3 := projectModelPlanToV3(config)
		var current modelPlanBundle
		switch parts[0] {
		case "v1":
			current, e = buildModelPlanBundle(config)
		case "v2":
			current, e = buildModelPlanBundleV2(config2)
		case "v3":
			current, e = buildModelPlanBundleV3(config3)
		}
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Contains(current.agents[managerAgentName], []byte(contract.RenderManagerSections())) {
			t.Fatal("current Manager not shared")
		}
		old, e := managerV60Bundle(current)
		if e != nil {
			t.Fatal(e)
		}
		files := map[string][]byte{"model-plan.json": old.manifest}
		for n, b := range old.agents {
			files[n] = b
		}
		if len(files) != len(want) {
			t.Fatal("predecessor shape")
		}
		for n, b := range files {
			h := sha256.Sum256(b)
			if hex.EncodeToString(h[:]) != want[n] {
				t.Errorf("historical bytes changed %s/%s", key, n)
			}
		}
		for n, b := range current.agents {
			id := strings.TrimSuffix(strings.TrimPrefix(n, "vgxness-"), ".md")
			r, ok := contract.Role(id)
			if !ok {
				t.Fatal(id)
			}
			if !bytes.Contains(b, []byte(r.Instructions)) {
				t.Errorf("current role not shared: %s", id)
			}
			oldHeader := strings.SplitN(string(old.agents[n]), "\n---\n", 2)[0]
			newHeader := strings.SplitN(string(b), "\n---\n", 2)[0]
			if oldHeader != newHeader {
				t.Errorf("native header changed %s", n)
			}
		}
		check := func(data []byte) (modelPlanBundle, error) {
			switch parts[0] {
			case "v1":
				return modelPlanBundleForManifest(data, config)
			case "v2":
				return modelPlanBundleForManifestV2(data, config2)
			default:
				return modelPlanBundleForManifestV3(data, config3)
			}
		}
		recognized, e := check(old.manifest)
		if e != nil || !bytes.Equal(recognized.manifest, old.manifest) {
			t.Errorf("old complete package rejected: %s %v", key, e)
		}
		if _, e = check(current.manifest); e != nil {
			t.Fatal(e)
		}
		mixed := map[string][]byte{}
		for n, b := range old.agents {
			mixed[n] = b
		}
		mixed[generalAgentName] = current.agents[generalAgentName]
		mix, e := encodeLike(old, mixed)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = check(mix.manifest); e == nil {
			t.Errorf("mixed package accepted: %s", key)
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
	p, e := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if e != nil {
		t.Fatal(e)
	}
	text := string(p.agents[managerAgentName])
	if len(corpus.Cases) != 11 {
		t.Fatal("missing scenarios")
	}
	for _, scenario := range corpus.Cases {
		if !strings.Contains(text, scenario.Fragment) {
			t.Errorf("native projection lacks scenario %s", scenario.ID)
		}
	}
}

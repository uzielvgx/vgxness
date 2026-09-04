package skills

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
)

type skillDefinition struct {
	name         string
	source       string
	files        map[string][]byte
	predecessors map[string]string
	packageExact bool
	legacy       []legacyDefinition
}

type legacyDefinition struct {
	name      string
	digests   map[string]string
	exactOnly bool
}

var memorySyncV12Predecessors = map[string]string{
	"LICENSE.txt":                   "904c73d094910aff6f8e7f0bd06ab953f55f879264680363095d03e64e9a28d7",
	"SKILL.md":                      "b1ca88880e74035b4b284cfa88d135cdb5593dc35285104400b5d8870971a83f",
	"agents/openai.yaml":            "a355e835187fefb7f55a35eddebb4e5ac6e8a412d3f2c52f26b1dd5c2064ae48",
	"references/client-workflow.md": "0969bcc01c53c37933d58a71713bc88fb683e207c35e0fb7930c14049c1041df",
	"skill-manifest.json":           "96eddcbd1dc0643084bb3e1ef8255e0e7e209f3f8fd2318bde309971da3c020b",
}

type catalog struct{ definitions []skillDefinition }

// resolvedCatalog uses the bundled catalog only when a catalog was not supplied.
// A supplied catalog, including an empty one, is validated by entries.
func (s *Service) resolvedCatalog() (catalog, error) {
	if s.catalog != nil {
		return *s.catalog, nil
	}
	return bundledCatalog()
}

func (s *Service) entries() (map[string][]byte, error) {
	c, err := s.resolvedCatalog()
	if err != nil {
		return nil, err
	}
	if len(c.definitions) == 0 {
		return nil, ErrInvalid
	}
	entries := make(map[string][]byte)
	owners := map[string]bool{}
	for _, definition := range c.definitions {
		if !validSkillName(definition.name) || (definition.source != "" && !validSkillName(definition.source)) || owners[definition.name] || len(definition.files) == 0 {
			return nil, ErrInvalid
		}
		owners[definition.name] = true
		for relative, content := range definition.files {
			if !validRelative(relative) {
				return nil, ErrInvalid
			}
			identity := definition.name + "/" + relative
			if _, exists := entries[identity]; exists {
				return nil, ErrInvalid
			}
			entries[identity] = content
		}
		if definition.packageExact && (len(definition.predecessors) == 0 || len(definition.predecessors) != len(definition.files)) {
			return nil, ErrInvalid
		}
		for relative := range definition.predecessors {
			if _, exists := definition.files[relative]; !validRelative(relative) || !exists {
				return nil, ErrInvalid
			}
		}
		legacy := map[string]bool{}
		for _, source := range definition.legacy {
			if !validSkillName(source.name) || source.name == definition.name || owners[source.name] || legacy[source.name] || len(source.digests) == 0 {
				return nil, ErrInvalid
			}
			legacy[source.name] = true
			owners[source.name] = true
			for relative := range source.digests {
				if _, exists := definition.files[relative]; !validRelative(relative) || !exists {
					return nil, ErrInvalid
				}
			}
		}
	}
	return entries, nil
}

func bundledCatalog() (catalog, error) {
	creator := skillDefinition{
		name:         "skills-creator",
		source:       "skills-creator",
		predecessors: predecessorDigests,
		legacy:       []legacyDefinition{{name: "agent-skill-engineer", digests: legacyV032Digests}},
	}
	entries, err := bundledFiles(creator.source)
	if err != nil {
		return catalog{}, err
	}
	creator.files = entries
	gitDelivery := skillDefinition{name: "git-delivery", source: "git-delivery", legacy: []legacyDefinition{{name: "stacked-pr", exactOnly: true, digests: map[string]string{"SKILL.md": "43d30fc18b5bf23c1ec450248bad2ba9283f5f63c9c5946733a4f5d2971c197f"}}}}
	gitDelivery.files, err = bundledFiles(gitDelivery.source)
	if err != nil {
		return catalog{}, err
	}
	crossPlatform := skillDefinition{name: "cross-platform", source: "cross-platform"}
	crossPlatform.files, err = bundledFiles(crossPlatform.source)
	if err != nil {
		return catalog{}, err
	}
	installerLifecycle := skillDefinition{name: "installer-lifecycle", source: "installer-lifecycle"}
	installerLifecycle.files, err = bundledFiles(installerLifecycle.source)
	if err != nil {
		return catalog{}, err
	}
	agentEvaluation := skillDefinition{name: "agent-evaluation", source: "agent-evaluation"}
	agentEvaluation.files, err = bundledFiles(agentEvaluation.source)
	if err != nil {
		return catalog{}, err
	}
	ciTriage := skillDefinition{name: "ci-triage", source: "ci-triage"}
	ciTriage.files, err = bundledFiles(ciTriage.source)
	if err != nil {
		return catalog{}, err
	}
	securityBoundary := skillDefinition{name: "security-boundary", source: "security-boundary"}
	securityBoundary.files, err = bundledFiles(securityBoundary.source)
	if err != nil {
		return catalog{}, err
	}
	documentationStrategy := skillDefinition{name: "documentation-strategy", source: "documentation-strategy"}
	documentationStrategy.files, err = bundledFiles(documentationStrategy.source)
	if err != nil {
		return catalog{}, err
	}
	productRequirements := skillDefinition{name: "product-requirements", source: "product-requirements"}
	productRequirements.files, err = bundledFiles(productRequirements.source)
	if err != nil {
		return catalog{}, err
	}
	softwareArchitectureDocs := skillDefinition{name: "software-architecture-docs", source: "software-architecture-docs"}
	softwareArchitectureDocs.files, err = bundledFiles(softwareArchitectureDocs.source)
	if err != nil {
		return catalog{}, err
	}
	userDocumentation := skillDefinition{name: "user-documentation", source: "user-documentation"}
	userDocumentation.files, err = bundledFiles(userDocumentation.source)
	if err != nil {
		return catalog{}, err
	}
	apiDocumentation := skillDefinition{name: "api-documentation", source: "api-documentation"}
	apiDocumentation.files, err = bundledFiles(apiDocumentation.source)
	if err != nil {
		return catalog{}, err
	}
	qualityTestDocumentation := skillDefinition{name: "quality-test-documentation", source: "quality-test-documentation"}
	qualityTestDocumentation.files, err = bundledFiles(qualityTestDocumentation.source)
	if err != nil {
		return catalog{}, err
	}
	operationsRunbooks := skillDefinition{name: "operations-runbooks", source: "operations-runbooks"}
	operationsRunbooks.files, err = bundledFiles(operationsRunbooks.source)
	if err != nil {
		return catalog{}, err
	}
	governanceComplianceDocs := skillDefinition{name: "governance-compliance-docs", source: "governance-compliance-docs"}
	governanceComplianceDocs.files, err = bundledFiles(governanceComplianceDocs.source)
	if err != nil {
		return catalog{}, err
	}
	releaseLifecycleDocs := skillDefinition{name: "release-lifecycle-docs", source: "release-lifecycle-docs"}
	releaseLifecycleDocs.files, err = bundledFiles(releaseLifecycleDocs.source)
	if err != nil {
		return catalog{}, err
	}
	endToEndTesting := skillDefinition{name: "end-to-end-testing", source: "end-to-end-testing"}
	endToEndTesting.files, err = bundledFiles(endToEndTesting.source)
	if err != nil {
		return catalog{}, err
	}
	memorySync := skillDefinition{name: "memory-sync", source: "memory-sync", predecessors: memorySyncV12Predecessors, packageExact: true}
	memorySync.files, err = bundledFiles(memorySync.source)
	if err != nil {
		return catalog{}, err
	}
	sddLifecycle := skillDefinition{name: "sdd-lifecycle", source: "sdd-lifecycle"}
	sddLifecycle.files, err = bundledFiles(sddLifecycle.source)
	if err != nil {
		return catalog{}, err
	}
	return catalog{definitions: []skillDefinition{creator, gitDelivery, crossPlatform, installerLifecycle, agentEvaluation, ciTriage, securityBoundary, documentationStrategy, productRequirements, softwareArchitectureDocs, userDocumentation, apiDocumentation, qualityTestDocumentation, operationsRunbooks, governanceComplianceDocs, releaseLifecycleDocs, endToEndTesting, memorySync, sddLifecycle}}, nil
}

func validSkillName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, character := range name {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

func native(identity string) string { return filepath.FromSlash(identity) }

func skillNames(entries map[string][]byte) []string {
	names := map[string]bool{}
	for identity := range entries {
		name, _, _ := strings.Cut(identity, "/")
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	return ordered
}

func (s *Service) predecessor(identity string, content []byte) bool {
	c, err := s.resolvedCatalog()
	if err != nil {
		return false
	}
	for _, definition := range c.definitions {
		if relative, ok := strings.CutPrefix(identity, definition.name+"/"); ok {
			actual := digest(content)
			if definition.predecessors[relative] == actual {
				return true
			}
			for _, legacy := range definition.legacy {
				if legacy.digests[relative] == actual || !legacy.exactOnly && bytes.Equal(content, definition.files[relative]) {
					return true
				}
			}
			return false
		}
	}
	return false
}

package skills

import (
	"path/filepath"
	"sort"
	"strings"
)

type skillDefinition struct {
	name         string
	source       string
	files        map[string][]byte
	predecessors map[string]string
	legacy       []legacyDefinition
}

type legacyDefinition struct {
	name    string
	digests map[string]string
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
	stackedPR := skillDefinition{name: "stacked-pr", source: "stacked-pr"}
	stackedPR.files, err = bundledFiles(stackedPR.source)
	if err != nil {
		return catalog{}, err
	}
	return catalog{definitions: []skillDefinition{creator, stackedPR}}, nil
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
				if legacy.digests[relative] == actual {
					return true
				}
			}
			return false
		}
	}
	return false
}

package skills

import (
	"path/filepath"
	"sort"
	"strings"
)

type skillDefinition struct {
	name         string
	files        map[string][]byte
	predecessors map[string]string
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
	skills := map[string]bool{}
	for _, definition := range c.definitions {
		if !validSkillName(definition.name) || skills[definition.name] || len(definition.files) == 0 {
			return nil, ErrInvalid
		}
		skills[definition.name] = true
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
			if !validRelative(relative) {
				return nil, ErrInvalid
			}
		}
	}
	return entries, nil
}

func bundledCatalog() (catalog, error) {
	definition := skillDefinition{name: "agent-skill-engineer", predecessors: predecessorDigests}
	entries, err := bundledFiles(definition.name)
	if err != nil {
		return catalog{}, err
	}
	definition.files = entries
	return catalog{definitions: []skillDefinition{definition}}, nil
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
			return definition.predecessors[relative] == digest(content)
		}
	}
	return false
}

package orchestration

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

//go:embed manager_contract.json
var managerContractBytes []byte

type ManagerContract struct {
	SchemaVersion string         `json:"schemaVersion"`
	Identity      string         `json:"identity"`
	Manager       ContractRole   `json:"manager"`
	Roles         []ContractRole `json:"roles"`
}
type ContractRole struct {
	ID             string   `json:"id"`
	ModelRole      string   `json:"modelRole,omitempty"`
	Aliases        []string `json:"aliases"`
	WriteAuthority bool     `json:"writeAuthority"`
	Instructions   string   `json:"instructions,omitempty"`
}

func ManagerContractJSON() []byte { return append([]byte(nil), managerContractBytes...) }
func ManagerContractDigest() string {
	var value any
	if err := json.Unmarshal(managerContractBytes, &value); err != nil {
		return ""
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return ""
	}
	sum := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return hex.EncodeToString(sum[:])
}
func LoadManagerContract() (ManagerContract, error) {
	var c ManagerContract
	if err := json.Unmarshal(managerContractBytes, &c); err != nil {
		return c, err
	}
	if c.SchemaVersion != "vgxness-manager-contract/v1" || c.Identity != ContractIdentity || c.Manager.ID != "manager" || len(c.Roles) != 12 {
		return c, errors.New("invalid manager contract")
	}
	seen := map[string]bool{"manager": true}
	required := []string{"explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger", "sdd-research", "sdd-proposal", "sdd-spec", "sdd-design", "sdd-tasks", "sdd-apply"}
	if strings.TrimSpace(c.Manager.Instructions) == "" {
		return c, errors.New("invalid manager instructions")
	}
	for index, r := range c.Roles {
		if r.ID != required[index] || seen[r.ID] || strings.TrimSpace(r.Instructions) == "" {
			return c, errors.New("invalid manager role")
		}
		seen[r.ID] = true
		for _, alias := range r.Aliases {
			if strings.TrimSpace(alias) == "" || seen[alias] {
				return c, errors.New("invalid manager role alias")
			}
			seen[alias] = true
		}
	}
	return c, nil
}
func (c ManagerContract) Role(name string) (ContractRole, bool) {
	if name == c.Manager.ID {
		return c.Manager, true
	}
	for _, r := range c.Roles {
		if name == r.ID {
			return r, true
		}
		for _, a := range r.Aliases {
			if name == a {
				return r, true
			}
		}
	}
	return ContractRole{}, false
}
func (c ManagerContract) CanWrite(name string) bool {
	r, ok := c.Role(name)
	return ok && r.WriteAuthority
}

func (c ManagerContract) RenderManagerSections() string {
	var roles []string
	for _, r := range c.Roles {
		authority := "read-only"
		if r.WriteAuthority {
			authority = "may write only within its bounded mission"
		}
		aliases := strings.Join(r.Aliases, ", ")
		if aliases == "" {
			aliases = "none"
		}
		roles = append(roles, "## "+r.ID+"\nAuthority: "+authority+". Aliases: "+aliases+".\n"+r.Instructions)
	}
	return c.Manager.Instructions + "\n\n# Delegated role contract\n" + strings.Join(roles, "\n\n") + "\n"
}

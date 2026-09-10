package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// InstallationReceipt records integrity of the installer's own artifacts. It is
// local ownership evidence, not a signature or protection from the same user
// rewriting both the receipt and its files. Callers anchor the managed root and
// perform the same identity/byte rechecks as other lifecycle writes.
type InstallationReceipt struct {
	Schema   int               `json:"schema"`
	Provider string            `json:"provider"`
	Contract string            `json:"contract"`
	Plan     string            `json:"plan,omitempty"`
	Files    map[string]string `json:"files"`
}

const ReceiptName = "installation-receipt.json"

func receiptPath(provider, name string) bool {
	roles := []string{"explore", "general", "verifier", "care-reviewer", "care-specialist", "care-challenger"}
	if provider == "codex" {
		if name == "AGENTS.md" || name == ".agents/plugins/marketplace.json" || name == "plugins/vgxness/.codex-plugin/plugin.json" {
			return true
		}
		for _, role := range roles {
			if name == "agents/"+role+".toml" {
				return true
			}
		}
	}
	if provider == "opencode" {
		if name == "agents/vgxness-manager.md" || name == "plugins/vgxness-memory-lifecycle.ts" || name == "vgxness/model-plan.json" {
			return true
		}
		for _, role := range roles {
			prefix := "vgxness-"
			if role == "explore" || role == "general" {
				prefix = ""
			}
			if name == "agents/"+prefix+role+".md" {
				return true
			}
		}
	}
	return false
}
func receiptDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func (r InstallationReceipt) valid() bool {
	if r.Schema != 1 || (r.Provider != "codex" && r.Provider != "opencode") || !receiptDigest(r.Contract) || len(r.Files) == 0 || len(r.Files) > 16 {
		return false
	}
	if r.Provider == "codex" {
		if r.Plan != "low" && r.Plan != "medium" && r.Plan != "high" && r.Plan != "ultra" {
			return false
		}
	} else if r.Plan != "" {
		return false
	}
	for name, digest := range r.Files {
		if !receiptPath(r.Provider, name) || !receiptDigest(digest) {
			return false
		}
	}
	return true
}
func EncodeReceipt(provider, contract, plan string, files map[string][]byte) ([]byte, error) {
	r := InstallationReceipt{Schema: 1, Provider: provider, Contract: contract, Plan: plan, Files: map[string]string{}}
	for name, data := range files {
		sum := sha256.Sum256(data)
		r.Files[name] = hex.EncodeToString(sum[:])
	}
	if !r.valid() {
		return nil, errors.New("invalid installation receipt")
	}
	data, err := json.Marshal(r)
	return append(data, '\n'), err
}
func DecodeReceipt(data []byte, provider string) (InstallationReceipt, error) {
	var r InstallationReceipt
	if len(data) > 16384 || json.Unmarshal(data, &r) != nil || !r.valid() || r.Provider != provider {
		return InstallationReceipt{}, errors.New("invalid installation receipt")
	}
	canonical, err := json.Marshal(r)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return InstallationReceipt{}, errors.New("noncanonical installation receipt")
	}
	return r, nil
}
func (r InstallationReceipt) Matches(name string, data []byte) bool {
	sum := sha256.Sum256(data)
	return r.Files[name] != "" && r.Files[name] == hex.EncodeToString(sum[:])
}

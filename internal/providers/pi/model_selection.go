package pi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/vgxness/vgxness/internal/agentmodels"
	"os"
	"path/filepath"
	"strings"
)

func settingsDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func applyModelSettings(value map[string]json.RawMessage, c agentmodels.Config) {
	value["vgxnessModels"], _ = json.Marshal(c)
	manager := c.Assignments["manager"]
	ref := strings.SplitN(manager.Model, "/", 2)
	value["defaultProvider"] = jsonString(ref[0])
	value["defaultModel"] = jsonString(ref[1])
	value["defaultThinkingLevel"] = jsonString(manager.Effort)
}
func modelSettings(agent string, requested *agentmodels.Config) (string, *agentmodels.Config, bool, error) {
	path := filepath.Join(agent, "settings.json")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return "", nil, false, errors.New("unsafe Pi settings")
		}
	} else if !os.IsNotExist(err) {
		return "", nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, false, err
	}
	value := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &value); err != nil {
			return "", nil, false, err
		}
	}
	if value == nil {
		return "", nil, false, errors.New("malformed Pi settings")
	}
	var installed *agentmodels.Config
	if raw, exists := value["vgxnessModels"]; exists {
		var c agentmodels.Config
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return "", nil, false, err
		}
		if err := c.Validate(); err != nil {
			return "", nil, false, err
		}
		installed = &c
	}
	if requested == nil {
		return settingsDigest(data), installed, false, nil
	}
	if err := requested.Validate(); err != nil {
		return "", nil, false, err
	}
	before, _ := json.Marshal(value)
	applyModelSettings(value, *requested)
	after, _ := json.Marshal(value)
	return settingsDigest(data), requested, !bytes.Equal(before, after), nil
}

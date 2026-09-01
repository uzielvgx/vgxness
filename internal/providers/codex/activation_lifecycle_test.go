package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type fakeCodexCLI struct {
	market, plugin, enabled bool
	root, version           string
	fail                    map[string]error
}

func (f *fakeCodexCLI) run(_ context.Context, _ string, args, _ []string) ([]byte, error) {
	key := fmt.Sprint(args)
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	switch key {
	case "[plugin list --json]":
		installed := []any{}
		if f.plugin {
			version := f.version
			if version == "" {
				version = "0.0.0"
			}
			installed = append(installed, map[string]any{"pluginId": pluginID, "name": marketplaceName, "marketplaceName": marketplaceName, "version": version, "installed": true, "enabled": f.enabled})
		}
		return json.Marshal(map[string]any{"installed": installed})
	case "[plugin marketplace list --json]":
		markets := []any{}
		if f.market {
			markets = append(markets, map[string]any{"name": marketplaceName, "root": f.root})
		}
		return json.Marshal(map[string]any{"marketplaces": markets})
	case "[plugin add vgxness@vgxness --json]":
		f.plugin, f.enabled = true, true
	case "[plugin remove vgxness@vgxness --json]":
		f.plugin = false
	case "[plugin marketplace remove vgxness --json]":
		f.market = false
	}
	if strings.HasPrefix(key, "[plugin marketplace add ") {
		f.market = true
	}
	return []byte(`{}`), nil
}

func fakeActivationIntegration(f *fakeCodexCLI) *Integration {
	s := NewIntegration()
	s.runner, s.codexBin = f.run, "fake"
	return s
}

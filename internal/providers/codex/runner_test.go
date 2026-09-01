package codex

import (
	"context"
	"encoding/json"
	"strings"
)

type unitCLIState struct{ market, plugin bool }

var unitCLI = map[string]*unitCLIState{}

func init() {
	defaultRunner = func(_ context.Context, _ string, args, env []string) ([]byte, error) {
		root := ""
		for _, v := range env {
			if strings.HasPrefix(v, "CODEX_HOME=") {
				root = strings.TrimPrefix(v, "CODEX_HOME=")
			}
		}
		s := unitCLI[root]
		if s == nil {
			s = &unitCLIState{}
			unitCLI[root] = s
		}
		switch strings.Join(args, " ") {
		case "plugin marketplace list --json":
			v := []any{}
			if s.market {
				v = []any{map[string]any{"name": marketplaceName, "root": root}}
			}
			return json.Marshal(map[string]any{"marketplaces": v})
		case "plugin list --json":
			v := []any{}
			if s.plugin {
				v = []any{map[string]any{"pluginId": pluginID, "name": marketplaceName, "marketplaceName": marketplaceName, "version": "0.0.0", "installed": true, "enabled": true}}
			}
			return json.Marshal(map[string]any{"installed": v})
		case "plugin marketplace add " + root + " --json":
			s.market = true
		case "plugin add " + pluginID + " --json":
			s.plugin = true
		case "plugin remove " + pluginID + " --json":
			s.plugin = false
		case "plugin marketplace remove " + marketplaceName + " --json":
			s.market = false
		}
		return []byte(`{}`), nil
	}
}

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
)

func TestRunCodexFailsClosed(t *testing.T) {
	_, err := runCodex(context.Background(), "/missing/codex", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "codex command failed") {
		t.Fatalf("runCodex() error = %v, want fail-closed command error", err)
	}
}

type fakeCodexCLI struct {
	market, plugin, enabled bool
	root, version           string
	fail                    map[string]error
	output                  map[string][]byte
	mutations               int
}

func (f *fakeCodexCLI) Run(_ context.Context, _ string, args, _ []string) ([]byte, error) {
	key := fmt.Sprint(args)
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	if output, ok := f.output[key]; ok {
		return output, nil
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
		f.mutations++
		f.plugin, f.enabled = true, true
	case "[plugin remove vgxness@vgxness --json]":
		f.mutations++
		f.plugin = false
	case "[plugin marketplace remove vgxness --json]":
		f.mutations++
		f.market = false
	}
	if strings.HasPrefix(key, "[plugin marketplace add ") {
		f.mutations++
		f.market = true
	}
	return []byte(`{}`), nil
}

func fakeActivationIntegration(f *fakeCodexCLI) *Integration {
	s := NewIntegration()
	s.runner, s.codexBin = f, "fake"
	return s
}

func TestActivateInspectionFailuresDoNotMutate(t *testing.T) {
	marketList := "[plugin marketplace list --json]"
	pluginList := "[plugin list --json]"
	for name, fake := range map[string]*fakeCodexCLI{
		"marketplace command": {fail: map[string]error{marketList: errors.New("marketplace list")}},
		"plugin command":      {fail: map[string]error{pluginList: errors.New("plugin list")}},
		"marketplace JSON":    {output: map[string][]byte{marketList: []byte(`{`)}},
		"plugin JSON":         {output: map[string][]byte{pluginList: []byte(`{`)}},
	} {
		t.Run(name, func(t *testing.T) {
			root := openActivationRoot(t)
			_, _, _, err := fakeActivationIntegration(fake).activate(context.Background(), root)
			if err == nil || fake.mutations != 0 || fake.market || fake.plugin {
				t.Fatalf("activate() err=%v mutations=%d market=%t plugin=%t; want inspection failure without mutation", err, fake.mutations, fake.market, fake.plugin)
			}
		})
	}
}

func TestActivateRejectsIncompleteInspectionJSONWithoutMutation(t *testing.T) {
	marketList := "[plugin marketplace list --json]"
	pluginList := "[plugin list --json]"
	for name, output := range map[string]map[string][]byte{
		"empty object":              {marketList: []byte(`{}`), pluginList: []byte(`{}`)},
		"null arrays":               {marketList: []byte(`{"marketplaces":null}`), pluginList: []byte(`{"installed":null}`)},
		"omitted marketplace array": {marketList: []byte(`{}`), pluginList: []byte(`{"installed":[]}`)},
		"omitted plugin array":      {marketList: []byte(`{"marketplaces":[]}`), pluginList: []byte(`{}`)},
		"wrong array types":         {marketList: []byte(`{"marketplaces":{}}`), pluginList: []byte(`{"installed":{}}`)},
		"incomplete marketplace":    {marketList: []byte(`{"marketplaces":[{"name":"vgxness"}]}`), pluginList: []byte(`{"installed":[]}`)},
		"incomplete plugin":         {marketList: []byte(`{"marketplaces":[]}`), pluginList: []byte(`{"installed":[{"pluginId":"vgxness@vgxness"}]}`)},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeCodexCLI{output: output}
			root := openActivationRoot(t)
			_, _, _, err := fakeActivationIntegration(fake).activate(context.Background(), root)
			if err == nil || fake.mutations != 0 {
				t.Fatalf("activate() err=%v mutations=%d; want invalid inspection without mutation", err, fake.mutations)
			}
		})
	}
}

func openActivationRoot(t *testing.T) *Root {
	t.Helper()
	root, err := OpenRoot(context.Background(), integration.Options{ConfigDir: t.TempDir()}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

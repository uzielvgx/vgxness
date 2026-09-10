package opencode

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/vgxness/vgxness/internal/modelplan"
)

// Only deterministic, compiled-in historical transformations are cached. No
// filesystem content, ownership verdict, or inspection result enters this cache.
const historicalBundleCacheLimit = 8 << 20

type historicalBundleSnapshot struct {
	Config     modelplan.ModelPlanConfig
	Resolved   modelplan.OpenCodePlan
	ConfigV2   *modelplan.ModelPlanConfigV2
	ResolvedV2 *modelplan.OpenCodePlanV2
	ConfigV3   *modelplan.ModelPlanConfigV3
	ResolvedV3 *modelplan.OpenCodePlanV3
	Agents     map[string][]byte
	Manifest   []byte
}

func snapshotHistoricalBundle(b modelPlanBundle) historicalBundleSnapshot {
	return historicalBundleSnapshot{b.config, b.resolved, b.configV2, b.resolvedV2, b.configV3, b.resolvedV3, b.agents, b.manifest}
}

func (s historicalBundleSnapshot) bundle() modelPlanBundle {
	return modelPlanBundle{s.Config, s.Resolved, s.ConfigV2, s.ResolvedV2, s.ConfigV3, s.ResolvedV3, s.Agents, s.Manifest}
}

func cloneHistoricalBundle(b modelPlanBundle) modelPlanBundle {
	b.resolved.Slots = maps.Clone(b.resolved.Slots)
	b.resolved.Roles = maps.Clone(b.resolved.Roles)
	if b.configV2 != nil {
		v := *b.configV2
		v.Slots = maps.Clone(v.Slots)
		b.configV2 = &v
	}
	if b.resolvedV2 != nil {
		v := *b.resolvedV2
		v.Slots = maps.Clone(v.Slots)
		v.Roles = maps.Clone(v.Roles)
		b.resolvedV2 = &v
	}
	if b.configV3 != nil {
		v := *b.configV3
		v.Assignments = maps.Clone(v.Assignments)
		b.configV3 = &v
	}
	if b.resolvedV3 != nil {
		v := *b.resolvedV3
		v.Assignments = slices.Clone(v.Assignments)
		b.resolvedV3 = &v
	}
	b.agents = maps.Clone(b.agents)
	for name, content := range b.agents {
		b.agents[name] = bytes.Clone(content)
	}
	b.manifest = bytes.Clone(b.manifest)
	return b
}

func buildOwnedHistoricalBundles(current modelPlanBundle, build func(modelPlanBundle) ([]modelPlanBundle, error)) ([]modelPlanBundle, error) {
	bundles, err := build(cloneHistoricalBundle(current))
	owned := slices.Clone(bundles)
	for i, bundle := range owned {
		owned[i] = cloneHistoricalBundle(bundle)
	}
	return owned, err
}

type historicalBundleCache struct {
	mu            sync.Mutex
	key           []byte
	value         []byte
	previousKey   []byte
	previousValue []byte
}

var historicalBundles historicalBundleCache

func supportedHistoricalModelPlanBundles(current modelPlanBundle) ([]modelPlanBundle, error) {
	// Embedded/composed prompt templates are immutable after package init.
	// Changing that invariant requires invalidation or removal of this cache.
	return historicalBundles.get(current, supportedHistoricalModelPlanBundlesUncached)
}

func encodeHistoricalBundles(snapshots []historicalBundleSnapshot) ([]byte, error) {
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(snapshots); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func decodeHistoricalBundles(data []byte) ([]modelPlanBundle, error) {
	var snapshots []historicalBundleSnapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snapshots); err != nil {
		return nil, err
	}
	if snapshots == nil {
		return nil, nil
	}
	bundles := make([]modelPlanBundle, len(snapshots))
	for i, snapshot := range snapshots {
		bundles[i] = snapshot.bundle()
	}
	return bundles, nil
}

func (c *historicalBundleCache) get(current modelPlanBundle, build func(modelPlanBundle) ([]modelPlanBundle, error)) ([]modelPlanBundle, error) {
	key, err := json.Marshal(snapshotHistoricalBundle(current))
	if err != nil || len(key) > historicalBundleCacheLimit {
		return buildOwnedHistoricalBundles(current, build)
	}
	var input historicalBundleSnapshot
	if json.Unmarshal(key, &input) != nil || !reflect.DeepEqual(current, input.bundle()) {
		// JSON can normalize invalid UTF-8 strings. Such input retains the
		// original algorithm's behavior and never aliases a cached identity.
		return buildOwnedHistoricalBundles(current, build)
	}

	// The uncached builder does not call this cache. Holding the lock also
	// coalesces identical concurrent misses without storing mutable bundles.
	c.mu.Lock()
	defer c.mu.Unlock()
	if bytes.Equal(key, c.key) && c.value != nil {
		bundles, decodeErr := decodeHistoricalBundles(c.value)
		if decodeErr == nil {
			return bundles, nil
		}
		c.key, c.value = nil, nil
	}
	if bytes.Equal(key, c.previousKey) && c.previousValue != nil {
		bundles, decodeErr := decodeHistoricalBundles(c.previousValue)
		if decodeErr == nil {
			c.key, c.previousKey = c.previousKey, c.key
			c.value, c.previousValue = c.previousValue, c.value
			return bundles, nil
		}
		c.previousKey, c.previousValue = nil, nil
	}
	bundles, err := buildOwnedHistoricalBundles(input.bundle(), build)
	if err != nil {
		return bundles, err
	}
	var snapshots []historicalBundleSnapshot
	if bundles != nil {
		snapshots = make([]historicalBundleSnapshot, len(bundles))
	}
	for i, bundle := range bundles {
		snapshots[i] = snapshotHistoricalBundle(bundle)
	}
	value, encodeErr := encodeHistoricalBundles(snapshots)
	if encodeErr != nil || len(value) > historicalBundleCacheLimit-len(key) {
		return bundles, nil
	}
	// Retain the lossless JSON contract even though cached artifact bytes use
	// a binary encoding to avoid repeatedly scanning/base64-decoding templates.
	jsonValue, jsonErr := json.Marshal(snapshots)
	var jsonRoundTrip []historicalBundleSnapshot
	if jsonErr != nil || json.Unmarshal(jsonValue, &jsonRoundTrip) != nil || !reflect.DeepEqual(snapshots, jsonRoundTrip) {
		return bundles, nil
	}
	decoded, decodeErr := decodeHistoricalBundles(value)
	if decodeErr != nil || !reflect.DeepEqual(bundles, decoded) {
		return bundles, nil
	}
	// Keep the most recent surviving entry only when both fit the same
	// total budget. An observed corrupt current slot must not discard a
	// valid previous slot. Failed or lossy builds never reach this point.
	previousKey, previousValue := c.key, c.value
	if previousValue == nil {
		previousKey, previousValue = c.previousKey, c.previousValue
	}
	if len(previousKey)+len(previousValue) > historicalBundleCacheLimit-len(key)-len(value) {
		previousKey, previousValue = nil, nil
	}
	// Retain exact-capacity storage, not spare marshal/buffer allocations.
	newKey, newValue := make([]byte, len(key)), make([]byte, len(value))
	copy(newKey, key)
	copy(newValue, value)
	c.key, c.value, c.previousKey, c.previousValue = newKey, newValue, previousKey, previousValue

	return bundles, nil
}

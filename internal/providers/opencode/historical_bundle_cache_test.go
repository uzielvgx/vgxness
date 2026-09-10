package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

func TestModelPlanMarkerCountEquivalent(t *testing.T) {
	inputs := [][]byte{nil, {}, []byte("aaaaa"), []byte("banana banana"), []byte("日本日本"), []byte("👋🌎👋"), {0xff, 0, 0xfe, 0xff}}
	markers := [][]byte{nil, {}, []byte("a"), []byte("ana"), []byte("日本"), []byte("👋"), {0xff}, []byte(managerPreviousMarker)}
	check := func(data, marker []byte) {
		t.Helper()
		if got, want := countModelPlanMarker(data, marker), bytes.Count(data, marker); got != want {
			t.Fatalf("marker count differs: got %d want %d, data %x marker %x", got, want, data, marker)
		}
	}
	for _, data := range inputs {
		for _, marker := range markers {
			check(data, marker)
		}
		check(data, data)
	}
	random := rand.New(rand.NewSource(518))
	for i := 0; i < 256; i++ {
		data := make([]byte, random.Intn(1024))
		_, _ = random.Read(data)
		marker := make([]byte, random.Intn(64))
		_, _ = random.Read(marker)
		check(data, marker)
		if len(data) != 0 {
			start := random.Intn(len(data))
			end := start + random.Intn(len(data)-start+1)
			check(data, data[start:end])
		}
	}
}

func TestHistoricalBundleCacheEquivalent(t *testing.T) {
	v1, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	v2, err := buildModelPlanBundleV2(modelplan.DefaultModelPlanConfigV2())
	if err != nil {
		t.Fatal(err)
	}
	v3, err := buildModelPlanBundleV3(projectModelPlanToV3(modelplan.DefaultModelPlanConfig()))
	if err != nil {
		t.Fatal(err)
	}
	for i, current := range []modelPlanBundle{v1, v2, v3, {}} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			want, wantErr := supportedHistoricalModelPlanBundlesUncached(current)
			var cache historicalBundleCache
			calls := 0
			build := func(b modelPlanBundle) ([]modelPlanBundle, error) {
				calls++
				return supportedHistoricalModelPlanBundlesUncached(b)
			}
			for attempt := 0; attempt < 2; attempt++ {
				got, gotErr := cache.get(current, build)
				if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
					t.Fatalf("cached historical result differs on attempt %d: %v / %v", attempt, gotErr, wantErr)
				}
			}
			if wantErr == nil && (calls != 1 || len(cache.value) == 0) {
				t.Fatal("valid historical bundle did not benefit from cache", calls)
			}
			if wantErr != nil && calls != 2 {
				t.Fatal("invalid historical bundle cached")
			}
		})
	}
}

func cacheTestBundle() modelPlanBundle {
	v2 := modelplan.DefaultModelPlanConfigV2()
	v3 := projectModelPlanToV3(modelplan.DefaultModelPlanConfig())
	return modelPlanBundle{
		config:   modelplan.DefaultModelPlanConfig(),
		resolved: modelplan.OpenCodePlan{Slots: map[modelplan.Capability]string{modelplan.CapabilityEfficient: "model"}},
		configV2: &v2, resolvedV2: &modelplan.OpenCodePlanV2{Slots: v2.Slots},
		configV3: &v3, resolvedV3: &modelplan.OpenCodePlanV3{Assignments: []modelplan.OpenCodeAgentAssignmentV3{{Model: "model"}}},
		agents: map[string][]byte{"agent": []byte("template")}, manifest: []byte("manifest"),
	}
}

func TestHistoricalBundleCacheIdentityAndIsolation(t *testing.T) {
	// A new bundle field must be represented in the cache snapshot and tests.
	if reflect.TypeFor[modelPlanBundle]().NumField() != 8 {
		t.Fatal("update complete cache identity for new bundle fields")
	}
	bundleType, snapshotType := reflect.TypeFor[modelPlanBundle](), reflect.TypeFor[historicalBundleSnapshot]()
	if bundleType.NumField() != snapshotType.NumField() {
		t.Fatal("snapshot inventory differs")
	}
	for i := 0; i < bundleType.NumField(); i++ {
		b, s := bundleType.Field(i), snapshotType.Field(i)
		if b.Type != s.Type || !strings.EqualFold(b.Name, s.Name) {
			t.Fatal("snapshot field differs", b.Name, s.Name)
		}
	}
	changes := []func(*modelPlanBundle){
		func(b *modelPlanBundle) { b.config.Provider = "other" },
		func(b *modelPlanBundle) { b.resolved.Slots[modelplan.CapabilityEfficient] = "other" },
		func(b *modelPlanBundle) { b.configV2.Provider = "other" },
		func(b *modelPlanBundle) { b.resolvedV2.Provider = "other" },
		func(b *modelPlanBundle) { b.configV3.Provider = "other" },
		func(b *modelPlanBundle) { b.resolvedV3.Assignments[0].Model = "other" },
		func(b *modelPlanBundle) { b.agents["agent"][0] = 'X' },
		func(b *modelPlanBundle) { b.manifest[0] = 'X' },
		func(b *modelPlanBundle) { delete(b.configV2.Slots, modelplan.CapabilityEfficient) },
		func(b *modelPlanBundle) { delete(b.resolvedV2.Slots, modelplan.CapabilityEfficient) },
		func(b *modelPlanBundle) {
			for name := range b.configV3.Assignments {
				delete(b.configV3.Assignments, name)
			}
		},
		func(b *modelPlanBundle) { delete(b.agents, "agent") },
	}
	for i, change := range changes {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var cache historicalBundleCache
			calls := 0
			build := func(b modelPlanBundle) ([]modelPlanBundle, error) { calls++; return []modelPlanBundle{b}, nil }
			input := cacheTestBundle()
			first, err := cache.get(input, build)
			if err != nil {
				t.Fatal(err)
			}
			change(&first[0])
			second, err := cache.get(input, build)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !reflect.DeepEqual(second[0], input) {
				t.Fatal("returned mutation contaminated cache or input")
			}
			change(&second[0])
			third, _ := cache.get(input, build)
			if !reflect.DeepEqual(third[0], input) {
				t.Fatal("cache hit returned shared mutable state")
			}
			changed := cacheTestBundle()
			change(&changed)
			got, _ := cache.get(changed, build)
			if calls != 2 || !reflect.DeepEqual(got[0], changed) {
				t.Fatal("distinct complete input reused cached entry")
			}
		})
	}
}

func TestHistoricalBundleCacheCapacityBoundary(t *testing.T) {
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			var cache historicalBundleCache
			warm := cacheTestBundle()
			identity := func(b modelPlanBundle) ([]modelPlanBundle, error) { return []modelPlanBundle{b}, nil }
			if _, err := cache.get(warm, identity); err != nil {
				t.Fatal(err)
			}
			priorKey, priorValue := bytes.Clone(cache.key), bytes.Clone(cache.value)
			input := cacheTestBundle()
			input.config.Provider = "capacity"
			key, err := json.Marshal(snapshotHistoricalBundle(input))
			if err != nil {
				t.Fatal(err)
			}
			result := cacheTestBundle()
			result.config.Provider = ""
			base, err := encodeHistoricalBundles([]historicalBundleSnapshot{snapshotHistoricalBundle(result)})
			if err != nil {
				t.Fatal(err)
			}
			result.config.Provider = strings.Repeat("x", historicalBundleCacheLimit+delta-len(key)-len(base))
			for attempt := 0; attempt < 5; attempt++ {
				encoded, err := encodeHistoricalBundles([]historicalBundleSnapshot{snapshotHistoricalBundle(result)})
				if err != nil {
					t.Fatal(err)
				}
				adjustment := historicalBundleCacheLimit + delta - len(key) - len(encoded)
				if adjustment == 0 {
					break
				}
				result.config.Provider = strings.Repeat("x", len(result.config.Provider)+adjustment)
			}
			calls := 0
			build := func(modelPlanBundle) ([]modelPlanBundle, error) { calls++; return []modelPlanBundle{result}, nil }
			for i := 0; i < 2; i++ {
				got, err := cache.get(input, build)
				if err != nil || !reflect.DeepEqual(got, []modelPlanBundle{result}) {
					t.Fatal("capacity changed result", err)
				}
				got[0].agents["agent"][0] = 'X'
				if result.agents["agent"][0] == 'X' {
					t.Fatal("bypass/miss result aliases builder output")
				}
			}
			if delta <= 0 {
				if calls != 1 || len(cache.key)+len(cache.value) != historicalBundleCacheLimit+delta {
					t.Fatal("cacheable boundary missed", calls)
				}
				if cap(cache.key)+cap(cache.value) != len(cache.key)+len(cache.value) {
					t.Fatal("cache retained spare allocation capacity")
				}
			} else if calls != 2 || !bytes.Equal(cache.key, priorKey) || !bytes.Equal(cache.value, priorValue) {
				t.Fatal("oversize entry retained or displaced prior success")
			}
		})
	}
}

func TestHistoricalBundleCacheLimitsAndErrors(t *testing.T) {
	for _, mode := range []string{"error", "large-input", "large-output", "invalid-utf8-input", "invalid-utf8-output"} {
		t.Run(mode, func(t *testing.T) {
			var cache historicalBundleCache
			input := cacheTestBundle()
			if mode == "large-input" {
				input.manifest = make([]byte, historicalBundleCacheLimit)
			}
			if mode == "invalid-utf8-input" {
				input.config.Provider = string([]byte{0xff})
			}
			calls := 0
			failure := errors.New("construction rejected")
			build := func(b modelPlanBundle) ([]modelPlanBundle, error) {
				calls++
				if mode == "error" {
					return nil, failure
				}
				if mode == "large-output" {
					b.manifest = make([]byte, historicalBundleCacheLimit)
				}
				if mode == "invalid-utf8-output" {
					b.config.Provider = string([]byte{0xff})
				}
				return []modelPlanBundle{b}, nil
			}
			for i := 0; i < 2; i++ {
				got, err := cache.get(input, build)
				if mode == "error" && !errors.Is(err, failure) {
					t.Fatal("lost construction error")
				}
				if mode != "error" && err != nil {
					t.Fatal(err)
				}
				if mode == "invalid-utf8-input" || mode == "invalid-utf8-output" {
					if got[0].config.Provider != string([]byte{0xff}) {
						t.Fatal("normalized original result")
					}
				}
			}
			if calls != 2 || len(cache.key)+len(cache.value) != 0 {
				t.Fatal("error, oversize or lossy result retained")
			}
		})
	}
	var cache historicalBundleCache
	build := func(b modelPlanBundle) ([]modelPlanBundle, error) { return []modelPlanBundle{b}, nil }
	for i := 0; i < 8; i++ {
		input := cacheTestBundle()
		input.config.Provider = fmt.Sprint(i)
		if _, err := cache.get(input, build); err != nil {
			t.Fatal(err)
		}
		if len(cache.key)+len(cache.value) > historicalBundleCacheLimit {
			t.Fatal("cache capacity exceeded")
		}
	}
}

func TestHistoricalBundleCacheConcurrent(t *testing.T) {
	var cache historicalBundleCache
	var calls atomic.Int32
	build := func(b modelPlanBundle) ([]modelPlanBundle, error) { calls.Add(1); return []modelPlanBundle{b}, nil }
	input := cacheTestBundle()
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Go(func() {
			result, err := cache.get(input, build)
			if err != nil || len(result) != 1 {
				t.Errorf("concurrent result: %v", err)
				return
			}
			if !reflect.DeepEqual(result[0], input) {
				t.Error("concurrent value changed")
			}
			result[0].agents["agent"][0] = 'X'
		})
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("identical construction repeated %d times", calls.Load())
	}
}

func BenchmarkHistoricalBundleCache(b *testing.B) {
	input, err := buildModelPlanBundleV3(projectModelPlanToV3(modelplan.DefaultModelPlanConfig()))
	if err != nil {
		b.Fatal(err)
	}
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprint(cached), func(b *testing.B) {
			var cache historicalBundleCache
			for b.Loop() {
				var err error
				if cached {
					_, err = cache.get(input, supportedHistoricalModelPlanBundlesUncached)
				} else {
					_, err = supportedHistoricalModelPlanBundlesUncached(input)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Original independent reconstruction is the compatibility oracle.
func historicalBundlesIndependentOracle(current modelPlanBundle) ([]modelPlanBundle, error) {
	bundles := []modelPlanBundle{current}
	if isSharedManagerBundle(current) {
		old, e := managerV60Bundle(current)
		if e != nil {
			return nil, e
		}
		bundles = append(bundles, old)
		previous, e := sharedBundleForContract(old, orchestration.PreviousManagerContract(), true)
		if e != nil {
			return nil, e
		}
		bundles = append(bundles, previous)
	}
	for _, predecessor := range []func(modelPlanBundle) (modelPlanBundle, error){
		immediatePredecessor,
		previousCAREV1ModelPlanBundle,
		previousV57ModelPlanBundle,
		previousV56ModelPlanBundle,
		previousV55ModelPlanBundle,
		previousV54ModelPlanBundle,
		previousV53ModelPlanBundle,
		previousV52ModelPlanBundle,
		previousV51ModelPlanBundle,
		previousV50ModelPlanBundle,
		previousV49ModelPlanBundle,
		previousV48ModelPlanBundle,
		previousV47ModelPlanBundle,
		previousV46ModelPlanBundle,
		previousV45ModelPlanBundle,
		previousV44ModelPlanBundle,
		previousV43ModelPlanBundle,
	} {
		bundle, err := predecessor(current)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, bundle)
	}
	legacyCurrent, err := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	if err != nil {
		return nil, err
	}
	for _, predecessor := range []func(modelPlanBundle) (modelPlanBundle, error){
		previousV45ModelPlanBundle,
		previousV44ModelPlanBundle,
		previousV43ModelPlanBundle,
	} {
		bundle, err := predecessor(legacyCurrent)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

func TestHistoricalBundleChainEquivalent(t *testing.T) {
	v1, e1 := buildModelPlanBundle(modelplan.DefaultModelPlanConfig())
	v2, e2 := buildModelPlanBundleV2(modelplan.DefaultModelPlanConfigV2())
	v3, e3 := buildModelPlanBundleV3(projectModelPlanToV3(modelplan.DefaultModelPlanConfig()))
	if e1 != nil || e2 != nil || e3 != nil {
		t.Fatal(e1, e2, e3)
	}
	for i, base := range []modelPlanBundle{v1, v2, v3, {}} {
		cases := []modelPlanBundle{base}
		for _, change := range []func(*modelPlanBundle){
			func(b *modelPlanBundle) { b.configV2 = nil }, func(b *modelPlanBundle) { b.resolvedV2 = nil },
			func(b *modelPlanBundle) { b.configV3 = nil }, func(b *modelPlanBundle) { b.resolvedV3 = nil },
			func(b *modelPlanBundle) { v := modelplan.DefaultModelPlanConfigV2(); b.configV2 = &v },
			func(b *modelPlanBundle) {
				v := projectModelPlanToV3(modelplan.DefaultModelPlanConfig())
				b.configV3 = &v
			},
			func(b *modelPlanBundle) { b.manifest = []byte("invalid manifest") },
		} {
			altered := cloneHistoricalBundle(base)
			change(&altered)
			cases = append(cases, altered)
		}

		if len(base.agents) > 0 {
			frozen, err := managerV60Bundle(base)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, frozen)
			for _, name := range []string{managerAgentName, generalAgentName, verifierAgentName, sddApplyName, exploreAgentName} {
				missing := cloneHistoricalBundle(base)
				delete(missing.agents, name)
				cases = append(cases, missing)
				malformed := cloneHistoricalBundle(base)
				malformed.agents[name] = []byte("invalid marker \xff")
				cases = append(cases, malformed)
			}
		}
		for j, current := range cases {
			t.Run(fmt.Sprintf("%d/%d", i, j), func(t *testing.T) {
				want, werr := historicalBundlesIndependentOracle(cloneHistoricalBundle(current))
				got, gerr := supportedHistoricalModelPlanBundlesUncached(cloneHistoricalBundle(current))
				if !reflect.DeepEqual(got, want) || fmt.Sprint(gerr) != fmt.Sprint(werr) || errors.Is(gerr, integration.ErrInvalid) != errors.Is(werr, integration.ErrInvalid) {
					t.Fatalf("historical chain differs: %v / %v", gerr, werr)
				}
			})
		}
	}
}

func BenchmarkHistoricalBundleChain(b *testing.B) {
	input, err := buildModelPlanBundleV3(projectModelPlanToV3(modelplan.DefaultModelPlanConfig()))
	if err != nil {
		b.Fatal(err)
	}
	for _, entry := range []struct {
		name  string
		build func(modelPlanBundle) ([]modelPlanBundle, error)
	}{{"independent", historicalBundlesIndependentOracle}, {"chain", supportedHistoricalModelPlanBundlesUncached}} {
		b.Run(entry.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := entry.build(input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestHistoricalBundleCacheAlternatingProduction(t *testing.T) {
	current, err := buildModelPlanBundleV3(projectModelPlanToV3(modelplan.DefaultModelPlanConfig()))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := managerV60Bundle(current)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []modelPlanBundle{current, frozen}
	var cache historicalBundleCache
	calls := 0
	total := 0
	build := func(b modelPlanBundle) ([]modelPlanBundle, error) {
		calls++
		return supportedHistoricalModelPlanBundlesUncached(b)
	}
	for i := 0; i < 16; i++ {
		input := inputs[i%2]
		got, err := cache.get(input, build)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got[0], input) {
			t.Fatal("wrong identity on alternating hit")
		}
		got[0].agents[managerAgentName][0] = 'X'
		if i < 2 {
			retained := cap(cache.key) + cap(cache.value)
			total += retained
			t.Logf("production entry%d retained%d", i, retained)
		}
	}
	t.Logf("production pair retained%d limit%d", total, historicalBundleCacheLimit)
	if total > historicalBundleCacheLimit {
		t.Fatal("production pair exceeds budget")
	}
	if calls != 2 {
		t.Fatalf("alternating inputs constructed%d times, want2", calls)
	}
	if cache.value == nil || cache.previousValue == nil || historicalCacheRetained(&cache) != total {
		t.Fatal("production pair not resident under measured budget")
	}
}

func historicalCacheRetained(c *historicalBundleCache) int {
	return cap(c.key) + cap(c.value) + cap(c.previousKey) + cap(c.previousValue)
}

func TestHistoricalBundleCacheTwoEntryLRU(t *testing.T) {
	var cache historicalBundleCache
	calls := map[string]int{}
	build := func(b modelPlanBundle) ([]modelPlanBundle, error) {
		calls[b.config.Provider]++
		return []modelPlanBundle{b}, nil
	}
	for _, name := range []string{"A", "B", "A", "C", "A", "B"} {
		input := cacheTestBundle()
		input.config.Provider = name
		got, err := cache.get(input, build)
		if err != nil || !reflect.DeepEqual(got, []modelPlanBundle{input}) {
			t.Fatal("LRU changed output", err)
		}
		got[0].agents["agent"][0] = 'X'
		if historicalCacheRetained(&cache) > historicalBundleCacheLimit {
			t.Fatal("cache exceeded combined budget")
		}
	}
	if !reflect.DeepEqual(calls, map[string]int{"A": 1, "B": 2, "C": 1}) {
		t.Fatal("wrong LRU eviction", calls)
	}
}

func TestHistoricalBundleCacheCombinedCapacity(t *testing.T) {
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			var cache historicalBundleCache
			warm := cacheTestBundle()
			warm.config.Provider = "warm"
			identity := func(b modelPlanBundle) ([]modelPlanBundle, error) { return []modelPlanBundle{b}, nil }
			if _, err := cache.get(warm, identity); err != nil {
				t.Fatal(err)
			}
			priorSize := historicalCacheRetained(&cache)
			input := cacheTestBundle()
			input.config.Provider = "combined"
			key, err := json.Marshal(snapshotHistoricalBundle(input))
			if err != nil {
				t.Fatal(err)
			}
			result := cacheTestBundle()
			result.config.Provider = ""
			value, err := encodeHistoricalBundles([]historicalBundleSnapshot{snapshotHistoricalBundle(result)})
			if err != nil {
				t.Fatal(err)
			}
			result.config.Provider = strings.Repeat("x", historicalBundleCacheLimit+delta-priorSize-len(key)-len(value))
			for i := 0; i < 5; i++ {
				value, err = encodeHistoricalBundles([]historicalBundleSnapshot{snapshotHistoricalBundle(result)})
				if err != nil {
					t.Fatal(err)
				}
				adjustment := historicalBundleCacheLimit + delta - priorSize - len(key) - len(value)
				if adjustment == 0 {
					break
				}
				result.config.Provider = strings.Repeat("x", len(result.config.Provider)+adjustment)
			}
			builds := 0
			build := func(modelPlanBundle) ([]modelPlanBundle, error) { builds++; return []modelPlanBundle{result}, nil }
			for i := 0; i < 2; i++ {
				got, err := cache.get(input, build)
				if err != nil || !reflect.DeepEqual(got, []modelPlanBundle{result}) {
					t.Fatal("combined capacity changed output", err)
				}
				got[0].agents["agent"][0] = 'X'
			}
			if builds != 1 {
				t.Fatal("new entry was not retained", builds)
			}
			retained := historicalCacheRetained(&cache)
			if retained > historicalBundleCacheLimit {
				t.Fatal("combined capacity overflow", retained)
			}
			if delta <= 0 {
				if cache.previousValue == nil || retained != historicalBundleCacheLimit+delta {
					t.Fatal("fitting previous entry not retained", retained)
				}
			} else if cache.previousValue != nil {
				t.Fatal("nonfitting previous entry retained")
			}
			if retained != len(cache.key)+len(cache.value)+len(cache.previousKey)+len(cache.previousValue) {
				t.Fatal("spare capacities retained")
			}
		})
	}
}

func TestHistoricalBundleCacheBypassPreservesBoth(t *testing.T) {
	for _, kind := range []string{"error", "lossy-input", "lossy-output", "large-input", "large-output"} {
		t.Run(kind, func(t *testing.T) {
			var cache historicalBundleCache
			identity := func(b modelPlanBundle) ([]modelPlanBundle, error) { return []modelPlanBundle{b}, nil }
			for _, name := range []string{"A", "B"} {
				b := cacheTestBundle()
				b.config.Provider = name
				if _, err := cache.get(b, identity); err != nil {
					t.Fatal(err)
				}
			}
			before := [][]byte{bytes.Clone(cache.key), bytes.Clone(cache.value), bytes.Clone(cache.previousKey), bytes.Clone(cache.previousValue)}
			input := cacheTestBundle()
			input.config.Provider = "bypass"
			output := cloneHistoricalBundle(input)
			sentinel := errors.New("build failed")
			var expected error
			switch kind {
			case "error":
				expected = sentinel
			case "lossy-input":
				input.config.Provider = "\xff"
			case "lossy-output":
				output.config.Provider = "\xff"
			case "large-input":
				input.agents["huge"] = make([]byte, historicalBundleCacheLimit)
			case "large-output":
				output.agents["huge"] = make([]byte, historicalBundleCacheLimit)
			}
			got, err := cache.get(input, func(modelPlanBundle) ([]modelPlanBundle, error) { return []modelPlanBundle{output}, expected })
			if !errors.Is(err, expected) || !reflect.DeepEqual(got, []modelPlanBundle{output}) {
				t.Fatal("bypass changed original result", err)
			}
			after := [][]byte{cache.key, cache.value, cache.previousKey, cache.previousValue}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("bypass changed entries or recency")
			}
			got[0].agents["agent"][0] = 'X'
			if output.agents["agent"][0] == 'X' {
				t.Fatal("bypass returned builder alias")
			}
		})
	}
}

func TestHistoricalBundleCacheCorruptSlotPreservesOther(t *testing.T) {
	for _, corrupt := range []string{"current", "previous"} {
		t.Run(corrupt, func(t *testing.T) {
			var cache historicalBundleCache
			calls := map[string]int{}
			build := func(b modelPlanBundle) ([]modelPlanBundle, error) {
				calls[b.config.Provider]++
				return []modelPlanBundle{b}, nil
			}
			get := func(name string) {
				t.Helper()
				b := cacheTestBundle()
				b.config.Provider = name
				got, err := cache.get(b, build)
				if err != nil || !reflect.DeepEqual(got, []modelPlanBundle{b}) {
					t.Fatal("corrupt recovery result", err)
				}
				got[0].agents["agent"][0] = 'X'
			}
			get("A")
			get("B")
			if corrupt == "current" {
				cache.value = []byte("invalid gob")
				get("B")
				get("A")
				if calls["A"] != 1 || calls["B"] != 2 {
					t.Fatal("current corruption discarded survivor", calls)
				}
			} else {
				cache.previousValue = []byte("invalid gob")
				get("A")
				get("B")
				if calls["A"] != 2 || calls["B"] != 1 {
					t.Fatal("previous corruption discarded survivor", calls)
				}
			}
		})
	}
}

func TestHistoricalBundleCacheConcurrentPair(t *testing.T) {
	var cache historicalBundleCache
	var calls atomic.Int32
	var wg sync.WaitGroup
	build := func(b modelPlanBundle) ([]modelPlanBundle, error) { calls.Add(1); return []modelPlanBundle{b}, nil }
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b := cacheTestBundle()
			b.config.Provider = fmt.Sprint(i % 2)
			got, err := cache.get(b, build)
			if err != nil || !reflect.DeepEqual(got, []modelPlanBundle{b}) {
				t.Error("concurrent pair changed result", err)
				return
			}
			got[0].agents["agent"][0] = 'X'
		}(i)
	}
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatal("concurrent pair rebuilt", calls.Load())
	}
	if cache.value == nil || cache.previousValue == nil || historicalCacheRetained(&cache) > historicalBundleCacheLimit {
		t.Fatal("concurrent pair residency invalid")
	}
}

func BenchmarkHistoricalBundleAlternating(b *testing.B) {
	current, err := buildModelPlanBundleV3(projectModelPlanToV3(modelplan.DefaultModelPlanConfig()))
	if err != nil {
		b.Fatal(err)
	}
	frozen, err := managerV60Bundle(current)
	if err != nil {
		b.Fatal(err)
	}
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprint(cached), func(b *testing.B) {
			var cache historicalBundleCache
			for b.Loop() {
				for _, input := range []modelPlanBundle{current, frozen} {
					var err error
					if cached {
						_, err = cache.get(input, supportedHistoricalModelPlanBundlesUncached)
					} else {
						_, err = supportedHistoricalModelPlanBundlesUncached(input)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

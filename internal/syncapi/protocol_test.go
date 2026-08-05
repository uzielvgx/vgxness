package syncapi

import (
	"encoding/json"
	"errors"
	"github.com/vgxness/vgxness/internal/syncservice"
	"math"
	"strings"
	"testing"
)

func TestRequestDecodingBoundsAndIdentityFields(t *testing.T) {
	for _, b := range [][]byte{[]byte(`{"protocol_version":1,"owner_id":"forged","items":[]}`), []byte(`{"protocol_version":1,"device_id":"forged","items":[]}`), []byte(`{"protocol_version":2,"items":[]}`), []byte(`{"protocol_version":1,"items":[]}`), []byte(`{"protocol_version":1,"items":[{},{},{},{},{},{},{},{},{},{},{},{},{},{},{},{},{}]}`), []byte(strings.Repeat("[", MaxJSONDepth+1) + strings.Repeat("]", MaxJSONDepth+1)), make([]byte, MaxBodyBytes+1)} {
		if _, e := DecodePushRequest(b); e == nil || strings.Contains(e.Error(), "forged") {
			t.Fatalf("unsafe request: %v", e)
		}
	}
}

func TestDecodeDiscoveryResponseIsStrict(t *testing.T) {
	valid := []byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","capabilities":["bootstrap_discovery"]}`)
	if _, err := DecodeDiscoveryResponse(valid); err != nil {
		t.Fatalf("valid discovery: %v", err)
	}
	for _, body := range [][]byte{
		[]byte(`{"protocol_version":1,"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","capabilities":["bootstrap_discovery"]}`),
		[]byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","capabilities":["bootstrap_discovery"],"extra":true}`),
		[]byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","capabilities":["bootstrap_discovery"]}{}`),
		[]byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","capabilities":["bootstrap_discovery","bootstrap_discovery"]}`),
		[]byte{0xff},
	} {
		if _, err := DecodeDiscoveryResponse(body); err == nil {
			t.Fatalf("accepted invalid discovery: %q", body)
		}
	}
}

func TestStrictResponseDecodersEnforceSharedBoundary(t *testing.T) {
	capabilities := []byte(`{"protocol_version":1,"capabilities":["bootstrap_discovery"]}`)
	if _, err := DecodeCapabilitiesResponse(capabilities); err != nil {
		t.Fatalf("valid capabilities: %v", err)
	}
	for _, body := range [][]byte{
		[]byte(`{"protocol_version":1,"protocol_version":1,"capabilities":["bootstrap_discovery"]}`),
		[]byte(`{"protocol_version":1,"capabilities":["bootstrap_discovery"],"extra":true}`),
		append(append([]byte{}, capabilities...), []byte(`{}`)...),
		[]byte(strings.Repeat("[", MaxJSONDepth+1) + strings.Repeat("]", MaxJSONDepth+1)),
		[]byte{0xff},
	} {
		if _, err := DecodeCapabilitiesResponse(body); err == nil {
			t.Fatalf("accepted invalid capabilities: %q", body)
		}
	}

	pull := []byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","position":0,"has_more":false}`)
	if _, err := DecodeStrictPullResponse(append(pull, []byte(strings.Repeat(" ", MaxBodyBytes))...)); err != nil {
		t.Fatalf("valid pull response under pull limit: %v", err)
	}
	for _, body := range [][]byte{
		[]byte(`{"protocol_version":1,"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","position":0,"has_more":false}`),
		[]byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","position":0,"has_more":false,"extra":true}`),
		append(append([]byte{}, pull...), []byte(`{}`)...),
		[]byte(strings.Repeat("[", MaxJSONDepth+1) + strings.Repeat("]", MaxJSONDepth+1)),
		[]byte{0xff},
		append(pull, []byte(strings.Repeat(" ", MaxPullResponseBytes))...),
	} {
		if _, err := DecodeStrictPullResponse(body); err == nil {
			t.Fatalf("accepted invalid strict pull response: %q", body)
		}
	}
}

func TestDecodeStrictPullResponseRequiresScalarFields(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"history_id":"123e4567-e89b-12d3-a456-426614174000","position":0,"has_more":false}`),
		[]byte(`{"protocol_version":1,"position":0,"has_more":false}`),
		[]byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","has_more":false}`),
		[]byte(`{"protocol_version":1,"history_id":"123e4567-e89b-12d3-a456-426614174000","position":0}`),
	} {
		if _, err := DecodeStrictPullResponse(body); err == nil {
			t.Fatalf("accepted pull response missing required scalar: %s", body)
		}
	}
}

func TestDecodeStrictPullResponseRejectsNestedDuplicateChangeFields(t *testing.T) {
	change := strings.Replace(pullProjectChange(1), `"sequence":1`, `"sequence":1,"sequence":1`, 1)
	body := []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + change + `]}`)
	if _, err := DecodeStrictPullResponse(body); err == nil {
		t.Fatalf("accepted nested duplicate change field: %s", body)
	}
}

func TestSafeErrorMappingAndResponseSize(t *testing.T) {
	for e, c := range map[error]ErrorCode{syncservice.ErrInvalidMutation: ErrorInvalidInput, syncservice.ErrLimitExceeded: ErrorLimitExceeded, syncservice.ErrUnsupportedSemantic: ErrorUnsupportedSemantic, ErrUnsupportedVersion: ErrorUnsupportedVersion} {
		if CodeFor(e) != c {
			t.Fatal("code")
		}
	}
	if !errors.Is(ErrUnsupportedVersion, ErrUnsupportedVersion) {
		t.Fatal("sentinel")
	}
	x := strings.Repeat("x", MaxBodyBytes+1)
	b := []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":0,"has_more":false,"future":"` + x + `"}`)
	if _, e := DecodePullResponse(b); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodePullResponse(append(b, make([]byte, MaxPullResponseBytes-len(b)+1)...)); CodeFor(e) != ErrorLimitExceeded {
		t.Fatal("response limit")
	}
}
func TestPullAndResponseProtocolFoundations(t *testing.T) {
	c := `{"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":0}`
	p, e := DecodePullRequest([]byte(`{"protocol_version":1,"cursor":` + c + `}`))
	if e != nil || p.Limit != DefaultPullLimit {
		t.Fatal("default")
	}
	for b, w := range map[string]ErrorCode{`{"protocol_version":2,"cursor":` + c + `}`: ErrorUnsupportedVersion, `{"protocol_version":1,"cursor":` + c + `,"limit":26}`: ErrorLimitExceeded, `{"protocol_version":1,"cursor":{"history_id":"bad","position":0}}`: ErrorCursor} {
		if _, e := DecodePullRequest([]byte(b)); CodeFor(e) != w {
			t.Fatal("pull class")
		}
	}
	r, e := DecodePullResponse([]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"has_more":false,"future":"ok"}`))
	if e != nil || r.Position != 1 {
		t.Fatal("additive")
	}
	if _, e := DecodePullResponse([]byte(`{"protocol_version":2,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"has_more":false}`)); CodeFor(e) != ErrorUnsupportedVersion {
		t.Fatal("response version")
	}
}
func TestProtocolConstantsAndSafeCodes(t *testing.T) {
	if ProtocolVersion != 1 || MediaType != "application/vnd.vgxness.sync+json;version=1" {
		t.Fatal("constants")
	}
	for _, c := range []ErrorCode{ErrorInvalidInput, ErrorLimitExceeded, ErrorUnsupportedVersion, ErrorUnsupportedSemantic, ErrorUnavailable, ErrorUnauthorized, ErrorRevoked, ErrorConflict, ErrorHistory, ErrorCursor} {
		if c.String() == "" || strings.Contains(c.String(), "content") {
			t.Fatal("code")
		}
	}
}
func TestPullResponseHardening(t *testing.T) {
	b := `{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"has_more":false,`
	for _, s := range []string{b + `"changes":[{"sequence":0,"mutation":{}}]}`, b + `"changes":[{"sequence":1,"mutation":{}},{"sequence":1,"mutation":{}}]}`, b + `"changes":[{"sequence":2,"mutation":{}}]}`} {
		if _, e := DecodePullResponse([]byte(s)); e == nil {
			t.Fatal("change")
		}
	}
	if _, e := DecodePullResponse(append([]byte(`{"protocol_version":1}`), 0xff)); e == nil {
		t.Fatal("response UTF-8")
	}
	if _, e := DecodePushRequest(append([]byte(`{"protocol_version":1,"items":[]}`), 0xff)); e == nil {
		t.Fatal("request UTF-8")
	}
	if CodeFor(syncservice.ErrInvalidCursor) != ErrorCursor {
		t.Fatal("cursor")
	}
}

func TestValidatePullResponseWatermarkAndBounds(t *testing.T) {
	legacy := []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"has_more":false}`)
	if response, err := DecodePullResponse(legacy); err != nil || response.Watermark != 0 {
		t.Fatalf("legacy response = %+v, %v", response, err)
	}
	for _, body := range [][]byte{
		[]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":0,"watermark":-1,"has_more":false}`),
		[]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":2,"watermark":1,"has_more":false}`),
		[]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":3,"watermark":3,"has_more":false,"changes":[` + pullProjectChange(1) + `,` + pullProjectChange(3) + `]}`),
		[]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + pullProjectChange(2) + `]}`),
		[]byte(strings.Replace(string(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[`+pullProjectChange(1)+`]}`), `"canonical_version":1`, `"canonical_version":0`, 1)),
		[]byte(strings.Replace(string(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[`+pullProjectChange(1)+`]}`), `"canonical_version":1`, `"canonical_version":-1`, 1)),
		[]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":true,"changes":[` + pullProjectChange(1) + `]}`),
		[]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":2,"has_more":false,"changes":[` + pullProjectChange(1) + `]}`),
	} {
		if _, err := DecodePullResponse(body); err == nil {
			t.Fatalf("accepted invalid watermark: %s", body)
		}
	}
	if _, err := DecodePullResponse([]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + pullProjectChange(1) + `]}`)); err != nil {
		t.Fatalf("valid canonical version: %v", err)
	}
	if _, err := DecodePullResponse([]byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":2,"has_more":true,"changes":[` + pullProjectChange(1) + `]}`)); err != nil {
		t.Fatalf("valid incomplete watermark: %v", err)
	}
}

func TestPullChangeHashContractAndDecoder(t *testing.T) {
	change := pullProjectChangeValue(1)
	hash, err := syncservice.CanonicalChangeHash(change)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "11d715b99da25ca73ef871a74b0543901f57937632ca7ea47eee5ec4157bac08" {
		t.Fatalf("canonical hash = %q", hash)
	}
	change.ChangeHash = hash
	if err := syncservice.VerifyChangeHash(change); err != nil {
		t.Fatal(err)
	}
	sequenceChanged := change
	sequenceChanged.Sequence++
	versionChanged := change
	versionChanged.CanonicalVersion++
	mutationChanged := change
	project := *change.Mutation.Project
	project.ID = "other"
	mutationChanged.Mutation.Project = &project
	for _, changed := range []syncservice.Change{sequenceChanged, versionChanged, mutationChanged} {
		if err := syncservice.VerifyChangeHash(changed); err == nil {
			t.Fatalf("accepted changed value: %+v", changed)
		}
	}
	encoded, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	response := func(changeJSON string) []byte {
		return []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + changeJSON + `]}`)
	}
	if _, err := DecodePullResponse(response(string(encoded))); err != nil {
		t.Fatalf("valid response: %v", err)
	}
	for _, changeJSON := range []string{
		strings.Replace(string(encoded), `,"change_hash":"`+hash+`"`, "", 1),
		strings.Replace(string(encoded), hash, strings.ToUpper(hash), 1),
		strings.Replace(string(encoded), hash, hash[:63], 1),
		strings.Replace(string(encoded), hash, strings.Repeat("g", 64), 1),
		strings.Replace(string(encoded), hash, strings.Repeat("0", 64), 1),
	} {
		if _, err := DecodePullResponse(response(changeJSON)); err == nil {
			t.Fatalf("accepted invalid hash: %s", changeJSON)
		}
	}
	unknown := strings.Replace(string(response(string(encoded))), `"changes"`, `"future_root":true,"changes"`, 1)
	if _, err := DecodePullResponse([]byte(unknown)); err != nil {
		t.Fatalf("unknown additive fields: %v", err)
	}
}

func TestDecodePullResponseRejectsInvalidSpecialHashEnvelope(t *testing.T) {
	change := pullProjectChange(1)
	invalid := strings.Replace(change, `,"change_hash"`, `,"hash_version":2,"change_disposition":"accepted","conflict_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","change_hash"`, 1)
	body := []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + invalid + `]}`)
	if _, err := DecodePullResponse(body); err == nil {
		t.Fatal("accepted malformed v2 accepted envelope")
	}
}

func TestDecodePullResponseRejectsExplicitZeroHashVersion(t *testing.T) {
	change := strings.TrimSuffix(pullProjectChange(1), "}") + `,"hash_version":0}`
	body := []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + change + `]}`)
	if _, err := DecodePullResponse(body); err == nil {
		t.Fatal("accepted explicit hash_version zero")
	}
}

func TestDecodePullResponseRejectsUnknownChangeFieldsAndDispositionAlias(t *testing.T) {
	for _, field := range []string{`"future_change":true`, `"disposition":"accepted"`} {
		change := strings.TrimSuffix(pullProjectChange(1), "}") + `,` + field + `}`
		body := []byte(`{"protocol_version":1,"history_id":"8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91","position":1,"watermark":1,"has_more":false,"changes":[` + change + `]}`)
		if _, err := DecodePullResponse(body); err == nil {
			t.Fatalf("accepted unknown change field %s", field)
		}
	}
}

func pullProjectChange(sequence int) string {
	change := pullProjectChangeValue(int64(sequence))
	hash, err := syncservice.CanonicalChangeHash(change)
	if err != nil {
		panic(err)
	}
	change.ChangeHash = hash
	encoded, err := json.Marshal(change)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func pullProjectChangeValue(sequence int64) syncservice.Change {
	return syncservice.Change{Sequence: sequence, CanonicalVersion: 1, Mutation: syncservice.Mutation{MutationID: "8aef6b18-a0ce-4b2f-b2b1-ef935ac0dd91", RecordID: "project", RecordKind: syncservice.RecordKindProject, Kind: syncservice.MutationCreate, Project: &syncservice.Project{ID: "project"}}}
}

func TestPushResponseIsRequestBoundOrderedAndTerminal(t *testing.T) {
	request := PushRequest{ProtocolVersion: 1, Items: []syncservice.Mutation{{MutationID: "a", BaseVersion: 7}, {MutationID: "b", BaseVersion: 12}, {MutationID: "c", BaseVersion: 4}}}
	valid := PushResponse{ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "a", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(9), Version: 8}, {MutationID: "b", Disposition: syncservice.DispositionPreviouslyAccepted, Sequence: resultSequence(3), Version: 13}, {MutationID: "c", Disposition: syncservice.DispositionRejected, Code: "unsupported_semantic"}}}
	if err := ValidatePushResponse(request, valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePushResponse(PushRequest{Items: []syncservice.Mutation{{MutationID: "overflow", BaseVersion: math.MaxInt64}}}, PushResponse{ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "overflow", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(1), Version: math.MinInt64}}}); err == nil {
		t.Fatal("accepted overflowed version")
	}
	for _, response := range []PushResponse{{ProtocolVersion: 1, Results: valid.Results[:2]}, {ProtocolVersion: 1, Results: []syncservice.Result{valid.Results[1], valid.Results[0], valid.Results[2]}}, {ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "other", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(1), Version: 1}}}} {
		if err := ValidatePushResponse(request, response); err == nil {
			t.Fatalf("accepted unmatched response: %+v", response)
		}
	}
	for _, result := range []syncservice.Result{{MutationID: "a", Disposition: syncservice.DispositionConflict, Sequence: resultSequence(1)}, {MutationID: "a", Disposition: syncservice.DispositionRejected, Code: "contains content"}, {MutationID: "a", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(2)}, {MutationID: "a", Disposition: syncservice.DispositionAccepted, Version: 1}} {
		if err := ValidatePushResponse(PushRequest{Items: []syncservice.Mutation{{MutationID: "a"}}}, PushResponse{ProtocolVersion: 1, Results: []syncservice.Result{result}}); err == nil {
			t.Fatalf("accepted invalid result: %+v", result)
		}
	}
	for _, response := range []PushResponse{
		{ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "a", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(9), Version: 7}, {MutationID: "b", Disposition: syncservice.DispositionPreviouslyAccepted, Sequence: resultSequence(3), Version: 13}, {MutationID: "c", Disposition: syncservice.DispositionRejected, Code: "unsupported_semantic"}}},
		{ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "a", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(9), Version: 8}, {MutationID: "b", Disposition: syncservice.DispositionPreviouslyAccepted, Sequence: resultSequence(9), Version: 13}, {MutationID: "c", Disposition: syncservice.DispositionRejected, Code: "unsupported_semantic"}}},
	} {
		if err := ValidatePushResponse(request, response); err == nil {
			t.Fatalf("accepted invalid version or sequence owner: %+v", response)
		}
	}
	duplicateRequest := PushRequest{ProtocolVersion: 1, Items: []syncservice.Mutation{{MutationID: "a", BaseVersion: 7}, {MutationID: "a", BaseVersion: 7}}}
	duplicateResponse := PushResponse{ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "a", Disposition: syncservice.DispositionAccepted, Sequence: resultSequence(9), Version: 8}, {MutationID: "a", Disposition: syncservice.DispositionPreviouslyAccepted, Sequence: resultSequence(9), Version: 8}}}
	if err := ValidatePushResponse(duplicateRequest, duplicateResponse); err != nil {
		t.Fatal(err)
	}
	conflict := PushResponse{ProtocolVersion: 1, Results: []syncservice.Result{{MutationID: "a", Disposition: syncservice.DispositionConflict, Sequence: resultSequence(1), Version: 9}}}
	if err := ValidatePushResponse(PushRequest{ProtocolVersion: 1, Items: []syncservice.Mutation{{MutationID: "a"}}}, conflict); err != nil {
		t.Fatalf("valid terminal conflict: %v", err)
	}
	for _, result := range []syncservice.Result{
		{MutationID: "a", Disposition: syncservice.DispositionConflict, Sequence: resultSequence(1)},
		{MutationID: "a", Disposition: syncservice.DispositionConflict, Version: 1},
		{MutationID: "a", Disposition: syncservice.DispositionConflict, Sequence: resultSequence(1), Version: 1, Code: "stale_base"},
		{MutationID: "a", Disposition: syncservice.DispositionConflict, Sequence: resultSequence(1), Version: 1, Retryable: true},
	} {
		if err := ValidatePushResponse(PushRequest{ProtocolVersion: 1, Items: []syncservice.Mutation{{MutationID: "a"}}}, PushResponse{ProtocolVersion: 1, Results: []syncservice.Result{result}}); err == nil {
			t.Fatalf("accepted malformed conflict: %+v", result)
		}
	}
}

func resultSequence(value int64) *int64 { return &value }

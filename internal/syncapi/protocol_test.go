package syncapi

import (
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
}

func resultSequence(value int64) *int64 { return &value }

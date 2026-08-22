package orchestration

import (
	"sort"
	"sync"
)

const maxObservationEntries = 1024

// ReadinessObservation intentionally contains no mission, path, source, identity, or credential fields.
type ReadinessObservation struct {
	Activation          ActivationClass
	Status              ReadinessStatus
	Reasons             []ReasonCode
	Risks               []RiskCategory
	InvalidationTrigger ReasonCode
	ElapsedBucket       string
	WriteLaunched       bool
}
type AggregateObserver struct {
	mu      sync.Mutex
	limit   int
	entries []ReadinessObservation
}

func NewAggregateObserver(limit int) *AggregateObserver {
	if limit < 1 {
		limit = 1
	}
	if limit > maxObservationEntries {
		limit = maxObservationEntries
	}
	return &AggregateObserver{limit: limit}
}
func (o *AggregateObserver) Observe(v ReadinessObservation) {
	if o == nil {
		return
	}
	if v.Activation != ActivationExempt && v.Activation != ActivationLight && v.Activation != ActivationFull {
		v.Activation = ActivationFull
	}
	if v.Status != ReadinessReady && v.Status != ReadinessInconclusive && v.Status != ReadinessBlocked {
		v.Status = ReadinessBlocked
	}
	if v.ElapsedBucket != "fast" && v.ElapsedBucket != "slow" {
		v.ElapsedBucket = "unknown"
	}
	v.Reasons = uniqueReasons(v.Reasons)
	v.Risks = uniqueRisks(v.Risks)
	if !validReason(v.InvalidationTrigger) {
		v.InvalidationTrigger = "binding_mismatch"
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.entries) == o.limit {
		o.entries = append(o.entries[:0], o.entries[1:]...)
	}
	o.entries = append(o.entries, v)
}
func uniqueReasons(in []ReasonCode) []ReasonCode {
	out := make([]ReasonCode, 0, len(in))
	for _, x := range in {
		if !validReason(x) {
			x = "binding_mismatch"
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return dedupe(out)
}
func validReason(v ReasonCode) bool {
	switch v {
	case "required_field_missing", "binding_mismatch", "target_hash_mismatch", "candidate_mismatch", "envelope_digest_mismatch", "status_forged", "evidence_unavailable", "risk_evidence_gap", "consequential_unknown":
		return true
	}
	return false
}
func uniqueRisks(in []RiskCategory) []RiskCategory {
	out := make([]RiskCategory, 0, len(in))
	for _, x := range in {
		if validRisk(x) {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	n := 0
	for _, x := range out {
		if n == 0 || out[n-1] != x {
			out[n] = x
			n++
		}
	}
	return out[:n]
}
func (o *AggregateObserver) Snapshot() []ReadinessObservation {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]ReadinessObservation, len(o.entries))
	for i, v := range o.entries {
		out[i] = v
		out[i].Reasons = append([]ReasonCode(nil), v.Reasons...)
		out[i].Risks = append([]RiskCategory(nil), v.Risks...)
	}
	return out
}

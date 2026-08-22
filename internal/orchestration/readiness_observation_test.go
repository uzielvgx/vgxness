package orchestration

import (
	"reflect"
	"sync"
	"testing"
)

func TestReadinessObservationSanitizesFullControlledVocabulary(t *testing.T) {
	o := NewAggregateObserver(2)
	risks := []RiskCategory{"sdd", "delivery", "frozen", "cross-platform", "lifecycle-recovery", "authorization-security", "secrets", "payments", "installer", "data-loss-exposure", "shell-process", "durability", "identity-digest", "provider-template", "unknown-risk"}
	o.Observe(ReadinessObservation{Activation: "bad", Status: "bad", Reasons: []ReasonCode{"z", "a", "a"}, Risks: append(risks, "secret"), ElapsedBucket: "unbounded"})
	s := o.Snapshot()
	if len(s) != 1 || s[0].Activation != ActivationFull || s[0].Status != ReadinessBlocked || s[0].ElapsedBucket != "unknown" || len(s[0].Risks) != len(risks) {
		t.Fatalf("sanitized observation = %+v", s)
	}
	s[0].Reasons[0], s[0].Risks[0] = "changed", "changed"
	if o.Snapshot()[0].Reasons[0] == "changed" || o.Snapshot()[0].Risks[0] == "changed" {
		t.Fatal("snapshot aliases observer state")
	}
}

func TestReadinessObservationHardLimitAndConcurrentSnapshots(t *testing.T) {
	o := NewAggregateObserver(3)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.Observe(ReadinessObservation{Activation: ActivationFull, Status: ReadinessBlocked, Reasons: []ReasonCode{"binding_mismatch"}, Risks: []RiskCategory{"sdd"}})
			_ = o.Snapshot()
		}()
	}
	wg.Wait()
	if got := len(o.Snapshot()); got != 3 {
		t.Fatalf("hard limit = %d, want 3", got)
	}
}

func TestReadinessObservationCapsExtremeRequestedLimit(t *testing.T) {
	o := NewAggregateObserver(int(^uint(0) >> 1))
	if o.limit > 1024 {
		t.Fatalf("observer limit = %d, want hard internal cap", o.limit)
	}
}

func TestReadinessObservationPreservesStatusForgedReason(t *testing.T) {
	in := completeBuildInput()
	e, got := BuildReadiness(in)
	if got.Status != ReadinessReady {
		t.Fatalf("valid envelope = %+v", got)
	}
	e.Status = ReadinessBlocked
	e.EnvelopeDigest = EnvelopeDigest(e)
	if got := ValidateReadiness(e, in.Binding, nil); got.Status != ReadinessBlocked || !reflect.DeepEqual(got.Reasons, []ReasonCode{"status_forged"}) {
		t.Fatalf("forged status = %+v, want controlled status_forged", got)
	}
	if !validReason("status_forged") {
		t.Fatal("status_forged must be a valid controlled reason")
	}

	o := NewAggregateObserver(1)
	o.Observe(ReadinessObservation{Activation: ActivationFull, Status: ReadinessBlocked, Reasons: []ReasonCode{"status_forged", "status_forged"}})
	if got := o.Snapshot()[0].Reasons; !reflect.DeepEqual(got, []ReasonCode{"status_forged"}) {
		t.Fatalf("observed reasons = %v, want deduplicated status_forged", got)
	}
}

func TestReadinessObservationSanitizesInvalidationTrigger(t *testing.T) {
	o := NewAggregateObserver(2)
	o.Observe(ReadinessObservation{InvalidationTrigger: "target_hash_mismatch"})
	o.Observe(ReadinessObservation{InvalidationTrigger: "untrusted-trigger"})

	got := o.Snapshot()
	if got[0].InvalidationTrigger != "target_hash_mismatch" {
		t.Fatalf("valid invalidation trigger = %q", got[0].InvalidationTrigger)
	}
	if got[1].InvalidationTrigger != "binding_mismatch" {
		t.Fatalf("invalid invalidation trigger = %q", got[1].InvalidationTrigger)
	}
}

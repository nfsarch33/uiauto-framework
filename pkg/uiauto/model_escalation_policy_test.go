package uiauto

import (
	"testing"
	"time"
)

func TestDefaultEscalationPolicy(t *testing.T) {
	p := DefaultEscalationPolicy()
	if len(p.TierOrder) != 3 {
		t.Errorf("TierOrder len = %d, want 3", len(p.TierOrder))
	}
	if p.TierOrder[0] != TierLight || p.TierOrder[1] != TierSmart || p.TierOrder[2] != TierVLM {
		t.Errorf("TierOrder = %v, want [Light, Smart, VLM]", p.TierOrder)
	}
	if p.CooldownAfterTrip != 60*time.Second {
		t.Errorf("CooldownAfterTrip = %v, want 60s", p.CooldownAfterTrip)
	}
	if p.MaxEscalations != 10 {
		t.Errorf("MaxEscalations = %d, want 10", p.MaxEscalations)
	}
}

func TestNextTier(t *testing.T) {
	p := DefaultEscalationPolicy()

	next, ok := p.NextTier(TierLight)
	if !ok || next != TierSmart {
		t.Errorf("NextTier(Light) = %v/%v, want Smart/true", next, ok)
	}

	next, ok = p.NextTier(TierSmart)
	if !ok || next != TierVLM {
		t.Errorf("NextTier(Smart) = %v/%v, want VLM/true", next, ok)
	}

	next, ok = p.NextTier(TierVLM)
	if ok {
		t.Errorf("NextTier(VLM) should return false, got %v/%v", next, ok)
	}
}

func TestPrevTier(t *testing.T) {
	p := DefaultEscalationPolicy()

	prev, ok := p.PrevTier(TierVLM)
	if !ok || prev != TierSmart {
		t.Errorf("PrevTier(VLM) = %v/%v, want Smart/true", prev, ok)
	}

	prev, ok = p.PrevTier(TierSmart)
	if !ok || prev != TierLight {
		t.Errorf("PrevTier(Smart) = %v/%v, want Light/true", prev, ok)
	}

	prev, ok = p.PrevTier(TierLight)
	if ok {
		t.Errorf("PrevTier(Light) should return false, got %v/%v", prev, ok)
	}
}

func TestShouldEscalate_ConsecutiveFailures(t *testing.T) {
	p := DefaultEscalationPolicy()

	if p.ShouldEscalate(TierLight, 2, 0.8, 10) {
		t.Error("should not escalate with 2 failures (threshold 3)")
	}
	if !p.ShouldEscalate(TierLight, 3, 0.8, 10) {
		t.Error("should escalate with 3 consecutive failures")
	}
}

func TestShouldEscalate_SuccessRate(t *testing.T) {
	p := DefaultEscalationPolicy()

	if p.ShouldEscalate(TierLight, 0, 0.6, 10) {
		t.Error("should not escalate with 60% success (threshold 50%)")
	}
	if !p.ShouldEscalate(TierLight, 0, 0.4, 10) {
		t.Error("should escalate with 40% success rate")
	}
	if p.ShouldEscalate(TierLight, 0, 0.4, 3) {
		t.Error("should not escalate with only 3 attempts (min 5)")
	}
}

func TestShouldEscalate_UnknownTier(t *testing.T) {
	p := DefaultEscalationPolicy()
	if p.ShouldEscalate(TierVLM, 100, 0, 100) {
		t.Error("VLM has no escalation condition")
	}
}

func TestShouldDemote(t *testing.T) {
	p := DefaultEscalationPolicy()

	if p.ShouldDemote(TierSmart, 2, 0.95) {
		t.Error("should not demote with only 2 attempts (min 3)")
	}
	if !p.ShouldDemote(TierSmart, 5, 0.95) {
		t.Error("should demote with 95% success and 5 attempts")
	}
	if p.ShouldDemote(TierSmart, 5, 0.7) {
		t.Error("should not demote with 70% success rate")
	}
	if p.ShouldDemote(TierLight, 10, 1.0) {
		t.Error("Light has no demotion condition (already lowest)")
	}
}

func TestCustomPolicy(t *testing.T) {
	p := ModelEscalationPolicy{
		TierOrder: []ModelTier{TierSmart, TierVLM},
		EscalateConditions: map[ModelTier]EscalationCondition{
			TierSmart: {ConsecutiveFailures: 1},
		},
		DemoteConditions: map[ModelTier]EscalationCondition{
			TierVLM: {MinAttempts: 1},
		},
	}

	next, ok := p.NextTier(TierSmart)
	if !ok || next != TierVLM {
		t.Errorf("NextTier(Smart) = %v/%v, want VLM/true", next, ok)
	}

	_, ok = p.PrevTier(TierSmart)
	if ok {
		t.Error("PrevTier(Smart) should return false in 2-tier policy")
	}

	if !p.ShouldEscalate(TierSmart, 1, 0, 0) {
		t.Error("should escalate with 1 failure (threshold 1)")
	}
}

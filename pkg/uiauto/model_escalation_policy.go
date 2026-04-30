package uiauto

import "time"

// EscalationCondition defines when a tier upgrade or downgrade should trigger.
type EscalationCondition struct {
	ConsecutiveFailures int     // failures at current tier before escalating
	SuccessRateBelow    float64 // if success rate drops below, escalate (0 = disabled)
	MinAttempts         int     // minimum attempts before rate-based decisions kick in
}

// ModelEscalationPolicy is a configurable policy that governs tier transitions.
type ModelEscalationPolicy struct {
	TierOrder          []ModelTier // ordered progression, e.g. [Light, Smart, VLM]
	EscalateConditions map[ModelTier]EscalationCondition
	DemoteConditions   map[ModelTier]EscalationCondition
	CooldownAfterTrip  time.Duration // how long to wait after a circuit-breaker trip
	MaxEscalations     int           // 0 = unlimited; cap escalation storms
}

// DefaultEscalationPolicy returns production defaults that match the existing
// hardcoded behavior but in a configurable struct.
func DefaultEscalationPolicy() ModelEscalationPolicy {
	return ModelEscalationPolicy{
		TierOrder: []ModelTier{TierLight, TierSmart, TierVLM},
		EscalateConditions: map[ModelTier]EscalationCondition{
			TierLight: {ConsecutiveFailures: 3, SuccessRateBelow: 0.5, MinAttempts: 5},
			TierSmart: {ConsecutiveFailures: 2, SuccessRateBelow: 0, MinAttempts: 0},
		},
		DemoteConditions: map[ModelTier]EscalationCondition{
			TierSmart: {ConsecutiveFailures: 0, SuccessRateBelow: 0, MinAttempts: 3},
			TierVLM:   {ConsecutiveFailures: 0, SuccessRateBelow: 0, MinAttempts: 3},
		},
		CooldownAfterTrip: 60 * time.Second,
		MaxEscalations:    10,
	}
}

// NextTier returns the next tier to escalate to, or false if already at max.
func (p *ModelEscalationPolicy) NextTier(current ModelTier) (ModelTier, bool) {
	for i, t := range p.TierOrder {
		if t == current && i+1 < len(p.TierOrder) {
			return p.TierOrder[i+1], true
		}
	}
	return current, false
}

// PrevTier returns the previous tier to demote to, or false if already at min.
func (p *ModelEscalationPolicy) PrevTier(current ModelTier) (ModelTier, bool) {
	for i, t := range p.TierOrder {
		if t == current && i > 0 {
			return p.TierOrder[i-1], true
		}
	}
	return current, false
}

// ShouldEscalate checks whether the given tier's escalation condition is met.
func (p *ModelEscalationPolicy) ShouldEscalate(tier ModelTier, consecutiveFailures int, successRate float64, totalAttempts int) bool {
	cond, ok := p.EscalateConditions[tier]
	if !ok {
		return false
	}
	if cond.ConsecutiveFailures > 0 && consecutiveFailures >= cond.ConsecutiveFailures {
		return true
	}
	if cond.SuccessRateBelow > 0 && totalAttempts >= cond.MinAttempts && successRate < cond.SuccessRateBelow {
		return true
	}
	return false
}

// ShouldDemote checks whether the given tier's demotion condition is met.
func (p *ModelEscalationPolicy) ShouldDemote(tier ModelTier, totalAttempts int, successRate float64) bool {
	cond, ok := p.DemoteConditions[tier]
	if !ok {
		return false
	}
	return totalAttempts >= cond.MinAttempts && successRate >= 0.9
}

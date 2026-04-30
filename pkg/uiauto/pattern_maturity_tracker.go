package uiauto

import (
	"context"
	"sync"
	"time"
)

// MaturityLevel represents how mature a pattern is.
type MaturityLevel int

// Pattern maturity levels from initial discovery through to trusted/degraded.
const (
	MaturityNew MaturityLevel = iota
	MaturityTesting
	MaturityStable
	MaturityTrusted
	MaturityDegraded
)

func (m MaturityLevel) String() string {
	switch m {
	case MaturityNew:
		return "new"
	case MaturityTesting:
		return "testing"
	case MaturityStable:
		return "stable"
	case MaturityTrusted:
		return "trusted"
	case MaturityDegraded:
		return "degraded"
	default:
		return "unknown"
	}
}

// MaturityConfig controls thresholds for maturity level transitions.
type MaturityConfig struct {
	TestingSuccesses int           // successes to move from New -> Testing
	StableSuccesses  int           // successes to move from Testing -> Stable
	TrustedSuccesses int           // successes to move from Stable -> Trusted
	DegradedFailures int           // consecutive failures to enter Degraded
	RecoveryWindow   time.Duration // if all recent uses within this window succeeded, allow recovery
}

// DefaultMaturityConfig returns production defaults.
func DefaultMaturityConfig() MaturityConfig {
	return MaturityConfig{
		TestingSuccesses: 2,
		StableSuccesses:  5,
		TrustedSuccesses: 15,
		DegradedFailures: 3,
		RecoveryWindow:   10 * time.Minute,
	}
}

// PatternMaturity holds maturity state for a single pattern.
type PatternMaturity struct {
	PatternID            string        `json:"pattern_id"`
	Level                MaturityLevel `json:"level"`
	TotalSuccesses       int           `json:"total_successes"`
	TotalFailures        int           `json:"total_failures"`
	ConsecutiveFailures  int           `json:"consecutive_failures"`
	ConsecutiveSuccesses int           `json:"consecutive_successes"`
	LastUsed             time.Time     `json:"last_used"`
	CreatedAt            time.Time     `json:"created_at"`
}

// SuccessRate returns the pattern's overall success rate.
func (pm *PatternMaturity) SuccessRate() float64 {
	total := pm.TotalSuccesses + pm.TotalFailures
	if total == 0 {
		return 0
	}
	return float64(pm.TotalSuccesses) / float64(total)
}

// PatternMaturityTracker monitors per-pattern maturity over time.
type PatternMaturityTracker struct {
	mu        sync.RWMutex
	patterns  map[string]*PatternMaturity
	config    MaturityConfig
	listeners []MaturityChangeListener
}

// MaturityChangeEvent is emitted on level transitions.
type MaturityChangeEvent struct {
	PatternID string
	From      MaturityLevel
	To        MaturityLevel
	Reason    string
	Timestamp time.Time
}

// MaturityChangeListener receives maturity change events.
type MaturityChangeListener func(event MaturityChangeEvent)

// NewPatternMaturityTracker creates a tracker with the given config.
func NewPatternMaturityTracker(cfg MaturityConfig) *PatternMaturityTracker {
	return &PatternMaturityTracker{
		patterns: make(map[string]*PatternMaturity),
		config:   cfg,
	}
}

// OnChange registers a listener for maturity transitions.
func (t *PatternMaturityTracker) OnChange(listener MaturityChangeListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, listener)
}

func (t *PatternMaturityTracker) emit(evt MaturityChangeEvent) {
	for _, l := range t.listeners {
		l(evt)
	}
}

func (t *PatternMaturityTracker) getOrCreate(patternID string) *PatternMaturity {
	pm, ok := t.patterns[patternID]
	if !ok {
		pm = &PatternMaturity{
			PatternID: patternID,
			Level:     MaturityNew,
			CreatedAt: time.Now(),
		}
		t.patterns[patternID] = pm
	}
	return pm
}

// RecordSuccess records a successful pattern use and may promote maturity.
func (t *PatternMaturityTracker) RecordSuccess(_ context.Context, patternID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	pm := t.getOrCreate(patternID)
	pm.TotalSuccesses++
	pm.ConsecutiveSuccesses++
	pm.ConsecutiveFailures = 0
	pm.LastUsed = time.Now()

	oldLevel := pm.Level
	t.maybePromote(pm)
	if pm.Level != oldLevel {
		t.emit(MaturityChangeEvent{
			PatternID: patternID,
			From:      oldLevel,
			To:        pm.Level,
			Reason:    "promotion_on_success",
			Timestamp: time.Now(),
		})
	}
}

// RecordFailure records a failed pattern use and may demote maturity.
func (t *PatternMaturityTracker) RecordFailure(_ context.Context, patternID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	pm := t.getOrCreate(patternID)
	pm.TotalFailures++
	pm.ConsecutiveFailures++
	pm.ConsecutiveSuccesses = 0
	pm.LastUsed = time.Now()

	oldLevel := pm.Level
	if pm.ConsecutiveFailures >= t.config.DegradedFailures && pm.Level != MaturityDegraded {
		pm.Level = MaturityDegraded
	}
	if pm.Level != oldLevel {
		t.emit(MaturityChangeEvent{
			PatternID: patternID,
			From:      oldLevel,
			To:        pm.Level,
			Reason:    "degradation_on_failures",
			Timestamp: time.Now(),
		})
	}
}

func (t *PatternMaturityTracker) maybePromote(pm *PatternMaturity) {
	switch pm.Level {
	case MaturityNew:
		if pm.TotalSuccesses >= t.config.TestingSuccesses {
			pm.Level = MaturityTesting
		}
	case MaturityTesting:
		if pm.TotalSuccesses >= t.config.StableSuccesses {
			pm.Level = MaturityStable
		}
	case MaturityStable:
		if pm.TotalSuccesses >= t.config.TrustedSuccesses {
			pm.Level = MaturityTrusted
		}
	case MaturityDegraded:
		if pm.ConsecutiveSuccesses >= t.config.TestingSuccesses {
			pm.Level = MaturityTesting
		}
	case MaturityTrusted:
		// already at max
	}
}

// GetMaturity returns the maturity state for a pattern.
func (t *PatternMaturityTracker) GetMaturity(_ context.Context, patternID string) (PatternMaturity, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	pm, ok := t.patterns[patternID]
	if !ok {
		return PatternMaturity{}, false
	}
	return *pm, true
}

// AllMaturities returns a copy of all pattern maturities.
func (t *PatternMaturityTracker) AllMaturities() map[string]PatternMaturity {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]PatternMaturity, len(t.patterns))
	for k, v := range t.patterns {
		out[k] = *v
	}
	return out
}

// SuggestTier recommends the model tier to use for a pattern based on its maturity.
func (t *PatternMaturityTracker) SuggestTier(_ context.Context, patternID string) ModelTier {
	t.mu.RLock()
	defer t.mu.RUnlock()

	pm, ok := t.patterns[patternID]
	if !ok {
		return TierSmart
	}

	switch pm.Level {
	case MaturityTrusted, MaturityStable:
		return TierLight
	case MaturityTesting:
		return TierSmart
	case MaturityDegraded:
		return TierVLM
	default:
		return TierSmart
	}
}

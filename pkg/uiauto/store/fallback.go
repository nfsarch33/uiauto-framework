package store

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the circuit breaker state.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // normal operation, using primary
	CircuitOpen                         // primary failed, using fallback
	CircuitHalfOpen                     // testing primary again
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig controls the breaker behavior.
type CircuitBreakerConfig struct {
	FailureThreshold int           // consecutive failures to open (default 3)
	HalfOpenTimeout  time.Duration // time before trying primary again (default 30s)
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		HalfOpenTimeout:  30 * time.Second,
	}
}

// FallbackStore wraps a primary and fallback PatternStore with a circuit breaker.
type FallbackStore struct {
	primary  PatternStore
	fallback PatternStore
	config   CircuitBreakerConfig
	logger   *slog.Logger

	mu              sync.RWMutex
	state           CircuitState
	failures        int
	lastFailureTime time.Time
}

// NewFallbackStore creates a resilient store with automatic failover.
func NewFallbackStore(primary, fallback PatternStore, cfg CircuitBreakerConfig) *FallbackStore {
	return &FallbackStore{
		primary:  primary,
		fallback: fallback,
		config:   cfg,
		state:    CircuitClosed,
		logger:   slog.Default(),
	}
}

// State returns the current circuit breaker state.
func (f *FallbackStore) State() CircuitState {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

// Get tries primary, falls back to secondary on circuit open.
func (f *FallbackStore) Get(ctx context.Context, id string) (Pattern, bool) {
	if f.shouldUsePrimary() {
		p, ok := f.primary.Get(ctx, id)
		if ok {
			f.recordSuccess()
			return p, true
		}
		// Not found is not a failure, just absent
		return p, false
	}
	return f.fallback.Get(ctx, id)
}

// Set writes to the active store.
func (f *FallbackStore) Set(ctx context.Context, p Pattern) error {
	if f.shouldUsePrimary() {
		err := f.primary.Set(ctx, p)
		if err != nil {
			f.recordFailure()
			f.logger.Warn("primary set failed, using fallback",
				slog.String("pattern", p.ID),
				slog.String("error", err.Error()),
			)
			return f.fallback.Set(ctx, p)
		}
		f.recordSuccess()
		// Write-through to fallback for resilience
		f.fallback.Set(ctx, p)
		return nil
	}
	return f.fallback.Set(ctx, p)
}

// Load returns patterns from the active store.
func (f *FallbackStore) Load(ctx context.Context) (map[string]Pattern, error) {
	if f.shouldUsePrimary() {
		result, err := f.primary.Load(ctx)
		if err != nil {
			f.recordFailure()
			f.logger.Warn("primary load failed, using fallback",
				slog.String("error", err.Error()),
			)
			return f.fallback.Load(ctx)
		}
		f.recordSuccess()
		return result, nil
	}
	return f.fallback.Load(ctx)
}

// DecayConfidence applies decay on the active store.
func (f *FallbackStore) DecayConfidence(ctx context.Context, olderThan time.Duration, factor float64) error {
	if f.shouldUsePrimary() {
		err := f.primary.DecayConfidence(ctx, olderThan, factor)
		if err != nil {
			f.recordFailure()
			return f.fallback.DecayConfidence(ctx, olderThan, factor)
		}
		f.recordSuccess()
		return nil
	}
	return f.fallback.DecayConfidence(ctx, olderThan, factor)
}

// BoostConfidence boosts on the active store.
func (f *FallbackStore) BoostConfidence(ctx context.Context, id string, boost float64) error {
	if f.shouldUsePrimary() {
		err := f.primary.BoostConfidence(ctx, id, boost)
		if err != nil {
			f.recordFailure()
			return f.fallback.BoostConfidence(ctx, id, boost)
		}
		f.recordSuccess()
		return nil
	}
	return f.fallback.BoostConfidence(ctx, id, boost)
}

func (f *FallbackStore) shouldUsePrimary() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch f.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(f.lastFailureTime) > f.config.HalfOpenTimeout {
			f.state = CircuitHalfOpen
			f.logger.Info("circuit breaker half-open, testing primary")
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return true
}

func (f *FallbackStore) recordFailure() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failures++
	f.lastFailureTime = time.Now()

	if f.failures >= f.config.FailureThreshold {
		f.state = CircuitOpen
		f.logger.Warn("circuit breaker opened",
			slog.Int("failures", f.failures),
			slog.Duration("half_open_timeout", f.config.HalfOpenTimeout),
		)
	}
}

func (f *FallbackStore) recordSuccess() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.state == CircuitHalfOpen {
		f.logger.Info("circuit breaker closed, primary recovered")
	}
	f.state = CircuitClosed
	f.failures = 0
}

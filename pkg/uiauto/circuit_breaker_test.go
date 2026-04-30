package uiauto

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_StartsInClosedState(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig())
	assert.Equal(t, CircuitClosed, cb.State())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     1 * time.Minute,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitClosed, cb.State(), "should still be closed after 2 failures")
	assert.True(t, cb.Allow())

	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State(), "should open after 3 consecutive failures")
	assert.False(t, cb.Allow(), "should block requests when open")
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     1 * time.Minute,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordFailure()

	assert.Equal(t, CircuitClosed, cb.State(),
		"success between failures should reset consecutive count")
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterCooldown(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenDuration:     50 * time.Millisecond,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	time.Sleep(60 * time.Millisecond)

	assert.Equal(t, CircuitHalfOpen, cb.State())
	assert.True(t, cb.Allow(), "half-open should allow probe requests")
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	cb.Allow() // transitions to half-open

	cb.RecordSuccess()
	assert.Equal(t, CircuitHalfOpen, cb.State(), "one success not enough yet")

	cb.RecordSuccess()
	assert.Equal(t, CircuitClosed, cb.State(), "two successes should close the circuit")
}

func TestCircuitBreaker_FailureInHalfOpenReOpens(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)

	cb.Allow() // transitions to half-open

	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State(), "failure in half-open should re-open")
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenDuration:     1 * time.Minute,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.State())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker("smart_llm", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     1 * time.Minute,
	})

	cb.RecordSuccess()
	cb.RecordSuccess()
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	stats := cb.Stats()
	assert.Equal(t, "smart_llm", stats.Name)
	assert.Equal(t, CircuitOpen, stats.State)
	assert.Equal(t, "open", stats.StateStr)
	assert.Equal(t, 3, stats.ConsecutiveFailures)
	assert.Equal(t, 2, stats.TotalSuccesses)
	assert.Equal(t, 3, stats.TotalFailures)
	assert.Equal(t, 1, stats.TotalTrips)
	assert.False(t, stats.LastFailure.IsZero())
	// New stats fields
	assert.Equal(t, 3, stats.FailureCount)
	assert.Equal(t, 2, stats.SuccessCount)
	assert.Equal(t, 5, stats.TotalRequests)
	assert.InDelta(t, 0.6, stats.FailureRate, 0.01)
	assert.False(t, stats.LastStateChange.IsZero())
}

func TestCircuitBreaker_DefaultConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	assert.Equal(t, 3, cfg.FailureThreshold)
	assert.Equal(t, 2, cfg.SuccessThreshold)
	assert.Equal(t, 30*time.Second, cfg.OpenDuration)
	assert.Equal(t, 1, cfg.HalfOpenMax)
}

func TestCircuitBreaker_InvalidConfigDefaults(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{})
	require.NotNil(t, cb)

	// Should use defaults for zero values
	assert.Equal(t, CircuitClosed, cb.State())
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State(), "default threshold of 3 should apply")
}

func TestCircuitState_String(t *testing.T) {
	assert.Equal(t, "closed", CircuitClosed.String())
	assert.Equal(t, "open", CircuitOpen.String())
	assert.Equal(t, "half_open", CircuitHalfOpen.String())
	assert.Equal(t, "unknown", CircuitState(99).String())
}

func TestCircuitBreaker_MultipleTrips(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	})

	// First trip
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())
	time.Sleep(15 * time.Millisecond)

	// Probe succeeds, close
	cb.Allow()
	cb.RecordSuccess()
	assert.Equal(t, CircuitClosed, cb.State())

	// Second trip
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	stats := cb.Stats()
	assert.Equal(t, 2, stats.TotalTrips)
}

func TestCircuitBreaker_AdaptiveThreshold_HighFailureRateTripsEarlier(t *testing.T) {
	// With WindowSize=10 and base threshold 3, high failure rate (>50%) lowers effective threshold to 2.
	cb := NewCircuitBreaker("adaptive", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     1 * time.Minute,
		WindowSize:       10,
	})
	// Fill window with mostly failures (8/10 = 80%)
	for i := 0; i < 8; i++ {
		cb.RecordFailure()
	}
	for i := 0; i < 2; i++ {
		cb.RecordSuccess()
	}
	// Reset consecutive count
	cb.RecordSuccess()
	// Now 2 consecutive failures should trip (effective threshold = 2)
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State(), "high failure rate should lower threshold to 2")
}

func TestCircuitBreaker_AdaptiveThreshold_LowFailureRateMoreLenient(t *testing.T) {
	// With WindowSize=50 and base threshold 3, low failure rate (<10%) raises effective threshold to 4.
	// Use a larger window so that after 2-3 failures the rate stays below 10%.
	cb := NewCircuitBreaker("adaptive", CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     1 * time.Minute,
		WindowSize:       50,
	})
	// Fill window with all successes (0% failure rate)
	for i := 0; i < 50; i++ {
		cb.RecordSuccess()
	}
	// Reset consecutive count
	cb.RecordSuccess()
	// 3 consecutive failures should NOT trip (effective threshold = 4; window stays <10% failure)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitClosed, cb.State(), "low failure rate should raise threshold to 4")
	// 4th failure should trip
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())
}

func TestCircuitBreaker_StatsCorrectness(t *testing.T) {
	cb := NewCircuitBreaker("stats_test", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	})

	stats := cb.Stats()
	assert.Equal(t, "closed", stats.StateStr)
	assert.Equal(t, 0, stats.TotalRequests)
	assert.Equal(t, 0.0, stats.FailureRate)
	assert.Equal(t, 0, stats.ConsecutiveSuccesses)
	assert.Equal(t, 0, stats.ConsecutiveFailures)

	cb.RecordSuccess()
	cb.RecordSuccess()
	stats = cb.Stats()
	assert.Equal(t, 2, stats.SuccessCount)
	assert.Equal(t, 2, stats.TotalRequests)
	assert.Equal(t, 0.0, stats.FailureRate)
	assert.Equal(t, 2, stats.ConsecutiveSuccesses)

	cb.RecordFailure()
	stats = cb.Stats()
	assert.Equal(t, 2, stats.SuccessCount)
	assert.Equal(t, 1, stats.FailureCount)
	assert.Equal(t, 3, stats.TotalRequests)
	assert.InDelta(t, 1.0/3.0, stats.FailureRate, 0.001)
	assert.Equal(t, 1, stats.ConsecutiveFailures)
}

func TestCircuitBreaker_StateTransitionsTracking(t *testing.T) {
	reg := prometheus.NewRegistry()
	cb := NewCircuitBreaker("transitions", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
		Registerer:       reg,
	})

	// closed -> open
	cb.RecordFailure()
	cb.RecordFailure()
	stats := cb.Stats()
	assert.Equal(t, CircuitOpen, stats.State)
	assert.False(t, stats.LastStateChange.IsZero())

	// open -> half_open (via Allow after cooldown)
	time.Sleep(15 * time.Millisecond)
	cb.Allow()
	stats = cb.Stats()
	assert.Equal(t, CircuitHalfOpen, stats.State)

	// half_open -> closed (2 successes)
	cb.RecordSuccess()
	cb.RecordSuccess()
	stats = cb.Stats()
	assert.Equal(t, CircuitClosed, stats.State)

	// Verify Prometheus transitions counter
	metrics, _ := reg.Gather()
	var transitionsFound bool
	for _, mf := range metrics {
		if mf.GetName() == "uiauto_circuit_breaker_transitions_total" {
			transitionsFound = true
			assert.GreaterOrEqual(t, len(mf.GetMetric()), 1)
			break
		}
	}
	assert.True(t, transitionsFound, "transitions counter should be registered")
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := NewCircuitBreaker("recovery", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 3,
		OpenDuration:     10 * time.Millisecond,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	time.Sleep(15 * time.Millisecond)
	assert.True(t, cb.Allow(), "should allow probe in half-open")
	assert.Equal(t, CircuitHalfOpen, cb.State())

	// One success not enough
	cb.RecordSuccess()
	assert.Equal(t, CircuitHalfOpen, cb.State())
	// Two more successes close the circuit
	cb.RecordSuccess()
	cb.RecordSuccess()
	assert.Equal(t, CircuitClosed, cb.State())

	stats := cb.Stats()
	assert.Equal(t, 3, stats.ConsecutiveSuccesses)
	assert.Equal(t, 0, stats.ConsecutiveFailures)
}

func TestCircuitBreaker_HalfOpenFailureReOpens(t *testing.T) {
	cb := NewCircuitBreaker("reopen", CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	time.Sleep(15 * time.Millisecond)
	cb.Allow()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())
	// Should block again (lastFailure just set, cooldown not elapsed)
	assert.False(t, cb.Allow())
	time.Sleep(15 * time.Millisecond)
	// After cooldown, Allow transitions to half-open
	assert.True(t, cb.Allow())
}

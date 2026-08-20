package domheal

import (
	"sync"
	"time"
)

// CircuitBreaker tracks consecutive failures and opens when threshold is reached.
// After the cooldown has elapsed, it transitions to half-open and allows a single probe.
type CircuitBreaker struct {
	mu            sync.Mutex
	failures      int
	threshold     int
	cooldown      time.Duration
	state         string // "closed", "open", "half-open"
	lastFailureAt time.Time
	now           func() time.Time // clock source; nil means time.Now (injectable in tests)
}

// NewCircuitBreaker creates a circuit breaker with threshold consecutive failures
// and cooldown seconds before half-open.
func NewCircuitBreaker(threshold, cooldownSec int) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		cooldown:  time.Duration(cooldownSec) * time.Second,
		state:     "closed",
	}
}

// clock returns the current time from the injected clock, defaulting to time.Now.
// time.Now values carry a monotonic reading, so cooldown measurement is immune
// to wall-clock steps such as NTP corrections.
func (cb *CircuitBreaker) clock() time.Time {
	if cb.now != nil {
		return cb.now()
	}
	return time.Now()
}

// Allow returns true if the circuit breaker permits the request.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "closed":
		return true
	case "half-open":
		return true
	case "open":
		if cb.clock().Sub(cb.lastFailureAt) >= cb.cooldown {
			cb.state = "half-open"
			return true
		}
		return false
	}
	return true
}

// RecordSuccess resets the circuit breaker.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = "closed"
}

// RecordFailure increments failure count and opens the circuit if threshold is reached.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailureAt = cb.clock()
	if cb.failures >= cb.threshold {
		cb.state = "open"
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Failures returns the current consecutive failure count.
func (cb *CircuitBreaker) Failures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

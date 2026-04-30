package domheal

import (
	"sync"
	"time"
)

// CircuitBreaker tracks consecutive failures and opens when threshold is reached.
// After cooldown seconds, it transitions to half-open and allows a single probe.
type CircuitBreaker struct {
	mu            sync.Mutex
	failures      int
	threshold     int
	cooldownSec   int
	state         string // "closed", "open", "half-open"
	lastFailureAt int64
}

// NewCircuitBreaker creates a circuit breaker with threshold consecutive failures
// and cooldown seconds before half-open.
func NewCircuitBreaker(threshold, cooldownSec int) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:   threshold,
		cooldownSec: cooldownSec,
		state:       "closed",
	}
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
		elapsed := time.Now().Unix() - cb.lastFailureAt
		if elapsed >= int64(cb.cooldownSec) {
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
	cb.lastFailureAt = time.Now().Unix()
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

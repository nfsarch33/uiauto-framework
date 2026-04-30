package uiauto

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	cbStateGauge     *prometheus.GaugeVec
	cbTransitionsCtr *prometheus.CounterVec
	cbMetricsOnce    sync.Once
)

func ensureCircuitBreakerMetrics(reg prometheus.Registerer) {
	if reg == nil {
		return
	}
	cbMetricsOnce.Do(func() {
		cbStateGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Name:      "circuit_breaker_state",
			Help:      "Circuit breaker state: 0=closed, 1=half_open, 2=open",
		}, []string{"name"})
		cbTransitionsCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Name:      "circuit_breaker_transitions_total",
			Help:      "Total circuit breaker state transitions",
		}, []string{"name", "from_state", "to_state"})
		reg.MustRegister(cbStateGauge, cbTransitionsCtr)
	})
}

// CircuitState represents the state of a circuit breaker.
type CircuitState int

// Circuit breaker states.
const (
	CircuitClosed   CircuitState = iota // healthy, requests flow through
	CircuitOpen                         // unhealthy, requests are rejected
	CircuitHalfOpen                     // probing, one request allowed through
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig defines thresholds and timing for the circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           // consecutive failures to trip open
	SuccessThreshold int           // consecutive successes in half-open to close
	OpenDuration     time.Duration // how long to stay open before probing
	HalfOpenMax      int           // max concurrent requests in half-open
	// Adaptive tuning: sliding window size for success/failure rate.
	// If > 0, effective failure threshold is adjusted based on recent failure rate.
	WindowSize int
	// Registerer for Prometheus metrics (optional). If nil, no metrics are registered.
	Registerer prometheus.Registerer
}

// DefaultCircuitBreakerConfig returns production-safe defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     30 * time.Second,
		HalfOpenMax:      1,
	}
}

// CircuitBreaker implements the circuit breaker pattern for protecting
// expensive or unreliable operations (Smart LLM, VLM calls).
type CircuitBreaker struct {
	mu sync.RWMutex

	name   string
	config CircuitBreakerConfig
	state  CircuitState

	consecutiveFailures  int
	consecutiveSuccesses int
	lastFailure          time.Time
	totalTrips           int
	totalSuccesses       int
	totalFailures        int

	// Adaptive threshold: sliding window of recent outcomes (true=success, false=failure).
	recentOutcomes []bool
	outcomeIdx     int
	outcomesPushed int // number of outcomes recorded (capped at len(recentOutcomes))

	// State transition tracking.
	lastStateChange time.Time

	// Prometheus: when set, metrics are registered and updated.
	registerer prometheus.Registerer
}

// NewCircuitBreaker creates a new circuit breaker in the closed state.
func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = 1
	}
	cb := &CircuitBreaker{
		name:            name,
		config:          cfg,
		state:           CircuitClosed,
		lastStateChange: time.Now(),
		registerer:      cfg.Registerer,
	}
	if cfg.WindowSize > 0 {
		cb.recentOutcomes = make([]bool, cfg.WindowSize)
	}
	if cfg.Registerer != nil {
		ensureCircuitBreakerMetrics(cfg.Registerer)
		cb.updateStateGauge()
	}
	return cb
}

// stateGaugeValue returns the numeric value for Prometheus (0=closed, 1=half_open, 2=open).
func (cb *CircuitBreaker) stateGaugeValue() float64 {
	switch cb.state {
	case CircuitClosed:
		return 0
	case CircuitHalfOpen:
		return 1
	case CircuitOpen:
		return 2
	default:
		return -1
	}
}

func (cb *CircuitBreaker) updateStateGauge() {
	if cb.registerer != nil && cbStateGauge != nil {
		cbStateGauge.WithLabelValues(cb.name).Set(cb.stateGaugeValue())
	}
}

func (cb *CircuitBreaker) recordTransition(from, to CircuitState) {
	cb.lastStateChange = time.Now()
	if cb.registerer != nil && cbTransitionsCtr != nil {
		cbTransitionsCtr.WithLabelValues(cb.name, from.String(), to.String()).Inc()
	}
}

// windowFailureRate returns the failure rate over the sliding window (0..1).
// Returns 0 if window is empty or not configured.
func (cb *CircuitBreaker) windowFailureRate() float64 {
	if len(cb.recentOutcomes) == 0 || cb.outcomesPushed == 0 {
		return 0
	}
	n := cb.outcomesPushed
	if n > len(cb.recentOutcomes) {
		n = len(cb.recentOutcomes)
	}
	var failures int
	for i := 0; i < n; i++ {
		if !cb.recentOutcomes[i] {
			failures++
		}
	}
	return float64(failures) / float64(n)
}

// effectiveFailureThreshold returns the threshold to use, possibly adjusted by recent performance.
func (cb *CircuitBreaker) effectiveFailureThreshold() int {
	base := cb.config.FailureThreshold
	if len(cb.recentOutcomes) == 0 {
		return base
	}
	rate := cb.windowFailureRate()
	// High failure rate: trip earlier (lower threshold).
	if rate > 0.5 {
		if base > 1 {
			return base - 1
		}
		return 1
	}
	// Low failure rate: be more lenient (higher threshold, cap at 5).
	if rate < 0.1 && base < 5 {
		return base + 1
	}
	return base
}

func (cb *CircuitBreaker) pushOutcome(success bool) {
	if len(cb.recentOutcomes) == 0 {
		return
	}
	cb.recentOutcomes[cb.outcomeIdx] = success
	cb.outcomeIdx = (cb.outcomeIdx + 1) % len(cb.recentOutcomes)
	if cb.outcomesPushed < len(cb.recentOutcomes) {
		cb.outcomesPushed++
	}
}

// Allow checks if a request should be allowed through.
// Returns true if the request can proceed, false if the circuit is open.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailure) >= cb.config.OpenDuration {
			from := cb.state
			cb.state = CircuitHalfOpen
			cb.consecutiveSuccesses = 0
			cb.recordTransition(from, CircuitHalfOpen)
			cb.updateStateGauge()
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful operation.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses++
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses++
	cb.pushOutcome(true)

	if cb.state == CircuitHalfOpen {
		if cb.consecutiveSuccesses >= cb.config.SuccessThreshold {
			from := cb.state
			cb.state = CircuitClosed
			cb.recordTransition(from, CircuitClosed)
			cb.updateStateGauge()
		}
	}
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Compute effective threshold before pushing this failure (so window reflects prior state).
	effThreshold := cb.effectiveFailureThreshold()

	cb.totalFailures++
	cb.consecutiveFailures++
	cb.consecutiveSuccesses = 0
	cb.lastFailure = time.Now()
	cb.pushOutcome(false)
	if cb.state == CircuitClosed && cb.consecutiveFailures >= effThreshold {
		from := cb.state
		cb.state = CircuitOpen
		cb.totalTrips++
		cb.recordTransition(from, CircuitOpen)
		cb.updateStateGauge()
	} else if cb.state == CircuitHalfOpen {
		from := cb.state
		cb.state = CircuitOpen
		cb.totalTrips++
		cb.recordTransition(from, CircuitOpen)
		cb.updateStateGauge()
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Check if open circuit should transition to half-open
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.config.OpenDuration {
		return CircuitHalfOpen
	}
	return cb.state
}

// Reset forces the circuit breaker back to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != CircuitClosed {
		from := cb.state
		cb.state = CircuitClosed
		cb.recordTransition(from, CircuitClosed)
		cb.updateStateGauge()
	}
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
}

// CircuitBreakerStats holds observable circuit breaker metrics.
type CircuitBreakerStats struct {
	Name                 string
	State                CircuitState
	StateStr             string    // string form of state (closed, half_open, open)
	FailureCount         int       // alias for TotalFailures
	SuccessCount         int       // alias for TotalSuccesses
	TotalRequests        int       // TotalSuccesses + TotalFailures
	FailureRate          float64   // TotalFailures / TotalRequests, or 0 if no requests
	LastStateChange      time.Time // when state last changed
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	TotalTrips           int
	TotalSuccesses       int
	TotalFailures        int
	LastFailure          time.Time
}

// Stats returns a snapshot of circuit breaker metrics.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	total := cb.totalSuccesses + cb.totalFailures
	var failureRate float64
	if total > 0 {
		failureRate = float64(cb.totalFailures) / float64(total)
	}

	return CircuitBreakerStats{
		Name:                 cb.name,
		State:                cb.state,
		StateStr:             cb.state.String(),
		FailureCount:         cb.totalFailures,
		SuccessCount:         cb.totalSuccesses,
		TotalRequests:        total,
		FailureRate:          failureRate,
		LastStateChange:      cb.lastStateChange,
		ConsecutiveFailures:  cb.consecutiveFailures,
		ConsecutiveSuccesses: cb.consecutiveSuccesses,
		TotalTrips:           cb.totalTrips,
		TotalSuccesses:       cb.totalSuccesses,
		TotalFailures:        cb.totalFailures,
		LastFailure:          cb.lastFailure,
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = fmt.Errorf("circuit breaker is open")

package domheal

import (
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 1) // 3 failures, 1 second cooldown

	// Deterministic clock so the test never depends on wall-clock behaviour:
	// the sleep-based version flaked under full-suite load (scheduler stalls
	// and NTP steps between RecordFailure and Allow). The base sits 999 ms
	// into a wall second: Unix()-second cooldown arithmetic truncates that
	// offset and would report the 1 s cooldown elapsed only 1 ms after a
	// failure recorded here.
	fakeNow := time.Unix(1_000_000, int64(999*time.Millisecond))
	cb.now = func() time.Time { return fakeNow }
	advance := func(d time.Duration) { fakeNow = fakeNow.Add(d) }

	// Should allow initially
	if !cb.Allow() {
		t.Error("expected Allow() to be true initially")
	}

	// Record 3 failures -> open
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Errorf("expected state open, got %s", cb.State())
	}
	if cb.Failures() != 3 {
		t.Errorf("expected 3 failures, got %d", cb.Failures())
	}

	// Should not allow right after the failures
	if cb.Allow() {
		t.Error("expected Allow() to be false after 3 failures")
	}

	// Still blocked 1 ms later, across the wall-second boundary
	advance(1 * time.Millisecond)
	if cb.Allow() {
		t.Error("expected Allow() to be false 1ms after 3 failures")
	}

	// Still blocked just before the cooldown elapses
	advance(998 * time.Millisecond) // 999 ms since last failure
	if cb.Allow() {
		t.Error("expected Allow() to be false 999ms after 3 failures")
	}

	// Exactly at the cooldown, the breaker half-opens and allows a probe
	advance(1 * time.Millisecond) // 1 s since last failure
	if !cb.Allow() {
		t.Error("expected Allow() to be true once cooldown elapsed")
	}
	if cb.State() != "half-open" {
		t.Errorf("expected state half-open, got %s", cb.State())
	}

	// Failure in half-open should immediately reopen
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Errorf("expected state open after failure in half-open, got %s", cb.State())
	}
	if cb.Allow() {
		t.Error("expected Allow() to be false after reopening")
	}

	// Success in half-open should close and reset
	advance(1 * time.Second)
	if !cb.Allow() { // transition to half-open
		t.Error("expected Allow() to be true after second cooldown")
	}
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Errorf("expected state closed after success in half-open, got %s", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("expected 0 failures, got %d", cb.Failures())
	}

	// A lone failure followed by success keeps the breaker closed
	cb.RecordFailure()
	cb.RecordSuccess()
	if !cb.Allow() {
		t.Error("expected Allow() to be true after RecordSuccess")
	}
	if cb.State() != "closed" {
		t.Errorf("expected state closed, got %s", cb.State())
	}

	// Unknown state falls back to allowing
	cb.mu.Lock()
	cb.state = "invalid"
	cb.mu.Unlock()
	if !cb.Allow() {
		t.Error("expected Allow() to be true for invalid state (fallback)")
	}
}

// TestCircuitBreaker_RealClockDefault exercises the default time source
// (no injected clock). The cooldown is far longer than any plausible test
// runtime, so the assertions hold under arbitrary scheduler delay without
// sleeping.
func TestCircuitBreaker_RealClockDefault(t *testing.T) {
	cb := NewCircuitBreaker(1, 3600)
	if !cb.Allow() {
		t.Error("expected Allow() to be true initially")
	}
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("expected state open, got %s", cb.State())
	}
	if cb.Allow() {
		t.Error("expected Allow() to be false while open with 1h cooldown")
	}
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Fatalf("expected state closed after success, got %s", cb.State())
	}
}

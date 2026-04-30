package domheal

import (
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 1) // 3 failures, 1 second reset

	// Should allow initially
	if !cb.Allow() {
		t.Error("expected Allow() to be true initially")
	}

	// Record 3 failures
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	// Should not allow after 3 failures
	if cb.Allow() {
		t.Error("expected Allow() to be false after 3 failures")
	}

	// Wait for reset
	time.Sleep(1100 * time.Millisecond)

	// Should allow after reset
	if !cb.Allow() {
		t.Error("expected Allow() to be true after reset timeout")
	}

	// Record success should reset failures
	cb.RecordFailure()
	cb.RecordSuccess()
	if !cb.Allow() {
		t.Error("expected Allow() to be true after RecordSuccess")
	}

	if cb.State() != "closed" {
		t.Errorf("expected state closed, got %s", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("expected 0 failures, got %d", cb.Failures())
	}

	// Test open state
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Errorf("expected state open, got %s", cb.State())
	}
	if cb.Failures() != 3 {
		t.Errorf("expected 3 failures, got %d", cb.Failures())
	}
	if cb.Allow() {
		t.Error("expected Allow() to be false when open")
	}

	// Wait for reset
	time.Sleep(1100 * time.Millisecond)

	// Should be half-open on next Allow
	if !cb.Allow() {
		t.Error("expected Allow() to be true after reset timeout")
	}
	if cb.State() != "half-open" {
		t.Errorf("expected state half-open, got %s", cb.State())
	}

	// Failure in half-open should immediately open
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Errorf("expected state open after failure in half-open, got %s", cb.State())
	}

	// Success in half-open should close
	time.Sleep(1100 * time.Millisecond)
	cb.Allow() // transition to half-open
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Errorf("expected state closed after success in half-open, got %s", cb.State())
	}

	// Test invalid state
	cb.mu.Lock()
	cb.state = "invalid"
	cb.mu.Unlock()
	if !cb.Allow() {
		t.Error("expected Allow() to be true for invalid state (fallback)")
	}
}

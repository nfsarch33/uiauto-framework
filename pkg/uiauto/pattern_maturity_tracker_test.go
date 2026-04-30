package uiauto

import (
	"context"
	"testing"
)

func TestMaturityLevel_String(t *testing.T) {
	tests := []struct {
		level MaturityLevel
		want  string
	}{
		{MaturityNew, "new"},
		{MaturityTesting, "testing"},
		{MaturityStable, "stable"},
		{MaturityTrusted, "trusted"},
		{MaturityDegraded, "degraded"},
		{MaturityLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("MaturityLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestPatternMaturityTracker_PromotionPath(t *testing.T) {
	cfg := DefaultMaturityConfig()
	tracker := NewPatternMaturityTracker(cfg)
	ctx := context.Background()

	pm, ok := tracker.GetMaturity(ctx, "btn-login")
	if ok {
		t.Error("should not find unregistered pattern")
	}

	// First success -> auto-creates at New
	tracker.RecordSuccess(ctx, "btn-login")
	pm, ok = tracker.GetMaturity(ctx, "btn-login")
	if !ok {
		t.Fatal("should find pattern after first success")
	}
	if pm.Level != MaturityNew {
		t.Errorf("level = %v, want New (need %d for Testing)", pm.Level, cfg.TestingSuccesses)
	}

	// Reach Testing threshold
	tracker.RecordSuccess(ctx, "btn-login")
	pm, _ = tracker.GetMaturity(ctx, "btn-login")
	if pm.Level != MaturityTesting {
		t.Errorf("level = %v, want Testing after %d successes", pm.Level, cfg.TestingSuccesses)
	}

	// Reach Stable threshold
	for i := pm.TotalSuccesses; i < cfg.StableSuccesses; i++ {
		tracker.RecordSuccess(ctx, "btn-login")
	}
	pm, _ = tracker.GetMaturity(ctx, "btn-login")
	if pm.Level != MaturityStable {
		t.Errorf("level = %v, want Stable after %d successes", pm.Level, cfg.StableSuccesses)
	}

	// Reach Trusted threshold
	for i := pm.TotalSuccesses; i < cfg.TrustedSuccesses; i++ {
		tracker.RecordSuccess(ctx, "btn-login")
	}
	pm, _ = tracker.GetMaturity(ctx, "btn-login")
	if pm.Level != MaturityTrusted {
		t.Errorf("level = %v, want Trusted after %d successes", pm.Level, cfg.TrustedSuccesses)
	}
}

func TestPatternMaturityTracker_Degradation(t *testing.T) {
	cfg := DefaultMaturityConfig()
	tracker := NewPatternMaturityTracker(cfg)
	ctx := context.Background()

	// Build up to Stable
	for i := 0; i < cfg.StableSuccesses; i++ {
		tracker.RecordSuccess(ctx, "nav-link")
	}
	pm, _ := tracker.GetMaturity(ctx, "nav-link")
	if pm.Level != MaturityStable {
		t.Fatalf("expected Stable, got %v", pm.Level)
	}

	// Consecutive failures to trigger degradation
	for i := 0; i < cfg.DegradedFailures; i++ {
		tracker.RecordFailure(ctx, "nav-link")
	}
	pm, _ = tracker.GetMaturity(ctx, "nav-link")
	if pm.Level != MaturityDegraded {
		t.Errorf("level = %v, want Degraded after %d failures", pm.Level, cfg.DegradedFailures)
	}
}

func TestPatternMaturityTracker_RecoveryFromDegraded(t *testing.T) {
	cfg := DefaultMaturityConfig()
	tracker := NewPatternMaturityTracker(cfg)
	ctx := context.Background()

	// Build up and degrade
	for i := 0; i < cfg.StableSuccesses; i++ {
		tracker.RecordSuccess(ctx, "form-input")
	}
	for i := 0; i < cfg.DegradedFailures; i++ {
		tracker.RecordFailure(ctx, "form-input")
	}
	pm, _ := tracker.GetMaturity(ctx, "form-input")
	if pm.Level != MaturityDegraded {
		t.Fatalf("expected Degraded, got %v", pm.Level)
	}

	// Recovery: consecutive successes should move back to Testing
	for i := 0; i < cfg.TestingSuccesses; i++ {
		tracker.RecordSuccess(ctx, "form-input")
	}
	pm, _ = tracker.GetMaturity(ctx, "form-input")
	if pm.Level != MaturityTesting {
		t.Errorf("level = %v, want Testing after recovery", pm.Level)
	}
}

func TestPatternMaturityTracker_SuccessRate(t *testing.T) {
	cfg := DefaultMaturityConfig()
	tracker := NewPatternMaturityTracker(cfg)
	ctx := context.Background()

	tracker.RecordSuccess(ctx, "test-elem")
	tracker.RecordSuccess(ctx, "test-elem")
	tracker.RecordFailure(ctx, "test-elem")

	pm, _ := tracker.GetMaturity(ctx, "test-elem")
	rate := pm.SuccessRate()
	expected := 2.0 / 3.0
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("SuccessRate = %f, want ~%f", rate, expected)
	}
}

func TestPatternMaturityTracker_SuggestTier(t *testing.T) {
	cfg := MaturityConfig{
		TestingSuccesses: 1,
		StableSuccesses:  2,
		TrustedSuccesses: 3,
		DegradedFailures: 2,
	}
	tracker := NewPatternMaturityTracker(cfg)
	ctx := context.Background()

	// Unknown pattern -> Smart
	tier := tracker.SuggestTier(ctx, "unknown")
	if tier != TierSmart {
		t.Errorf("SuggestTier(unknown) = %v, want Smart", tier)
	}

	// New -> Smart
	tracker.RecordSuccess(ctx, "p1")
	pm, _ := tracker.GetMaturity(ctx, "p1")
	if pm.Level != MaturityTesting {
		t.Fatalf("expected Testing with 1 success, got %v", pm.Level)
	}
	tier = tracker.SuggestTier(ctx, "p1")
	if tier != TierSmart {
		t.Errorf("SuggestTier(Testing) = %v, want Smart", tier)
	}

	// Stable -> Light
	tracker.RecordSuccess(ctx, "p1")
	tier = tracker.SuggestTier(ctx, "p1")
	if tier != TierLight {
		t.Errorf("SuggestTier(Stable) = %v, want Light", tier)
	}

	// Degraded -> VLM
	tracker.RecordFailure(ctx, "p1")
	tracker.RecordFailure(ctx, "p1")
	pm, _ = tracker.GetMaturity(ctx, "p1")
	if pm.Level != MaturityDegraded {
		t.Fatalf("expected Degraded, got %v", pm.Level)
	}
	tier = tracker.SuggestTier(ctx, "p1")
	if tier != TierVLM {
		t.Errorf("SuggestTier(Degraded) = %v, want VLM", tier)
	}
}

func TestPatternMaturityTracker_ChangeListener(t *testing.T) {
	cfg := MaturityConfig{
		TestingSuccesses: 1,
		StableSuccesses:  2,
		TrustedSuccesses: 3,
		DegradedFailures: 1,
	}
	tracker := NewPatternMaturityTracker(cfg)
	ctx := context.Background()

	var events []MaturityChangeEvent
	tracker.OnChange(func(evt MaturityChangeEvent) {
		events = append(events, evt)
	})

	tracker.RecordSuccess(ctx, "listener-test") // New -> Testing
	tracker.RecordSuccess(ctx, "listener-test") // Testing -> Stable
	tracker.RecordFailure(ctx, "listener-test") // Stable -> Degraded

	if len(events) != 3 {
		t.Fatalf("expected 3 change events, got %d", len(events))
	}

	if events[0].From != MaturityNew || events[0].To != MaturityTesting {
		t.Errorf("event[0]: %v -> %v, want New -> Testing", events[0].From, events[0].To)
	}
	if events[1].From != MaturityTesting || events[1].To != MaturityStable {
		t.Errorf("event[1]: %v -> %v, want Testing -> Stable", events[1].From, events[1].To)
	}
	if events[2].From != MaturityStable || events[2].To != MaturityDegraded {
		t.Errorf("event[2]: %v -> %v, want Stable -> Degraded", events[2].From, events[2].To)
	}
}

func TestPatternMaturityTracker_AllMaturities(t *testing.T) {
	tracker := NewPatternMaturityTracker(DefaultMaturityConfig())
	ctx := context.Background()

	tracker.RecordSuccess(ctx, "p1")
	tracker.RecordSuccess(ctx, "p2")
	tracker.RecordFailure(ctx, "p3")

	all := tracker.AllMaturities()
	if len(all) != 3 {
		t.Errorf("AllMaturities count = %d, want 3", len(all))
	}
	for _, id := range []string{"p1", "p2", "p3"} {
		if _, ok := all[id]; !ok {
			t.Errorf("missing pattern %s", id)
		}
	}
}

func TestEmptyMaturity_SuccessRate(t *testing.T) {
	pm := PatternMaturity{}
	if pm.SuccessRate() != 0 {
		t.Errorf("SuccessRate of empty = %f, want 0", pm.SuccessRate())
	}
}

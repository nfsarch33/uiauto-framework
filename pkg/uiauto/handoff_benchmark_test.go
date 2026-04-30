package uiauto

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandoffPipeline_DistilledVsOriginal_Simulation(t *testing.T) {
	type modelProfile struct {
		name         string
		lightSuccess float64
		smartSuccess float64
		vlmSuccess   float64
		lightLatency time.Duration
		smartLatency time.Duration
		vlmLatency   time.Duration
	}
	profiles := []modelProfile{
		{
			name:         "distilled-27B",
			lightSuccess: 0.85,
			smartSuccess: 0.92,
			vlmSuccess:   0.98,
			lightLatency: 180 * time.Millisecond,
			smartLatency: 2200 * time.Millisecond,
			vlmLatency:   8 * time.Second,
		},
		{
			name:         "original-35B",
			lightSuccess: 0.80,
			smartSuccess: 0.95,
			vlmSuccess:   0.99,
			lightLatency: 350 * time.Millisecond,
			smartLatency: 4500 * time.Millisecond,
			vlmLatency:   12 * time.Second,
		},
	}

	type benchResult struct {
		model            string
		totalActions     int
		successCount     int
		failCount        int
		avgLatency       time.Duration
		tierDistribution map[ModelTier]int
		handoffCount     int
	}

	results := make([]benchResult, len(profiles))

	for pi, profile := range profiles {
		rng := rand.New(rand.NewSource(42))
		handoffs := NewInMemoryHandoffStore()
		lb := NewLatencyBudget(DefaultTierBudgets())
		fc := NewFallbackChain(
			DefaultFallbackChain(),
			func(tier ModelTier) bool { return true },
			WithFallbackBudget(lb),
			WithFallbackHandoffs(handoffs),
		)

		totalActions := 100
		successCount := 0
		failCount := 0
		var totalLatency time.Duration
		tierDist := map[ModelTier]int{}

		for i := 0; i < totalActions; i++ {
			patternID := "pattern_sim"
			actionStart := time.Now()

			tier, err := fc.Execute(context.Background(), patternID, func(ctx context.Context, tier ModelTier) error {
				var successRate float64
				var simLatency time.Duration
				switch tier {
				case TierLight:
					successRate = profile.lightSuccess
					simLatency = profile.lightLatency
				case TierSmart:
					successRate = profile.smartSuccess
					simLatency = profile.smartLatency
				case TierVLM:
					successRate = profile.vlmSuccess
					simLatency = profile.vlmLatency
				}
				_ = simLatency
				if rng.Float64() < successRate {
					return nil
				}
				return errors.New("simulated failure")
			})

			elapsed := time.Since(actionStart)
			totalLatency += elapsed

			if err == nil {
				successCount++
				tierDist[tier]++
			} else {
				failCount++
			}
		}

		results[pi] = benchResult{
			model:            profile.name,
			totalActions:     totalActions,
			successCount:     successCount,
			failCount:        failCount,
			avgLatency:       totalLatency / time.Duration(totalActions),
			tierDistribution: tierDist,
			handoffCount:     len(handoffs.Recent(1000)),
		}
	}

	for _, r := range results {
		t.Logf("Model: %s | Success: %d/%d (%.1f%%) | Handoffs: %d | Tier dist: Light=%d Smart=%d VLM=%d",
			r.model, r.successCount, r.totalActions,
			float64(r.successCount)/float64(r.totalActions)*100,
			r.handoffCount,
			r.tierDistribution[TierLight],
			r.tierDistribution[TierSmart],
			r.tierDistribution[TierVLM],
		)
	}

	// Both models should achieve >90% success rate through fallback chain
	for _, r := range results {
		assert.Greater(t, float64(r.successCount)/float64(r.totalActions), 0.90,
			"%s should have >90%% success rate", r.model)
	}
}

func TestHandoffPipeline_TierTransitionMetrics(t *testing.T) {
	handoffs := NewInMemoryHandoffStore()
	lb := NewLatencyBudget(DefaultTierBudgets())
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackBudget(lb),
		WithFallbackHandoffs(handoffs),
	)

	callCount := 0
	tier, err := fc.Execute(context.Background(), "btn_submit", func(ctx context.Context, tier ModelTier) error {
		callCount++
		if callCount <= 2 {
			return errors.New("tier failed")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, TierVLM, tier)

	recent := handoffs.Recent(10)
	assert.GreaterOrEqual(t, len(recent), 2)

	// Verify the handoff chain traces: light->smart failure, smart->vlm failure, then vlm success
	found := false
	for _, h := range recent {
		if h.ToTier == "vlm" && h.Success {
			found = true
			break
		}
	}
	assert.True(t, found, "should have a successful VLM handoff recorded")
}

func TestHandoffPipeline_LatencyViolationTracking(t *testing.T) {
	lb := NewLatencyBudget([]TierBudget{
		{Tier: TierLight, MaxLatency: 10 * time.Millisecond},
		{Tier: TierSmart, MaxLatency: 50 * time.Millisecond},
		{Tier: TierVLM, MaxLatency: 100 * time.Millisecond},
	})
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackBudget(lb),
	)

	_, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)
	// Light tier budget is 10ms, but we slept 20ms -- violation should be recorded
	assert.GreaterOrEqual(t, lb.ViolationCount(TierLight), int64(1))
}

func TestHandoffPipeline_PhaseTrackerIntegration(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 3)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())

	// Simulate smart successes to move to cruise
	for i := 0; i < 3; i++ {
		pt.RecordSuccess(TierSmart)
	}
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())

	// Simulate light failures to escalate
	for i := 0; i < 3; i++ {
		pt.RecordFailure(TierLight)
	}
	assert.Equal(t, PhaseEscalation, pt.CurrentPhase())

	// VLM success should return to discovery
	pt.RecordSuccess(TierVLM)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())

	stats := pt.Stats()
	assert.GreaterOrEqual(t, stats.TransitionCount, 3)
	assert.GreaterOrEqual(t, stats.EscalationCount, int64(1))
}

func BenchmarkFallbackChain_LightSuccess(b *testing.B) {
	fc := NewFallbackChain(DefaultFallbackChain(), func(tier ModelTier) bool { return true })
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fc.Execute(ctx, "p1", func(ctx context.Context, tier ModelTier) error {
			return nil
		})
	}
}

func BenchmarkFallbackChain_FallbackToSmart(b *testing.B) {
	fc := NewFallbackChain(DefaultFallbackChain(), func(tier ModelTier) bool { return true })
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fc.Execute(ctx, "p1", func(ctx context.Context, tier ModelTier) error {
			if tier == TierLight {
				return errors.New("light failed")
			}
			return nil
		})
	}
}

func BenchmarkFallbackChain_FullEscalation(b *testing.B) {
	fc := NewFallbackChain(DefaultFallbackChain(), func(tier ModelTier) bool { return true })
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fc.Execute(ctx, "p1", func(ctx context.Context, tier ModelTier) error {
			if tier == TierVLM {
				return nil
			}
			return errors.New("failed")
		})
	}
}

func BenchmarkLatencyBudget_Record(b *testing.B) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.Record(TierLight, 200*time.Millisecond)
	}
}

package uiauto

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTierBudgets(t *testing.T) {
	budgets := DefaultTierBudgets()
	assert.Len(t, budgets, 3)
	assert.Equal(t, TierLight, budgets[0].Tier)
	assert.Equal(t, 500*time.Millisecond, budgets[0].MaxLatency)
	assert.Equal(t, TierSmart, budgets[1].Tier)
	assert.Equal(t, 5*time.Second, budgets[1].MaxLatency)
	assert.Equal(t, TierVLM, budgets[2].Tier)
	assert.Equal(t, 15*time.Second, budgets[2].MaxLatency)
}

func TestLatencyBudget_Record_WithinBudget(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ok := lb.Record(TierLight, 200*time.Millisecond)
	assert.True(t, ok)
	assert.Equal(t, int64(0), lb.ViolationCount(TierLight))
}

func TestLatencyBudget_Record_Violation(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ok := lb.Record(TierLight, 600*time.Millisecond)
	assert.False(t, ok)
	assert.Equal(t, int64(1), lb.ViolationCount(TierLight))
}

func TestLatencyBudget_Record_SmartWithinBudget(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ok := lb.Record(TierSmart, 3*time.Second)
	assert.True(t, ok)
	assert.Equal(t, int64(0), lb.ViolationCount(TierSmart))
}

func TestLatencyBudget_Record_VLMViolation(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ok := lb.Record(TierVLM, 20*time.Second)
	assert.False(t, ok)
	assert.Equal(t, int64(1), lb.ViolationCount(TierVLM))
}

func TestLatencyBudget_Record_UnknownTier(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ok := lb.Record(ModelTier(99), time.Hour)
	assert.True(t, ok)
	assert.Equal(t, int64(0), lb.ViolationCount(ModelTier(99)))
}

func TestLatencyBudget_MultipleViolations(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	for i := 0; i < 5; i++ {
		lb.Record(TierLight, 600*time.Millisecond)
	}
	assert.Equal(t, int64(5), lb.ViolationCount(TierLight))
}

func TestLatencyBudget_ContextWithDeadline(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ctx := context.Background()
	tctx, cancel := lb.ContextWithDeadline(ctx, TierLight)
	defer cancel()
	dl, ok := tctx.Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(500*time.Millisecond), dl, 100*time.Millisecond)
}

func TestLatencyBudget_ContextWithDeadline_UnknownTier(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	ctx := context.Background()
	tctx, cancel := lb.ContextWithDeadline(ctx, ModelTier(99))
	defer cancel()
	_, ok := tctx.Deadline()
	assert.False(t, ok)
}

func TestDefaultFallbackChain(t *testing.T) {
	fc := DefaultFallbackChain()
	assert.Equal(t, []ModelTier{TierLight, TierSmart, TierVLM}, fc.Tiers)
}

func TestFallbackChain_FirstTierSucceeds(t *testing.T) {
	fc := NewFallbackChain(DefaultFallbackChain(), func(tier ModelTier) bool { return true })
	tier, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, TierLight, tier)
}

func TestFallbackChain_FallsBackToSmart(t *testing.T) {
	handoffs := NewInMemoryHandoffStore()
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackHandoffs(handoffs),
	)
	tier, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		if tier == TierLight {
			return errors.New("light failed")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, TierSmart, tier)

	recent := handoffs.Recent(10)
	require.Len(t, recent, 2)
	// Recent returns most recent first: success handoff, then the failure transition
	assert.Equal(t, "light", recent[0].FromTier)
	assert.Equal(t, "smart", recent[0].ToTier)
	assert.True(t, recent[0].Success)
	assert.Equal(t, "light", recent[1].FromTier)
	assert.Equal(t, "smart", recent[1].ToTier)
	assert.False(t, recent[1].Success)
}

func TestFallbackChain_FallsBackToVLM(t *testing.T) {
	fc := NewFallbackChain(DefaultFallbackChain(), func(tier ModelTier) bool { return true })
	tier, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		if tier == TierVLM {
			return nil
		}
		return errors.New("failed")
	})
	require.NoError(t, err)
	assert.Equal(t, TierVLM, tier)
}

func TestFallbackChain_AllTiersFail(t *testing.T) {
	fc := NewFallbackChain(DefaultFallbackChain(), func(tier ModelTier) bool { return true })
	_, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		return errors.New("tier failed")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all tiers exhausted")
}

func TestFallbackChain_SkipsUnavailableTier(t *testing.T) {
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return tier != TierSmart },
	)
	calls := make(map[ModelTier]bool)
	tier, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		calls[tier] = true
		if tier == TierLight {
			return errors.New("light failed")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, TierVLM, tier)
	assert.True(t, calls[TierLight])
	assert.False(t, calls[TierSmart])
	assert.True(t, calls[TierVLM])
}

func TestFallbackChain_WithLatencyBudget(t *testing.T) {
	lb := NewLatencyBudget(DefaultTierBudgets())
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackBudget(lb),
	)
	_, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		return nil
	})
	require.NoError(t, err)
}

func TestFallbackChain_AllUnavailable(t *testing.T) {
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return false },
	)
	_, err := fc.Execute(context.Background(), "p1", func(ctx context.Context, tier ModelTier) error {
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all tiers exhausted")
}

func TestFallbackChain_HandoffRecordsOnSuccess(t *testing.T) {
	handoffs := NewInMemoryHandoffStore()
	fc := NewFallbackChain(
		DefaultFallbackChain(),
		func(tier ModelTier) bool { return true },
		WithFallbackHandoffs(handoffs),
	)
	call := 0
	_, err := fc.Execute(context.Background(), "btn_1", func(ctx context.Context, tier ModelTier) error {
		call++
		if call <= 2 {
			return errors.New("failed")
		}
		return nil
	})
	require.NoError(t, err)

	recent := handoffs.Recent(10)
	assert.GreaterOrEqual(t, len(recent), 2)
}

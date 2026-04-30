// ADR-019: layer 1 (TestRole_AutoPromoter), layer 2 (state/action/reward via
// Submit→Promote loop), layer 4 (Sandbox/Approval gates), layer 5
// (capsule_id observability tag).
package evolver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedClock returns a deterministic time for the auto-promoter under test.
func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

// staticResolver returns a GeneResolver that always answers with the supplied
// category. Tests opt into a per-mutation resolver only when the category
// itself is the variable under test.
func staticResolver(cat GeneCategory) GeneResolver {
	return func(_ string) (GeneCategory, error) { return cat, nil }
}

// setupAutoPromoter is the shared scaffold for every TestRole_AutoPromoter case.
// It instantiates a real CapsuleStore in a t.TempDir() so SaveCapsule actually
// persists, plus an EvaluationHarness with the production scoring rule.
func setupAutoPromoter(t *testing.T, cfg AutoPromoterConfig) (*AutoPromoter, *PromotionPipeline, *CapsuleStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)

	pipeline := NewPromotionPipeline(PromotionPipelineConfig{
		AutoPromoteLowRisk: true,
		RequireHITL:        false,
		PassThreshold:      0.6,
	}, store, harness)

	if cfg.Now == nil {
		cfg.Now = fixedClock(time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC))
	}
	if cfg.GeneResolver == nil {
		cfg.GeneResolver = staticResolver(GeneCategorySelector)
	}
	ap := NewAutoPromoter(pipeline, cfg)
	return ap, pipeline, store
}

func reversibleLowRisk(id string) Mutation {
	mut := testMutation()
	mut.ID = id
	mut.RiskEstimate = RiskLow
	return mut
}

// TestRole_AutoPromoter_HappyPath verifies the full Submit → Promote → capsule
// persistence loop fires when every guardrail passes.
func TestRole_AutoPromoter_HappyPath(t *testing.T) {
	ap, _, store := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories:    []GeneCategory{GeneCategorySelector},
		MaxRollbacksPerDay:   3,
		MinCooldownPerSignal: 0,
	})

	ctx := context.Background()
	mut := reversibleLowRisk("mut-auto-1")
	sig := testSignal()

	rec, err := ap.Process(ctx, mut, sig, func(m Mutation, e EvaluationResult) (*Capsule, error) {
		return &Capsule{
			ID:        "cap-auto-1",
			Name:      "auto-promoted-selector-fix",
			GeneIDs:   []string{m.GeneID},
			CreatedAt: time.Now(),
			Metadata:  map[string]string{"signal_id": m.SignalID},
		}, nil
	})

	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, PromotionPromoted, rec.Status, "happy path must reach Promoted")
	assert.Equal(t, "cap-auto-1", rec.CapsuleID)

	// Capsule landed on disk with the auto-promote provenance stamp.
	saved, err := store.LoadCapsule(ctx, "cap-auto-1")
	require.NoError(t, err)
	assert.Equal(t, "auto-promoted-selector-fix", saved.Name)
	assert.Equal(t, "evoloop", saved.Metadata["auto_promoted_by"], "capsule must carry the auto-promote provenance tag")
	assert.NotEmpty(t, saved.Metadata["auto_promoted_reason"])

	m := ap.Metrics()
	assert.Equal(t, 1, m.AutoPromoted)
	assert.Equal(t, 0, m.GuardrailRejected)
}

// TestRole_AutoPromoter_HighRiskBlocked rejects mutations that fail the hard
// risk gate, regardless of evaluation score.
func TestRole_AutoPromoter_HighRiskBlocked(t *testing.T) {
	ap, _, store := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories: []GeneCategory{GeneCategorySelector},
	})

	ctx := context.Background()
	mut := reversibleLowRisk("mut-high")
	mut.RiskEstimate = RiskHigh

	rec, err := ap.Process(ctx, mut, testSignal(), failBuilder)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, PromotionRejected, rec.Status)
	assert.Contains(t, rec.Reason, "auto-promote")

	// No capsule was even attempted; CapsuleStore is empty.
	_, loadErr := store.LoadCapsule(ctx, "cap-high")
	assert.Error(t, loadErr, "high-risk mutation must not produce a capsule")

	assert.Equal(t, 1, ap.Metrics().GuardrailRejected)
}

// TestRole_AutoPromoter_DisallowedCategoryBlocked guards against an upstream
// gene catalogue slipping through with a category that hasn't been opted in.
func TestRole_AutoPromoter_DisallowedCategoryBlocked(t *testing.T) {
	ap, _, _ := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories: []GeneCategory{GeneCategorySelector},
		GeneResolver:      staticResolver(GeneCategoryWorkflow), // not on the allow-list
	})
	ctx := context.Background()

	rec, err := ap.Process(ctx, reversibleLowRisk("mut-cat"), testSignal(), failBuilder)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, PromotionRejected, rec.Status)
	assert.Contains(t, rec.Reason, "category")
	assert.Equal(t, 1, ap.Metrics().GuardrailRejected)
}

// TestVerifier_AutoPromoter_RollbackBudgetCircuitBreaks records that once the
// rolling 24h rollback budget is exhausted, even a clean mutation gets gated.
func TestVerifier_AutoPromoter_RollbackBudgetCircuitBreaks(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	ap, _, _ := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories:    []GeneCategory{GeneCategorySelector},
		MaxRollbacksPerDay:   2,
		MinCooldownPerSignal: 0,
		Now:                  fixedClock(now),
	})

	// Two rollbacks within the past 24h — exactly at the threshold.
	ap.RecordRollback("cap-a", now.Add(-1*time.Hour))
	ap.RecordRollback("cap-b", now.Add(-23*time.Hour))

	ctx := context.Background()
	rec, err := ap.Process(ctx, reversibleLowRisk("mut-budget"), testSignal(), failBuilder)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, PromotionRejected, rec.Status)
	assert.Contains(t, rec.Reason, "rollback budget")
	assert.Equal(t, 1, ap.Metrics().CircuitBreakerTripped)
}

// TestVerifier_AutoPromoter_RollbackOutsideWindowDoesNotCount confirms the
// rolling-24h window logic — old rollbacks fall off and the gate clears.
func TestVerifier_AutoPromoter_RollbackOutsideWindowDoesNotCount(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	ap, _, _ := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories:    []GeneCategory{GeneCategorySelector},
		MaxRollbacksPerDay:   1,
		MinCooldownPerSignal: 0,
		Now:                  fixedClock(now),
	})

	// 25h ago — outside the window, should not count.
	ap.RecordRollback("cap-stale", now.Add(-25*time.Hour))

	ctx := context.Background()
	rec, err := ap.Process(ctx, reversibleLowRisk("mut-window"), testSignal(), goodBuilder("cap-window"))
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, PromotionPromoted, rec.Status, "stale rollback must not block fresh auto-promote")
}

// TestVerifier_AutoPromoter_CooldownPerSignal verifies the cooldown window
// stops a single signal source from spamming auto-promotes.
func TestVerifier_AutoPromoter_CooldownPerSignal(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	ap, _, _ := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories:    []GeneCategory{GeneCategorySelector},
		MaxRollbacksPerDay:   100,
		MinCooldownPerSignal: 10 * time.Minute,
		Now:                  clock,
	})

	ctx := context.Background()
	sig := testSignal()
	sig.ID = "sig-x"

	// First promotion lands.
	rec, err := ap.Process(ctx, reversibleLowRisk("mut-1"), sig, goodBuilder("cap-1"))
	require.NoError(t, err)
	require.Equal(t, PromotionPromoted, rec.Status)

	// Second promotion under the same signal, within the cooldown window — gated.
	rec2, err := ap.Process(ctx, reversibleLowRisk("mut-2"), sig, failBuilder)
	require.NoError(t, err)
	require.NotNil(t, rec2)
	assert.Equal(t, PromotionRejected, rec2.Status)
	assert.Contains(t, rec2.Reason, "cooldown")
	assert.Equal(t, 1, ap.Metrics().CooldownGated)
}

// TestRole_AutoPromoter_BuilderErrorRollsBackPipeline guarantees that if the
// capsule builder fails AFTER pipeline.Submit returned Approved, the pipeline
// state is reverted (rejected with the builder error) so the next reconcile
// loop doesn't try to Promote a capsule that doesn't exist.
func TestRole_AutoPromoter_BuilderErrorRollsBackPipeline(t *testing.T) {
	ap, pipeline, _ := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories: []GeneCategory{GeneCategorySelector},
	})

	ctx := context.Background()
	rec, err := ap.Process(ctx, reversibleLowRisk("mut-builder-err"), testSignal(), failBuilder)
	require.Error(t, err, "builder error must surface to caller")
	require.NotNil(t, rec)
	assert.Equal(t, PromotionRejected, rec.Status, "pipeline must roll back to rejected on builder failure")
	assert.Contains(t, rec.Reason, "build capsule")

	// Defensive: explicit pipeline lookup matches.
	got, ok := pipeline.GetRecord("mut-builder-err")
	require.True(t, ok)
	assert.Equal(t, PromotionRejected, got.Status)
	assert.Equal(t, 1, ap.Metrics().BuilderFailed)
}

// TestRole_AutoPromoter_DefaultConfig anchors the production defaults
// (allow-list, rollback budget, cooldown) so a future PR can't quietly
// disable the gates by editing the constant.
func TestRole_AutoPromoter_DefaultConfig(t *testing.T) {
	cfg := DefaultAutoPromoterConfig()
	assert.ElementsMatch(t, []GeneCategory{
		GeneCategorySelector, GeneCategoryPattern, GeneCategoryResilience,
	}, cfg.AllowedCategories)
	assert.Equal(t, 3, cfg.MaxRollbacksPerDay)
	assert.Equal(t, 5*time.Minute, cfg.MinCooldownPerSignal)
	require.NotNil(t, cfg.GeneResolver, "default resolver must not be nil")
	require.NotNil(t, cfg.Now, "default clock must not be nil")
}

// TestRole_AutoPromoter_ResolverErrorRejects verifies a transient failure
// from the gene catalogue surfaces as a guardrail rejection, not a panic or
// a silent pass-through.
func TestRole_AutoPromoter_ResolverErrorRejects(t *testing.T) {
	resolverErr := autoPromoterTestError("gene store offline")
	ap, _, _ := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories: []GeneCategory{GeneCategorySelector},
		GeneResolver:      func(_ string) (GeneCategory, error) { return "", resolverErr },
	})
	rec, err := ap.Process(context.Background(), reversibleLowRisk("mut-resolver"), testSignal(), failBuilder)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, PromotionRejected, rec.Status)
	assert.Contains(t, rec.Reason, "gene resolver")
	assert.Equal(t, 1, ap.Metrics().GuardrailRejected)
}

// TestApproval_AutoPromoter_ConcurrentProcessIsSafe is the harness-engineering
// concurrency check — ten goroutines hammering Process on distinct mutations
// must produce ten capsules and zero races.
func TestApproval_AutoPromoter_ConcurrentProcessIsSafe(t *testing.T) {
	ap, _, store := setupAutoPromoter(t, AutoPromoterConfig{
		AllowedCategories:    []GeneCategory{GeneCategorySelector},
		MaxRollbacksPerDay:   100,
		MinCooldownPerSignal: 0,
	})

	ctx := context.Background()
	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := "mut-c-" + uniq(i)
			cap := "cap-c-" + uniq(i)
			sig := testSignal()
			sig.ID = "sig-" + uniq(i)
			_, err := ap.Process(ctx, reversibleLowRisk(id), sig, goodBuilder(cap))
			require.NoError(t, err)
		}()
	}
	wg.Wait()

	for i := 0; i < N; i++ {
		_, err := store.LoadCapsule(ctx, "cap-c-"+uniq(i))
		require.NoError(t, err, "every concurrent capsule must persist")
	}
	assert.Equal(t, N, ap.Metrics().AutoPromoted)
}

// helpers --------------------------------------------------------------------

func uniq(i int) string { return string(rune('a' + i)) }

// goodBuilder returns a CapsuleBuilder that produces a capsule with the
// supplied ID and the canonical auto-promote provenance.
func goodBuilder(id string) CapsuleBuilder {
	return func(m Mutation, e EvaluationResult) (*Capsule, error) {
		return &Capsule{
			ID:        id,
			Name:      "test-" + id,
			GeneIDs:   []string{m.GeneID},
			CreatedAt: time.Now(),
		}, nil
	}
}

// failBuilder always returns an error; used to verify the pipeline rolls back
// to rejected on builder failure.
func failBuilder(_ Mutation, _ EvaluationResult) (*Capsule, error) {
	return nil, assertCapsuleBuilderError
}

// assertCapsuleBuilderError is a sentinel so tests can match the error path.
var assertCapsuleBuilderError = autoPromoterTestError("builder boom")

type autoPromoterTestError string

func (e autoPromoterTestError) Error() string { return string(e) }

package evolver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPipeline(t *testing.T) (*PromotionPipeline, *CapsuleStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	cfg := DefaultPromotionConfig()
	cfg.RequireHITL = true
	pipeline := NewPromotionPipeline(cfg, store, harness)
	return pipeline, store
}

func TestPromotion_SubmitAndEvaluate(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	rec, err := pipeline.Submit(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.NotNil(t, rec.Evaluation)
	assert.Equal(t, PromotionEvaluated, rec.Status, "passing mutation should be evaluated")
}

func TestPromotion_SubmitFailingMutation(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	mut := testMutation()
	mut.ID = "mut-fail"
	mut.RiskEstimate = RiskHigh
	mut.GeneID = ""
	mut.Reasoning = "x"

	rec, err := pipeline.Submit(ctx, mut, testSignal())
	require.NoError(t, err)
	assert.Equal(t, PromotionRejected, rec.Status, "failing mutation should be rejected")
	assert.Contains(t, rec.Reason, "below threshold")
}

func TestPromotion_ApproveAndPromote(t *testing.T) {
	pipeline, store := setupPipeline(t)
	ctx := context.Background()

	rec, _ := pipeline.Submit(ctx, testMutation(), testSignal())
	require.Equal(t, PromotionEvaluated, rec.Status)

	err := pipeline.Approve("mut-001", "reviewer@test.com", "looks good")
	require.NoError(t, err)

	rec2, ok := pipeline.GetRecord("mut-001")
	require.True(t, ok)
	assert.Equal(t, PromotionApproved, rec2.Status)
	assert.Equal(t, "reviewer@test.com", rec2.ReviewedBy)
	assert.NotNil(t, rec2.ReviewedAt)

	capsule := &Capsule{
		ID:          "cap-001",
		Name:        "selector-fix-login",
		Description: "Fix login submit selector drift",
		GeneIDs:     []string{"gene-selector-fix"},
		CreatedAt:   time.Now(),
	}
	err = pipeline.Promote(ctx, "mut-001", capsule)
	require.NoError(t, err)

	rec3, _ := pipeline.GetRecord("mut-001")
	assert.Equal(t, PromotionPromoted, rec3.Status)
	assert.Equal(t, "cap-001", rec3.CapsuleID)
	assert.NotNil(t, rec3.PromotedAt)

	saved, err := store.LoadCapsule(ctx, "cap-001")
	require.NoError(t, err)
	assert.Equal(t, "selector-fix-login", saved.Name)
}

func TestPromotion_Reject(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	pipeline.Submit(ctx, testMutation(), testSignal())

	err := pipeline.Reject("mut-001", "reviewer@test.com", "too risky for prod")
	require.NoError(t, err)

	rec, ok := pipeline.GetRecord("mut-001")
	require.True(t, ok)
	assert.Equal(t, PromotionRejected, rec.Status)
	assert.Equal(t, "too risky for prod", rec.Reason)
}

func TestPromotion_Rollback(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	pipeline.Submit(ctx, testMutation(), testSignal())
	pipeline.Approve("mut-001", "reviewer", "ok")
	capsule := &Capsule{
		ID: "cap-rollback", Name: "test", Description: "test",
		GeneIDs: []string{"gene-test"}, CreatedAt: time.Now(),
	}
	pipeline.Promote(ctx, "mut-001", capsule)

	err := pipeline.Rollback("mut-001", "caused regression in staging")
	require.NoError(t, err)

	rec, _ := pipeline.GetRecord("mut-001")
	assert.Equal(t, PromotionRolledBack, rec.Status)
	assert.NotNil(t, rec.RolledBackAt)
}

func TestPromotion_PromoteWithoutApproval_Fails(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	pipeline.Submit(ctx, testMutation(), testSignal())

	capsule := &Capsule{
		ID: "cap-noauth", Name: "test", Description: "test",
		GeneIDs: []string{"gene-test"}, CreatedAt: time.Now(),
	}
	err := pipeline.Promote(ctx, "mut-001", capsule)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected approved")
}

func TestPromotion_ApproveUnknownMutation_Fails(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	err := pipeline.Approve("nonexistent", "reviewer", "ok")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPromotion_RollbackNonPromoted_Fails(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	pipeline.Submit(ctx, testMutation(), testSignal())

	err := pipeline.Rollback("mut-001", "premature rollback")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected promoted")
}

func TestPromotion_AutoPromoteLowRisk(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewCapsuleStore(dir)
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)

	cfg := PromotionPipelineConfig{
		AutoPromoteLowRisk: true,
		RequireHITL:        false,
		PassThreshold:      0.6,
	}
	pipeline := NewPromotionPipeline(cfg, store, harness)

	ctx := context.Background()
	mut := testMutation()
	mut.RiskEstimate = RiskLow

	rec, err := pipeline.Submit(ctx, mut, testSignal())
	require.NoError(t, err)
	assert.Equal(t, PromotionApproved, rec.Status, "low-risk passing mutation should auto-approve")
	assert.Equal(t, "auto_promote", rec.ReviewedBy)
}

func TestPromotion_Metrics(t *testing.T) {
	pipeline, _ := setupPipeline(t)
	ctx := context.Background()

	pipeline.Submit(ctx, testMutation(), testSignal())
	pipeline.Approve("mut-001", "reviewer", "ok")

	m := pipeline.Metrics()
	assert.Equal(t, 1, m.TotalSubmitted)
	assert.Equal(t, 1, m.TotalEvaluated)
	assert.Equal(t, 1, m.TotalApproved)
	assert.Equal(t, 0, m.TotalPromoted)
}

func TestPromotion_FullLifecycle(t *testing.T) {
	pipeline, store := setupPipeline(t)
	ctx := context.Background()

	// Submit
	rec, err := pipeline.Submit(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.Equal(t, PromotionEvaluated, rec.Status)

	// Approve
	err = pipeline.Approve("mut-001", "lead@team.com", "verified in staging")
	require.NoError(t, err)

	// Promote
	capsule := &Capsule{
		ID: "cap-lifecycle", Name: "login-fix", Description: "Fix login drift",
		GeneIDs: []string{"gene-login-fix"}, CreatedAt: time.Now(),
	}
	err = pipeline.Promote(ctx, "mut-001", capsule)
	require.NoError(t, err)

	// Verify capsule persisted
	saved, err := store.LoadCapsule(ctx, "cap-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "login-fix", saved.Name)

	// Rollback
	err = pipeline.Rollback("mut-001", "regression in prod")
	require.NoError(t, err)

	// Verify final metrics
	m := pipeline.Metrics()
	assert.Equal(t, 1, m.TotalSubmitted)
	assert.Equal(t, 1, m.TotalEvaluated)
	assert.Equal(t, 1, m.TotalApproved)
	assert.Equal(t, 1, m.TotalPromoted)
	assert.Equal(t, 1, m.TotalRolledBack)
}

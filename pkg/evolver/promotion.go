package evolver

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PromotionStatus tracks a mutation through the promotion pipeline.
type PromotionStatus string

// PromotionStatus values.
const (
	PromotionPending    PromotionStatus = "pending"
	PromotionEvaluated  PromotionStatus = "evaluated"
	PromotionApproved   PromotionStatus = "approved"
	PromotionRejected   PromotionStatus = "rejected"
	PromotionPromoted   PromotionStatus = "promoted"
	PromotionRolledBack PromotionStatus = "rolled_back"
)

// PromotionRecord tracks a single mutation through the pipeline.
type PromotionRecord struct {
	MutationID   string            `json:"mutation_id"`
	CapsuleID    string            `json:"capsule_id,omitempty"`
	Status       PromotionStatus   `json:"status"`
	Evaluation   *EvaluationResult `json:"evaluation,omitempty"`
	ReviewedBy   string            `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time        `json:"reviewed_at,omitempty"`
	PromotedAt   *time.Time        `json:"promoted_at,omitempty"`
	RolledBackAt *time.Time        `json:"rolled_back_at,omitempty"`
	Reason       string            `json:"reason,omitempty"`
}

// PromotionPipelineConfig controls the pipeline behavior.
type PromotionPipelineConfig struct {
	AutoPromoteLowRisk bool
	RequireHITL        bool
	PassThreshold      float64
}

// DefaultPromotionConfig returns conservative defaults (HITL required).
func DefaultPromotionConfig() PromotionPipelineConfig {
	return PromotionPipelineConfig{
		AutoPromoteLowRisk: false,
		RequireHITL:        true,
		PassThreshold:      0.6,
	}
}

// PromotionPipeline manages the lifecycle of mutations from evaluation to
// promotion into the capsule store.
type PromotionPipeline struct {
	mu      sync.Mutex
	cfg     PromotionPipelineConfig
	store   *CapsuleStore
	harness *EvaluationHarness
	records map[string]*PromotionRecord
	metrics PromotionMetrics
}

// PromotionMetrics tracks pipeline throughput.
type PromotionMetrics struct {
	TotalSubmitted  int `json:"total_submitted"`
	TotalEvaluated  int `json:"total_evaluated"`
	TotalApproved   int `json:"total_approved"`
	TotalRejected   int `json:"total_rejected"`
	TotalPromoted   int `json:"total_promoted"`
	TotalRolledBack int `json:"total_rolled_back"`
}

// NewPromotionPipeline creates a pipeline with the given dependencies.
func NewPromotionPipeline(cfg PromotionPipelineConfig, store *CapsuleStore, harness *EvaluationHarness) *PromotionPipeline {
	if cfg.PassThreshold <= 0 {
		cfg.PassThreshold = 0.6
	}
	return &PromotionPipeline{
		cfg:     cfg,
		store:   store,
		harness: harness,
		records: make(map[string]*PromotionRecord),
	}
}

// Submit adds a mutation to the pipeline for evaluation.
func (p *PromotionPipeline) Submit(ctx context.Context, mut Mutation, sig Signal) (*PromotionRecord, error) {
	p.mu.Lock()
	rec := &PromotionRecord{
		MutationID: mut.ID,
		Status:     PromotionPending,
	}
	p.records[mut.ID] = rec
	p.metrics.TotalSubmitted++
	p.mu.Unlock()

	eval, err := p.harness.Evaluate(ctx, mut, sig)
	if err != nil {
		return rec, fmt.Errorf("evaluate mutation %s: %w", mut.ID, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	rec.Evaluation = &eval
	rec.Status = PromotionEvaluated
	p.metrics.TotalEvaluated++

	if !eval.Pass {
		rec.Status = PromotionRejected
		rec.Reason = fmt.Sprintf("score %.1f below threshold %.1f", eval.Score, p.cfg.PassThreshold*eval.MaxScore)
		p.metrics.TotalRejected++
		return rec, nil
	}

	// Auto-promote low-risk mutations that pass evaluation
	if p.cfg.AutoPromoteLowRisk && mut.RiskEstimate == RiskLow && !p.cfg.RequireHITL {
		rec.Status = PromotionApproved
		rec.ReviewedBy = "auto_promote"
		now := time.Now()
		rec.ReviewedAt = &now
		p.metrics.TotalApproved++
	}

	return rec, nil
}

// Approve marks a mutation as approved by a human reviewer.
func (p *PromotionPipeline) Approve(mutationID, reviewerID, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.records[mutationID]
	if !ok {
		return fmt.Errorf("mutation %s not found", mutationID)
	}
	if rec.Status != PromotionEvaluated {
		return fmt.Errorf("mutation %s in state %s, expected %s", mutationID, rec.Status, PromotionEvaluated)
	}

	rec.Status = PromotionApproved
	rec.ReviewedBy = reviewerID
	now := time.Now()
	rec.ReviewedAt = &now
	rec.Reason = reason
	p.metrics.TotalApproved++
	return nil
}

// Reject marks a mutation as rejected by a human reviewer.
func (p *PromotionPipeline) Reject(mutationID, reviewerID, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.records[mutationID]
	if !ok {
		return fmt.Errorf("mutation %s not found", mutationID)
	}

	rec.Status = PromotionRejected
	rec.ReviewedBy = reviewerID
	now := time.Now()
	rec.ReviewedAt = &now
	rec.Reason = reason
	p.metrics.TotalRejected++
	return nil
}

// Promote persists an approved mutation as a capsule in the store.
func (p *PromotionPipeline) Promote(ctx context.Context, mutationID string, capsule *Capsule) error {
	p.mu.Lock()
	rec, ok := p.records[mutationID]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("mutation %s not found", mutationID)
	}
	if rec.Status != PromotionApproved {
		p.mu.Unlock()
		return fmt.Errorf("mutation %s in state %s, expected %s", mutationID, rec.Status, PromotionApproved)
	}
	p.mu.Unlock()

	if err := p.store.SaveCapsule(ctx, capsule); err != nil {
		return fmt.Errorf("save capsule: %w", err)
	}

	p.mu.Lock()
	rec.CapsuleID = capsule.ID
	rec.Status = PromotionPromoted
	now := time.Now()
	rec.PromotedAt = &now
	p.metrics.TotalPromoted++
	p.mu.Unlock()

	return nil
}

// Rollback marks a promoted mutation as rolled back.
func (p *PromotionPipeline) Rollback(mutationID, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	rec, ok := p.records[mutationID]
	if !ok {
		return fmt.Errorf("mutation %s not found", mutationID)
	}
	if rec.Status != PromotionPromoted {
		return fmt.Errorf("mutation %s in state %s, expected %s", mutationID, rec.Status, PromotionPromoted)
	}

	rec.Status = PromotionRolledBack
	now := time.Now()
	rec.RolledBackAt = &now
	rec.Reason = reason
	p.metrics.TotalRolledBack++
	return nil
}

// GetRecord returns a promotion record by mutation ID.
func (p *PromotionPipeline) GetRecord(mutationID string) (*PromotionRecord, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.records[mutationID]
	return rec, ok
}

// Metrics returns current pipeline metrics.
func (p *PromotionPipeline) Metrics() PromotionMetrics {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.metrics
}

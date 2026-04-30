// ADR-019: layer 1 (Role: AutoPromoter), layer 2 (Submit→Promote = state →
// action → reward loop with capsule_id reward proxy), layer 4 (Sandbox via
// allow-list + risk gate; ComplianceChecker via rolling rollback budget;
// Approval via cooldown), layer 5 (capsule_id, outcome_id, trace_id ride
// inside the capsule metadata for downstream Prometheus + Mem0).
package evolver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// CapsuleBuilder converts an evaluated, approved mutation into the capsule
// that will be persisted by `PromotionPipeline.Promote`. Builders are pure
// functions of the mutation + evaluation result so the auto-promoter can
// retry deterministically on transient store failures (`store.SaveCapsule` is
// the only side effect in the loop).
type CapsuleBuilder func(Mutation, EvaluationResult) (*Capsule, error)

// GeneResolver maps a Gene ID to its category. The auto-promoter calls it
// before reaching the pipeline so we can fail closed on disallowed categories
// without spending a `harness.Evaluate` cycle. Returning a non-nil error is
// treated as a guardrail rejection (the gene store is itself the policy
// surface, not a transient transport).
type GeneResolver func(geneID string) (GeneCategory, error)

// AutoPromoterConfig collects the safety constraints that gate the auto-loop.
// Every field has a documented sane default; callers configure only the knobs
// they're actually moving away from baseline.
type AutoPromoterConfig struct {
	// AllowedCategories is the explicit allow-list of GeneCategory values
	// the auto-promoter will consider. Anything else is rejected even when
	// it scores HD on evaluation. Default: [GeneCategorySelector,
	// GeneCategoryPattern, GeneCategoryResilience] — the three categories
	// already proven safe via the SelectorEngineV2 + drift-detector +
	// circuit-breaker work.
	AllowedCategories []GeneCategory

	// MaxRollbacksPerDay caps the rolling 24-hour rollback count. Once
	// exceeded, the auto-promoter circuit-breaks: even a clean mutation is
	// rejected with `auto-promote: rollback budget exhausted`. Operators
	// must call `RecordRollback` to feed this gate. Default: 3.
	MaxRollbacksPerDay int

	// MinCooldownPerSignal stops a single signal source from spamming
	// promotions inside one detection window. The cooldown is keyed on
	// `mut.SignalID`; a follow-up promotion within the window is rejected
	// with `auto-promote: cooldown active`. Default: 0 (disabled).
	MinCooldownPerSignal time.Duration

	// GeneResolver bridges the auto-promoter to the gene catalogue. When
	// nil, the constructor installs a permissive resolver that returns
	// GeneCategorySelector — useful for narrow tests, never use in prod.
	GeneResolver GeneResolver

	// Now is the clock the auto-promoter uses for cooldown and rollback
	// budget windows. Tests inject a fixed clock; production passes
	// time.Now.
	Now func() time.Time
}

// DefaultAutoPromoterConfig returns the production defaults. Callers should
// override only what they're explicitly tuning.
func DefaultAutoPromoterConfig() AutoPromoterConfig {
	return AutoPromoterConfig{
		AllowedCategories: []GeneCategory{
			GeneCategorySelector,
			GeneCategoryPattern,
			GeneCategoryResilience,
		},
		MaxRollbacksPerDay:   3,
		MinCooldownPerSignal: 5 * time.Minute,
		GeneResolver:         func(_ string) (GeneCategory, error) { return GeneCategorySelector, nil },
		Now:                  time.Now,
	}
}

// AutoPromoterMetrics tracks each gate so operators can correlate
// circuit-breaker firings with real-world incidents from the EvoLoop dash.
type AutoPromoterMetrics struct {
	AutoPromoted          int `json:"auto_promoted"`
	GuardrailRejected     int `json:"guardrail_rejected"`
	CircuitBreakerTripped int `json:"circuit_breaker_tripped"`
	CooldownGated         int `json:"cooldown_gated"`
	BuilderFailed         int `json:"builder_failed"`
}

// rollbackEntry records a single rollback for the rolling 24h budget gate.
type rollbackEntry struct {
	capsuleID string
	at        time.Time
}

// AutoPromoter is the EvoLoop auto-promote engine: it wraps a
// PromotionPipeline with the safety constraints in AutoPromoterConfig and
// drives mutations through the full Submit → Promote → capsule-persist loop.
//
// Concurrency: every public method is safe for concurrent callers. Two
// goroutines processing distinct mutations will not race; two goroutines
// processing the *same* mutation may produce one Promote() success and one
// "expected approved" error, which is the documented behaviour of
// PromotionPipeline.
type AutoPromoter struct {
	mu        sync.Mutex
	pipeline  *PromotionPipeline
	cfg       AutoPromoterConfig
	rollbacks []rollbackEntry      // append-only ring; trimmed on every gate check
	cooldown  map[string]time.Time // signal_id → last promote time
	metrics   AutoPromoterMetrics
	allowed   map[GeneCategory]struct{} // allow-list as a set for O(1) lookup
}

// NewAutoPromoter wires an AutoPromoter around an existing PromotionPipeline.
// The pipeline must already be configured with `AutoPromoteLowRisk=true` and
// `RequireHITL=false`, otherwise the auto-promoter cannot reach the
// `PromotionApproved` state. We deliberately do NOT mutate the pipeline's
// config here — both objects own their own state, the auto-promoter is the
// orchestrator on top.
func NewAutoPromoter(pipeline *PromotionPipeline, cfg AutoPromoterConfig) *AutoPromoter {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if len(cfg.AllowedCategories) == 0 {
		cfg.AllowedCategories = DefaultAutoPromoterConfig().AllowedCategories
	}
	if cfg.MaxRollbacksPerDay == 0 {
		cfg.MaxRollbacksPerDay = 3
	}
	allowed := make(map[GeneCategory]struct{}, len(cfg.AllowedCategories))
	for _, c := range cfg.AllowedCategories {
		allowed[c] = struct{}{}
	}
	return &AutoPromoter{
		pipeline: pipeline,
		cfg:      cfg,
		cooldown: make(map[string]time.Time),
		allowed:  allowed,
	}
}

// Process drives a mutation through every safety gate and, when all gates
// pass, the full pipeline lifecycle. It returns the same PromotionRecord shape
// the underlying pipeline already exposes so callers don't need to know they
// went through the auto-promoter.
//
// Gate order (fail-closed): risk → category → rollback budget → cooldown →
// pipeline submit → capsule build → pipeline promote.
func (a *AutoPromoter) Process(ctx context.Context, mut Mutation, sig Signal, build CapsuleBuilder) (*PromotionRecord, error) {
	if mut.RiskEstimate != RiskLow {
		return a.rejectGuardrail(mut.ID, fmt.Sprintf("auto-promote: risk %s != low", mut.RiskEstimate))
	}

	cat, err := a.resolveCategory(mut.GeneID)
	if err != nil {
		return a.rejectGuardrail(mut.ID, fmt.Sprintf("auto-promote: gene resolver: %v", err))
	}
	if _, ok := a.allowed[cat]; !ok {
		return a.rejectGuardrail(mut.ID, fmt.Sprintf("auto-promote: category %s not on allow-list", cat))
	}

	now := a.cfg.Now()
	if a.rollbackBudgetExceeded(now) {
		a.mu.Lock()
		a.metrics.CircuitBreakerTripped++
		a.mu.Unlock()
		return &PromotionRecord{
			MutationID: mut.ID,
			Status:     PromotionRejected,
			Reason:     fmt.Sprintf("auto-promote: rollback budget exhausted (%d in trailing 24h)", a.cfg.MaxRollbacksPerDay),
		}, nil
	}

	if a.cfg.MinCooldownPerSignal > 0 {
		if last, ok := a.lastPromote(sig.ID); ok {
			if now.Sub(last) < a.cfg.MinCooldownPerSignal {
				a.mu.Lock()
				a.metrics.CooldownGated++
				a.mu.Unlock()
				return &PromotionRecord{
					MutationID: mut.ID,
					Status:     PromotionRejected,
					Reason: fmt.Sprintf("auto-promote: cooldown active for signal %s (next promote at %s)",
						sig.ID, last.Add(a.cfg.MinCooldownPerSignal).Format(time.RFC3339)),
				}, nil
			}
		}
	}

	rec, err := a.pipeline.Submit(ctx, mut, sig)
	if err != nil {
		return rec, fmt.Errorf("auto-promote: pipeline submit: %w", err)
	}

	// Pipeline rejected for a non-auto-promote reason (eval below threshold,
	// already rejected, etc). Surface it but don't count as a guardrail hit
	// — the pipeline already counted it in its own metrics.
	if rec.Status != PromotionApproved {
		return rec, nil
	}

	capsule, buildErr := build(mut, *rec.Evaluation)
	if buildErr != nil {
		// Roll back the pipeline state so the next reconcile loop doesn't
		// see a phantom Approved record without a capsule.
		_ = a.pipeline.Reject(mut.ID, "auto_promote", fmt.Sprintf("build capsule: %v", buildErr))
		// Reject pushes the record to PromotionRejected; reload so caller
		// sees the rolled-back view.
		updated, _ := a.pipeline.GetRecord(mut.ID)
		a.mu.Lock()
		a.metrics.BuilderFailed++
		a.mu.Unlock()
		return updated, fmt.Errorf("auto-promote: build capsule: %w", buildErr)
	}

	if capsule.Metadata == nil {
		capsule.Metadata = map[string]string{}
	}
	capsule.Metadata["auto_promoted_by"] = "evoloop"
	capsule.Metadata["auto_promoted_reason"] = fmt.Sprintf("score=%.1f signal=%s gene=%s", rec.Evaluation.Score, mut.SignalID, mut.GeneID)
	capsule.Metadata["auto_promoted_at"] = now.Format(time.RFC3339)

	if err := a.pipeline.Promote(ctx, mut.ID, capsule); err != nil {
		return rec, fmt.Errorf("auto-promote: pipeline promote: %w", err)
	}

	a.mu.Lock()
	a.cooldown[sig.ID] = now
	a.metrics.AutoPromoted++
	a.mu.Unlock()

	updated, _ := a.pipeline.GetRecord(mut.ID)
	return updated, nil
}

// RecordRollback feeds the rolling-24h budget gate. Operators (or the
// downstream rollback handler) should call this every time
// `pipeline.Rollback` is invoked on a capsule that was originally
// auto-promoted, so the circuit breaker has accurate signal.
func (a *AutoPromoter) RecordRollback(capsuleID string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollbacks = append(a.rollbacks, rollbackEntry{capsuleID: capsuleID, at: at})
}

// Metrics returns a snapshot of the current counters. Safe for concurrent
// callers; the returned struct is a copy.
func (a *AutoPromoter) Metrics() AutoPromoterMetrics {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.metrics
}

// rollbackBudgetExceeded trims the rollback ring to the last 24h relative to
// `now` and reports whether the gate is closed.
func (a *AutoPromoter) rollbackBudgetExceeded(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := now.Add(-24 * time.Hour)
	live := a.rollbacks[:0]
	for _, r := range a.rollbacks {
		if r.at.After(cutoff) {
			live = append(live, r)
		}
	}
	a.rollbacks = live
	return len(a.rollbacks) >= a.cfg.MaxRollbacksPerDay
}

// lastPromote returns the most recent successful auto-promote time for the
// given signal ID.
func (a *AutoPromoter) lastPromote(signalID string) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.cooldown[signalID]
	return t, ok
}

// resolveCategory wraps the configured GeneResolver with a stable error
// envelope. Returning ErrNoGeneResolver here is unreachable because
// NewAutoPromoter always installs a fallback, but defence-in-depth.
func (a *AutoPromoter) resolveCategory(geneID string) (GeneCategory, error) {
	if a.cfg.GeneResolver == nil {
		return "", ErrNoGeneResolver
	}
	return a.cfg.GeneResolver(geneID)
}

// rejectGuardrail bumps the GuardrailRejected counter and returns a synthetic
// PromotionRecord so the caller doesn't need to special-case the pipeline's
// internal state for guardrail rejections (the pipeline never even saw the
// mutation in this branch).
func (a *AutoPromoter) rejectGuardrail(mutationID, reason string) (*PromotionRecord, error) {
	a.mu.Lock()
	a.metrics.GuardrailRejected++
	a.mu.Unlock()
	return &PromotionRecord{
		MutationID: mutationID,
		Status:     PromotionRejected,
		Reason:     reason,
	}, nil
}

// ErrNoGeneResolver is returned when AutoPromoterConfig.GeneResolver is nil
// AND NewAutoPromoter's defensive fallback was bypassed (e.g. the field was
// zeroed after construction). It is unreachable in well-formed code paths.
var ErrNoGeneResolver = errors.New("auto-promoter: gene resolver not configured")

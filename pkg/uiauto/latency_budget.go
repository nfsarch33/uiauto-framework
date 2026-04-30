package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// TierBudget defines the latency ceiling for a model tier.
type TierBudget struct {
	Tier       ModelTier
	MaxLatency time.Duration
}

// DefaultTierBudgets returns production-recommended latency budgets.
func DefaultTierBudgets() []TierBudget {
	return []TierBudget{
		{Tier: TierLight, MaxLatency: 500 * time.Millisecond},
		{Tier: TierSmart, MaxLatency: 5 * time.Second},
		{Tier: TierVLM, MaxLatency: 15 * time.Second},
	}
}

// LatencyBudget enforces per-tier latency ceilings and records violations.
type LatencyBudget struct {
	budgets    map[ModelTier]time.Duration
	violations map[ModelTier]*int64
	mu         sync.RWMutex
	logger     *slog.Logger

	violationCounter *prometheus.CounterVec
	latencyHist      *prometheus.HistogramVec
}

// LatencyBudgetOption configures LatencyBudget.
type LatencyBudgetOption func(*LatencyBudget)

// WithLatencyPrometheus registers latency budget metrics.
func WithLatencyPrometheus(reg prometheus.Registerer) LatencyBudgetOption {
	return func(lb *LatencyBudget) {
		lb.violationCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ironclaw_uiauto_latency_violation_total",
			Help: "Count of latency budget violations per tier",
		}, []string{"tier"})
		lb.latencyHist = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ironclaw_uiauto_tier_latency_seconds",
			Help:    "Observed latency per tier in seconds",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30},
		}, []string{"tier"})
		reg.MustRegister(lb.violationCounter, lb.latencyHist)
	}
}

// WithLatencyLogger sets the logger.
func WithLatencyLogger(l *slog.Logger) LatencyBudgetOption {
	return func(lb *LatencyBudget) { lb.logger = l }
}

// NewLatencyBudget creates a LatencyBudget with the given tier budgets.
func NewLatencyBudget(budgets []TierBudget, opts ...LatencyBudgetOption) *LatencyBudget {
	lb := &LatencyBudget{
		budgets:    make(map[ModelTier]time.Duration, len(budgets)),
		violations: make(map[ModelTier]*int64, len(budgets)),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, b := range budgets {
		lb.budgets[b.Tier] = b.MaxLatency
		v := int64(0)
		lb.violations[b.Tier] = &v
	}
	for _, opt := range opts {
		opt(lb)
	}
	return lb
}

// ContextWithDeadline returns a context with a deadline set to the tier's latency budget.
// If the tier has no budget, the parent context is returned unchanged.
func (lb *LatencyBudget) ContextWithDeadline(parent context.Context, tier ModelTier) (context.Context, context.CancelFunc) {
	lb.mu.RLock()
	budget, ok := lb.budgets[tier]
	lb.mu.RUnlock()
	if !ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, budget)
}

// Record records an observed latency for a tier and flags violations.
func (lb *LatencyBudget) Record(tier ModelTier, latency time.Duration) bool {
	lb.mu.RLock()
	budget, hasBudget := lb.budgets[tier]
	vPtr := lb.violations[tier]
	lb.mu.RUnlock()

	if lb.latencyHist != nil {
		lb.latencyHist.WithLabelValues(tier.String()).Observe(latency.Seconds())
	}

	if !hasBudget {
		return true
	}

	if latency > budget {
		if vPtr != nil {
			atomic.AddInt64(vPtr, 1)
		}
		if lb.violationCounter != nil {
			lb.violationCounter.WithLabelValues(tier.String()).Inc()
		}
		lb.logger.Warn("latency budget violated",
			"tier", tier.String(),
			"budget", budget,
			"actual", latency)
		return false
	}
	return true
}

// ViolationCount returns the total violation count for a tier.
func (lb *LatencyBudget) ViolationCount(tier ModelTier) int64 {
	lb.mu.RLock()
	vPtr, ok := lb.violations[tier]
	lb.mu.RUnlock()
	if !ok || vPtr == nil {
		return 0
	}
	return atomic.LoadInt64(vPtr)
}

// FallbackChainConfig defines ordered fallback tiers with availability checks.
type FallbackChainConfig struct {
	Tiers []ModelTier
}

// DefaultFallbackChain returns Light -> Smart -> VLM.
func DefaultFallbackChain() FallbackChainConfig {
	return FallbackChainConfig{
		Tiers: []ModelTier{TierLight, TierSmart, TierVLM},
	}
}

// TierAvailability checks whether a tier can be used right now.
type TierAvailability func(tier ModelTier) bool

// FallbackChain executes actions through a prioritized tier list,
// falling back to the next tier when a tier fails or is unavailable.
type FallbackChain struct {
	config       FallbackChainConfig
	availability TierAvailability
	budget       *LatencyBudget
	handoffs     *InMemoryHandoffStore
	logger       *slog.Logger
}

// FallbackChainOption configures FallbackChain.
type FallbackChainOption func(*FallbackChain)

// WithFallbackBudget attaches latency budget enforcement.
func WithFallbackBudget(lb *LatencyBudget) FallbackChainOption {
	return func(fc *FallbackChain) { fc.budget = lb }
}

// WithFallbackHandoffs attaches a handoff store for recording tier transitions.
func WithFallbackHandoffs(store *InMemoryHandoffStore) FallbackChainOption {
	return func(fc *FallbackChain) { fc.handoffs = store }
}

// WithFallbackLogger sets the logger.
func WithFallbackLogger(l *slog.Logger) FallbackChainOption {
	return func(fc *FallbackChain) { fc.logger = l }
}

// NewFallbackChain creates a FallbackChain.
func NewFallbackChain(config FallbackChainConfig, avail TierAvailability, opts ...FallbackChainOption) *FallbackChain {
	fc := &FallbackChain{
		config:       config,
		availability: avail,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(fc)
	}
	return fc
}

// Execute runs a tier-aware function through the fallback chain.
// The tierFunc receives the current tier and should return an error if the tier fails.
func (fc *FallbackChain) Execute(ctx context.Context, patternID string, tierFunc func(ctx context.Context, tier ModelTier) error) (ModelTier, error) {
	var lastErr error
	for i, tier := range fc.config.Tiers {
		if fc.availability != nil && !fc.availability(tier) {
			fc.logger.Debug("tier unavailable, skipping", "tier", tier.String())
			continue
		}

		tierCtx := ctx
		var cancel context.CancelFunc
		if fc.budget != nil {
			tierCtx, cancel = fc.budget.ContextWithDeadline(ctx, tier)
		} else {
			cancel = func() {}
		}

		start := time.Now()
		err := tierFunc(tierCtx, tier)
		elapsed := time.Since(start)
		cancel()

		if fc.budget != nil {
			fc.budget.Record(tier, elapsed)
		}

		if err == nil {
			if i > 0 && fc.handoffs != nil {
				prevTier := fc.config.Tiers[i-1]
				fc.handoffs.Insert(ModelHandoff{
					PatternID: patternID,
					FromTier:  prevTier.String(),
					ToTier:    tier.String(),
					Reason:    "fallback_success",
					Success:   true,
				})
			}
			return tier, nil
		}

		lastErr = err
		fc.logger.Debug("tier failed, trying next",
			"tier", tier.String(),
			"elapsed", elapsed,
			"error", err)

		if i < len(fc.config.Tiers)-1 && fc.handoffs != nil {
			nextTier := fc.config.Tiers[i+1]
			fc.handoffs.Insert(ModelHandoff{
				PatternID: patternID,
				FromTier:  tier.String(),
				ToTier:    nextTier.String(),
				Reason:    fmt.Sprintf("tier_%s_failed", tier.String()),
				Success:   false,
			})
		}
	}
	return TierVLM, fmt.Errorf("all tiers exhausted: %w", lastErr)
}

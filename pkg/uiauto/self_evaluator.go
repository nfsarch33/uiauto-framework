package uiauto

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// EffectivenessScore summarises how well the UI automation system is performing.
type EffectivenessScore struct {
	Timestamp time.Time `json:"timestamp"`

	// Action metrics
	ActionSuccessRate float64 `json:"action_success_rate"`
	CacheHitRate      float64 `json:"cache_hit_rate"`

	// Healing metrics
	HealSuccessRate     float64            `json:"heal_success_rate"`
	HealFrequency       float64            `json:"heal_frequency_per_hour"`
	HealMethodBreakdown map[string]float64 `json:"heal_method_breakdown"`

	// Model tier distribution
	TierDistribution map[string]float64 `json:"tier_distribution"`
	PromotionRate    float64            `json:"promotion_rate"`
	DemotionRate     float64            `json:"demotion_rate"`

	// Cost estimate (token-based)
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`

	// Overall score (0.0 to 1.0)
	OverallScore float64 `json:"overall_score"`
}

// CostConfig holds per-tier token cost assumptions.
type CostConfig struct {
	LightCostPerAction float64
	SmartCostPerAction float64
	VLMCostPerAction   float64
}

// DefaultCostConfig returns conservative cost estimates.
func DefaultCostConfig() CostConfig {
	return CostConfig{
		LightCostPerAction: 0.0001,
		SmartCostPerAction: 0.003,
		VLMCostPerAction:   0.01,
	}
}

// SelfEvaluator periodically scores the effectiveness of the UI automation
// system and feeds adjustments back to the PatternTracker (confidence decay,
// boost, etc.).
type SelfEvaluator struct {
	mu        sync.Mutex
	agent     *MemberAgent
	costs     CostConfig
	history   []EffectivenessScore
	maxHist   int
	startTime time.Time
}

// NewSelfEvaluator creates an evaluator tied to a MemberAgent.
func NewSelfEvaluator(agent *MemberAgent, costs CostConfig) *SelfEvaluator {
	return &SelfEvaluator{
		agent:     agent,
		costs:     costs,
		maxHist:   1000,
		startTime: time.Now(),
	}
}

// Evaluate computes an EffectivenessScore from the current MemberAgent metrics.
func (e *SelfEvaluator) Evaluate() EffectivenessScore {
	agg := e.agent.Metrics()
	now := time.Now()
	elapsed := now.Sub(e.startTime).Hours()
	if elapsed < 0.001 {
		elapsed = 0.001
	}

	score := EffectivenessScore{
		Timestamp:           now,
		HealMethodBreakdown: make(map[string]float64),
		TierDistribution:    make(map[string]float64),
	}

	totalActions := agg.Executor.SuccessActions + agg.Executor.FailedActions
	if totalActions > 0 {
		score.ActionSuccessRate = float64(agg.Executor.SuccessActions) / float64(totalActions)
	}

	totalCache := agg.Executor.CacheHits + agg.Executor.CacheMisses
	if totalCache > 0 {
		score.CacheHitRate = float64(agg.Executor.CacheHits) / float64(totalCache)
	}

	if agg.Healer.TotalAttempts > 0 {
		score.HealSuccessRate = float64(agg.Healer.SuccessfulHeals) / float64(agg.Healer.TotalAttempts)
		score.HealFrequency = float64(agg.Healer.TotalAttempts) / elapsed
	}

	totalHeals := agg.Healer.FingerprintHeals + agg.Healer.StructuralHeals + agg.Healer.SmartLLMHeals + agg.Healer.VLMHeals
	if totalHeals > 0 {
		score.HealMethodBreakdown["fingerprint"] = float64(agg.Healer.FingerprintHeals) / float64(totalHeals)
		score.HealMethodBreakdown["structural"] = float64(agg.Healer.StructuralHeals) / float64(totalHeals)
		score.HealMethodBreakdown["smart_llm"] = float64(agg.Healer.SmartLLMHeals) / float64(totalHeals)
		score.HealMethodBreakdown["vlm"] = float64(agg.Healer.VLMHeals) / float64(totalHeals)
	}

	totalRouterAttempts := agg.Router.LightAttempts + agg.Router.SmartAttempts + agg.Router.VLMAttempts
	if totalRouterAttempts > 0 {
		score.TierDistribution["light"] = float64(agg.Router.LightAttempts) / float64(totalRouterAttempts)
		score.TierDistribution["smart"] = float64(agg.Router.SmartAttempts) / float64(totalRouterAttempts)
		score.TierDistribution["vlm"] = float64(agg.Router.VLMAttempts) / float64(totalRouterAttempts)
	}
	if agg.Router.ActionCount > 0 {
		score.PromotionRate = float64(agg.Router.Promotions) / float64(agg.Router.ActionCount)
		score.DemotionRate = float64(agg.Router.Demotions) / float64(agg.Router.ActionCount)
	}

	score.EstimatedCostUSD = float64(agg.Router.LightAttempts)*e.costs.LightCostPerAction +
		float64(agg.Router.SmartAttempts)*e.costs.SmartCostPerAction +
		float64(agg.Router.VLMAttempts)*e.costs.VLMCostPerAction

	score.OverallScore = e.computeOverall(score)

	e.mu.Lock()
	e.history = append(e.history, score)
	if len(e.history) > e.maxHist {
		e.history = e.history[len(e.history)-e.maxHist:]
	}
	e.mu.Unlock()

	return score
}

// computeOverall produces a weighted composite score.
func (e *SelfEvaluator) computeOverall(s EffectivenessScore) float64 {
	const (
		wAction  = 0.30
		wCache   = 0.15
		wHeal    = 0.25
		wCheap   = 0.15 // higher = more light-tier usage
		wLowHeal = 0.15 // lower heal frequency = better UI stability
	)

	cheapScore := s.TierDistribution["light"]

	healFreqScore := 1.0
	if s.HealFrequency > 0 {
		healFreqScore = 1.0 / (1.0 + s.HealFrequency/10.0)
	}

	return s.ActionSuccessRate*wAction +
		s.CacheHitRate*wCache +
		s.HealSuccessRate*wHeal +
		cheapScore*wCheap +
		healFreqScore*wLowHeal
}

// FeedbackToTracker adjusts PatternTracker confidence based on the latest
// evaluation. Patterns that are frequently healed get decayed; stable patterns
// get boosted.
func (e *SelfEvaluator) FeedbackToTracker(ctx context.Context) {
	score := e.Latest()
	if score == nil {
		return
	}

	tracker := e.agent.tracker

	if score.HealFrequency > 5 {
		_ = tracker.store.DecayConfidence(ctx, 24*time.Hour, 0.9)
	} else if score.ActionSuccessRate > 0.95 && score.HealFrequency < 1 {
		_ = tracker.store.DecayConfidence(ctx, 0, 1.05)
	}
}

// Latest returns the most recent score, or nil if none.
func (e *SelfEvaluator) Latest() *EffectivenessScore {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.history) == 0 {
		return nil
	}
	s := e.history[len(e.history)-1]
	return &s
}

// History returns the last n scores.
func (e *SelfEvaluator) History(n int) []EffectivenessScore {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n > len(e.history) {
		n = len(e.history)
	}
	out := make([]EffectivenessScore, n)
	copy(out, e.history[len(e.history)-n:])
	return out
}

// SaveHistory persists the evaluation history to a JSON file.
func (e *SelfEvaluator) SaveHistory(path string) error {
	e.mu.Lock()
	data, err := json.MarshalIndent(e.history, "", "  ")
	e.mu.Unlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadHistory reads evaluation history from a JSON file.
func (e *SelfEvaluator) LoadHistory(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var hist []EffectivenessScore
	if err := json.Unmarshal(data, &hist); err != nil {
		return err
	}
	e.mu.Lock()
	e.history = hist
	e.mu.Unlock()
	return nil
}

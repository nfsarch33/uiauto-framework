package budget

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// GPURequirements describes the hardware needed for a local model tier.
type GPURequirements struct {
	VRAMPerGPU_GB  int    `json:"vram_per_gpu_gb"`
	GPUCount       int    `json:"gpu_count"`
	Quantization   string `json:"quantization"` // e.g. "Q4_K_M", "Q6_K", "FP16"
	TensorParallel bool   `json:"tensor_parallel"`
}

// Tier represents a model tier with cost characteristics.
type Tier struct {
	Name         string           `json:"name"`
	CostPerCall  float64          `json:"cost_per_call"` // USD
	AvgLatencyMs int              `json:"avg_latency_ms"`
	SuccessRate  float64          `json:"success_rate"`
	IsLocal      bool             `json:"is_local"`
	GPU          *GPURequirements `json:"gpu,omitempty"`
}

// BudgetConfig defines spending limits and routing rules.
type BudgetConfig struct {
	DailyCapUSD   float64       `json:"daily_cap_usd"`
	MonthlyCapUSD float64       `json:"monthly_cap_usd"`
	PreferLocal   bool          `json:"prefer_local"`
	ResetInterval time.Duration `json:"reset_interval"`
}

// DefaultBudgetConfig returns conservative defaults.
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		DailyCapUSD:   1.00,
		MonthlyCapUSD: 20.00,
		PreferLocal:   true,
		ResetInterval: 24 * time.Hour,
	}
}

// Router selects the cheapest viable tier while respecting budget constraints.
type Router struct {
	tiers  []Tier
	config BudgetConfig
	logger *slog.Logger

	mu           sync.Mutex
	dailySpend   float64
	monthlySpend float64
	lastReset    time.Time
	callCounts   map[string]int
}

// NewRouter creates a budget-aware model router.
func NewRouter(tiers []Tier, cfg BudgetConfig) *Router {
	return &Router{
		tiers:      tiers,
		config:     cfg,
		logger:     slog.Default(),
		lastReset:  time.Now(),
		callCounts: make(map[string]int),
	}
}

// SelectTier returns the best tier for the current budget state.
// Prioritizes local models if PreferLocal is set and budget is tight.
func (r *Router) SelectTier() (*Tier, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.maybeReset()

	// Try tiers in order of cost (cheapest first)
	for i := range r.tiers {
		t := &r.tiers[i]
		if r.config.PreferLocal && t.IsLocal {
			return t, nil
		}
		if r.canAfford(t) {
			return t, nil
		}
	}

	// Fallback: if local tier exists, always allow it
	for i := range r.tiers {
		if r.tiers[i].IsLocal {
			return &r.tiers[i], nil
		}
	}

	return nil, fmt.Errorf("budget exhausted: daily=$%.2f/$%.2f, monthly=$%.2f/$%.2f",
		r.dailySpend, r.config.DailyCapUSD,
		r.monthlySpend, r.config.MonthlyCapUSD)
}

// RecordCall tracks a completed API call's cost.
func (r *Router) RecordCall(tierName string, cost float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dailySpend += cost
	r.monthlySpend += cost
	r.callCounts[tierName]++
}

// Spend returns current spend totals.
func (r *Router) Spend() (daily, monthly float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dailySpend, r.monthlySpend
}

// CallCounts returns per-tier call counts.
func (r *Router) CallCounts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int, len(r.callCounts))
	for k, v := range r.callCounts {
		out[k] = v
	}
	return out
}

// BudgetRemaining returns the remaining daily budget.
func (r *Router) BudgetRemaining() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.config.DailyCapUSD - r.dailySpend
}

func (r *Router) canAfford(t *Tier) bool {
	if t.IsLocal {
		return true
	}
	return r.dailySpend+t.CostPerCall <= r.config.DailyCapUSD &&
		r.monthlySpend+t.CostPerCall <= r.config.MonthlyCapUSD
}

func (r *Router) maybeReset() {
	if time.Since(r.lastReset) >= r.config.ResetInterval {
		r.dailySpend = 0
		r.lastReset = time.Now()
	}
}

// DefaultTiers returns the standard 4-tier model hierarchy.
func DefaultTiers() []Tier {
	return []Tier{
		{Name: "local-qwen", CostPerCall: 0, AvgLatencyMs: 200, SuccessRate: 0.85, IsLocal: true},
		{Name: "gemini-flash", CostPerCall: 0.001, AvgLatencyMs: 150, SuccessRate: 0.92, IsLocal: false},
		{Name: "claude-haiku", CostPerCall: 0.005, AvgLatencyMs: 300, SuccessRate: 0.95, IsLocal: false},
		{Name: "claude-opus", CostPerCall: 0.025, AvgLatencyMs: 500, SuccessRate: 0.99, IsLocal: false},
	}
}

// DefaultLocalTiers returns WSL fleet tiers for 2x RTX 3090 (24GB each).
func DefaultLocalTiers() []Tier {
	return []Tier{
		{
			Name: "wsl-fast", CostPerCall: 0, AvgLatencyMs: 120, SuccessRate: 0.82, IsLocal: true,
			GPU: &GPURequirements{VRAMPerGPU_GB: 7, GPUCount: 1, Quantization: "Q6_K"},
		},
		{
			Name: "wsl-smart", CostPerCall: 0, AvgLatencyMs: 350, SuccessRate: 0.90, IsLocal: true,
			GPU: &GPURequirements{VRAMPerGPU_GB: 18, GPUCount: 1, Quantization: "Q4_K_M"},
		},
		{
			Name: "wsl-powerful", CostPerCall: 0, AvgLatencyMs: 800, SuccessRate: 0.95, IsLocal: true,
			GPU: &GPURequirements{VRAMPerGPU_GB: 19, GPUCount: 2, Quantization: "Q4_K_M", TensorParallel: true},
		},
	}
}

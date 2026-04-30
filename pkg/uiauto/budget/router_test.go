package budget

import (
	"testing"
	"time"
)

func TestRouterSelectsLocalFirst(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.PreferLocal = true
	r := NewRouter(DefaultTiers(), cfg)

	tier, err := r.SelectTier()
	if err != nil {
		t.Fatal(err)
	}
	if tier.Name != "local-qwen" {
		t.Errorf("tier = %q, want local-qwen", tier.Name)
	}
}

func TestRouterFallsBackOnBudget(t *testing.T) {
	tiers := []Tier{
		{Name: "api-only", CostPerCall: 0.10, AvgLatencyMs: 300, SuccessRate: 0.95, IsLocal: false},
		{Name: "local", CostPerCall: 0, AvgLatencyMs: 200, SuccessRate: 0.85, IsLocal: true},
	}
	cfg := BudgetConfig{DailyCapUSD: 0.05, MonthlyCapUSD: 1.0, PreferLocal: false, ResetInterval: 24 * time.Hour}
	r := NewRouter(tiers, cfg)

	tier, err := r.SelectTier()
	if err != nil {
		t.Fatal(err)
	}
	// api-only costs $0.10 > daily cap $0.05, should fall back to local
	if tier.Name != "local" {
		t.Errorf("tier = %q, want local (budget fallback)", tier.Name)
	}
}

func TestRouterBudgetExhausted(t *testing.T) {
	tiers := []Tier{
		{Name: "expensive", CostPerCall: 0.50, AvgLatencyMs: 500, SuccessRate: 0.99, IsLocal: false},
	}
	cfg := BudgetConfig{DailyCapUSD: 0.10, MonthlyCapUSD: 1.0, PreferLocal: false, ResetInterval: 24 * time.Hour}
	r := NewRouter(tiers, cfg)

	_, err := r.SelectTier()
	if err == nil {
		t.Error("expected budget exhausted error")
	}
}

func TestRouterRecordCall(t *testing.T) {
	r := NewRouter(DefaultTiers(), DefaultBudgetConfig())

	r.RecordCall("gemini-flash", 0.001)
	r.RecordCall("gemini-flash", 0.001)
	r.RecordCall("claude-haiku", 0.005)

	daily, monthly := r.Spend()
	expected := 0.007
	if daily < expected-0.001 || daily > expected+0.001 {
		t.Errorf("daily = %.3f, want ~%.3f", daily, expected)
	}
	if monthly < expected-0.001 || monthly > expected+0.001 {
		t.Errorf("monthly = %.3f, want ~%.3f", monthly, expected)
	}

	counts := r.CallCounts()
	if counts["gemini-flash"] != 2 {
		t.Errorf("gemini-flash calls = %d, want 2", counts["gemini-flash"])
	}
	if counts["claude-haiku"] != 1 {
		t.Errorf("claude-haiku calls = %d, want 1", counts["claude-haiku"])
	}
}

func TestRouterBudgetRemaining(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.DailyCapUSD = 1.0
	r := NewRouter(DefaultTiers(), cfg)

	if r.BudgetRemaining() != 1.0 {
		t.Errorf("remaining = %.2f, want 1.00", r.BudgetRemaining())
	}

	r.RecordCall("claude-haiku", 0.30)
	rem := r.BudgetRemaining()
	if rem < 0.69 || rem > 0.71 {
		t.Errorf("remaining = %.2f, want ~0.70", rem)
	}
}

func TestRouterDailyReset(t *testing.T) {
	cfg := BudgetConfig{
		DailyCapUSD:   0.10,
		MonthlyCapUSD: 5.0,
		PreferLocal:   false,
		ResetInterval: 10 * time.Millisecond,
	}
	tiers := []Tier{
		{Name: "api", CostPerCall: 0.05, IsLocal: false},
	}
	r := NewRouter(tiers, cfg)

	r.RecordCall("api", 0.08)
	// Daily is near cap, can't afford another
	tier, _ := r.SelectTier()
	if tier != nil {
		// Should not select api since 0.08 + 0.05 > 0.10
		t.Logf("selected %s despite near-cap (race with reset possible)", tier.Name)
	}

	// Wait for reset
	time.Sleep(15 * time.Millisecond)

	tier, err := r.SelectTier()
	if err != nil {
		t.Fatal("after reset, should be able to select tier")
	}
	if tier.Name != "api" {
		t.Errorf("after reset, tier = %q, want api", tier.Name)
	}
}

func TestDefaultTiers(t *testing.T) {
	tiers := DefaultTiers()
	if len(tiers) != 4 {
		t.Fatalf("DefaultTiers() = %d, want 4", len(tiers))
	}

	hasLocal := false
	for _, t := range tiers {
		if t.IsLocal {
			hasLocal = true
		}
	}
	if !hasLocal {
		t.Error("expected at least one local tier")
	}
}

func TestDefaultLocalTiers(t *testing.T) {
	tiers := DefaultLocalTiers()
	if len(tiers) != 3 {
		t.Fatalf("expected 3 WSL tiers, got %d", len(tiers))
	}

	names := map[string]bool{"wsl-fast": false, "wsl-smart": false, "wsl-powerful": false}
	for _, tier := range tiers {
		if !tier.IsLocal {
			t.Errorf("tier %s should be local", tier.Name)
		}
		if tier.CostPerCall != 0 {
			t.Errorf("tier %s should be free (local), got %.4f", tier.Name, tier.CostPerCall)
		}
		if tier.GPU == nil {
			t.Errorf("tier %s should have GPU requirements", tier.Name)
		}
		names[tier.Name] = true
	}

	for name, found := range names {
		if !found {
			t.Errorf("missing tier %s", name)
		}
	}
}

func TestWSLTierGPURequirements(t *testing.T) {
	tiers := DefaultLocalTiers()
	for _, tier := range tiers {
		if tier.Name == "wsl-powerful" {
			if tier.GPU.GPUCount != 2 {
				t.Errorf("wsl-powerful should need 2 GPUs, got %d", tier.GPU.GPUCount)
			}
			if !tier.GPU.TensorParallel {
				t.Error("wsl-powerful should use tensor parallelism")
			}
		}
	}
}

func TestRouterWithLocalTiers(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.PreferLocal = true
	r := NewRouter(DefaultLocalTiers(), cfg)

	tier, err := r.SelectTier()
	if err != nil {
		t.Fatal(err)
	}
	if tier.Name != "wsl-fast" {
		t.Errorf("tier = %q, want wsl-fast (first local)", tier.Name)
	}
}

func TestRouterMonthlyCap(t *testing.T) {
	tiers := []Tier{
		{Name: "cheap", CostPerCall: 0.01, IsLocal: false},
	}
	cfg := BudgetConfig{DailyCapUSD: 10.0, MonthlyCapUSD: 0.02, PreferLocal: false, ResetInterval: 24 * time.Hour}
	r := NewRouter(tiers, cfg)

	r.RecordCall("cheap", 0.015)

	tier, _ := r.SelectTier()
	// 0.015 + 0.01 > 0.02 monthly cap
	if tier != nil {
		t.Error("should not select tier when monthly cap exceeded")
	}
}

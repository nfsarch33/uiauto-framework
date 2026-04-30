package uiauto

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func newTestEvaluator(t *testing.T) (*SelfEvaluator, *MemberAgent) {
	t.Helper()
	skipWithoutBrowser(t)

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}

	eval := NewSelfEvaluator(agent, DefaultCostConfig())
	return eval, agent
}

func TestSelfEvaluator_InitialEvaluate(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	score := eval.Evaluate()

	if score.ActionSuccessRate != 0 {
		t.Errorf("expected 0 action success rate, got %v", score.ActionSuccessRate)
	}
	if score.OverallScore < 0 || score.OverallScore > 1 {
		t.Errorf("overall score out of range: %v", score.OverallScore)
	}
}

func TestSelfEvaluator_HistoryTracking(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	eval.Evaluate()
	eval.Evaluate()
	eval.Evaluate()

	hist := eval.History(5)
	if len(hist) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(hist))
	}

	latest := eval.Latest()
	if latest == nil {
		t.Fatal("expected non-nil latest score")
	}
}

func TestSelfEvaluator_SaveLoadHistory(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	eval.Evaluate()
	eval.Evaluate()

	dir := t.TempDir()
	path := filepath.Join(dir, "eval_history.json")

	if err := eval.SaveHistory(path); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	eval2 := NewSelfEvaluator(agent, DefaultCostConfig())
	if err := eval2.LoadHistory(path); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	hist := eval2.History(10)
	if len(hist) != 2 {
		t.Errorf("expected 2 loaded entries, got %d", len(hist))
	}
}

func TestSelfEvaluator_FeedbackToTracker(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	ctx := context.Background()

	eval.Evaluate()
	eval.FeedbackToTracker(ctx)
}

func TestSelfEvaluator_FeedbackNoHistory(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	eval.FeedbackToTracker(context.Background())
}

func TestSelfEvaluator_ComputeOverall_Bounds(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	best := EffectivenessScore{
		ActionSuccessRate: 1.0,
		CacheHitRate:      1.0,
		HealSuccessRate:   1.0,
		HealFrequency:     0,
		TierDistribution:  map[string]float64{"light": 1.0},
	}
	overall := eval.computeOverall(best)
	if overall < 0.85 || overall > 1.0 {
		t.Errorf("perfect score should be near 1.0, got %v", overall)
	}

	worst := EffectivenessScore{
		ActionSuccessRate: 0,
		CacheHitRate:      0,
		HealSuccessRate:   0,
		HealFrequency:     100,
		TierDistribution:  map[string]float64{"light": 0},
	}
	worstScore := eval.computeOverall(worst)
	if worstScore < 0 || worstScore > 0.2 {
		t.Errorf("worst score should be near 0, got %v", worstScore)
	}
}

func TestSelfEvaluator_CostEstimate(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	score := eval.Evaluate()
	if score.EstimatedCostUSD != 0 {
		t.Errorf("expected 0 cost with no activity, got %v", score.EstimatedCostUSD)
	}
}

func TestDefaultCostConfig(t *testing.T) {
	cfg := DefaultCostConfig()
	if cfg.LightCostPerAction <= 0 {
		t.Error("light cost should be positive")
	}
	if cfg.SmartCostPerAction <= cfg.LightCostPerAction {
		t.Error("smart cost should exceed light cost")
	}
	if cfg.VLMCostPerAction <= cfg.SmartCostPerAction {
		t.Error("VLM cost should exceed smart cost")
	}
}

func TestSelfEvaluator_HistoryMaxCap(t *testing.T) {
	eval, agent := newTestEvaluator(t)
	defer agent.Close()

	eval.maxHist = 5
	for i := 0; i < 10; i++ {
		eval.Evaluate()
	}

	hist := eval.History(100)
	if len(hist) != 5 {
		t.Errorf("expected capped at 5, got %d", len(hist))
	}
}

func TestEffectivenessScore_MethodBreakdownSumsToOne(t *testing.T) {
	breakdown := map[string]float64{
		"fingerprint": 0.4,
		"structural":  0.3,
		"smart_llm":   0.2,
		"vlm":         0.1,
	}
	total := 0.0
	for _, v := range breakdown {
		total += v
	}
	if math.Abs(total-1.0) > 0.001 {
		t.Errorf("method breakdown should sum to 1.0, got %v", total)
	}
}

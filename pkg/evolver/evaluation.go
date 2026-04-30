package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// EvaluationResult captures the outcome of evaluating a mutation.
type EvaluationResult struct {
	MutationID    string        `json:"mutation_id"`
	Score         float64       `json:"score"`
	MaxScore      float64       `json:"max_score"`
	Pass          bool          `json:"pass"`
	Method        string        `json:"method"`
	Reasoning     string        `json:"reasoning"`
	Duration      time.Duration `json:"duration_ns"`
	EvaluatedAt   time.Time     `json:"evaluated_at"`
	EvaluatorID   string        `json:"evaluator_id"`
	RubricResults []RubricScore `json:"rubric_results,omitempty"`
}

// RubricScore captures the score for a single rubric criterion.
type RubricScore struct {
	Criterion   string  `json:"criterion"`
	Weight      float64 `json:"weight"`
	Score       float64 `json:"score"`
	MaxScore    float64 `json:"max_score"`
	Explanation string  `json:"explanation,omitempty"`
}

// EvaluationConfig controls the evaluation harness.
type EvaluationConfig struct {
	PassThreshold float64
	MaxScore      float64
	Rubric        []RubricCriterion
	LLMAsJudge    bool
}

// RubricCriterion defines a scoring dimension.
type RubricCriterion struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	MaxScore    float64 `json:"max_score"`
}

// DefaultEvaluationConfig returns production defaults.
func DefaultEvaluationConfig() EvaluationConfig {
	return EvaluationConfig{
		PassThreshold: 0.6,
		MaxScore:      10.0,
		LLMAsJudge:    false,
		Rubric: []RubricCriterion{
			{Name: "correctness", Description: "Does the mutation address the root cause?", Weight: 0.4, MaxScore: 10},
			{Name: "safety", Description: "Could the mutation cause regressions or side effects?", Weight: 0.3, MaxScore: 10},
			{Name: "efficiency", Description: "Does the mutation improve latency/cost?", Weight: 0.2, MaxScore: 10},
			{Name: "clarity", Description: "Is the mutation reasoning clear and actionable?", Weight: 0.1, MaxScore: 10},
		},
	}
}

// EvaluationHarness scores mutations using rule-based checks and optional
// LLM-as-judge for quality assessment. Thread-safe for concurrent evaluation.
type EvaluationHarness struct {
	mu      sync.Mutex
	cfg     EvaluationConfig
	llm     LLMProvider
	results []EvaluationResult
}

// NewEvaluationHarness creates a harness with the given config.
func NewEvaluationHarness(cfg EvaluationConfig, llm LLMProvider) *EvaluationHarness {
	if cfg.MaxScore <= 0 {
		cfg.MaxScore = 10.0
	}
	if cfg.PassThreshold <= 0 {
		cfg.PassThreshold = 0.6
	}
	return &EvaluationHarness{
		cfg: cfg,
		llm: llm,
	}
}

// Evaluate scores a mutation and returns the result.
func (h *EvaluationHarness) Evaluate(ctx context.Context, mut Mutation, sig Signal) (EvaluationResult, error) {
	start := time.Now()
	result := EvaluationResult{
		MutationID:  mut.ID,
		MaxScore:    h.cfg.MaxScore,
		Method:      "rule_based",
		EvaluatedAt: time.Now().UTC(),
		EvaluatorID: "evaluation_harness",
	}

	// Rule-based scoring
	rubricResults := h.scoreByRules(mut, sig)
	result.RubricResults = rubricResults

	var weightedSum, totalWeight float64
	for _, r := range rubricResults {
		weightedSum += r.Score * r.Weight
		totalWeight += r.MaxScore * r.Weight
	}
	if totalWeight > 0 {
		result.Score = (weightedSum / totalWeight) * h.cfg.MaxScore
	}

	// LLM-as-judge enhancement
	if h.cfg.LLMAsJudge && h.llm != nil {
		llmResult, err := h.llmJudge(ctx, mut, sig, rubricResults)
		if err == nil {
			result.Method = "llm_as_judge"
			result.Score = (result.Score + llmResult.Score) / 2.0
			result.Reasoning = llmResult.Reasoning
		}
	}

	if result.Reasoning == "" {
		result.Reasoning = h.generateRuleReasoning(rubricResults)
	}

	result.Pass = result.Score >= h.cfg.PassThreshold*h.cfg.MaxScore
	result.Duration = time.Since(start)

	h.mu.Lock()
	h.results = append(h.results, result)
	h.mu.Unlock()

	return result, nil
}

func (h *EvaluationHarness) scoreByRules(mut Mutation, sig Signal) []RubricScore {
	var results []RubricScore

	for _, criterion := range h.cfg.Rubric {
		score := h.scoreCriterion(criterion, mut, sig)
		results = append(results, RubricScore{
			Criterion: criterion.Name,
			Weight:    criterion.Weight,
			Score:     score,
			MaxScore:  criterion.MaxScore,
		})
	}
	return results
}

func (h *EvaluationHarness) scoreCriterion(c RubricCriterion, mut Mutation, sig Signal) float64 {
	switch c.Name {
	case "correctness":
		score := c.MaxScore * 0.5
		if mut.GeneID != "" {
			score += c.MaxScore * 0.3
		}
		if mut.Reasoning != "" && len(mut.Reasoning) > 20 {
			score += c.MaxScore * 0.2
		}
		return score

	case "safety":
		switch mut.RiskEstimate {
		case RiskLow:
			return c.MaxScore * 0.9
		case RiskMedium:
			return c.MaxScore * 0.6
		case RiskHigh:
			return c.MaxScore * 0.3
		}
		return c.MaxScore * 0.5

	case "efficiency":
		score := c.MaxScore * 0.5
		if sig.Type == SignalHighLatency || sig.Type == SignalCostSpike {
			if strings.Contains(strings.ToLower(mut.Reasoning), "cache") ||
				strings.Contains(strings.ToLower(mut.Reasoning), "lighter") ||
				strings.Contains(strings.ToLower(mut.Reasoning), "cheaper") {
				score += c.MaxScore * 0.3
			}
		}
		return score

	case "clarity":
		if len(mut.Reasoning) > 50 {
			return c.MaxScore * 0.8
		}
		if len(mut.Reasoning) > 20 {
			return c.MaxScore * 0.5
		}
		return c.MaxScore * 0.2

	default:
		return c.MaxScore * 0.5
	}
}

func (h *EvaluationHarness) generateRuleReasoning(rubrics []RubricScore) string {
	var parts []string
	for _, r := range rubrics {
		pct := 0.0
		if r.MaxScore > 0 {
			pct = (r.Score / r.MaxScore) * 100
		}
		parts = append(parts, fmt.Sprintf("%s: %.0f%%", r.Criterion, pct))
	}
	return "Rule-based scores: " + strings.Join(parts, ", ")
}

type llmJudgeResult struct {
	Score     float64 `json:"score"`
	Reasoning string  `json:"reasoning"`
}

func (h *EvaluationHarness) llmJudge(ctx context.Context, mut Mutation, sig Signal, rules []RubricScore) (*llmJudgeResult, error) {
	rulesJSON, _ := json.Marshal(rules)
	prompt := fmt.Sprintf(
		"Evaluate this AI agent evolution mutation.\n"+
			"Signal: type=%s severity=%s desc=%q\n"+
			"Mutation: id=%s risk=%s reasoning=%q gene=%s\n"+
			"Rule scores: %s\n"+
			"Score 0-%.0f on overall quality. Reply as JSON: {\"score\": N, \"reasoning\": \"...\"}",
		sig.Type, sig.Severity, sig.Description,
		mut.ID, mut.RiskEstimate, mut.Reasoning, mut.GeneID,
		string(rulesJSON), h.cfg.MaxScore,
	)

	resp, err := h.llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm judge: %w", err)
	}

	var result llmJudgeResult
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return &llmJudgeResult{
			Score:     h.cfg.MaxScore * 0.5,
			Reasoning: resp,
		}, nil
	}
	return &result, nil
}

// Results returns all evaluation results.
func (h *EvaluationHarness) Results() []EvaluationResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]EvaluationResult, len(h.results))
	copy(out, h.results)
	return out
}

// PassRate returns the fraction of evaluations that passed.
func (h *EvaluationHarness) PassRate() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.results) == 0 {
		return 0
	}
	var passed int
	for _, r := range h.results {
		if r.Pass {
			passed++
		}
	}
	return float64(passed) / float64(len(h.results))
}

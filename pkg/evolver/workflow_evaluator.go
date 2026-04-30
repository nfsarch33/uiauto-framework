package evolver

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkflowEvalConfig controls workflow evaluation behaviour.
type WorkflowEvalConfig struct {
	PassThreshold float64
	Rubric        []WorkflowRubricCriterion
	MaxConcurrent int
	Timeout       time.Duration
}

// WorkflowRubricCriterion defines a scoring dimension for workflow evaluation.
type WorkflowRubricCriterion struct {
	Name        string
	Description string
	Weight      float64
	MaxScore    float64
}

// DefaultWorkflowEvalConfig returns production defaults with EvoAgentX-inspired criteria.
func DefaultWorkflowEvalConfig() WorkflowEvalConfig {
	return WorkflowEvalConfig{
		PassThreshold: 0.6,
		MaxConcurrent: 4,
		Timeout:       30 * time.Second,
		Rubric: []WorkflowRubricCriterion{
			{
				Name:        "task_completion",
				Description: "Did the workflow complete its task?",
				Weight:      0.3,
				MaxScore:    10,
			},
			{
				Name:        "latency",
				Description: "Was the workflow fast enough?",
				Weight:      0.2,
				MaxScore:    10,
			},
			{
				Name:        "cost_efficiency",
				Description: "Model tier usage optimality",
				Weight:      0.2,
				MaxScore:    10,
			},
			{
				Name:        "error_rate",
				Description: "Node-level error frequency",
				Weight:      0.15,
				MaxScore:    10,
			},
			{
				Name:        "graph_simplicity",
				Description: "Fewer nodes/edges for same output = better",
				Weight:      0.15,
				MaxScore:    10,
			},
		},
	}
}

// WorkflowEvalResult captures the outcome of evaluating a workflow graph.
type WorkflowEvalResult struct {
	GraphID         string
	GraphVersion    int
	OverallScore    float64
	Pass            bool
	CriterionScores []WorkflowCriterionScore
	Duration        time.Duration
	EvaluatedAt     time.Time
	Notes           string
}

// WorkflowCriterionScore captures the score for a single criterion.
type WorkflowCriterionScore struct {
	Name        string
	Score       float64
	MaxScore    float64
	Explanation string
}

// WorkflowCompareResult captures the outcome of comparing two workflow graphs.
type WorkflowCompareResult struct {
	BaselineScore  float64
	CandidateScore float64
	Improvement    float64 // percentage
	Winner         string  // "baseline" or "candidate"
	Recommendation string
}

// workflowEvalMetrics holds extracted graph metrics for scoring.
type workflowEvalMetrics struct {
	successRate  float64
	avgLatencyMs float64
	avgCost      float64
	errorRate    float64
	nodeCount    int
	edgeCount    int
}

// RubricWorkflowEvaluator scores workflow graphs using a rubric and optional LLM-as-judge.
// Thread-safe for concurrent evaluation. Implements WorkflowEvaluator for use with WorkflowOptimizer.
type RubricWorkflowEvaluator struct {
	cfg     WorkflowEvalConfig
	llm     LLMProvider
	mu      sync.Mutex
	history []WorkflowEvalResult
}

// Ensure RubricWorkflowEvaluator implements WorkflowEvaluator.
var _ WorkflowEvaluator = (*RubricWorkflowEvaluator)(nil)

// NewWorkflowEvaluator creates a rubric-based evaluator with the given config and optional LLM.
func NewWorkflowEvaluator(cfg WorkflowEvalConfig, llm LLMProvider) *RubricWorkflowEvaluator {
	if cfg.PassThreshold <= 0 {
		cfg.PassThreshold = 0.6
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if len(cfg.Rubric) == 0 {
		cfg = DefaultWorkflowEvalConfig()
	}
	return &RubricWorkflowEvaluator{
		cfg: cfg,
		llm: llm,
	}
}

// Evaluate implements WorkflowEvaluator for use with WorkflowOptimizer.
// Returns the overall score; use EvaluateFull for the full result.
func (e *RubricWorkflowEvaluator) Evaluate(ctx context.Context, graph *WorkflowGraph) (float64, error) {
	res, err := e.EvaluateFull(ctx, graph)
	if err != nil {
		return 0, err
	}
	return res.OverallScore, nil
}

// EvaluateFull scores a workflow graph using the rubric and returns the full result.
// Uses graph.Metrics for latency/cost/error_rate; node/edge counts for simplicity;
// SuccessRate for task_completion. Optional LLM-as-judge for qualitative assessment.
func (e *RubricWorkflowEvaluator) EvaluateFull(ctx context.Context, graph *WorkflowGraph) (*WorkflowEvalResult, error) {
	if graph == nil {
		return nil, fmt.Errorf("workflow evaluator: graph is nil")
	}

	// Extract metrics under read lock for consistent scoring
	graph.mu.RLock()
	successRate := 0.0
	if graph.Metrics.TotalRuns > 0 {
		successRate = float64(graph.Metrics.SuccessRuns) / float64(graph.Metrics.TotalRuns)
	}
	errorRate := 0.0
	if graph.Metrics.TotalRuns > 0 {
		errorRate = float64(graph.Metrics.FailedRuns) / float64(graph.Metrics.TotalRuns)
	}
	metrics := workflowEvalMetrics{
		successRate:  successRate,
		avgLatencyMs: graph.Metrics.AvgLatencyMs,
		avgCost:      graph.Metrics.AvgCost,
		errorRate:    errorRate,
		nodeCount:    len(graph.Nodes),
		edgeCount:    len(graph.Edges),
	}
	graph.mu.RUnlock()

	start := time.Now()
	result := &WorkflowEvalResult{
		GraphID:      graph.ID,
		GraphVersion: graph.Version,
		EvaluatedAt:  time.Now().UTC(),
	}

	// Score each criterion
	for _, c := range e.cfg.Rubric {
		score, explanation := e.scoreCriterion(c, metrics)
		result.CriterionScores = append(result.CriterionScores, WorkflowCriterionScore{
			Name:        c.Name,
			Score:       score,
			MaxScore:    c.MaxScore,
			Explanation: explanation,
		})
	}

	// Compute weighted overall score (normalised 0-10)
	var weightedSum, totalWeight float64
	for i, c := range e.cfg.Rubric {
		if i >= len(result.CriterionScores) {
			break
		}
		s := result.CriterionScores[i]
		weightedSum += s.Score * c.Weight
		totalWeight += c.MaxScore * c.Weight
	}
	if totalWeight > 0 {
		result.OverallScore = (weightedSum / totalWeight) * 10.0
	}

	// LLM-as-judge enhancement when configured
	if e.llm != nil && e.cfg.Timeout > 0 {
		llmCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
		defer cancel()
		if notes := e.llmJudge(llmCtx, graph, metrics, result); notes != "" {
			result.Notes = notes
		}
	}

	result.Pass = result.OverallScore >= e.cfg.PassThreshold*10.0
	result.Duration = time.Since(start)

	e.mu.Lock()
	e.history = append(e.history, *result)
	e.mu.Unlock()

	return result, nil
}

func (e *RubricWorkflowEvaluator) scoreCriterion(c WorkflowRubricCriterion, m workflowEvalMetrics) (score float64, detail string) {
	switch c.Name {
	case "task_completion":
		score := m.successRate * c.MaxScore
		return score, fmt.Sprintf("success_rate=%.2f", m.successRate)

	case "latency":
		// Lower latency = higher score. Assume 5000ms is poor, 500ms is good.
		var score float64
		switch {
		case m.avgLatencyMs <= 500:
			score = c.MaxScore * 0.95
		case m.avgLatencyMs <= 2000:
			score = c.MaxScore * 0.7
		case m.avgLatencyMs <= 5000:
			score = c.MaxScore * 0.4
		default:
			score = c.MaxScore * 0.5
		}
		return score, fmt.Sprintf("avg_latency_ms=%.0f", m.avgLatencyMs)

	case "cost_efficiency":
		var score float64
		switch {
		case m.avgCost <= 0.01:
			score = c.MaxScore * 0.95
		case m.avgCost <= 0.05:
			score = c.MaxScore * 0.75
		case m.avgCost <= 0.10:
			score = c.MaxScore * 0.5
		default:
			score = c.MaxScore * 0.2
		}
		return score, fmt.Sprintf("cost_usd=%.4f", m.avgCost)

	case "error_rate":
		// Lower error rate = higher score
		score := (1.0 - m.errorRate) * c.MaxScore
		return score, fmt.Sprintf("error_rate=%.2f", m.errorRate)

	case "graph_simplicity":
		// Fewer nodes+edges = better. Assume 20 total is baseline, 5 is excellent.
		total := m.nodeCount + m.edgeCount
		var score float64
		switch {
		case total <= 5:
			score = c.MaxScore * 0.95
		case total <= 10:
			score = c.MaxScore * 0.8
		case total <= 20:
			score = c.MaxScore * 0.6
		case total <= 50:
			score = c.MaxScore * 0.4
		default:
			score = c.MaxScore * 0.5
		}
		return score, fmt.Sprintf("nodes=%d edges=%d", m.nodeCount, m.edgeCount)

	default:
		return c.MaxScore * 0.5, "default"
	}
}

func (e *RubricWorkflowEvaluator) llmJudge(ctx context.Context, g *WorkflowGraph, m workflowEvalMetrics, res *WorkflowEvalResult) string {
	if e.llm == nil {
		return ""
	}
	prompt := fmt.Sprintf(
		"Evaluate this workflow graph. ID=%s v%d. Metrics: success=%.2f latency_ms=%.0f cost=%.4f error=%.2f. Nodes=%d Edges=%d. "+
			"Scores: %v. Provide a brief qualitative assessment (1-2 sentences) or empty string if not applicable.",
		g.ID, g.Version, m.successRate, m.avgLatencyMs, m.avgCost, m.errorRate, m.nodeCount, m.edgeCount,
		res.CriterionScores,
	)
	resp, err := e.llm.Complete(ctx, prompt)
	if err != nil {
		return ""
	}
	return resp
}

// CompareGraphs compares baseline and candidate graphs and returns a recommendation.
func (e *RubricWorkflowEvaluator) CompareGraphs(ctx context.Context, baseline, candidate *WorkflowGraph) (*WorkflowCompareResult, error) {
	if baseline == nil || candidate == nil {
		return nil, fmt.Errorf("workflow evaluator: baseline and candidate must be non-nil")
	}

	baseRes, err := e.EvaluateFull(ctx, baseline)
	if err != nil {
		return nil, fmt.Errorf("evaluate baseline: %w", err)
	}

	candRes, err := e.EvaluateFull(ctx, candidate)
	if err != nil {
		return nil, fmt.Errorf("evaluate candidate: %w", err)
	}

	out := &WorkflowCompareResult{
		BaselineScore:  baseRes.OverallScore,
		CandidateScore: candRes.OverallScore,
	}

	if baseRes.OverallScore > 0 {
		out.Improvement = ((candRes.OverallScore - baseRes.OverallScore) / baseRes.OverallScore) * 100
	}

	switch {
	case candRes.OverallScore > baseRes.OverallScore:
		out.Winner = "candidate"
		out.Recommendation = fmt.Sprintf("Candidate improves over baseline by %.1f%%. Consider promoting.", out.Improvement)
	case baseRes.OverallScore > candRes.OverallScore:
		out.Winner = "baseline"
		out.Recommendation = fmt.Sprintf("Baseline outperforms candidate by %.1f%%. Keep baseline.", -out.Improvement)
	default:
		out.Winner = "baseline"
		out.Recommendation = "Scores are equal. Prefer baseline for stability."
	}

	return out, nil
}

// History returns all evaluation results. Caller must not mutate the slice.
func (e *RubricWorkflowEvaluator) History() []WorkflowEvalResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]WorkflowEvalResult, len(e.history))
	copy(out, e.history)
	return out
}

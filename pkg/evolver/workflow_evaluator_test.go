package evolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// graphWithGoodMetrics returns a graph with high success rate, low latency, low cost.
func graphWithGoodMetrics() *WorkflowGraph {
	g := newTestGraph()
	for i := 0; i < 9; i++ {
		g.RecordRun(true, 400, 0.005)
	}
	g.RecordRun(false, 500, 0.01)
	return g
}

// graphWithBadMetrics returns a graph with low success rate, high latency, high cost.
func graphWithBadMetrics() *WorkflowGraph {
	g := newTestGraph()
	for i := 0; i < 3; i++ {
		g.RecordRun(true, 4000, 0.15)
	}
	for i := 0; i < 7; i++ {
		g.RecordRun(false, 6000, 0.20)
	}
	return g
}

func TestDefaultWorkflowEvalConfig(t *testing.T) {
	cfg := DefaultWorkflowEvalConfig()
	assert.Equal(t, 0.6, cfg.PassThreshold)
	assert.Equal(t, 4, cfg.MaxConcurrent)
	assert.NotZero(t, cfg.Timeout)
	assert.Len(t, cfg.Rubric, 5)

	names := make(map[string]bool)
	for _, c := range cfg.Rubric {
		names[c.Name] = true
	}
	assert.True(t, names["task_completion"])
	assert.True(t, names["latency"])
	assert.True(t, names["cost_efficiency"])
	assert.True(t, names["error_rate"])
	assert.True(t, names["graph_simplicity"])
}

func TestWorkflowEvaluator_Evaluate(t *testing.T) {
	cfg := DefaultWorkflowEvalConfig()
	eval := NewWorkflowEvaluator(cfg, &mockLLM{})

	// Good metrics -> higher score
	goodGraph := graphWithGoodMetrics()
	score, err := eval.Evaluate(context.Background(), goodGraph)
	assert.NoError(t, err)
	assert.Greater(t, score, 5.0)
	assert.LessOrEqual(t, score, 10.0)

	// Bad metrics -> lower score
	badGraph := graphWithBadMetrics()
	badScore, err := eval.Evaluate(context.Background(), badGraph)
	assert.NoError(t, err)
	assert.Less(t, badScore, score)

	// Nil graph
	_, err = eval.Evaluate(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorkflowEvaluator_EvaluateFull(t *testing.T) {
	cfg := DefaultWorkflowEvalConfig()
	eval := NewWorkflowEvaluator(cfg, &mockLLM{})

	goodGraph := graphWithGoodMetrics()
	res, err := eval.EvaluateFull(context.Background(), goodGraph)
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, goodGraph.ID, res.GraphID)
	assert.Equal(t, goodGraph.Version, res.GraphVersion)
	assert.Len(t, res.CriterionScores, 5)
	assert.Greater(t, res.OverallScore, 0.0)
	assert.False(t, res.EvaluatedAt.IsZero())
	assert.NotZero(t, res.Duration)
}

func TestWorkflowEvaluator_CompareGraphs(t *testing.T) {
	cfg := DefaultWorkflowEvalConfig()
	eval := NewWorkflowEvaluator(cfg, &mockLLM{})

	baseline := graphWithBadMetrics()
	candidate := graphWithGoodMetrics()

	// Candidate should win (better metrics)
	result, err := eval.CompareGraphs(context.Background(), baseline, candidate)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "candidate", result.Winner)
	assert.Greater(t, result.CandidateScore, result.BaselineScore)
	assert.Greater(t, result.Improvement, 0.0)
	assert.Contains(t, result.Recommendation, "Candidate")

	// Baseline wins when we swap
	result2, err := eval.CompareGraphs(context.Background(), candidate, baseline)
	assert.NoError(t, err)
	assert.Equal(t, "baseline", result2.Winner)
	assert.Contains(t, result2.Recommendation, "Baseline")

	// Nil inputs
	_, err = eval.CompareGraphs(context.Background(), nil, candidate)
	assert.Error(t, err)
	_, err = eval.CompareGraphs(context.Background(), baseline, nil)
	assert.Error(t, err)
}

func TestWorkflowEvaluator_History(t *testing.T) {
	cfg := DefaultWorkflowEvalConfig()
	eval := NewWorkflowEvaluator(cfg, &mockLLM{})

	history := eval.History()
	assert.Empty(t, history)

	g := graphWithGoodMetrics()
	_, _ = eval.EvaluateFull(context.Background(), g)
	_, _ = eval.EvaluateFull(context.Background(), g)

	history = eval.History()
	assert.Len(t, history, 2)
	assert.Equal(t, g.ID, history[0].GraphID)
	assert.Equal(t, g.ID, history[1].GraphID)

	// Caller must not mutate - we copy, so mutating our copy is safe
	history[0].OverallScore = 999
	history2 := eval.History()
	assert.NotEqual(t, 999.0, history2[0].OverallScore)
}

package evolver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultOptimizationConfig(t *testing.T) {
	cfg := DefaultOptimizationConfig()
	assert.Equal(t, StrategyHillClimb, cfg.Strategy)
	assert.Equal(t, 10, cfg.MaxIterations)
	assert.Equal(t, 0.05, cfg.ImprovementThreshold)
	assert.Equal(t, 0.1, cfg.ExplorationRate)
	assert.Equal(t, 3, cfg.MutationsPerIteration)
	assert.NotZero(t, cfg.Timeout)
	assert.NotNil(t, cfg.Logger)
}

func TestWorkflowOptimizer_Optimize_HillClimb(t *testing.T) {
	cfg := DefaultOptimizationConfig()
	cfg.MaxIterations = 5
	cfg.MutationsPerIteration = 2
	cfg.Timeout = 30 * time.Second

	eval := NewWorkflowEvaluator(DefaultWorkflowEvalConfig(), &mockLLM{})
	opt := NewWorkflowOptimizer(cfg, eval, &mockLLM{})

	g := newTestGraph()
	g.RecordRun(true, 500, 0.02)

	result, err := opt.Optimize(context.Background(), g)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.BestGraph)
	assert.GreaterOrEqual(t, result.BestScore, 0.0)
	assert.LessOrEqual(t, result.Iterations, 5)
	assert.NotZero(t, result.Duration)
	assert.Len(t, result.ImprovementHistory, result.Iterations+1)
}

func TestWorkflowOptimizer_Optimize_Convergence(t *testing.T) {
	cfg := DefaultOptimizationConfig()
	cfg.MaxIterations = 20
	cfg.MutationsPerIteration = 1
	cfg.ImprovementThreshold = 0.5 // High threshold so we converge quickly
	cfg.Timeout = 30 * time.Second

	eval := NewWorkflowEvaluator(DefaultWorkflowEvalConfig(), &mockLLM{})
	opt := NewWorkflowOptimizer(cfg, eval, &mockLLM{})

	g := newTestGraph()
	g.RecordRun(true, 300, 0.01)
	g.RecordRun(true, 350, 0.01)

	result, err := opt.Optimize(context.Background(), g)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// May converge early due to no improvement for 3 iterations
	if result.Converged {
		assert.Less(t, result.Iterations, cfg.MaxIterations)
	}
}

func TestWorkflowOptimizer_Optimize_ContextCancel(t *testing.T) {
	cfg := DefaultOptimizationConfig()
	cfg.MaxIterations = 100
	cfg.MutationsPerIteration = 2
	cfg.Timeout = 100 * time.Millisecond

	eval := NewWorkflowEvaluator(DefaultWorkflowEvalConfig(), &mockLLM{})
	opt := NewWorkflowOptimizer(cfg, eval, &mockLLM{})

	g := newTestGraph()
	g.RecordRun(true, 500, 0.02)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := opt.Optimize(ctx, g)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.BestGraph)
}

func TestWorkflowOptimizer_Optimize_ContextTimeout(t *testing.T) {
	cfg := DefaultOptimizationConfig()
	cfg.MaxIterations = 100
	cfg.MutationsPerIteration = 2
	cfg.Timeout = 50 * time.Millisecond

	eval := NewWorkflowEvaluator(DefaultWorkflowEvalConfig(), &mockLLM{})
	opt := NewWorkflowOptimizer(cfg, eval, &mockLLM{})

	g := newTestGraph()
	g.RecordRun(true, 500, 0.02)

	result, err := opt.Optimize(context.Background(), g)
	// May complete or timeout depending on speed
	if err != nil {
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	}
	assert.NotNil(t, result)
}

func TestWorkflowOptimizer_History(t *testing.T) {
	cfg := DefaultOptimizationConfig()
	cfg.MaxIterations = 2
	cfg.Timeout = 10 * time.Second

	eval := NewWorkflowEvaluator(DefaultWorkflowEvalConfig(), &mockLLM{})
	opt := NewWorkflowOptimizer(cfg, eval, &mockLLM{})

	history := opt.History()
	assert.Empty(t, history)

	g := newTestGraph()
	g.RecordRun(true, 500, 0.02)
	_, _ = opt.Optimize(context.Background(), g)
	_, _ = opt.Optimize(context.Background(), g)

	history = opt.History()
	assert.Len(t, history, 2)
	assert.NotNil(t, history[0].BestGraph)
	assert.NotNil(t, history[1].BestGraph)
}

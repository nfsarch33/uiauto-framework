package evolver

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

// OptimizationStrategy selects the search strategy for the optimization loop.
type OptimizationStrategy string

// OptimizationStrategy values.
const (
	StrategyHillClimb    OptimizationStrategy = "hill_climb"
	StrategyRandomSearch OptimizationStrategy = "random_search"
	StrategyBandit       OptimizationStrategy = "bandit"
)

// OptimizationConfig controls the optimization loop behaviour.
type OptimizationConfig struct {
	Strategy              OptimizationStrategy
	MaxIterations         int
	ImprovementThreshold  float64 // minimum improvement % to accept (e.g. 0.05 = 5%)
	ExplorationRate       float64 // 0.0-1.0, for bandit
	MutationsPerIteration int
	Timeout               time.Duration
	Logger                *slog.Logger
}

// DefaultOptimizationConfig returns HillClimb, 10 iterations, 5% threshold.
func DefaultOptimizationConfig() OptimizationConfig {
	return OptimizationConfig{
		Strategy:              StrategyHillClimb,
		MaxIterations:         10,
		ImprovementThreshold:  0.05,
		ExplorationRate:       0.1,
		MutationsPerIteration: 3,
		Timeout:               5 * time.Minute,
		Logger:                slog.Default(),
	}
}

// OptimizationResult holds the outcome of an optimization run.
type OptimizationResult struct {
	BestGraph          *WorkflowGraph
	BestScore          float64
	Iterations         int
	TotalMutations     int
	ImprovementHistory []float64
	Duration           time.Duration
	Converged          bool
}

// WorkflowEvaluator scores a workflow graph. Higher is better.
// Implemented by RubricWorkflowEvaluator and compatible evaluators.
type WorkflowEvaluator interface {
	Evaluate(ctx context.Context, graph *WorkflowGraph) (float64, error)
}

// WorkflowOptimizer runs an EvoAgentX-inspired optimization loop over workflow graphs.
type WorkflowOptimizer struct {
	cfg       OptimizationConfig
	evaluator WorkflowEvaluator
	llm       LLMProvider
	mu        sync.Mutex
	runs      []OptimizationResult
}

// NewWorkflowOptimizer creates an optimizer with the given config, evaluator, and LLM.
func NewWorkflowOptimizer(cfg OptimizationConfig, evaluator WorkflowEvaluator, llm LLMProvider) *WorkflowOptimizer {
	return &WorkflowOptimizer{
		cfg:       cfg,
		evaluator: evaluator,
		llm:       llm,
		runs:      nil,
	}
}

// Optimize runs the main optimization loop on the given graph.
func (o *WorkflowOptimizer) Optimize(ctx context.Context, graph *WorkflowGraph) (*OptimizationResult, error) {
	start := time.Now()
	if o.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
		defer cancel()
	}

	working := graph.Clone()
	baseline, err := o.evaluator.Evaluate(ctx, working)
	if err != nil {
		return nil, fmt.Errorf("baseline evaluation: %w", err)
	}

	result := &OptimizationResult{
		BestGraph:          working.Clone(),
		BestScore:          baseline,
		Iterations:         0,
		TotalMutations:     0,
		ImprovementHistory: []float64{baseline},
		Duration:           0,
		Converged:          false,
	}

	noImprovementCount := 0
	bestSoFar := baseline

	for iter := 0; iter < o.cfg.MaxIterations; iter++ {
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			o.recordRun(result)
			return result, ctx.Err()
		default:
		}

		mutations := o.generateMutations(working)
		if len(mutations) == 0 {
			break
		}

		accepted := false
		for _, mut := range mutations {
			candidate := working.Clone()
			if err := o.applyMutation(candidate, mut); err != nil {
				if o.cfg.Logger != nil {
					o.cfg.Logger.Debug("apply mutation failed", "err", err, "mutation", mut.Type)
				}
				continue
			}

			score, err := o.evaluator.Evaluate(ctx, candidate)
			if err != nil {
				continue
			}

			result.TotalMutations++
			improvement := 0.0
			if bestSoFar > 0 {
				improvement = (score - bestSoFar) / bestSoFar
			} else if score > bestSoFar {
				improvement = 1.0
			}

			if improvement >= o.cfg.ImprovementThreshold {
				working = candidate
				bestSoFar = score
				result.BestGraph = candidate.Clone()
				result.BestScore = score
				accepted = true
				noImprovementCount = 0
				break
			}
		}

		result.Iterations = iter + 1
		result.ImprovementHistory = append(result.ImprovementHistory, bestSoFar)

		if !accepted {
			noImprovementCount++
			if noImprovementCount >= 3 {
				result.Converged = true
				break
			}
		}
	}

	result.Duration = time.Since(start)
	o.recordRun(result)
	return result, nil
}

// generateMutations creates candidate mutations based on the configured strategy.
func (o *WorkflowOptimizer) generateMutations(graph *WorkflowGraph) []WorkflowMutation {
	graph.mu.RLock()
	nodeIDs := make([]string, 0, len(graph.Nodes))
	for id := range graph.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	graph.mu.RUnlock()

	if len(nodeIDs) == 0 {
		return nil
	}

	var mutations []WorkflowMutation
	n := o.cfg.MutationsPerIteration
	if n <= 0 {
		n = 1
	}

	explore := false
	switch o.cfg.Strategy {
	case StrategyRandomSearch:
		explore = true
	case StrategyBandit:
		explore = rand.Float64() < o.cfg.ExplorationRate
	case StrategyHillClimb:
		explore = false
	}

	for i := 0; i < n; i++ {
		nodeID := nodeIDs[rand.IntN(len(nodeIDs))]
		mut := o.pickMutation(graph, nodeID, explore)
		if mut.ID != "" {
			mutations = append(mutations, mut)
		}
	}

	return mutations
}

func (o *WorkflowOptimizer) pickMutation(graph *WorkflowGraph, nodeID string, explore bool) WorkflowMutation {
	types := []WorkflowMutationType{MutatePrompt, MutateModelTier, SwapTool}
	idx := rand.IntN(len(types))
	if explore {
		idx = rand.IntN(len(types))
	}

	t := types[idx]
	now := time.Now().UTC()
	return WorkflowMutation{
		ID:        fmt.Sprintf("opt-mut-%d", now.UnixNano()),
		GraphID:   graph.ID,
		Type:      t,
		NodeID:    nodeID,
		Reason:    "optimization loop",
		CreatedAt: now,
	}
}

// applyMutation applies a mutation to a graph clone.
func (o *WorkflowOptimizer) applyMutation(graph *WorkflowGraph, mut WorkflowMutation) error {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	node, ok := graph.Nodes[mut.NodeID]
	if !ok {
		return fmt.Errorf("node %q not found", mut.NodeID)
	}

	switch mut.Type {
	case MutatePrompt:
		node.PromptTemplate += "\n\n[Optimized: focus on key outputs.]"
	case MutateModelTier:
		tiers := []string{"fast", "balanced", "powerful", "vlm"}
		idx := rand.IntN(len(tiers))
		node.ModelTier = tiers[idx]
	case SwapTool:
		if len(node.ToolIDs) > 0 {
			swapIdx := rand.IntN(len(node.ToolIDs))
			node.ToolIDs[swapIdx] = "tool_" + fmt.Sprintf("%d", rand.IntN(100))
		}
	default:
		return fmt.Errorf("unsupported mutation type %q", mut.Type)
	}

	graph.UpdatedAt = time.Now().UTC()
	return nil
}

func (o *WorkflowOptimizer) recordRun(r *OptimizationResult) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.runs = append(o.runs, *r)
}

// History returns all optimization runs.
func (o *WorkflowOptimizer) History() []OptimizationResult {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]OptimizationResult, len(o.runs))
	copy(out, o.runs)
	return out
}

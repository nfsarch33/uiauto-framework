package evolver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// TextGradConfig controls TextGrad-style prompt optimization.
type TextGradConfig struct {
	MaxIterations        int
	LearningRate         float64 // 0..1, controls magnitude of prompt changes
	ImprovementThreshold float64
	Logger               *slog.Logger
}

// DefaultTextGradConfig returns sensible defaults for prompt tuning.
func DefaultTextGradConfig() TextGradConfig {
	return TextGradConfig{
		MaxIterations:        5,
		LearningRate:         0.3,
		ImprovementThreshold: 0.02,
		Logger:               slog.Default(),
	}
}

// PromptGradient represents the "gradient" feedback for a prompt.
type PromptGradient struct {
	NodeID     string   `json:"node_id"`
	Score      float64  `json:"score"`
	Feedback   string   `json:"feedback"`
	Weaknesses []string `json:"weaknesses"`
	Suggestion string   `json:"suggestion"`
}

// TextGradResult captures one TextGrad optimization run.
type TextGradResult struct {
	NodeID          string           `json:"node_id"`
	OriginalPrompt  string           `json:"original_prompt"`
	OptimizedPrompt string           `json:"optimized_prompt"`
	OriginalScore   float64          `json:"original_score"`
	FinalScore      float64          `json:"final_score"`
	Iterations      int              `json:"iterations"`
	Gradients       []PromptGradient `json:"gradients"`
	Duration        time.Duration    `json:"duration_ns"`
	Improved        bool             `json:"improved"`
}

// TextGradOptimizer applies gradient-like feedback to iteratively improve prompts.
type TextGradOptimizer struct {
	cfg       TextGradConfig
	evaluator WorkflowEvaluator
	llm       LLMProvider
}

// NewTextGradOptimizer creates a TextGrad prompt optimizer.
func NewTextGradOptimizer(cfg TextGradConfig, evaluator WorkflowEvaluator, llm LLMProvider) *TextGradOptimizer {
	return &TextGradOptimizer{cfg: cfg, evaluator: evaluator, llm: llm}
}

// OptimizePrompt applies TextGrad to a single node's prompt within the graph.
func (tg *TextGradOptimizer) OptimizePrompt(ctx context.Context, graph *WorkflowGraph, nodeID string) (*TextGradResult, error) {
	start := time.Now()

	graph.mu.RLock()
	node, ok := graph.Nodes[nodeID]
	graph.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("node %q not found", nodeID)
	}

	result := &TextGradResult{
		NodeID:         nodeID,
		OriginalPrompt: node.PromptTemplate,
	}

	baseline, err := tg.evaluator.Evaluate(ctx, graph)
	if err != nil {
		return nil, fmt.Errorf("baseline evaluation: %w", err)
	}
	result.OriginalScore = baseline

	currentPrompt := node.PromptTemplate
	bestScore := baseline

	for iter := 0; iter < tg.cfg.MaxIterations; iter++ {
		gradient := tg.computeGradient(ctx, graph, nodeID, currentPrompt, bestScore)
		result.Gradients = append(result.Gradients, gradient)

		newPrompt := tg.applyGradient(currentPrompt, gradient)
		if newPrompt == currentPrompt {
			break
		}

		graph.mu.Lock()
		node.PromptTemplate = newPrompt
		graph.mu.Unlock()

		score, err := tg.evaluator.Evaluate(ctx, graph)
		if err != nil {
			graph.mu.Lock()
			node.PromptTemplate = currentPrompt
			graph.mu.Unlock()
			continue
		}

		improvement := 0.0
		if bestScore > 0 {
			improvement = (score - bestScore) / bestScore
		}

		if improvement >= tg.cfg.ImprovementThreshold {
			currentPrompt = newPrompt
			bestScore = score
			result.Iterations = iter + 1
		} else {
			graph.mu.Lock()
			node.PromptTemplate = currentPrompt
			graph.mu.Unlock()
		}
	}

	result.OptimizedPrompt = currentPrompt
	result.FinalScore = bestScore
	result.Duration = time.Since(start)
	result.Improved = bestScore > baseline
	return result, nil
}

// computeGradient simulates the "backward pass" by analyzing prompt weaknesses.
func (tg *TextGradOptimizer) computeGradient(_ context.Context, _ *WorkflowGraph, nodeID, prompt string, score float64) PromptGradient {
	var weaknesses []string
	var suggestion string

	if !strings.Contains(strings.ToLower(prompt), "step") &&
		!strings.Contains(strings.ToLower(prompt), "first") {
		weaknesses = append(weaknesses, "lacks step-by-step structure")
		suggestion = "Add numbered steps for clarity."
	}
	if !strings.Contains(strings.ToLower(prompt), "output") &&
		!strings.Contains(strings.ToLower(prompt), "return") &&
		!strings.Contains(strings.ToLower(prompt), "respond") {
		weaknesses = append(weaknesses, "unclear output format")
		suggestion = "Specify the expected output format."
	}
	if len(prompt) < 50 {
		weaknesses = append(weaknesses, "prompt too brief")
		suggestion = "Expand with context and constraints."
	}
	if len(prompt) > 2000 {
		weaknesses = append(weaknesses, "prompt too verbose")
		suggestion = "Trim redundant instructions."
	}

	feedback := fmt.Sprintf("Score: %.2f. Weaknesses: %d found.", score, len(weaknesses))

	return PromptGradient{
		NodeID:     nodeID,
		Score:      score,
		Feedback:   feedback,
		Weaknesses: weaknesses,
		Suggestion: suggestion,
	}
}

// applyGradient modifies the prompt based on gradient feedback.
func (tg *TextGradOptimizer) applyGradient(prompt string, gradient PromptGradient) string {
	if len(gradient.Weaknesses) == 0 {
		return prompt
	}

	var additions []string
	for _, w := range gradient.Weaknesses {
		switch {
		case strings.Contains(w, "step-by-step"):
			additions = append(additions, "\n\nPlease follow these steps:\n1. Analyze the input carefully.\n2. Apply the specified criteria.\n3. Produce structured output.")
		case strings.Contains(w, "output format"):
			additions = append(additions, "\n\nOutput format: Respond with structured JSON containing your analysis.")
		case strings.Contains(w, "too brief"):
			additions = append(additions, "\n\nProvide thorough analysis with supporting evidence and reasoning.")
		case strings.Contains(w, "too verbose"):
			idx := len(prompt)
			if idx > 1800 {
				prompt = prompt[:1800]
			}
		}
	}

	return prompt + strings.Join(additions, "")
}

// AFlowConfig controls topology optimization.
type AFlowConfig struct {
	MaxAttempts int
	Logger      *slog.Logger
}

// DefaultAFlowConfig returns sensible defaults.
func DefaultAFlowConfig() AFlowConfig {
	return AFlowConfig{MaxAttempts: 5, Logger: slog.Default()}
}

// AFlowResult captures one AFlow topology optimization.
type AFlowResult struct {
	OriginalNodeCount int           `json:"original_node_count"`
	FinalNodeCount    int           `json:"final_node_count"`
	OriginalEdgeCount int           `json:"original_edge_count"`
	FinalEdgeCount    int           `json:"final_edge_count"`
	NodesAdded        []string      `json:"nodes_added"`
	NodesRemoved      []string      `json:"nodes_removed"`
	EdgesAdded        int           `json:"edges_added"`
	EdgesRemoved      int           `json:"edges_removed"`
	OriginalScore     float64       `json:"original_score"`
	FinalScore        float64       `json:"final_score"`
	Improved          bool          `json:"improved"`
	Duration          time.Duration `json:"duration_ns"`
}

// AFlowOptimizer optimizes workflow graph topology by pruning low-value nodes
// and adding parallelism where independent subgraphs exist.
type AFlowOptimizer struct {
	cfg       AFlowConfig
	evaluator WorkflowEvaluator
}

// NewAFlowOptimizer creates a topology optimizer.
func NewAFlowOptimizer(cfg AFlowConfig, evaluator WorkflowEvaluator) *AFlowOptimizer {
	return &AFlowOptimizer{cfg: cfg, evaluator: evaluator}
}

// Optimize tries to improve graph topology by pruning and restructuring.
func (af *AFlowOptimizer) Optimize(ctx context.Context, graph *WorkflowGraph) (*AFlowResult, error) {
	start := time.Now()

	graph.mu.RLock()
	origNodes := len(graph.Nodes)
	origEdges := len(graph.Edges)
	graph.mu.RUnlock()

	baseline, err := af.evaluator.Evaluate(ctx, graph)
	if err != nil {
		return nil, fmt.Errorf("aflow baseline: %w", err)
	}

	result := &AFlowResult{
		OriginalNodeCount: origNodes,
		OriginalEdgeCount: origEdges,
		OriginalScore:     baseline,
	}

	// Try pruning leaf nodes with low-value patterns
	pruned := af.pruneLeafNodes(ctx, graph, baseline)
	result.NodesRemoved = pruned

	// Try adding parallel paths for independent sequential chains
	added := af.addParallelPaths(graph)
	result.NodesAdded = added

	graph.mu.RLock()
	result.FinalNodeCount = len(graph.Nodes)
	result.FinalEdgeCount = len(graph.Edges)
	graph.mu.RUnlock()

	result.EdgesAdded = result.FinalEdgeCount - origEdges + len(pruned)
	result.EdgesRemoved = origEdges - result.FinalEdgeCount + result.EdgesAdded

	finalScore, err := af.evaluator.Evaluate(ctx, graph)
	if err != nil {
		result.FinalScore = baseline
	} else {
		result.FinalScore = finalScore
	}
	result.Improved = result.FinalScore > baseline
	result.Duration = time.Since(start)

	return result, nil
}

// pruneLeafNodes removes nodes with no outgoing edges that don't improve the score.
func (af *AFlowOptimizer) pruneLeafNodes(ctx context.Context, graph *WorkflowGraph, baseline float64) []string {
	graph.mu.RLock()
	outDegree := make(map[string]int)
	for id := range graph.Nodes {
		outDegree[id] = 0
	}
	for _, e := range graph.Edges {
		outDegree[e.From]++
	}
	graph.mu.RUnlock()

	var pruned []string
	for id, deg := range outDegree {
		if deg > 0 || id == graph.EntryNodeID {
			continue
		}

		clone := graph.Clone()
		_ = clone.RemoveNode(id)

		score, err := af.evaluator.Evaluate(ctx, clone)
		if err != nil {
			continue
		}

		if score >= baseline {
			_ = graph.RemoveNode(id)
			pruned = append(pruned, id)
			baseline = score
		}
	}

	return pruned
}

// addParallelPaths identifies sequential chains that could run in parallel.
func (af *AFlowOptimizer) addParallelPaths(graph *WorkflowGraph) []string {
	// Identify nodes with multiple independent predecessors (fan-in pattern)
	graph.mu.RLock()
	inDegree := make(map[string]int)
	for id := range graph.Nodes {
		inDegree[id] = 0
	}
	for _, e := range graph.Edges {
		inDegree[e.To]++
	}
	graph.mu.RUnlock()

	var added []string
	for id, deg := range inDegree {
		if deg < 2 || id == graph.EntryNodeID {
			continue
		}
		graph.mu.RLock()
		node, ok := graph.Nodes[id]
		graph.mu.RUnlock()
		if !ok {
			continue
		}
		if node.Config == nil {
			node.Config = make(map[string]string)
		}
		node.Config["parallel_fan_in"] = "true"
		added = append(added, id+"(parallel)")
	}

	return added
}

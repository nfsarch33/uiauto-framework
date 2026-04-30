package evolver

import (
	"context"
	"strings"
	"testing"
	"time"
)

type scoringEvaluator struct {
	baseScore  float64
	bonusPerFn func(graph *WorkflowGraph) float64
}

func (e *scoringEvaluator) Evaluate(_ context.Context, graph *WorkflowGraph) (float64, error) {
	score := e.baseScore
	if e.bonusPerFn != nil {
		score += e.bonusPerFn(graph)
	}
	return score, nil
}

func TestTextGradOptimizer_BasicPromptImprovement(t *testing.T) {
	eval := &scoringEvaluator{
		baseScore: 0.5,
		bonusPerFn: func(g *WorkflowGraph) float64 {
			g.mu.RLock()
			defer g.mu.RUnlock()
			node := g.Nodes["scraper"]
			if node == nil {
				return 0
			}
			bonus := 0.0
			prompt := strings.ToLower(node.PromptTemplate)
			if strings.Contains(prompt, "step") {
				bonus += 0.1
			}
			if strings.Contains(prompt, "output") || strings.Contains(prompt, "json") {
				bonus += 0.1
			}
			if len(node.PromptTemplate) > 100 {
				bonus += 0.05
			}
			return bonus
		},
	}

	graph := NewWorkflowGraph("wf1", "test-workflow")
	_ = graph.AddNode(WorkflowNode{
		ID: "scraper", Name: "Web Scraper",
		PromptTemplate: "Scrape the page.",
		ModelTier:      "fast",
	})
	graph.EntryNodeID = "scraper"

	tg := NewTextGradOptimizer(DefaultTextGradConfig(), eval, nil)
	result, err := tg.OptimizePrompt(context.Background(), graph, "scraper")
	if err != nil {
		t.Fatalf("textgrad: %v", err)
	}

	if !result.Improved {
		t.Error("expected improvement after TextGrad optimization")
	}
	if result.FinalScore <= result.OriginalScore {
		t.Errorf("final score %.2f should exceed original %.2f", result.FinalScore, result.OriginalScore)
	}
	if len(result.Gradients) == 0 {
		t.Error("expected at least one gradient")
	}
	if result.OptimizedPrompt == result.OriginalPrompt {
		t.Error("prompt should have been modified")
	}
}

func TestTextGradOptimizer_AlreadyOptimal(t *testing.T) {
	eval := &scoringEvaluator{baseScore: 0.95}

	graph := NewWorkflowGraph("wf2", "optimal")
	_ = graph.AddNode(WorkflowNode{
		ID: "node1", Name: "Already Good",
		PromptTemplate: "Please follow these steps:\n1. Analyze input.\n2. Check criteria.\n3. Output structured JSON response with all findings.",
		ModelTier:      "balanced",
	})
	graph.EntryNodeID = "node1"

	tg := NewTextGradOptimizer(DefaultTextGradConfig(), eval, nil)
	result, err := tg.OptimizePrompt(context.Background(), graph, "node1")
	if err != nil {
		t.Fatalf("textgrad: %v", err)
	}

	if result.FinalScore < result.OriginalScore {
		t.Error("score should not decrease for already-optimal prompt")
	}
}

func TestTextGradOptimizer_NodeNotFound(t *testing.T) {
	eval := &scoringEvaluator{baseScore: 0.5}
	graph := NewWorkflowGraph("wf3", "empty")
	tg := NewTextGradOptimizer(DefaultTextGradConfig(), eval, nil)

	_, err := tg.OptimizePrompt(context.Background(), graph, "missing")
	if err == nil {
		t.Error("expected error for missing node")
	}
}

func TestAFlowOptimizer_PruneUnusedNodes(t *testing.T) {
	callCount := 0
	eval := &scoringEvaluator{
		baseScore: 0.7,
		bonusPerFn: func(g *WorkflowGraph) float64 {
			callCount++
			g.mu.RLock()
			defer g.mu.RUnlock()
			if len(g.Nodes) <= 2 {
				return 0.1
			}
			return 0
		},
	}

	graph := NewWorkflowGraph("wf4", "with-dead-leaf")
	_ = graph.AddNode(WorkflowNode{ID: "entry", Name: "Entry", ModelTier: "fast"})
	_ = graph.AddNode(WorkflowNode{ID: "processor", Name: "Processor", ModelTier: "balanced"})
	_ = graph.AddNode(WorkflowNode{ID: "dead_leaf", Name: "Dead Leaf", ModelTier: "fast"})
	_ = graph.AddEdge(WorkflowEdge{From: "entry", To: "processor"})
	_ = graph.AddEdge(WorkflowEdge{From: "entry", To: "dead_leaf"})
	graph.EntryNodeID = "entry"

	af := NewAFlowOptimizer(DefaultAFlowConfig(), eval)
	result, err := af.Optimize(context.Background(), graph)
	if err != nil {
		t.Fatalf("aflow: %v", err)
	}

	if result.OriginalNodeCount != 3 {
		t.Errorf("expected 3 original nodes, got %d", result.OriginalNodeCount)
	}
	if result.FinalScore < result.OriginalScore {
		t.Error("pruning should not decrease score")
	}
}

func TestAFlowOptimizer_ParallelFanIn(t *testing.T) {
	eval := &scoringEvaluator{
		baseScore: 0.6,
		bonusPerFn: func(g *WorkflowGraph) float64 {
			g.mu.RLock()
			defer g.mu.RUnlock()
			if _, ok := g.Nodes["merge"]; !ok {
				return -0.5 // penalize removing merge
			}
			return 0
		},
	}

	graph := NewWorkflowGraph("wf5", "fan-in")
	_ = graph.AddNode(WorkflowNode{ID: "a", Name: "Source A", ModelTier: "fast"})
	_ = graph.AddNode(WorkflowNode{ID: "b", Name: "Source B", ModelTier: "fast"})
	_ = graph.AddNode(WorkflowNode{ID: "merge", Name: "Merge", ModelTier: "balanced"})
	_ = graph.AddEdge(WorkflowEdge{From: "a", To: "merge"})
	_ = graph.AddEdge(WorkflowEdge{From: "b", To: "merge"})
	graph.EntryNodeID = "a"

	af := NewAFlowOptimizer(DefaultAFlowConfig(), eval)
	result, err := af.Optimize(context.Background(), graph)
	if err != nil {
		t.Fatalf("aflow: %v", err)
	}

	graph.mu.RLock()
	mergeNode, ok := graph.Nodes["merge"]
	graph.mu.RUnlock()

	if !ok {
		t.Fatal("merge node was unexpectedly removed")
	}
	if mergeNode.Config["parallel_fan_in"] != "true" {
		t.Error("merge node should be marked for parallel fan-in")
	}
	if len(result.NodesAdded) == 0 {
		t.Error("expected parallel annotation in NodesAdded")
	}
}

func TestAFlowOptimizer_EmptyGraph(t *testing.T) {
	eval := &scoringEvaluator{baseScore: 0}
	graph := NewWorkflowGraph("wf6", "empty")

	af := NewAFlowOptimizer(DefaultAFlowConfig(), eval)
	result, err := af.Optimize(context.Background(), graph)
	if err != nil {
		t.Fatalf("aflow empty: %v", err)
	}
	if result.OriginalNodeCount != 0 {
		t.Errorf("expected 0 nodes, got %d", result.OriginalNodeCount)
	}
}

func TestTextGradAndAFlow_Combined(t *testing.T) {
	eval := &scoringEvaluator{
		baseScore: 0.5,
		bonusPerFn: func(g *WorkflowGraph) float64 {
			g.mu.RLock()
			defer g.mu.RUnlock()
			bonus := 0.0
			for _, n := range g.Nodes {
				if strings.Contains(strings.ToLower(n.PromptTemplate), "step") {
					bonus += 0.05
				}
			}
			if len(g.Nodes) <= 3 {
				bonus += 0.05
			}
			return bonus
		},
	}

	graph := NewWorkflowGraph("wf7", "combined-test")
	_ = graph.AddNode(WorkflowNode{
		ID: "scraper", Name: "Scraper",
		PromptTemplate: "Scrape page content.",
		ModelTier:      "fast",
	})
	_ = graph.AddNode(WorkflowNode{
		ID: "analyzer", Name: "Analyzer",
		PromptTemplate: "Analyze the data.",
		ModelTier:      "balanced",
	})
	_ = graph.AddNode(WorkflowNode{
		ID: "dead", Name: "Unused",
		PromptTemplate: "Nothing.",
		ModelTier:      "fast",
	})
	_ = graph.AddEdge(WorkflowEdge{From: "scraper", To: "analyzer"})
	graph.EntryNodeID = "scraper"

	// Phase 1: TextGrad on scraper
	tg := NewTextGradOptimizer(TextGradConfig{
		MaxIterations:        3,
		LearningRate:         0.3,
		ImprovementThreshold: 0.01,
	}, eval, nil)

	tgResult, err := tg.OptimizePrompt(context.Background(), graph, "scraper")
	if err != nil {
		t.Fatalf("textgrad: %v", err)
	}

	// Phase 2: AFlow topology optimization
	af := NewAFlowOptimizer(DefaultAFlowConfig(), eval)
	afResult, err := af.Optimize(context.Background(), graph)
	if err != nil {
		t.Fatalf("aflow: %v", err)
	}

	if tgResult.Duration == 0 {
		t.Error("textgrad duration should be > 0")
	}
	if afResult.Duration == 0 {
		t.Error("aflow duration should be > 0")
	}

	_ = tgResult
	_ = afResult
}

func TestPromptGradient_Fields(t *testing.T) {
	g := PromptGradient{
		NodeID:     "n1",
		Score:      0.75,
		Feedback:   "good",
		Weaknesses: []string{"a", "b"},
		Suggestion: "improve",
	}
	if g.NodeID != "n1" || g.Score != 0.75 || len(g.Weaknesses) != 2 {
		t.Error("gradient field mismatch")
	}
}

func TestDefaultConfigs(t *testing.T) {
	tgCfg := DefaultTextGradConfig()
	if tgCfg.MaxIterations != 5 {
		t.Errorf("expected 5 max iterations, got %d", tgCfg.MaxIterations)
	}
	if tgCfg.LearningRate != 0.3 {
		t.Errorf("expected 0.3 learning rate, got %f", tgCfg.LearningRate)
	}

	afCfg := DefaultAFlowConfig()
	if afCfg.MaxAttempts != 5 {
		t.Errorf("expected 5 max attempts, got %d", afCfg.MaxAttempts)
	}
}

func TestTextGradResult_Fields(t *testing.T) {
	r := TextGradResult{
		NodeID:          "n1",
		OriginalPrompt:  "old",
		OptimizedPrompt: "new",
		OriginalScore:   0.5,
		FinalScore:      0.8,
		Iterations:      3,
		Duration:        100 * time.Millisecond,
		Improved:        true,
	}
	if !r.Improved || r.FinalScore != 0.8 || r.Iterations != 3 {
		t.Error("result field mismatch")
	}
}

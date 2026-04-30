package evolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// newTestGraph creates a 3-node scraper->processor->evaluator graph.
func newTestGraph() *WorkflowGraph {
	g := NewWorkflowGraph("test-graph", "Test Workflow")
	_ = g.AddNode(WorkflowNode{
		ID:        "scraper",
		Name:      "Scraper",
		AgentType: "scraper",
		ModelTier: "fast",
	})
	_ = g.AddNode(WorkflowNode{
		ID:        "processor",
		Name:      "Processor",
		AgentType: "processor",
		ModelTier: "balanced",
	})
	_ = g.AddNode(WorkflowNode{
		ID:        "evaluator",
		Name:      "Evaluator",
		AgentType: "evaluator",
		ModelTier: "powerful",
	})
	_ = g.AddEdge(WorkflowEdge{From: "scraper", To: "processor"})
	_ = g.AddEdge(WorkflowEdge{From: "processor", To: "evaluator"})
	g.EntryNodeID = "scraper"
	return g
}

func TestNewWorkflowGraph(t *testing.T) {
	g := NewWorkflowGraph("g1", "Graph One")
	assert.Equal(t, "g1", g.ID)
	assert.Equal(t, "Graph One", g.Name)
	assert.NotNil(t, g.Nodes)
	assert.Empty(t, g.Nodes)
	assert.Nil(t, g.Edges)
	assert.False(t, g.CreatedAt.IsZero())
	assert.False(t, g.UpdatedAt.IsZero())
}

func TestWorkflowGraph_AddNode(t *testing.T) {
	g := NewWorkflowGraph("g1", "Graph")

	err := g.AddNode(WorkflowNode{ID: "n1", Name: "Node 1"})
	assert.NoError(t, err)
	assert.Len(t, g.Nodes, 1)
	assert.Contains(t, g.Nodes, "n1")
	assert.Equal(t, "Node 1", g.Nodes["n1"].Name)

	// Duplicate
	err = g.AddNode(WorkflowNode{ID: "n1", Name: "Node 1 Again"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Empty ID
	err = g.AddNode(WorkflowNode{ID: "", Name: "No ID"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "id is required")

	// Empty Name
	err = g.AddNode(WorkflowNode{ID: "n2", Name: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestWorkflowGraph_AddEdge(t *testing.T) {
	g := newTestGraph()

	// Valid edge
	err := g.AddEdge(WorkflowEdge{From: "evaluator", To: "scraper"})
	assert.NoError(t, err)
	assert.Len(t, g.Edges, 3)

	// Invalid: From node does not exist
	err = g.AddEdge(WorkflowEdge{From: "nonexistent", To: "scraper"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "From node")

	// Invalid: To node does not exist
	err = g.AddEdge(WorkflowEdge{From: "scraper", To: "ghost"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "To node")

	// Invalid: self-loop
	err = g.AddEdge(WorkflowEdge{From: "scraper", To: "scraper"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "itself")

	// Invalid: empty From
	err = g.AddEdge(WorkflowEdge{From: "", To: "scraper"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "From is required")

	// Invalid: empty To
	err = g.AddEdge(WorkflowEdge{From: "scraper", To: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "To is required")
}

func TestWorkflowGraph_RemoveNode(t *testing.T) {
	g := newTestGraph()

	err := g.RemoveNode("processor")
	assert.NoError(t, err)
	assert.NotContains(t, g.Nodes, "processor")
	assert.Len(t, g.Edges, 0) // both edges involved processor

	// Remove non-existent
	err = g.RemoveNode("ghost")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// Remove entry node clears EntryNodeID
	g2 := newTestGraph()
	err = g2.RemoveNode("scraper")
	assert.NoError(t, err)
	assert.Empty(t, g2.EntryNodeID)
}

func TestWorkflowGraph_TopologicalSort(t *testing.T) {
	// Linear: scraper -> processor -> evaluator
	g := newTestGraph()
	order, err := g.TopologicalSort()
	assert.NoError(t, err)
	assert.Len(t, order, 3)
	assert.Equal(t, "scraper", order[0])
	assert.Equal(t, "processor", order[1])
	assert.Equal(t, "evaluator", order[2])

	// Diamond: a -> b, a -> c, b -> d, c -> d
	g2 := NewWorkflowGraph("diamond", "Diamond")
	for _, id := range []string{"a", "b", "c", "d"} {
		_ = g2.AddNode(WorkflowNode{ID: id, Name: id})
	}
	_ = g2.AddEdge(WorkflowEdge{From: "a", To: "b"})
	_ = g2.AddEdge(WorkflowEdge{From: "a", To: "c"})
	_ = g2.AddEdge(WorkflowEdge{From: "b", To: "d"})
	_ = g2.AddEdge(WorkflowEdge{From: "c", To: "d"})
	order, err = g2.TopologicalSort()
	assert.NoError(t, err)
	assert.Len(t, order, 4)
	aIdx, bIdx, cIdx, dIdx := indexOf(order, "a"), indexOf(order, "b"), indexOf(order, "c"), indexOf(order, "d")
	assert.True(t, aIdx < bIdx && aIdx < cIdx)
	assert.True(t, bIdx < dIdx && cIdx < dIdx)

	// Cycle detection
	g3 := NewWorkflowGraph("cycle", "Cycle")
	_ = g3.AddNode(WorkflowNode{ID: "x", Name: "X"})
	_ = g3.AddNode(WorkflowNode{ID: "y", Name: "Y"})
	_ = g3.AddEdge(WorkflowEdge{From: "x", To: "y"})
	_ = g3.AddEdge(WorkflowEdge{From: "y", To: "x"})
	_, err = g3.TopologicalSort()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")

	// Empty graph
	g4 := NewWorkflowGraph("empty", "Empty")
	order, err = g4.TopologicalSort()
	assert.NoError(t, err)
	assert.Nil(t, order)
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

func TestWorkflowGraph_Validate(t *testing.T) {
	// Valid
	g := newTestGraph()
	assert.NoError(t, g.Validate())

	// No entry node
	g2 := newTestGraph()
	g2.EntryNodeID = ""
	assert.Error(t, g2.Validate())
	assert.Contains(t, g2.Validate().Error(), "entry node is required")

	// Entry node does not exist
	g3 := newTestGraph()
	g3.EntryNodeID = "ghost"
	assert.Error(t, g3.Validate())
	assert.Contains(t, g3.Validate().Error(), "does not exist")

	// Orphan (unreachable from entry)
	g4 := NewWorkflowGraph("orphan", "Orphan")
	_ = g4.AddNode(WorkflowNode{ID: "a", Name: "A"})
	_ = g4.AddNode(WorkflowNode{ID: "b", Name: "B"})
	_ = g4.AddEdge(WorkflowEdge{From: "a", To: "b"})
	g4.EntryNodeID = "a"
	_ = g4.AddNode(WorkflowNode{ID: "orphan", Name: "Orphan"})
	assert.Error(t, g4.Validate())
	assert.Contains(t, g4.Validate().Error(), "unreachable")

	// Cycle
	g5 := NewWorkflowGraph("cycle", "Cycle")
	_ = g5.AddNode(WorkflowNode{ID: "x", Name: "X"})
	_ = g5.AddNode(WorkflowNode{ID: "y", Name: "Y"})
	_ = g5.AddEdge(WorkflowEdge{From: "x", To: "y"})
	_ = g5.AddEdge(WorkflowEdge{From: "y", To: "x"})
	g5.EntryNodeID = "x"
	assert.Error(t, g5.Validate())
	assert.Contains(t, g5.Validate().Error(), "cycle")

	// No nodes
	g6 := NewWorkflowGraph("empty", "Empty")
	assert.Error(t, g6.Validate())
	assert.Contains(t, g6.Validate().Error(), "no nodes")
}

func TestWorkflowGraph_Clone(t *testing.T) {
	g := newTestGraph()
	g.RecordRun(true, 100, 0.01)
	g.RecordRun(false, 200, 0.02)

	clone := g.Clone()
	assert.Equal(t, g.ID, clone.ID)
	assert.Equal(t, g.Name, clone.Name)
	assert.Equal(t, g.EntryNodeID, clone.EntryNodeID)
	assert.Equal(t, g.Metrics.TotalRuns, clone.Metrics.TotalRuns)
	assert.Len(t, clone.Nodes, len(g.Nodes))
	assert.Len(t, clone.Edges, len(g.Edges))

	// Mutate clone; original unchanged
	clone.Nodes["scraper"].Name = "Mutated Scraper"
	assert.NotEqual(t, g.Nodes["scraper"].Name, clone.Nodes["scraper"].Name)

	clone.Metrics.TotalRuns = 999
	assert.NotEqual(t, g.Metrics.TotalRuns, clone.Metrics.TotalRuns)
}

func TestWorkflowGraph_RecordRun(t *testing.T) {
	g := newTestGraph()

	g.RecordRun(true, 100, 0.01)
	assert.Equal(t, int64(1), g.Metrics.TotalRuns)
	assert.Equal(t, int64(1), g.Metrics.SuccessRuns)
	assert.Equal(t, int64(0), g.Metrics.FailedRuns)
	assert.InDelta(t, 100.0, g.Metrics.AvgLatencyMs, 0.01)
	assert.InDelta(t, 0.01, g.Metrics.AvgCost, 0.0001)
	assert.NotNil(t, g.Metrics.LastRunAt)

	g.RecordRun(false, 200, 0.02)
	assert.Equal(t, int64(2), g.Metrics.TotalRuns)
	assert.Equal(t, int64(1), g.Metrics.SuccessRuns)
	assert.Equal(t, int64(1), g.Metrics.FailedRuns)
	assert.InDelta(t, 150.0, g.Metrics.AvgLatencyMs, 0.01)
	assert.InDelta(t, 0.015, g.Metrics.AvgCost, 0.0001)
}

func TestWorkflowGraph_SuccessRate(t *testing.T) {
	g := newTestGraph()

	// Zero runs
	assert.Equal(t, 0.0, g.SuccessRate())

	// Mixed runs
	g.RecordRun(true, 100, 0.01)
	g.RecordRun(true, 100, 0.01)
	g.RecordRun(false, 100, 0.01)
	assert.InDelta(t, 2.0/3.0, g.SuccessRate(), 0.001)

	// All success
	g2 := newTestGraph()
	g2.RecordRun(true, 100, 0.01)
	assert.Equal(t, 1.0, g2.SuccessRate())

	// All failure
	g3 := newTestGraph()
	g3.RecordRun(false, 100, 0.01)
	assert.Equal(t, 0.0, g3.SuccessRate())
}

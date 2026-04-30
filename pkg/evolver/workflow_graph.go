package evolver

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// WorkflowNode represents a single node in an EvoAgentX-inspired workflow graph.
type WorkflowNode struct {
	ID             string
	Name           string
	AgentType      string   // e.g. "scraper", "vlm", "evaluator"
	PromptTemplate string   // system prompt for this node
	ToolIDs        []string // tools this node can use
	ModelTier      string   // fast/balanced/powerful/vlm
	Timeout        time.Duration
	Config         map[string]string // node-specific config
}

// WorkflowEdge represents a directed edge between workflow nodes.
type WorkflowEdge struct {
	From        string            // source node ID
	To          string            // target node ID
	Condition   string            // optional conditional routing expression
	DataMapping map[string]string // field mapping from output to input
}

// WorkflowMetrics holds run statistics for a workflow graph.
type WorkflowMetrics struct {
	TotalRuns    int64
	SuccessRuns  int64
	FailedRuns   int64
	AvgLatencyMs float64
	AvgCost      float64
	LastRunAt    *time.Time
}

// WorkflowGraph is a DAG of workflow nodes with edges and metrics.
type WorkflowGraph struct {
	ID          string
	Name        string
	Description string
	Version     int
	Nodes       map[string]*WorkflowNode
	Edges       []*WorkflowEdge
	EntryNodeID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Metrics     WorkflowMetrics
	mu          sync.RWMutex
}

// NewWorkflowGraph creates a new workflow graph with the given id and name.
func NewWorkflowGraph(id, name string) *WorkflowGraph {
	now := time.Now().UTC()
	return &WorkflowGraph{
		ID:        id,
		Name:      name,
		Nodes:     make(map[string]*WorkflowNode),
		Edges:     nil,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AddNode validates and adds a node to the graph.
func (g *WorkflowGraph) AddNode(node WorkflowNode) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if node.ID == "" {
		return fmt.Errorf("workflow node id is required")
	}
	if node.Name == "" {
		return fmt.Errorf("workflow node name is required")
	}
	if _, exists := g.Nodes[node.ID]; exists {
		return fmt.Errorf("workflow node %q already exists", node.ID)
	}

	n := node
	if n.Config == nil {
		n.Config = make(map[string]string)
	}
	g.Nodes[node.ID] = &n
	g.UpdatedAt = time.Now().UTC()
	return nil
}

// AddEdge validates node references and adds an edge to the graph.
func (g *WorkflowGraph) AddEdge(edge WorkflowEdge) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if edge.From == "" {
		return fmt.Errorf("workflow edge From is required")
	}
	if edge.To == "" {
		return fmt.Errorf("workflow edge To is required")
	}
	if _, ok := g.Nodes[edge.From]; !ok {
		return fmt.Errorf("workflow edge From node %q does not exist", edge.From)
	}
	if _, ok := g.Nodes[edge.To]; !ok {
		return fmt.Errorf("workflow edge To node %q does not exist", edge.To)
	}
	if edge.From == edge.To {
		return fmt.Errorf("workflow edge cannot connect node to itself")
	}

	e := edge
	if e.DataMapping == nil {
		e.DataMapping = make(map[string]string)
	}
	g.Edges = append(g.Edges, &e)
	g.UpdatedAt = time.Now().UTC()
	return nil
}

// RemoveNode removes a node and all connected edges.
func (g *WorkflowGraph) RemoveNode(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.Nodes[id]; !exists {
		return fmt.Errorf("workflow node %q does not exist", id)
	}

	delete(g.Nodes, id)
	var newEdges []*WorkflowEdge
	for _, e := range g.Edges {
		if e.From != id && e.To != id {
			newEdges = append(newEdges, e)
		}
	}
	g.Edges = newEdges
	if g.EntryNodeID == id {
		g.EntryNodeID = ""
	}
	g.UpdatedAt = time.Now().UTC()
	return nil
}

// TopologicalSort returns node IDs in execution order. Errors on cycle.
func (g *WorkflowGraph) TopologicalSort() ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.Nodes) == 0 {
		return nil, nil
	}

	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = 0
	}
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		inDegree[e.To]++
	}

	var queue []string
	for id, d := range inDegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}

	var order []string
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		order = append(order, u)
		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if len(order) != len(g.Nodes) {
		return nil, fmt.Errorf("workflow graph contains a cycle")
	}
	return order, nil
}

// Validate checks entry node exists, no orphans, no cycles, and DAG integrity.
func (g *WorkflowGraph) Validate() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.Nodes) == 0 {
		return fmt.Errorf("workflow graph has no nodes")
	}
	if g.EntryNodeID == "" {
		return fmt.Errorf("workflow graph entry node is required")
	}
	if _, ok := g.Nodes[g.EntryNodeID]; !ok {
		return fmt.Errorf("workflow graph entry node %q does not exist", g.EntryNodeID)
	}

	_, err := g.topologicalSortUnlocked()
	if err != nil {
		return err
	}

	reachable := make(map[string]bool)
	g.dfsReachable(g.EntryNodeID, reachable)
	for id := range g.Nodes {
		if !reachable[id] {
			return fmt.Errorf("workflow graph has unreachable node %q", id)
		}
	}

	return nil
}

func (g *WorkflowGraph) dfsReachable(from string, visited map[string]bool) {
	visited[from] = true
	for _, e := range g.Edges {
		if e.From == from && !visited[e.To] {
			g.dfsReachable(e.To, visited)
		}
	}
}

func (g *WorkflowGraph) topologicalSortUnlocked() ([]string, error) {
	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = 0
	}
	adj := make(map[string][]string)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		inDegree[e.To]++
	}

	var queue []string
	for id, d := range inDegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}

	var order []string
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		order = append(order, u)
		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if len(order) != len(g.Nodes) {
		return nil, fmt.Errorf("workflow graph contains a cycle")
	}
	return order, nil
}

// Clone returns a deep copy of the graph for sandbox experiments.
func (g *WorkflowGraph) Clone() *WorkflowGraph {
	g.mu.RLock()
	defer g.mu.RUnlock()

	clone := &WorkflowGraph{
		ID:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		Version:     g.Version,
		Nodes:       make(map[string]*WorkflowNode, len(g.Nodes)),
		Edges:       make([]*WorkflowEdge, len(g.Edges)),
		EntryNodeID: g.EntryNodeID,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
		Metrics:     g.Metrics,
	}

	for id, n := range g.Nodes {
		nc := *n
		if nc.ToolIDs != nil {
			nc.ToolIDs = append([]string(nil), nc.ToolIDs...)
		}
		if nc.Config != nil {
			nc.Config = make(map[string]string, len(nc.Config))
			for k, v := range n.Config {
				nc.Config[k] = v
			}
		}
		clone.Nodes[id] = &nc
	}

	for i, e := range g.Edges {
		ec := *e
		if ec.DataMapping != nil {
			ec.DataMapping = make(map[string]string, len(ec.DataMapping))
			for k, v := range e.DataMapping {
				ec.DataMapping[k] = v
			}
		}
		clone.Edges[i] = &ec
	}

	if g.Metrics.LastRunAt != nil {
		t := *g.Metrics.LastRunAt
		clone.Metrics.LastRunAt = &t
	}

	return clone
}

// RecordRun updates metrics atomically after a workflow run.
func (g *WorkflowGraph) RecordRun(success bool, latencyMs float64, cost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UTC()
	g.Metrics.TotalRuns++
	if success {
		g.Metrics.SuccessRuns++
	} else {
		g.Metrics.FailedRuns++
	}

	n := float64(g.Metrics.TotalRuns)
	g.Metrics.AvgLatencyMs = (g.Metrics.AvgLatencyMs*(n-1) + latencyMs) / n
	g.Metrics.AvgCost = (g.Metrics.AvgCost*(n-1) + cost) / n
	g.Metrics.LastRunAt = &now
	g.UpdatedAt = now
}

// SuccessRate returns the fraction of successful runs, or 0 if no runs.
func (g *WorkflowGraph) SuccessRate() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.Metrics.TotalRuns == 0 {
		return 0
	}
	return float64(g.Metrics.SuccessRuns) / float64(g.Metrics.TotalRuns)
}

// WorkflowMutationType is the type of a workflow mutation.
type WorkflowMutationType string

// WorkflowMutationType values.
const (
	MutatePrompt    WorkflowMutationType = "MutatePrompt"
	MutateModelTier WorkflowMutationType = "MutateModelTier"
	MutateRouting   WorkflowMutationType = "MutateRouting"
	AddNode         WorkflowMutationType = "AddNode"
	RemoveNode      WorkflowMutationType = "RemoveNode"
	SwapTool        WorkflowMutationType = "SwapTool"
)

// WorkflowMutation records a mutation applied to a workflow graph.
type WorkflowMutation struct {
	ID        string
	GraphID   string
	Type      WorkflowMutationType
	NodeID    string // target node, if applicable
	Before    json.RawMessage
	After     json.RawMessage
	Reason    string
	CreatedAt time.Time
}

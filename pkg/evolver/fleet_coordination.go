package evolver

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FleetNode represents a machine in the cross-machine fleet.
type FleetNode struct {
	ID       string    `json:"id"`
	Hostname string    `json:"hostname"`
	Platform string    `json:"platform"`
	LastSeen time.Time `json:"last_seen"`
	Status   string    `json:"status"`
}

// PatternShare represents a shared pattern between fleet nodes.
type PatternShare struct {
	SourceNode  string    `json:"source_node"`
	TargetNode  string    `json:"target_node"`
	PatternID   string    `json:"pattern_id"`
	PatternData string    `json:"pattern_data"`
	Timestamp   time.Time `json:"timestamp"`
}

// FleetCoordinator manages cross-machine pattern sharing and task delegation.
type FleetCoordinator struct {
	mu          sync.RWMutex
	nodes       map[string]*FleetNode
	shares      []PatternShare
	delegations []TaskDelegation
}

// TaskDelegation records a delegated task between fleet nodes.
type TaskDelegation struct {
	ID         string    `json:"id"`
	SourceNode string    `json:"source_node"`
	TargetNode string    `json:"target_node"`
	TaskType   string    `json:"task_type"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewFleetCoordinator creates a new fleet coordinator.
func NewFleetCoordinator() *FleetCoordinator {
	return &FleetCoordinator{
		nodes: make(map[string]*FleetNode),
	}
}

// RegisterNode adds or updates a fleet node.
func (fc *FleetCoordinator) RegisterNode(node FleetNode) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	node.LastSeen = time.Now()
	node.Status = "online"
	fc.nodes[node.ID] = &node
}

// Nodes returns all registered fleet nodes.
func (fc *FleetCoordinator) Nodes() []*FleetNode {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	nodes := make([]*FleetNode, 0, len(fc.nodes))
	for _, n := range fc.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// SharePattern shares a pattern from source to target node.
func (fc *FleetCoordinator) SharePattern(_ context.Context, share PatternShare) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if _, ok := fc.nodes[share.SourceNode]; !ok {
		return fmt.Errorf("source node %s not registered", share.SourceNode)
	}
	if _, ok := fc.nodes[share.TargetNode]; !ok {
		return fmt.Errorf("target node %s not registered", share.TargetNode)
	}
	share.Timestamp = time.Now()
	fc.shares = append(fc.shares, share)
	return nil
}

// Shares returns all pattern shares.
func (fc *FleetCoordinator) Shares() []PatternShare {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.shares
}

// DelegateTask creates a task delegation between nodes.
func (fc *FleetCoordinator) DelegateTask(_ context.Context, delegation TaskDelegation) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if _, ok := fc.nodes[delegation.SourceNode]; !ok {
		return fmt.Errorf("source node %s not registered", delegation.SourceNode)
	}
	delegation.CreatedAt = time.Now()
	delegation.Status = "pending"
	fc.delegations = append(fc.delegations, delegation)
	return nil
}

// Delegations returns all task delegations.
func (fc *FleetCoordinator) Delegations() []TaskDelegation {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.delegations
}

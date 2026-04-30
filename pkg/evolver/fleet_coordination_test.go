package evolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetCoordinator_RegisterAndSharePatterns(t *testing.T) {
	fc := NewFleetCoordinator()

	fc.RegisterNode(FleetNode{ID: "macbook", Hostname: "macbook-pro", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "wsl", Hostname: "wsl-ubuntu", Platform: "linux"})

	nodes := fc.Nodes()
	assert.Len(t, nodes, 2)

	err := fc.SharePattern(context.Background(), PatternShare{
		SourceNode: "macbook", TargetNode: "wsl",
		PatternID: "d2l-content-nav", PatternData: `{"selector": "a.d2l-link"}`,
	})
	require.NoError(t, err)

	shares := fc.Shares()
	assert.Len(t, shares, 1)
	assert.Equal(t, "d2l-content-nav", shares[0].PatternID)
}

func TestFleetCoordinator_DelegateTask(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "macbook", Hostname: "macbook-pro", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "wsl", Hostname: "wsl-ubuntu", Platform: "linux"})

	err := fc.DelegateTask(context.Background(), TaskDelegation{
		ID: "task-1", SourceNode: "macbook", TargetNode: "wsl", TaskType: "scrape",
	})
	require.NoError(t, err)

	delegations := fc.Delegations()
	assert.Len(t, delegations, 1)
	assert.Equal(t, "pending", delegations[0].Status)
}

func TestFleetCoordinator_ShareToUnregisteredNode(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "macbook", Hostname: "macbook-pro", Platform: "darwin"})

	err := fc.SharePattern(context.Background(), PatternShare{
		SourceNode: "macbook", TargetNode: "unknown",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

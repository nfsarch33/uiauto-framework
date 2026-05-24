// Test fixtures use synthetic node names for fleet coordination tests.

package evolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetCoordinator_RegisterAndSharePatterns(t *testing.T) {
	fc := NewFleetCoordinator()

	fc.RegisterNode(FleetNode{ID: "test-darwin", Hostname: "test-darwin-host", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "test-linux", Hostname: "test-linux-host", Platform: "linux"})

	nodes := fc.Nodes()
	assert.Len(t, nodes, 2)

	err := fc.SharePattern(context.Background(), PatternShare{
		SourceNode: "test-darwin", TargetNode: "test-linux",
		PatternID: "d2l-content-nav", PatternData: `{"selector": "a.d2l-link"}`,
	})
	require.NoError(t, err)

	shares := fc.Shares()
	assert.Len(t, shares, 1)
	assert.Equal(t, "d2l-content-nav", shares[0].PatternID)
}

func TestFleetCoordinator_DelegateTask(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "test-darwin", Hostname: "test-darwin-host", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "test-linux", Hostname: "test-linux-host", Platform: "linux"})

	err := fc.DelegateTask(context.Background(), TaskDelegation{
		ID: "task-1", SourceNode: "test-darwin", TargetNode: "test-linux", TaskType: "scrape",
	})
	require.NoError(t, err)

	delegations := fc.Delegations()
	assert.Len(t, delegations, 1)
	assert.Equal(t, "pending", delegations[0].Status)
}

func TestFleetCoordinator_ShareToUnregisteredNode(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "test-darwin", Hostname: "test-darwin-host", Platform: "darwin"})

	err := fc.SharePattern(context.Background(), PatternShare{
		SourceNode: "test-darwin", TargetNode: "unknown",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

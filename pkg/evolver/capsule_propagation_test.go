// Test fixtures use synthetic node names for capsule propagation tests.

package evolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapsulePropagation(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	require.NoError(t, store.SaveCapsule(ctx, &Capsule{
		ID: "cap-001", Name: "Improved scraper", Status: CapsuleStatusActive,
	}))
	require.NoError(t, store.SaveCapsule(ctx, &Capsule{
		ID: "cap-002", Name: "Better logging", Status: CapsuleStatusDraft,
	}))

	fleet, err := NewPersistentFleetCoordinator(store, nil)
	require.NoError(t, err)
	require.NoError(t, fleet.RegisterNode(ctx, FleetNode{ID: "test-node-1"}))
	require.NoError(t, fleet.RegisterNode(ctx, FleetNode{ID: "test-node-2"}))
	require.NoError(t, fleet.RegisterNode(ctx, FleetNode{ID: "test-node-3"}))

	cp := NewCapsulePropagator(store, fleet, nil)

	result, err := cp.Propagate(ctx, PropagationConfig{
		SourceNode:  "test-node-1",
		TargetNodes: []string{"test-node-2", "test-node-3"},
		MaxCapsules: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.CapsulesSent)
	assert.Equal(t, 2, result.TargetsReached)
	assert.Empty(t, result.Errors)
}

func TestCapsulePropagation_MaxCapsules(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, store.SaveCapsule(ctx, &Capsule{
			ID: "cap-" + string(rune('A'+i)), Status: CapsuleStatusActive,
		}))
	}

	fleet, err := NewPersistentFleetCoordinator(store, nil)
	require.NoError(t, err)
	require.NoError(t, fleet.RegisterNode(ctx, FleetNode{ID: "src"}))
	require.NoError(t, fleet.RegisterNode(ctx, FleetNode{ID: "dst"}))

	cp := NewCapsulePropagator(store, fleet, nil)
	result, err := cp.Propagate(ctx, PropagationConfig{
		SourceNode:  "src",
		TargetNodes: []string{"dst"},
		MaxCapsules: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.CapsulesSent)
}

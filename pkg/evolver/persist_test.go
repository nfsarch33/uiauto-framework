package evolver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStore_CapsuleRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	c := &Capsule{ID: "cap-001", Status: CapsuleStatus("draft")}

	require.NoError(t, store.SaveCapsule(ctx, c))
	loaded, err := store.LoadCapsule(ctx, "cap-001")
	require.NoError(t, err)
	assert.Equal(t, "cap-001", loaded.ID)
	assert.Equal(t, CapsuleStatus("draft"), loaded.Status)

	list, err := store.ListCapsules(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestFileStore_GeneRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	g := &Gene{ID: "gene-001", Name: "test-gene"}

	require.NoError(t, store.SaveGene(ctx, g))
	loaded, err := store.LoadGene(ctx, "gene-001")
	require.NoError(t, err)
	assert.Equal(t, "gene-001", loaded.ID)
}

func TestFileStore_EventPersistence(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	ev := &EvolutionEvent{ID: "ev-001", Type: "mutation", Timestamp: time.Now()}

	require.NoError(t, store.SaveEvent(ctx, ev))
	events, err := store.ListEvents(ctx)
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestFileStore_FleetNodePersistence(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	node := &FleetNode{ID: "wsl1", Hostname: "wsl1.local", Platform: "linux"}
	require.NoError(t, store.SaveFleetNode(ctx, node))

	nodes, err := store.ListFleetNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "wsl1", nodes[0].ID)
}

func TestFileStore_PatternSharePersistence(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	share := &PatternShare{
		SourceNode: "wsl1", TargetNode: "macbook1",
		PatternID: "p-001", Timestamp: time.Now(),
	}
	require.NoError(t, store.SavePatternShare(ctx, share))

	shares, err := store.ListPatternShares(ctx)
	require.NoError(t, err)
	assert.Len(t, shares, 1)
}

func TestFileStore_TaskDelegationPersistence(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	d := &TaskDelegation{ID: "del-001", SourceNode: "wsl1", TaskType: "benchmark"}
	require.NoError(t, store.SaveTaskDelegation(ctx, d))

	delegations, err := store.ListTaskDelegations(ctx)
	require.NoError(t, err)
	assert.Len(t, delegations, 1)
}

func TestFileStore_FeedbackSignalPersistence(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	sig := &FeedbackSignal{
		Source: "prometheus", Metric: "latency_p95",
		Value: 250, Threshold: 200, Timestamp: time.Now(),
	}
	require.NoError(t, store.SaveFeedbackSignal(ctx, sig))

	signals, err := store.ListFeedbackSignals(ctx)
	require.NoError(t, err)
	assert.Len(t, signals, 1)
}

func TestFileStore_FeedbackActionPersistence(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	a := &FeedbackAction{SignalSource: "prometheus", ActionType: "mutate"}
	require.NoError(t, store.SaveFeedbackAction(ctx, a))

	actions, err := store.ListFeedbackActions(ctx)
	require.NoError(t, err)
	assert.Len(t, actions, 1)
}

func TestPersistentFleetCoordinator(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	pfc, err := NewPersistentFleetCoordinator(store, nil)
	require.NoError(t, err)

	require.NoError(t, pfc.RegisterNode(ctx, FleetNode{ID: "wsl1", Hostname: "wsl1"}))
	require.NoError(t, pfc.RegisterNode(ctx, FleetNode{ID: "macbook1", Hostname: "macbook1"}))

	assert.Len(t, pfc.Nodes(), 2)

	require.NoError(t, pfc.SharePattern(ctx, PatternShare{
		SourceNode: "wsl1", TargetNode: "macbook1", PatternID: "p-001",
	}))
	assert.Len(t, pfc.Shares(), 1)

	require.NoError(t, pfc.DelegateTask(ctx, TaskDelegation{
		ID: "del-001", SourceNode: "wsl1", TaskType: "eval",
	}))
	assert.Len(t, pfc.Delegations(), 1)

	// Verify persistence: create a new coordinator from the same store
	pfc2, err := NewPersistentFleetCoordinator(store, nil)
	require.NoError(t, err)
	assert.Len(t, pfc2.Nodes(), 2)
}

func TestPersistentFeedbackLoop(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	pfl := NewPersistentFeedbackLoop(store, nil)

	require.NoError(t, pfl.IngestSignal(ctx, FeedbackSignal{
		Source: "prometheus", Metric: "latency_p95",
		Value: 300, Threshold: 200,
	}))

	actions, err := pfl.Evaluate(ctx)
	require.NoError(t, err)
	assert.Len(t, actions, 1)
	assert.Equal(t, "mutate", actions[0].ActionType)

	signals, err := store.ListFeedbackSignals(ctx)
	require.NoError(t, err)
	assert.Len(t, signals, 1)

	savedActions, err := store.ListFeedbackActions(ctx)
	require.NoError(t, err)
	assert.Len(t, savedActions, 1)
}

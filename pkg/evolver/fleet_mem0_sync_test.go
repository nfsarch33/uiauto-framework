package evolver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fleetMockMem0Client struct {
	added    []string
	searched []string
	addErr   error
	memories []Mem0Memory
}

func (m *fleetMockMem0Client) Add(_ context.Context, content string, _ map[string]string) (string, error) {
	if m.addErr != nil {
		return "", m.addErr
	}
	m.added = append(m.added, content)
	return "mem-" + content[:5], nil
}

func (m *fleetMockMem0Client) Search(_ context.Context, query string, _ int) ([]Mem0Memory, error) {
	m.searched = append(m.searched, query)
	return m.memories, nil
}

func TestFleetMem0Sync_SyncPatterns(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "mac", Hostname: "macbook", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "wsl", Hostname: "wsl-ubuntu", Platform: "linux"})

	_ = fc.SharePattern(context.Background(), PatternShare{
		SourceNode:  "mac",
		TargetNode:  "wsl",
		PatternID:   "pat-1",
		PatternData: "login selector: #btn-login",
	})
	_ = fc.SharePattern(context.Background(), PatternShare{
		SourceNode:  "wsl",
		TargetNode:  "mac",
		PatternID:   "pat-2",
		PatternData: "nav menu: .sidebar-nav",
	})

	client := &fleetMockMem0Client{}
	sync := NewFleetMem0Sync(fc, client, DefaultFleetMem0SyncConfig())

	synced, err := sync.SyncPatterns(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, synced)
	assert.Len(t, client.added, 2)

	s, f := sync.Stats()
	assert.Equal(t, 2, s)
	assert.Equal(t, 0, f)
}

func TestFleetMem0Sync_SyncPatterns_WithErrors(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "a", Hostname: "a", Platform: "linux"})
	fc.RegisterNode(FleetNode{ID: "b", Hostname: "b", Platform: "linux"})
	_ = fc.SharePattern(context.Background(), PatternShare{
		SourceNode: "a", TargetNode: "b", PatternID: "p1", PatternData: "data",
	})

	client := &fleetMockMem0Client{addErr: assert.AnError}
	sync := NewFleetMem0Sync(fc, client, DefaultFleetMem0SyncConfig())

	synced, err := sync.SyncPatterns(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, synced)

	_, f := sync.Stats()
	assert.Equal(t, 1, f)
}

func TestFleetMem0Sync_SearchFleetPatterns(t *testing.T) {
	fc := NewFleetCoordinator()
	client := &fleetMockMem0Client{
		memories: []Mem0Memory{
			{
				ID:      "m1",
				Content: "login selector",
				Score:   0.9,
				Metadata: map[string]string{
					"source_node": "mac",
					"target_node": "wsl",
					"pattern_id":  "pat-1",
				},
			},
			{
				ID:      "m2",
				Content: "low confidence",
				Score:   0.2,
				Metadata: map[string]string{
					"source_node": "mac",
					"target_node": "wsl",
					"pattern_id":  "pat-2",
				},
			},
		},
	}

	sync := NewFleetMem0Sync(fc, client, DefaultFleetMem0SyncConfig())
	shares, err := sync.SearchFleetPatterns(context.Background(), "login", 10)
	require.NoError(t, err)
	assert.Len(t, shares, 1, "low confidence should be filtered")
	assert.Equal(t, "pat-1", shares[0].PatternID)
}

func TestFleetMem0Sync_ImportPatterns(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "mac", Hostname: "macbook", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "wsl", Hostname: "wsl-ubuntu", Platform: "linux"})

	client := &fleetMockMem0Client{
		memories: []Mem0Memory{
			{
				ID:      "m1",
				Content: "login pattern",
				Score:   0.8,
				Metadata: map[string]string{
					"source_node": "mac",
					"pattern_id":  "imported-1",
				},
			},
		},
	}

	sync := NewFleetMem0Sync(fc, client, DefaultFleetMem0SyncConfig())
	imported, err := sync.ImportPatterns(context.Background(), "wsl", "login")
	require.NoError(t, err)
	assert.Equal(t, 1, imported)
	assert.Len(t, fc.Shares(), 1)
}

func TestFleetMem0Sync_SyncDelegations(t *testing.T) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "mac", Hostname: "macbook", Platform: "darwin"})
	fc.RegisterNode(FleetNode{ID: "wsl", Hostname: "wsl-ubuntu", Platform: "linux"})

	_ = fc.DelegateTask(context.Background(), TaskDelegation{
		ID:         "task-1",
		SourceNode: "mac",
		TargetNode: "wsl",
		TaskType:   "scrape",
	})

	client := &fleetMockMem0Client{}
	sync := NewFleetMem0Sync(fc, client, DefaultFleetMem0SyncConfig())

	synced, err := sync.SyncDelegations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, synced)
	assert.Len(t, client.added, 1)
}

func TestFleetMem0Sync_StartStop(t *testing.T) {
	fc := NewFleetCoordinator()
	client := &fleetMockMem0Client{}

	cfg := DefaultFleetMem0SyncConfig()
	cfg.SyncInterval = 50 * time.Millisecond
	sync := NewFleetMem0Sync(fc, client, cfg)

	sync.Start(context.Background())
	time.Sleep(100 * time.Millisecond)
	sync.Stop()
}

func TestDefaultFleetMem0SyncConfig(t *testing.T) {
	cfg := DefaultFleetMem0SyncConfig()
	assert.Equal(t, "ironclaw-fleet", cfg.AgentID)
	assert.Equal(t, 5*time.Minute, cfg.SyncInterval)
	assert.Equal(t, 0.5, cfg.MinConfidence)
	assert.NotNil(t, cfg.Logger)
}

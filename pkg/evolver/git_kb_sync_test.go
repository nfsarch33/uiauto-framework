package evolver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitKBSync_SyncAll_EmptyStore(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewCapsuleStore(storeDir)
	require.NoError(t, err)

	kbDir := filepath.Join(t.TempDir(), "evolver-kb")
	sync, err := NewGitKBSync(store, GitKBSyncConfig{KBDir: kbDir})
	require.NoError(t, err)

	result, err := sync.SyncAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.CapsulesSynced)
	assert.Equal(t, 0, result.EventsSynced)
	assert.Equal(t, 0, result.GenesSynced)

	_, err = os.Stat(filepath.Join(kbDir, "summary.json"))
	assert.NoError(t, err, "summary.json should exist even with empty store")
}

func TestGitKBSync_SyncAll_WithData(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewCapsuleStore(storeDir)
	require.NoError(t, err)

	ctx := context.Background()

	err = store.SaveCapsule(ctx, &Capsule{
		ID:          "cap-001",
		Name:        "test-capsule",
		Description: "A test capsule for sync",
		GeneIDs:     []string{"gene-001"},
		Status:      CapsuleStatusActive,
		Metrics:     CapsuleMetrics{SuccessRate: 0.95, AvgLatencyMs: 150},
		CreatedAt:   time.Now(),
	})
	require.NoError(t, err)

	err = store.SaveCapsule(ctx, &Capsule{
		ID:          "cap-002",
		Name:        "draft-capsule",
		Description: "A draft capsule",
		GeneIDs:     []string{"gene-002"},
		Status:      CapsuleStatusDraft,
		CreatedAt:   time.Now(),
	})
	require.NoError(t, err)

	err = store.SaveEvent(ctx, &EvolutionEvent{
		ID:        "evt-001",
		Timestamp: time.Now(),
		Type:      EventSignalDetected,
		ActorID:   "engine",
		Outcome:   EventOutcome{Success: true},
	})
	require.NoError(t, err)

	err = store.SaveGene(ctx, &Gene{
		ID:        "gene-001",
		Name:      "retry-on-timeout",
		Category:  GeneCategoryResilience,
		CreatedAt: time.Now(),
	})
	require.NoError(t, err)

	kbDir := filepath.Join(t.TempDir(), "evolver-kb")
	sync, err := NewGitKBSync(store, GitKBSyncConfig{KBDir: kbDir, AgentID: "test-agent"})
	require.NoError(t, err)

	result, err := sync.SyncAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, result.CapsulesSynced)
	assert.Equal(t, 1, result.EventsSynced)
	assert.Equal(t, 1, result.GenesSynced)
	assert.Greater(t, result.Duration, time.Duration(0))

	capData, err := os.ReadFile(filepath.Join(kbDir, "capsules", "cap-001.json"))
	require.NoError(t, err)
	var cap Capsule
	require.NoError(t, json.Unmarshal(capData, &cap))
	assert.Equal(t, "test-capsule", cap.Name)
	assert.Equal(t, CapsuleStatusActive, cap.Status)

	summaryData, err := os.ReadFile(filepath.Join(kbDir, "summary.json"))
	require.NoError(t, err)
	var summary kbSummary
	require.NoError(t, json.Unmarshal(summaryData, &summary))
	assert.Equal(t, "test-agent", summary.AgentID)
	assert.Equal(t, 2, summary.TotalCaps)
	assert.Equal(t, 1, summary.ActiveCaps)
	assert.Equal(t, 1, summary.DraftCaps)
}

func TestGitKBSync_RequiresKBDir(t *testing.T) {
	_, err := NewGitKBSync(nil, GitKBSyncConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "KBDir is required")
}

func TestGitKBSync_IdempotentReSync(t *testing.T) {
	storeDir := t.TempDir()
	store, err := NewCapsuleStore(storeDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = store.SaveCapsule(ctx, &Capsule{
		ID:        "cap-re",
		Name:      "resync-capsule",
		GeneIDs:   []string{"gene-x"},
		Status:    CapsuleStatusActive,
		CreatedAt: time.Now(),
	})
	require.NoError(t, err)

	kbDir := filepath.Join(t.TempDir(), "evolver-kb")
	sync, err := NewGitKBSync(store, GitKBSyncConfig{KBDir: kbDir})
	require.NoError(t, err)

	r1, err := sync.SyncAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, r1.CapsulesSynced)

	r2, err := sync.SyncAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, r2.CapsulesSynced)

	capData, err := os.ReadFile(filepath.Join(kbDir, "capsules", "cap-re.json"))
	require.NoError(t, err)
	assert.Contains(t, string(capData), "resync-capsule")
}

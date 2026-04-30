package domheal

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatternStore_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	pattern := DOMPattern{
		PageID:               "page1",
		SelectorHistory:      []string{".old-selector", ".new-selector"},
		LastKnownFingerprint: "fp-abc123",
		LastSeen:             time.Now(),
		RepairCount:          3,
		StableCount:          10,
		Confidence:           0.85,
	}

	err := ps.Save("page1", pattern)
	require.NoError(t, err)

	loaded, err := ps.Load("page1")
	require.NoError(t, err)
	assert.Equal(t, pattern.PageID, loaded.PageID)
	assert.Equal(t, pattern.SelectorHistory, loaded.SelectorHistory)
	assert.Equal(t, pattern.LastKnownFingerprint, loaded.LastKnownFingerprint)
	assert.Equal(t, pattern.RepairCount, loaded.RepairCount)
	assert.Equal(t, pattern.StableCount, loaded.StableCount)
	assert.InDelta(t, pattern.Confidence, loaded.Confidence, 1e-9)
	assert.WithinDuration(t, pattern.LastSeen, loaded.LastSeen, time.Second)
}

func TestPatternStore_LoadAllWithMultiplePatterns(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	p1 := DOMPattern{
		PageID:          "page1",
		SelectorHistory: []string{".sel1"},
		Confidence:      0.9,
	}
	p2 := DOMPattern{
		PageID:          "page2",
		SelectorHistory: []string{".sel2", ".sel2b"},
		Confidence:      0.75,
	}

	require.NoError(t, ps.Save("page1", p1))
	require.NoError(t, ps.Save("page2", p2))

	all, err := ps.LoadAll()
	require.NoError(t, err)
	assert.Len(t, all, 2)
	assert.Equal(t, "page1", all["page1"].PageID)
	assert.Equal(t, []string{".sel1"}, all["page1"].SelectorHistory)
	assert.Equal(t, "page2", all["page2"].PageID)
	assert.Equal(t, []string{".sel2", ".sel2b"}, all["page2"].SelectorHistory)
}

func TestPatternStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	_, err := ps.Load("nonexistent")
	assert.ErrorIs(t, err, ErrPatternNotFound)
}

func TestPatternStore_Delete(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	pattern := DOMPattern{
		PageID:     "page1",
		Confidence: 0.8,
	}
	require.NoError(t, ps.Save("page1", pattern))

	err := ps.Delete("page1")
	require.NoError(t, err)

	_, err = ps.Load("page1")
	assert.ErrorIs(t, err, ErrPatternNotFound)

	all, err := ps.LoadAll()
	require.NoError(t, err)
	assert.Len(t, all, 0)
}

func TestPatternStore_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	err := ps.Delete("nonexistent")
	assert.ErrorIs(t, err, ErrPatternNotFound)
}

func TestPatternStore_StatsCalculation(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	now := time.Now()
	old := now.Add(-31 * 24 * time.Hour)

	require.NoError(t, ps.Save("fresh", DOMPattern{
		PageID:     "fresh",
		LastSeen:   now,
		Confidence: 0.9,
	}))
	require.NoError(t, ps.Save("stale", DOMPattern{
		PageID:     "stale",
		LastSeen:   old,
		Confidence: 0.7,
	}))

	stats := ps.Stats()
	assert.Equal(t, 2, stats.TotalPatterns)
	assert.Equal(t, 1, stats.StalePatterns)
	assert.InDelta(t, 0.8, stats.AvgConfidence, 1e-9)
}

func TestPatternStore_StatsEmpty(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	stats := ps.Stats()
	assert.Equal(t, 0, stats.TotalPatterns)
	assert.Equal(t, 0, stats.StalePatterns)
	assert.Equal(t, 0.0, stats.AvgConfidence)
}

func TestPatternStore_ConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	pageIDs := []string{"pagea", "pageb", "pagec", "paged", "pagee", "pagef", "pageg", "pageh", "pagei", "pagej"}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pageID := pageIDs[idx%len(pageIDs)]
			pattern := DOMPattern{
				PageID:     pageID,
				Confidence: float64(idx) / 20.0,
			}
			_ = ps.Save(pageID, pattern)
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pageID := pageIDs[idx%len(pageIDs)]
			_, _ = ps.Load(pageID)
		}(i)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ps.LoadAll()
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ps.Stats()
		}()
	}

	wg.Wait()

	all, err := ps.LoadAll()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 1)
}

func TestPatternStore_StalePatternDetection(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	cutoff := time.Now().Add(-StaleThreshold)
	fresh := time.Now().Add(-12 * time.Hour)
	stale := cutoff.Add(-24 * time.Hour)

	require.NoError(t, ps.Save("fresh", DOMPattern{
		PageID:   "fresh",
		LastSeen: fresh,
	}))
	require.NoError(t, ps.Save("stale1", DOMPattern{
		PageID:   "stale1",
		LastSeen: stale,
	}))
	require.NoError(t, ps.Save("stale2", DOMPattern{
		PageID:   "stale2",
		LastSeen: cutoff.Add(-1 * time.Hour),
	}))

	stats := ps.Stats()
	assert.Equal(t, 3, stats.TotalPatterns)
	assert.Equal(t, 2, stats.StalePatterns)
}

func TestPatternStore_FileCorruptionHandling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patterns.json")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(path, []byte("not valid json {{{"), 0644))

	ps := NewPatternStore(dir)

	_, err := ps.Load("any")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")

	_, err = ps.LoadAll()
	assert.Error(t, err)

	stats := ps.Stats()
	assert.Equal(t, 0, stats.TotalPatterns)
}

func TestPatternStore_CreatesDirectoryIfNotExists(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "nested", "patterns")
	ps := NewPatternStore(subDir)

	pattern := DOMPattern{
		PageID:     "page1",
		Confidence: 0.9,
	}
	err := ps.Save("page1", pattern)
	require.NoError(t, err)

	_, err = os.Stat(subDir)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(subDir, "patterns.json"))
	require.NoError(t, err)
}

func TestPatternStore_SaveOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	ps := NewPatternStore(dir)

	require.NoError(t, ps.Save("page1", DOMPattern{
		PageID:     "page1",
		Confidence: 0.5,
	}))

	updated := DOMPattern{
		PageID:     "page1",
		Confidence: 0.95,
	}
	require.NoError(t, ps.Save("page1", updated))

	loaded, err := ps.Load("page1")
	require.NoError(t, err)
	assert.InDelta(t, 0.95, loaded.Confidence, 1e-9)
}

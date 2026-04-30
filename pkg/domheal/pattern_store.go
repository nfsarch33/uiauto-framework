package domheal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// StaleThreshold is the age after which a pattern is considered stale.
	StaleThreshold = 30 * 24 * time.Hour
)

var (
	// ErrPatternNotFound is returned when Load or Delete is called for a non-existent pageID.
	ErrPatternNotFound = errors.New("pattern not found")
)

// DOMPattern holds learned DOM pattern data for a page.
type DOMPattern struct {
	PageID               string    `json:"page_id"`
	SelectorHistory      []string  `json:"selector_history"`
	LastKnownFingerprint string    `json:"last_known_fingerprint"`
	LastSeen             time.Time `json:"last_seen"`
	RepairCount          int       `json:"repair_count"`
	StableCount          int       `json:"stable_count"`
	Confidence           float64   `json:"confidence"`
}

// PatternStoreStats holds aggregate statistics for the pattern store.
type PatternStoreStats struct {
	TotalPatterns int     `json:"total_patterns"`
	StalePatterns int     `json:"stale_patterns"`
	AvgConfidence float64 `json:"avg_confidence"`
}

// PatternStore persists learned DOM patterns to disk as JSON.
type PatternStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewPatternStore creates a PatternStore that persists to baseDir.
func NewPatternStore(baseDir string) *PatternStore {
	return &PatternStore{baseDir: baseDir}
}

// path returns the file path for the patterns JSON file.
func (ps *PatternStore) path() string {
	return filepath.Join(ps.baseDir, "patterns.json")
}

// loadMap reads the patterns map from disk.
// When baseDir is empty, returns an empty map (no persistence).
func (ps *PatternStore) loadMap() (map[string]DOMPattern, error) {
	if ps.baseDir == "" {
		return make(map[string]DOMPattern), nil
	}
	data, err := os.ReadFile(ps.path())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]DOMPattern), nil
		}
		return nil, err
	}
	var m map[string]DOMPattern
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid patterns file: %w", err)
	}
	if m == nil {
		m = make(map[string]DOMPattern)
	}
	return m, nil
}

// saveMap writes the patterns map to disk.
func (ps *PatternStore) saveMap(m map[string]DOMPattern) error {
	if ps.baseDir == "" {
		return nil
	}
	if err := os.MkdirAll(ps.baseDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ps.path(), data, 0644)
}

// Save persists a pattern for the given pageID.
func (ps *PatternStore) Save(pageID string, pattern DOMPattern) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	m, err := ps.loadMap()
	if err != nil {
		return err
	}
	pattern.PageID = pageID
	if pattern.LastSeen.IsZero() {
		pattern.LastSeen = time.Now()
	}
	m[pageID] = pattern
	return ps.saveMap(m)
}

// Load returns the pattern for the given pageID.
func (ps *PatternStore) Load(pageID string) (DOMPattern, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	m, err := ps.loadMap()
	if err != nil {
		return DOMPattern{}, err
	}
	p, ok := m[pageID]
	if !ok {
		return DOMPattern{}, ErrPatternNotFound
	}
	return p, nil
}

// LoadAll returns all persisted patterns.
func (ps *PatternStore) LoadAll() (map[string]DOMPattern, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	m, err := ps.loadMap()
	if err != nil {
		return nil, err
	}
	out := make(map[string]DOMPattern, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out, nil
}

// Delete removes the pattern for the given pageID.
func (ps *PatternStore) Delete(pageID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	m, err := ps.loadMap()
	if err != nil {
		return err
	}
	if _, ok := m[pageID]; !ok {
		return ErrPatternNotFound
	}
	delete(m, pageID)
	return ps.saveMap(m)
}

// Stats returns aggregate statistics for the pattern store.
func (ps *PatternStore) Stats() PatternStoreStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	m, err := ps.loadMap()
	if err != nil {
		return PatternStoreStats{}
	}
	stats := PatternStoreStats{
		TotalPatterns: len(m),
	}
	cutoff := time.Now().Add(-StaleThreshold)
	var sumConf float64
	for _, p := range m {
		if p.LastSeen.Before(cutoff) {
			stats.StalePatterns++
		}
		sumConf += p.Confidence
	}
	if stats.TotalPatterns > 0 {
		stats.AvgConfidence = sumConf / float64(stats.TotalPatterns)
	}
	return stats
}

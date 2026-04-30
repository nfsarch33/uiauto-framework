package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Pattern mirrors the uiauto.UIPattern fields relevant for storage.
type Pattern struct {
	ID          string            `json:"id"`
	Selector    string            `json:"selector"`
	Description string            `json:"description"`
	Fingerprint string            `json:"fingerprint"`
	LastSeen    time.Time         `json:"last_seen"`
	Confidence  float64           `json:"confidence"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// PatternStore defines the storage interface for UI patterns.
type PatternStore interface {
	Get(ctx context.Context, id string) (Pattern, bool)
	Set(ctx context.Context, pattern Pattern) error
	Load(ctx context.Context) (map[string]Pattern, error)
	DecayConfidence(ctx context.Context, olderThan time.Duration, decayFactor float64) error
	BoostConfidence(ctx context.Context, id string, boost float64) error
}

// JSONFileStore implements PatternStore backed by a JSON file on disk.
// This replaces SQLite for zero-dependency local fallback persistence.
type JSONFileStore struct {
	path     string
	patterns map[string]Pattern
	mu       sync.RWMutex
	logger   *slog.Logger
}

// NewJSONFileStore creates or opens a JSON-based pattern store.
// Pass ":memory:" for an in-memory-only store (no file persistence).
func NewJSONFileStore(path string) (*JSONFileStore, error) {
	s := &JSONFileStore{
		path:     path,
		patterns: make(map[string]Pattern),
		logger:   slog.Default(),
	}
	if path != ":memory:" {
		if err := s.loadFromDisk(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("json store: load: %w", err)
		}
	}
	return s, nil
}

// Get retrieves a pattern by ID.
func (s *JSONFileStore) Get(_ context.Context, id string) (Pattern, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.patterns[id]
	return p, ok
}

// Set upserts a pattern and persists to disk.
func (s *JSONFileStore) Set(_ context.Context, p Pattern) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns[p.ID] = p
	return s.saveToDisk()
}

// Load returns all patterns.
func (s *JSONFileStore) Load(_ context.Context) (map[string]Pattern, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]Pattern, len(s.patterns))
	for k, v := range s.patterns {
		result[k] = v
	}
	return result, nil
}

// DecayConfidence reduces confidence for patterns older than the threshold.
func (s *JSONFileStore) DecayConfidence(_ context.Context, olderThan time.Duration, factor float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	for id, p := range s.patterns {
		if p.LastSeen.Before(cutoff) {
			p.Confidence *= factor
			s.patterns[id] = p
		}
	}
	return s.saveToDisk()
}

// BoostConfidence increases a specific pattern's confidence (capped at 1.0).
func (s *JSONFileStore) BoostConfidence(_ context.Context, id string, boost float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.patterns[id]
	if !ok {
		return fmt.Errorf("json store: pattern %q not found", id)
	}
	p.Confidence += boost
	if p.Confidence > 1.0 {
		p.Confidence = 1.0
	}
	s.patterns[id] = p
	return s.saveToDisk()
}

// Close is a no-op for JSON file store (data is saved on every write).
func (s *JSONFileStore) Close() error { return nil }

// Count returns the number of stored patterns.
func (s *JSONFileStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.patterns)
}

func (s *JSONFileStore) loadFromDisk() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.patterns)
}

func (s *JSONFileStore) saveToDisk() error {
	if s.path == ":memory:" {
		return nil
	}
	data, err := json.MarshalIndent(s.patterns, "", "  ")
	if err != nil {
		return fmt.Errorf("json store: marshal: %w", err)
	}
	return os.WriteFile(s.path, data, 0644)
}

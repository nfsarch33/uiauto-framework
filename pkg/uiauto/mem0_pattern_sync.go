package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Mem0PatternClient abstracts Mem0 API for pattern persistence.
type Mem0PatternClient interface {
	Add(ctx context.Context, content string, metadata map[string]string) (string, error)
	Search(ctx context.Context, query string, limit int) ([]Mem0PatternMemory, error)
}

// Mem0PatternMemory represents a memory entry from Mem0.
type Mem0PatternMemory struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
	Score    float64           `json:"score"`
}

// PatternSyncConfig configures the pattern-to-Mem0 synchronization.
type PatternSyncConfig struct {
	AgentID       string
	MinConfidence float64
	Logger        *slog.Logger
}

// PatternMem0Syncer bridges UIPattern storage with Mem0 for fleet-wide sharing.
type PatternMem0Syncer struct {
	store  PatternStorage
	client Mem0PatternClient
	cfg    PatternSyncConfig
	mu     sync.Mutex
	synced int
	failed int
}

// NewPatternMem0Syncer creates a syncer between pattern storage and Mem0.
func NewPatternMem0Syncer(store PatternStorage, client Mem0PatternClient, cfg PatternSyncConfig) *PatternMem0Syncer {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.AgentID == "" {
		cfg.AgentID = "ui-agent"
	}
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.5
	}
	return &PatternMem0Syncer{
		store:  store,
		client: client,
		cfg:    cfg,
	}
}

// SyncPatterns pushes all patterns above MinConfidence to Mem0.
func (s *PatternMem0Syncer) SyncPatterns(ctx context.Context) (synced int, err error) {
	patterns, loadErr := s.store.Load(ctx)
	if loadErr != nil {
		return 0, fmt.Errorf("load patterns: %w", loadErr)
	}

	for id, p := range patterns {
		if p.Confidence < s.cfg.MinConfidence {
			continue
		}

		content := fmt.Sprintf("[UIPattern] %s: selector=%s confidence=%.2f last_seen=%s",
			id, p.Selector, p.Confidence, p.LastSeen.Format(time.RFC3339))

		meta := map[string]string{
			"type":       "ui_pattern",
			"agent_id":   s.cfg.AgentID,
			"pattern_id": id,
			"selector":   p.Selector,
			"confidence": fmt.Sprintf("%.2f", p.Confidence),
		}
		if p.Description != "" {
			meta["description"] = p.Description
		}

		_, addErr := s.client.Add(ctx, content, meta)
		if addErr != nil {
			s.mu.Lock()
			s.failed++
			s.mu.Unlock()
			s.cfg.Logger.Warn("mem0 pattern sync failed", "pattern", id, "err", addErr)
			continue
		}

		s.mu.Lock()
		s.synced++
		s.mu.Unlock()
		synced++
	}

	return synced, nil
}

// SearchPatterns queries Mem0 for UI patterns related to a description.
func (s *PatternMem0Syncer) SearchPatterns(ctx context.Context, query string, limit int) ([]UIPattern, error) {
	if limit <= 0 {
		limit = 5
	}

	memories, err := s.client.Search(ctx, "ui_pattern "+query, limit)
	if err != nil {
		return nil, fmt.Errorf("mem0 search: %w", err)
	}

	var results []UIPattern
	for _, mem := range memories {
		p := UIPattern{
			ID:          mem.Metadata["pattern_id"],
			Selector:    mem.Metadata["selector"],
			Description: mem.Metadata["description"],
			Confidence:  mem.Score,
			LastSeen:    time.Now(),
			Metadata:    mem.Metadata,
		}
		if p.ID != "" && p.Selector != "" {
			results = append(results, p)
		}
	}

	return results, nil
}

// SyncSinglePattern syncs a specific pattern to Mem0 after a successful repair.
func (s *PatternMem0Syncer) SyncSinglePattern(ctx context.Context, p UIPattern) error {
	if p.Confidence < s.cfg.MinConfidence {
		return nil
	}

	patternJSON, _ := json.Marshal(p)
	content := fmt.Sprintf("[UIPattern:Updated] %s: %s", p.ID, string(patternJSON))

	meta := map[string]string{
		"type":       "ui_pattern",
		"agent_id":   s.cfg.AgentID,
		"pattern_id": p.ID,
		"selector":   p.Selector,
		"confidence": fmt.Sprintf("%.2f", p.Confidence),
		"event":      "pattern_updated",
	}

	_, err := s.client.Add(ctx, content, meta)
	if err != nil {
		s.mu.Lock()
		s.failed++
		s.mu.Unlock()
		return fmt.Errorf("sync pattern %s: %w", p.ID, err)
	}

	s.mu.Lock()
	s.synced++
	s.mu.Unlock()
	return nil
}

// Stats returns sync statistics.
func (s *PatternMem0Syncer) Stats() (synced, failed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.synced, s.failed
}

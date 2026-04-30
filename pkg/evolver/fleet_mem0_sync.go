package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// FleetMem0SyncConfig configures fleet-wide pattern sharing via Mem0.
type FleetMem0SyncConfig struct {
	AgentID       string
	SyncInterval  time.Duration
	MinConfidence float64
	Logger        *slog.Logger
}

// DefaultFleetMem0SyncConfig returns production defaults.
func DefaultFleetMem0SyncConfig() FleetMem0SyncConfig {
	return FleetMem0SyncConfig{
		AgentID:       "ironclaw-fleet",
		SyncInterval:  5 * time.Minute,
		MinConfidence: 0.5,
		Logger:        slog.Default(),
	}
}

// FleetMem0Sync bridges FleetCoordinator pattern shares with Mem0 for persistence.
type FleetMem0Sync struct {
	coordinator *FleetCoordinator
	client      Mem0Client
	cfg         FleetMem0SyncConfig
	mu          sync.Mutex
	synced      int
	failed      int
	stopCh      chan struct{}
	running     bool
}

// NewFleetMem0Sync creates a sync bridge between fleet patterns and Mem0.
func NewFleetMem0Sync(coordinator *FleetCoordinator, client Mem0Client, cfg FleetMem0SyncConfig) *FleetMem0Sync {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &FleetMem0Sync{
		coordinator: coordinator,
		client:      client,
		cfg:         cfg,
		stopCh:      make(chan struct{}),
	}
}

// SyncPatterns pushes all fleet pattern shares to Mem0.
func (s *FleetMem0Sync) SyncPatterns(ctx context.Context) (int, error) {
	shares := s.coordinator.Shares()
	synced := 0

	for _, share := range shares {
		content := fmt.Sprintf("[FleetPattern] %s from %s to %s: %s",
			share.PatternID, share.SourceNode, share.TargetNode, share.PatternData)

		meta := map[string]string{
			"type":        "fleet_pattern",
			"agent_id":    s.cfg.AgentID,
			"source_node": share.SourceNode,
			"target_node": share.TargetNode,
			"pattern_id":  share.PatternID,
		}

		_, err := s.client.Add(ctx, content, meta)
		if err != nil {
			s.mu.Lock()
			s.failed++
			s.mu.Unlock()
			s.cfg.Logger.Warn("fleet mem0 sync failed", "pattern", share.PatternID, "err", err)
			continue
		}

		synced++
		s.mu.Lock()
		s.synced++
		s.mu.Unlock()
	}

	return synced, nil
}

// SearchFleetPatterns queries Mem0 for fleet-shared patterns matching a query.
func (s *FleetMem0Sync) SearchFleetPatterns(ctx context.Context, query string, limit int) ([]PatternShare, error) {
	if limit <= 0 {
		limit = 10
	}

	memories, err := s.client.Search(ctx, "fleet_pattern "+query, limit)
	if err != nil {
		return nil, fmt.Errorf("search fleet patterns: %w", err)
	}

	var shares []PatternShare
	for _, m := range memories {
		if m.Score < s.cfg.MinConfidence {
			continue
		}
		share := PatternShare{
			SourceNode:  m.Metadata["source_node"],
			TargetNode:  m.Metadata["target_node"],
			PatternID:   m.Metadata["pattern_id"],
			PatternData: m.Content,
		}
		shares = append(shares, share)
	}

	return shares, nil
}

// ImportPatterns fetches patterns from Mem0 and registers them in the coordinator.
func (s *FleetMem0Sync) ImportPatterns(ctx context.Context, nodeID string, query string) (int, error) {
	memories, err := s.client.Search(ctx, "fleet_pattern "+query, 20)
	if err != nil {
		return 0, fmt.Errorf("import fleet patterns: %w", err)
	}

	imported := 0
	for _, m := range memories {
		if m.Score < s.cfg.MinConfidence {
			continue
		}

		sourceNode := m.Metadata["source_node"]
		patternID := m.Metadata["pattern_id"]
		if sourceNode == "" || patternID == "" {
			continue
		}

		share := PatternShare{
			SourceNode:  sourceNode,
			TargetNode:  nodeID,
			PatternID:   patternID,
			PatternData: m.Content,
			Timestamp:   time.Now(),
		}

		if err := s.coordinator.SharePattern(ctx, share); err != nil {
			s.cfg.Logger.Debug("skip import", "pattern", patternID, "err", err)
			continue
		}
		imported++
	}

	return imported, nil
}

// SyncDelegations pushes task delegations to Mem0 for cross-machine visibility.
func (s *FleetMem0Sync) SyncDelegations(ctx context.Context) (int, error) {
	delegations := s.coordinator.Delegations()
	synced := 0

	for _, d := range delegations {
		data, _ := json.Marshal(d)
		content := fmt.Sprintf("[FleetTask] %s: %s from %s to %s (status=%s)",
			d.ID, d.TaskType, d.SourceNode, d.TargetNode, d.Status)

		meta := map[string]string{
			"type":      "fleet_delegation",
			"agent_id":  s.cfg.AgentID,
			"task_id":   d.ID,
			"task_type": d.TaskType,
			"status":    d.Status,
			"raw":       string(data),
		}

		_, err := s.client.Add(ctx, content, meta)
		if err != nil {
			s.mu.Lock()
			s.failed++
			s.mu.Unlock()
			continue
		}
		synced++
		s.mu.Lock()
		s.synced++
		s.mu.Unlock()
	}

	return synced, nil
}

// Start begins periodic sync in a background goroutine.
func (s *FleetMem0Sync) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(s.cfg.SyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := s.SyncPatterns(ctx); err != nil {
					s.cfg.Logger.Warn("fleet periodic sync failed", "err", err)
				}
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts the periodic sync goroutine.
func (s *FleetMem0Sync) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		close(s.stopCh)
		s.running = false
	}
}

// Stats returns sync statistics.
func (s *FleetMem0Sync) Stats() (synced, failed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.synced, s.failed
}

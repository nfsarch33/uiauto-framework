package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// GitKBSyncConfig configures capsule/event export to a Git-tracked KB directory.
type GitKBSyncConfig struct {
	KBDir   string
	AgentID string
	Logger  *slog.Logger
}

// GitKBSync exports evolver artifacts (capsules, events, genes, traces summary)
// to a Git-friendly directory layout under a knowledge base repo.
type GitKBSync struct {
	store *CapsuleStore
	cfg   GitKBSyncConfig
}

// NewGitKBSync creates a sync bridge between CapsuleStore and a Git KB directory.
func NewGitKBSync(store *CapsuleStore, cfg GitKBSyncConfig) (*GitKBSync, error) {
	if cfg.KBDir == "" {
		return nil, fmt.Errorf("git-kb-sync: KBDir is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.AgentID == "" {
		cfg.AgentID = "ironclaw-evolver"
	}

	dirs := []string{
		filepath.Join(cfg.KBDir, "capsules"),
		filepath.Join(cfg.KBDir, "events"),
		filepath.Join(cfg.KBDir, "genes"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create kb dir %s: %w", d, err)
		}
	}

	return &GitKBSync{store: store, cfg: cfg}, nil
}

// GitKBSyncResult reports what was exported.
type GitKBSyncResult struct {
	CapsulesSynced int           `json:"capsules_synced"`
	EventsSynced   int           `json:"events_synced"`
	GenesSynced    int           `json:"genes_synced"`
	Duration       time.Duration `json:"duration_ms"`
	KBDir          string        `json:"kb_dir"`
}

// SyncAll exports all capsules, events, and genes from CapsuleStore into the KB directory.
func (s *GitKBSync) SyncAll(ctx context.Context) (*GitKBSyncResult, error) {
	start := time.Now()
	result := &GitKBSyncResult{KBDir: s.cfg.KBDir}

	capsules, err := s.store.ListCapsules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list capsules: %w", err)
	}
	for _, c := range capsules {
		if err := s.writePrettyJSON(filepath.Join(s.cfg.KBDir, "capsules", c.ID+".json"), c); err != nil {
			s.cfg.Logger.Warn("failed to sync capsule", "id", c.ID, "err", err)
			continue
		}
		result.CapsulesSynced++
	}

	events, err := s.store.ListEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	for _, ev := range events {
		if err := s.writePrettyJSON(filepath.Join(s.cfg.KBDir, "events", ev.ID+".json"), ev); err != nil {
			s.cfg.Logger.Warn("failed to sync event", "id", ev.ID, "err", err)
			continue
		}
		result.EventsSynced++
	}

	genes, err := s.store.ListGenes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list genes: %w", err)
	}
	for _, g := range genes {
		if err := s.writePrettyJSON(filepath.Join(s.cfg.KBDir, "genes", g.ID+".json"), g); err != nil {
			s.cfg.Logger.Warn("failed to sync gene", "id", g.ID, "err", err)
			continue
		}
		result.GenesSynced++
	}

	if err := s.writeSummary(ctx, result); err != nil {
		s.cfg.Logger.Warn("failed to write summary", "err", err)
	}

	result.Duration = time.Since(start)
	s.cfg.Logger.Info("git-kb sync complete",
		"capsules", result.CapsulesSynced,
		"events", result.EventsSynced,
		"genes", result.GenesSynced,
		"duration", result.Duration)
	return result, nil
}

type kbSummary struct {
	Timestamp    time.Time `json:"timestamp"`
	AgentID      string    `json:"agent_id"`
	TotalCaps    int       `json:"total_capsules"`
	TotalEvents  int       `json:"total_events"`
	TotalGenes   int       `json:"total_genes"`
	ActiveCaps   int       `json:"active_capsules"`
	DraftCaps    int       `json:"draft_capsules"`
	RetiredCaps  int       `json:"retired_capsules"`
	RejectedCaps int       `json:"rejected_capsules"`
}

func (s *GitKBSync) writeSummary(ctx context.Context, result *GitKBSyncResult) error {
	capsules, _ := s.store.ListCapsules(ctx)
	summary := kbSummary{
		Timestamp:   time.Now(),
		AgentID:     s.cfg.AgentID,
		TotalCaps:   result.CapsulesSynced,
		TotalEvents: result.EventsSynced,
		TotalGenes:  result.GenesSynced,
	}
	for _, c := range capsules {
		switch c.Status {
		case CapsuleStatusActive:
			summary.ActiveCaps++
		case CapsuleStatusDraft:
			summary.DraftCaps++
		case CapsuleStatusRetired:
			summary.RetiredCaps++
		case CapsuleStatusRejected:
			summary.RejectedCaps++
		}
	}
	return s.writePrettyJSON(filepath.Join(s.cfg.KBDir, "summary.json"), summary)
}

func (s *GitKBSync) writePrettyJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Mem0Client abstracts the Mem0 memory API for testing.
type Mem0Client interface {
	Add(ctx context.Context, content string, metadata map[string]string) (string, error)
	Search(ctx context.Context, query string, limit int) ([]Mem0Memory, error)
}

// Mem0Memory represents a single memory entry from Mem0.
type Mem0Memory struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
	Score    float64           `json:"score"`
}

// EventLogEntry represents one append-only event in events.jsonl.
type EventLogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Type      string            `json:"type"` // capsule_saved, event_saved, gene_saved, mem0_synced
	EntityID  string            `json:"entity_id"`
	Summary   string            `json:"summary"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Mem0BridgeConfig configures the Mem0 bridge.
type Mem0BridgeConfig struct {
	EventLogPath string
	AgentID      string
	Logger       *slog.Logger
}

// Mem0Bridge syncs capsule store data with Mem0 and maintains events.jsonl.
type Mem0Bridge struct {
	store  *CapsuleStore
	client Mem0Client
	cfg    Mem0BridgeConfig
	mu     sync.Mutex
	synced int
	failed int
}

// NewMem0Bridge creates a bridge between the capsule store and Mem0.
func NewMem0Bridge(store *CapsuleStore, client Mem0Client, cfg Mem0BridgeConfig) *Mem0Bridge {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.AgentID == "" {
		cfg.AgentID = "ironclaw-evolver"
	}
	return &Mem0Bridge{
		store:  store,
		client: client,
		cfg:    cfg,
	}
}

// SyncCapsules pushes all capsules to Mem0 as memory entries.
func (b *Mem0Bridge) SyncCapsules(ctx context.Context) (int, error) {
	capsules, err := b.store.ListCapsules(ctx)
	if err != nil {
		return 0, fmt.Errorf("list capsules: %w", err)
	}

	synced := 0
	for _, c := range capsules {
		content := fmt.Sprintf("[Capsule] %s: %s (status=%s, success_rate=%.2f)",
			c.ID, c.Description, c.Status, c.Metrics.SuccessRate)

		meta := map[string]string{
			"type":     "evolution_capsule",
			"agent_id": b.cfg.AgentID,
			"status":   string(c.Status),
		}

		_, err := b.client.Add(ctx, content, meta)
		if err != nil {
			b.mu.Lock()
			b.failed++
			b.mu.Unlock()
			b.cfg.Logger.Warn("mem0 sync failed", "capsule", c.ID, "err", err)
			continue
		}

		synced++
		b.mu.Lock()
		b.synced++
		b.mu.Unlock()

		_ = b.appendEventLog(EventLogEntry{
			Timestamp: time.Now(),
			Type:      "mem0_synced",
			EntityID:  c.ID,
			Summary:   content,
			Metadata:  meta,
		})
	}

	return synced, nil
}

// SyncEvents pushes all evolution events to Mem0.
func (b *Mem0Bridge) SyncEvents(ctx context.Context) (int, error) {
	events, err := b.store.ListEvents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list events: %w", err)
	}

	synced := 0
	for _, ev := range events {
		content := fmt.Sprintf("[Event] %s: type=%s actor=%s success=%v",
			ev.ID, ev.Type, ev.ActorID, ev.Outcome.Success)

		meta := map[string]string{
			"type":       "evolution_event",
			"event_type": string(ev.Type),
			"agent_id":   b.cfg.AgentID,
		}

		_, err := b.client.Add(ctx, content, meta)
		if err != nil {
			b.mu.Lock()
			b.failed++
			b.mu.Unlock()
			continue
		}

		synced++
		b.mu.Lock()
		b.synced++
		b.mu.Unlock()
	}

	return synced, nil
}

// SearchRelated queries Mem0 for memories related to an evolution context.
func (b *Mem0Bridge) SearchRelated(ctx context.Context, query string, limit int) ([]Mem0Memory, error) {
	if limit <= 0 {
		limit = 5
	}
	return b.client.Search(ctx, query, limit)
}

// Stats returns sync statistics.
func (b *Mem0Bridge) Stats() (synced, failed int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.synced, b.failed
}

// appendEventLog writes an entry to events.jsonl.
func (b *Mem0Bridge) appendEventLog(entry EventLogEntry) error {
	if b.cfg.EventLogPath == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(b.cfg.EventLogPath), 0o755); err != nil {
		return err
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(b.cfg.EventLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadEventLog reads all entries from events.jsonl.
func ReadEventLog(path string) ([]EventLogEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []EventLogEntry
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var e EventLogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

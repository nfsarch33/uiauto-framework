package evolver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockMem0Client struct {
	memories []Mem0Memory
	addErr   error
	added    []string
}

func (m *mockMem0Client) Add(_ context.Context, content string, metadata map[string]string) (string, error) {
	if m.addErr != nil {
		return "", m.addErr
	}
	id := fmt.Sprintf("mem-%d", len(m.added))
	m.added = append(m.added, content)
	m.memories = append(m.memories, Mem0Memory{
		ID: id, Content: content, Metadata: metadata, Score: 0.9,
	})
	return id, nil
}

func (m *mockMem0Client) Search(_ context.Context, query string, limit int) ([]Mem0Memory, error) {
	var results []Mem0Memory
	for _, mem := range m.memories {
		if strings.Contains(strings.ToLower(mem.Content), strings.ToLower(query)) {
			results = append(results, mem)
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func TestMem0Bridge_SyncCapsules(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	ctx := context.Background()
	_ = store.SaveCapsule(ctx, &Capsule{
		ID: "cap-1", Description: "DOM healing improvement",
		Status: CapsuleStatusActive, Metrics: CapsuleMetrics{SuccessRate: 0.8},
	})
	_ = store.SaveCapsule(ctx, &Capsule{
		ID: "cap-2", Description: "VLM accuracy boost",
		Status: CapsuleStatusTesting, Metrics: CapsuleMetrics{SuccessRate: 0.9},
	})

	client := &mockMem0Client{}
	logPath := filepath.Join(dir, "events.jsonl")

	bridge := NewMem0Bridge(store, client, Mem0BridgeConfig{
		EventLogPath: logPath,
		AgentID:      "test-agent",
	})

	synced, err := bridge.SyncCapsules(ctx)
	if err != nil {
		t.Fatalf("sync capsules: %v", err)
	}
	if synced != 2 {
		t.Errorf("expected 2 synced, got %d", synced)
	}
	if len(client.added) != 2 {
		t.Errorf("expected 2 mem0 entries, got %d", len(client.added))
	}

	s, f := bridge.Stats()
	if s != 2 || f != 0 {
		t.Errorf("expected stats synced=2 failed=0, got synced=%d failed=%d", s, f)
	}

	entries, err := ReadEventLog(logPath)
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 event log entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Type != "mem0_synced" {
			t.Errorf("expected type mem0_synced, got %s", e.Type)
		}
	}
}

func TestMem0Bridge_SyncEvents(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	ctx := context.Background()
	_ = store.SaveEvent(ctx, &EvolutionEvent{
		ID: "ev-1", Type: EventMutationProposed,
		ActorID:   "test-agent",
		Timestamp: time.Now(),
	})

	client := &mockMem0Client{}
	bridge := NewMem0Bridge(store, client, Mem0BridgeConfig{AgentID: "test"})

	synced, err := bridge.SyncEvents(ctx)
	if err != nil {
		t.Fatalf("sync events: %v", err)
	}
	if synced != 1 {
		t.Errorf("expected 1 synced, got %d", synced)
	}
}

func TestMem0Bridge_SearchRelated(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &mockMem0Client{
		memories: []Mem0Memory{
			{ID: "m1", Content: "DOM healing pattern for D2L", Score: 0.9},
			{ID: "m2", Content: "VLM accuracy improvement", Score: 0.8},
		},
	}

	bridge := NewMem0Bridge(store, client, Mem0BridgeConfig{})

	results, err := bridge.SearchRelated(context.Background(), "DOM", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for DOM query, got %d", len(results))
	}
}

func TestMem0Bridge_SyncFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	ctx := context.Background()
	_ = store.SaveCapsule(ctx, &Capsule{
		ID: "cap-fail", Description: "Will fail sync",
		Status: CapsuleStatusDraft, Metrics: CapsuleMetrics{SuccessRate: 0.5},
	})

	client := &mockMem0Client{addErr: fmt.Errorf("mem0 unavailable")}
	bridge := NewMem0Bridge(store, client, Mem0BridgeConfig{})

	synced, err := bridge.SyncCapsules(ctx)
	if err != nil {
		t.Fatalf("sync should not return error, got: %v", err)
	}
	if synced != 0 {
		t.Errorf("expected 0 synced on failure, got %d", synced)
	}

	_, f := bridge.Stats()
	if f != 1 {
		t.Errorf("expected 1 failure, got %d", f)
	}
}

func TestEventLogEntry_Fields(t *testing.T) {
	e := EventLogEntry{
		Timestamp: time.Now(),
		Type:      "capsule_saved",
		EntityID:  "c1",
		Summary:   "test",
		Metadata:  map[string]string{"key": "val"},
	}
	if e.Type != "capsule_saved" || e.EntityID != "c1" {
		t.Error("field mismatch")
	}
}

func TestReadEventLog_NonExistent(t *testing.T) {
	entries, err := ReadEventLog("/tmp/nonexistent-event-log.jsonl")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %d", len(entries))
	}
}

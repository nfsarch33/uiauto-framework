package uiauto

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type mockMem0Client struct {
	added    []mockMem0Add
	memories []Mem0PatternMemory
	addErr   error
}

type mockMem0Add struct {
	content  string
	metadata map[string]string
}

func (m *mockMem0Client) Add(_ context.Context, content string, metadata map[string]string) (string, error) {
	if m.addErr != nil {
		return "", m.addErr
	}
	m.added = append(m.added, mockMem0Add{content: content, metadata: metadata})
	return fmt.Sprintf("mem-%d", len(m.added)), nil
}

func (m *mockMem0Client) Search(_ context.Context, _ string, limit int) ([]Mem0PatternMemory, error) {
	if limit > len(m.memories) {
		limit = len(m.memories)
	}
	return m.memories[:limit], nil
}

func newTestPatternStore(t *testing.T) PatternStorage {
	t.Helper()
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "patterns.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestPatternMem0Syncer_SyncPatterns(t *testing.T) {
	store := newTestPatternStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, UIPattern{
		ID:         "btn-login",
		Selector:   "button.login",
		Confidence: 0.9,
		LastSeen:   time.Now(),
	})
	_ = store.Set(ctx, UIPattern{
		ID:         "low-conf",
		Selector:   "div.unknown",
		Confidence: 0.3,
		LastSeen:   time.Now(),
	})

	client := &mockMem0Client{}
	syncer := NewPatternMem0Syncer(store, client, PatternSyncConfig{
		AgentID:       "test-ui-agent",
		MinConfidence: 0.5,
	})

	synced, err := syncer.SyncPatterns(ctx)
	if err != nil {
		t.Fatalf("SyncPatterns: %v", err)
	}
	if synced != 1 {
		t.Errorf("synced = %d, want 1 (only high-confidence pattern)", synced)
	}
	if len(client.added) != 1 {
		t.Errorf("client.added = %d, want 1", len(client.added))
	}
	if client.added[0].metadata["pattern_id"] != "btn-login" {
		t.Errorf("synced pattern_id = %q, want btn-login", client.added[0].metadata["pattern_id"])
	}

	s, f := syncer.Stats()
	if s != 1 || f != 0 {
		t.Errorf("Stats = %d/%d, want 1/0", s, f)
	}
}

func TestPatternMem0Syncer_SyncWithErrors(t *testing.T) {
	store := newTestPatternStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, UIPattern{
		ID:         "btn-submit",
		Selector:   "button[type=submit]",
		Confidence: 0.8,
		LastSeen:   time.Now(),
	})

	client := &mockMem0Client{addErr: fmt.Errorf("mem0 unavailable")}
	syncer := NewPatternMem0Syncer(store, client, PatternSyncConfig{})

	synced, err := syncer.SyncPatterns(ctx)
	if err != nil {
		t.Fatalf("SyncPatterns should not return error on individual failures: %v", err)
	}
	if synced != 0 {
		t.Errorf("synced = %d, want 0", synced)
	}

	s, f := syncer.Stats()
	if s != 0 || f != 1 {
		t.Errorf("Stats = %d/%d, want 0/1", s, f)
	}
}

func TestPatternMem0Syncer_SearchPatterns(t *testing.T) {
	store := newTestPatternStore(t)
	client := &mockMem0Client{
		memories: []Mem0PatternMemory{
			{
				ID:      "mem-1",
				Content: "[UIPattern] btn-login: selector=button.login",
				Metadata: map[string]string{
					"pattern_id":  "btn-login",
					"selector":    "button.login",
					"description": "Login button",
				},
				Score: 0.92,
			},
			{
				ID:      "mem-2",
				Content: "[UIPattern] nav-home: selector=a.home",
				Metadata: map[string]string{
					"pattern_id": "nav-home",
					"selector":   "a.home",
				},
				Score: 0.75,
			},
		},
	}
	syncer := NewPatternMem0Syncer(store, client, PatternSyncConfig{})

	ctx := context.Background()
	results, err := syncer.SearchPatterns(ctx, "login button", 5)
	if err != nil {
		t.Fatalf("SearchPatterns: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].ID != "btn-login" {
		t.Errorf("results[0].ID = %q, want btn-login", results[0].ID)
	}
	if results[0].Selector != "button.login" {
		t.Errorf("results[0].Selector = %q, want button.login", results[0].Selector)
	}
}

func TestPatternMem0Syncer_SyncSinglePattern(t *testing.T) {
	store := newTestPatternStore(t)
	client := &mockMem0Client{}
	syncer := NewPatternMem0Syncer(store, client, PatternSyncConfig{
		MinConfidence: 0.5,
	})

	ctx := context.Background()
	p := UIPattern{
		ID:         "form-email",
		Selector:   "input[name=email]",
		Confidence: 0.88,
		LastSeen:   time.Now(),
	}

	err := syncer.SyncSinglePattern(ctx, p)
	if err != nil {
		t.Fatalf("SyncSinglePattern: %v", err)
	}
	if len(client.added) != 1 {
		t.Fatalf("added = %d, want 1", len(client.added))
	}
	if client.added[0].metadata["event"] != "pattern_updated" {
		t.Errorf("event = %q, want pattern_updated", client.added[0].metadata["event"])
	}
}

func TestPatternMem0Syncer_SyncSinglePattern_LowConfidence(t *testing.T) {
	store := newTestPatternStore(t)
	client := &mockMem0Client{}
	syncer := NewPatternMem0Syncer(store, client, PatternSyncConfig{
		MinConfidence: 0.5,
	})

	ctx := context.Background()
	p := UIPattern{
		ID:         "low-conf-elem",
		Selector:   "div.maybe",
		Confidence: 0.3,
	}

	err := syncer.SyncSinglePattern(ctx, p)
	if err != nil {
		t.Fatalf("SyncSinglePattern: %v", err)
	}
	if len(client.added) != 0 {
		t.Error("should not sync low-confidence pattern")
	}
}

func TestPatternMem0Syncer_DefaultConfig(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	_ = os.WriteFile(storePath, []byte("{}"), 0644)

	store, err := NewPatternStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	syncer := NewPatternMem0Syncer(store, &mockMem0Client{}, PatternSyncConfig{})
	if syncer.cfg.AgentID != "ui-agent" {
		t.Errorf("default AgentID = %q, want ui-agent", syncer.cfg.AgentID)
	}
	if syncer.cfg.MinConfidence != 0.5 {
		t.Errorf("default MinConfidence = %f, want 0.5", syncer.cfg.MinConfidence)
	}
}

package evolver

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *CapsuleStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewCapsuleStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCapsuleStore_SaveLoadCapsule(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cap := &Capsule{
		ID:        "cap-1",
		Name:      "test capsule",
		Status:    CapsuleStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		GeneIDs:   []string{"g1"},
	}
	if err := s.SaveCapsule(ctx, cap); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadCapsule(ctx, "cap-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "test capsule" {
		t.Errorf("name=%s want test capsule", loaded.Name)
	}
}

func TestCapsuleStore_ListCapsules(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	for i, id := range []string{"c1", "c2", "c3"} {
		_ = s.SaveCapsule(ctx, &Capsule{
			ID:        id,
			Name:      id,
			Status:    CapsuleStatusActive,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	caps, err := s.ListCapsules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 3 {
		t.Fatalf("len=%d want 3", len(caps))
	}
	// Newest first (c3 was saved last, UpdatedAt overwritten by Save)
	if caps[0].ID != "c3" {
		t.Errorf("first=%s want c3", caps[0].ID)
	}
}

func TestCapsuleStore_SaveLoadGene(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	gene := &Gene{
		ID:        "g-1",
		Name:      "test gene",
		Category:  GeneCategoryTool,
		Payload:   json.RawMessage(`{"tool":"echo"}`),
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		BlastRadius: BlastRadius{
			AffectedModules: []string{"tools"},
			Level:           RiskLow,
		},
	}
	if err := s.SaveGene(ctx, gene); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadGene(ctx, "g-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "test gene" {
		t.Errorf("name=%s want test gene", loaded.Name)
	}
	if loaded.Category != GeneCategoryTool {
		t.Errorf("category=%s want tool", loaded.Category)
	}
}

func TestCapsuleStore_ListGenes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"g1", "g2"} {
		_ = s.SaveGene(ctx, &Gene{
			ID: id, Name: id, Category: GeneCategoryTool, Version: 1,
			Payload:     json.RawMessage(`{}`),
			BlastRadius: BlastRadius{Level: RiskLow},
			CreatedAt:   time.Now(),
		})
	}

	genes, err := s.ListGenes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(genes) != 2 {
		t.Fatalf("len=%d want 2", len(genes))
	}
}

func TestCapsuleStore_SaveListEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	for i, id := range []string{"e1", "e2"} {
		_ = s.SaveEvent(ctx, &EvolutionEvent{
			ID:        id,
			Type:      EventMutationProposed,
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}

	events, err := s.ListEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("len=%d want 2", len(events))
	}
	if events[0].ID != "e2" {
		t.Errorf("first=%s want e2", events[0].ID)
	}
}

func TestCapsuleStore_LoadNonExistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.LoadCapsule(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent capsule")
	}

	_, err = s.LoadGene(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent gene")
	}
}

func TestCapsuleStore_OverwriteUpdatesTimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cap := &Capsule{
		ID:        "overwrite",
		Name:      "v1",
		Status:    CapsuleStatusActive,
		CreatedAt: time.Now(),
	}
	_ = s.SaveCapsule(ctx, cap)
	first, _ := s.LoadCapsule(ctx, "overwrite")

	time.Sleep(10 * time.Millisecond)
	cap.Name = "v2"
	_ = s.SaveCapsule(ctx, cap)
	second, _ := s.LoadCapsule(ctx, "overwrite")

	if second.Name != "v2" {
		t.Errorf("name=%s want v2", second.Name)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Error("UpdatedAt should advance on overwrite")
	}
}

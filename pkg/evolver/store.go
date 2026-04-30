package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CapsuleStore persists capsules and events to a git-friendly directory layout:
//
//	<baseDir>/
//	  capsules/<id>.json
//	  events/<id>.json
//	  genes/<id>.json
type CapsuleStore struct {
	mu      sync.RWMutex
	baseDir string
}

// NewCapsuleStore returns a store rooted at baseDir.
func NewCapsuleStore(baseDir string) (*CapsuleStore, error) {
	for _, sub := range []string{"capsules", "events", "genes"} {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create %s dir: %w", sub, err)
		}
	}
	return &CapsuleStore{baseDir: baseDir}, nil
}

// SaveCapsule writes a capsule to disk.
func (s *CapsuleStore) SaveCapsule(_ context.Context, c *Capsule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal capsule: %w", err)
	}
	path := filepath.Join(s.baseDir, "capsules", c.ID+".json")
	return os.WriteFile(path, data, 0o644)
}

// LoadCapsule reads a capsule from disk.
func (s *CapsuleStore) LoadCapsule(_ context.Context, id string) (*Capsule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "capsules", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capsule %s: %w", id, err)
	}
	var c Capsule
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("unmarshal capsule: %w", err)
	}
	return &c, nil
}

// ListCapsules returns all capsules sorted by UpdatedAt descending.
func (s *CapsuleStore) ListCapsules(_ context.Context) ([]Capsule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.baseDir, "capsules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list capsules: %w", err)
	}

	var result []Capsule
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c Capsule
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		result = append(result, c)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

// SaveEvent persists an evolution event.
func (s *CapsuleStore) SaveEvent(_ context.Context, ev *EvolutionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	path := filepath.Join(s.baseDir, "events", ev.ID+".json")
	return os.WriteFile(path, data, 0o644)
}

// ListEvents returns all events sorted by Timestamp descending.
func (s *CapsuleStore) ListEvents(_ context.Context) ([]EvolutionEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.baseDir, "events")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	var result []EvolutionEvent
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var ev EvolutionEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		result = append(result, ev)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})
	return result, nil
}

// SaveGene persists a gene.
func (s *CapsuleStore) SaveGene(_ context.Context, g *Gene) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	g.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gene: %w", err)
	}
	path := filepath.Join(s.baseDir, "genes", g.ID+".json")
	return os.WriteFile(path, data, 0o644)
}

// LoadGene reads a gene from disk.
func (s *CapsuleStore) LoadGene(_ context.Context, id string) (*Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "genes", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gene %s: %w", id, err)
	}
	var g Gene
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("unmarshal gene: %w", err)
	}
	return &g, nil
}

// ListGenes returns all genes sorted by UpdatedAt descending.
func (s *CapsuleStore) ListGenes(_ context.Context) ([]Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.baseDir, "genes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list genes: %w", err)
	}

	var result []Gene
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var g Gene
		if err := json.Unmarshal(data, &g); err != nil {
			continue
		}
		result = append(result, g)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

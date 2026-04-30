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

// Store abstracts persistence for the evolver framework.
// Both filesystem and PostgreSQL implementations satisfy this interface.
type Store interface {
	SaveCapsule(ctx context.Context, c *Capsule) error
	LoadCapsule(ctx context.Context, id string) (*Capsule, error)
	ListCapsules(ctx context.Context) ([]Capsule, error)

	SaveEvent(ctx context.Context, ev *EvolutionEvent) error
	ListEvents(ctx context.Context) ([]EvolutionEvent, error)

	SaveGene(ctx context.Context, g *Gene) error
	LoadGene(ctx context.Context, id string) (*Gene, error)
	ListGenes(ctx context.Context) ([]Gene, error)

	SaveFleetNode(ctx context.Context, node *FleetNode) error
	ListFleetNodes(ctx context.Context) ([]FleetNode, error)

	SavePatternShare(ctx context.Context, share *PatternShare) error
	ListPatternShares(ctx context.Context) ([]PatternShare, error)

	SaveTaskDelegation(ctx context.Context, d *TaskDelegation) error
	ListTaskDelegations(ctx context.Context) ([]TaskDelegation, error)

	SaveFeedbackSignal(ctx context.Context, s *FeedbackSignal) error
	ListFeedbackSignals(ctx context.Context) ([]FeedbackSignal, error)

	SaveFeedbackAction(ctx context.Context, a *FeedbackAction) error
	ListFeedbackActions(ctx context.Context) ([]FeedbackAction, error)
}

// FileStore implements Store using JSON files on disk.
// Extends the original CapsuleStore with fleet and feedback persistence.
type FileStore struct {
	mu      sync.RWMutex
	baseDir string
}

// NewFileStore creates a file-based store at baseDir.
func NewFileStore(baseDir string) (*FileStore, error) {
	for _, sub := range []string{
		"capsules", "events", "genes",
		"fleet/nodes", "fleet/shares", "fleet/delegations",
		"feedback/signals", "feedback/actions",
	} {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create %s dir: %w", sub, err)
		}
	}
	return &FileStore{baseDir: baseDir}, nil
}

func (s *FileStore) saveJSON(subDir, id string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(s.baseDir, subDir, id+".json")
	return os.WriteFile(path, data, 0o644)
}

func (s *FileStore) loadJSON(subDir, id string, v any) error {
	path := filepath.Join(s.baseDir, subDir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s/%s: %w", subDir, id, err)
	}
	return json.Unmarshal(data, v)
}

func (s *FileStore) listJSONDir(subDir string) ([][]byte, error) {
	dir := filepath.Join(s.baseDir, subDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var items [][]byte
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		items = append(items, data)
	}
	return items, nil
}

func (s *FileStore) SaveCapsule(_ context.Context, c *Capsule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.UpdatedAt = time.Now()
	return s.saveJSON("capsules", c.ID, c)
}

func (s *FileStore) LoadCapsule(_ context.Context, id string) (*Capsule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var c Capsule
	if err := s.loadJSON("capsules", id, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *FileStore) ListCapsules(_ context.Context) ([]Capsule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("capsules")
	if err != nil {
		return nil, err
	}
	var result []Capsule
	for _, data := range items {
		var c Capsule
		if json.Unmarshal(data, &c) == nil {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func (s *FileStore) SaveEvent(_ context.Context, ev *EvolutionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveJSON("events", ev.ID, ev)
}

func (s *FileStore) ListEvents(_ context.Context) ([]EvolutionEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("events")
	if err != nil {
		return nil, err
	}
	var result []EvolutionEvent
	for _, data := range items {
		var ev EvolutionEvent
		if json.Unmarshal(data, &ev) == nil {
			result = append(result, ev)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})
	return result, nil
}

func (s *FileStore) SaveGene(_ context.Context, g *Gene) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.UpdatedAt = time.Now()
	return s.saveJSON("genes", g.ID, g)
}

func (s *FileStore) LoadGene(_ context.Context, id string) (*Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var g Gene
	if err := s.loadJSON("genes", id, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *FileStore) ListGenes(_ context.Context) ([]Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("genes")
	if err != nil {
		return nil, err
	}
	var result []Gene
	for _, data := range items {
		var g Gene
		if json.Unmarshal(data, &g) == nil {
			result = append(result, g)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func (s *FileStore) SaveFleetNode(_ context.Context, node *FleetNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveJSON("fleet/nodes", node.ID, node)
}

func (s *FileStore) ListFleetNodes(_ context.Context) ([]FleetNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("fleet/nodes")
	if err != nil {
		return nil, err
	}
	var result []FleetNode
	for _, data := range items {
		var n FleetNode
		if json.Unmarshal(data, &n) == nil {
			result = append(result, n)
		}
	}
	return result, nil
}

func (s *FileStore) SavePatternShare(_ context.Context, share *PatternShare) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("%s-%s-%d", share.SourceNode, share.PatternID, share.Timestamp.UnixMilli())
	return s.saveJSON("fleet/shares", id, share)
}

func (s *FileStore) ListPatternShares(_ context.Context) ([]PatternShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("fleet/shares")
	if err != nil {
		return nil, err
	}
	var result []PatternShare
	for _, data := range items {
		var ps PatternShare
		if json.Unmarshal(data, &ps) == nil {
			result = append(result, ps)
		}
	}
	return result, nil
}

func (s *FileStore) SaveTaskDelegation(_ context.Context, d *TaskDelegation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveJSON("fleet/delegations", d.ID, d)
}

func (s *FileStore) ListTaskDelegations(_ context.Context) ([]TaskDelegation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("fleet/delegations")
	if err != nil {
		return nil, err
	}
	var result []TaskDelegation
	for _, data := range items {
		var d TaskDelegation
		if json.Unmarshal(data, &d) == nil {
			result = append(result, d)
		}
	}
	return result, nil
}

func (s *FileStore) SaveFeedbackSignal(_ context.Context, sig *FeedbackSignal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("%s-%s-%d", sig.Source, sig.Metric, sig.Timestamp.UnixMilli())
	return s.saveJSON("feedback/signals", id, sig)
}

func (s *FileStore) ListFeedbackSignals(_ context.Context) ([]FeedbackSignal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("feedback/signals")
	if err != nil {
		return nil, err
	}
	var result []FeedbackSignal
	for _, data := range items {
		var s FeedbackSignal
		if json.Unmarshal(data, &s) == nil {
			result = append(result, s)
		}
	}
	return result, nil
}

func (s *FileStore) SaveFeedbackAction(_ context.Context, a *FeedbackAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("%s-%s-%d", a.SignalSource, a.ActionType, time.Now().UnixMilli())
	return s.saveJSON("feedback/actions", id, a)
}

func (s *FileStore) ListFeedbackActions(_ context.Context) ([]FeedbackAction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listJSONDir("feedback/actions")
	if err != nil {
		return nil, err
	}
	var result []FeedbackAction
	for _, data := range items {
		var a FeedbackAction
		if json.Unmarshal(data, &a) == nil {
			result = append(result, a)
		}
	}
	return result, nil
}

// Compile-time check: FileStore implements Store.
var _ Store = (*FileStore)(nil)

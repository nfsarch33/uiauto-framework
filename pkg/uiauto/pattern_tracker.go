package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
)

// UIPattern represents a discovered UI element pattern and its selector.
type UIPattern struct {
	ID          string                 `json:"id"`          // Unique identifier for the pattern (e.g., "login_button")
	Selector    string                 `json:"selector"`    // The CSS selector to find the element
	Description string                 `json:"description"` // Human-readable description
	Fingerprint domheal.DOMFingerprint `json:"fingerprint"` // The DOM fingerprint of the element or its container
	LastSeen    time.Time              `json:"last_seen"`   // When this pattern was last successfully used
	Confidence  float64                `json:"confidence"`  // Confidence score (0.0 to 1.0)
	Metadata    map[string]string      `json:"metadata"`    // Additional context
}

// PatternStorage defines the interface for persisting UI patterns.
type PatternStorage interface {
	Get(ctx context.Context, id string) (UIPattern, bool)
	Set(ctx context.Context, pattern UIPattern) error
	Load(ctx context.Context) (map[string]UIPattern, error)
	DecayConfidence(ctx context.Context, olderThan time.Duration, decayFactor float64) error
	BoostConfidence(ctx context.Context, id string, boost float64) error
}

// JSONPatternStore manages persistent storage of UI patterns.
type JSONPatternStore struct {
	mu       sync.RWMutex
	filePath string
	patterns map[string]UIPattern
}

// NewPatternStore creates a new pattern store backed by a JSON file.
func NewPatternStore(filePath string) (*JSONPatternStore, error) {
	store := &JSONPatternStore{
		filePath: filePath,
		patterns: make(map[string]UIPattern),
	}
	if _, err := store.Load(context.Background()); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load pattern store: %w", err)
	}
	return store, nil
}

// Load reads patterns from disk.
func (s *JSONPatternStore) Load(ctx context.Context) (map[string]UIPattern, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &s.patterns); err != nil {
		return nil, err
	}

	// Create a copy to return
	out := make(map[string]UIPattern)
	for k, v := range s.patterns {
		out[k] = v
	}
	return out, nil
}

// Save writes patterns to disk (acquires read lock).
func (s *JSONPatternStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

// saveLocked writes patterns to disk without acquiring the lock.
// Caller must hold at least a read lock on s.mu.
func (s *JSONPatternStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.patterns, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

// Get retrieves a pattern by ID.
func (s *JSONPatternStore) Get(ctx context.Context, id string) (UIPattern, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.patterns[id]
	return p, ok
}

// Set stores a pattern and saves to disk.
func (s *JSONPatternStore) Set(ctx context.Context, pattern UIPattern) error {
	s.mu.Lock()
	s.patterns[pattern.ID] = pattern
	s.mu.Unlock()
	return s.Save()
}

// DecayConfidence reduces confidence of old patterns.
func (s *JSONPatternStore) DecayConfidence(ctx context.Context, olderThan time.Duration, decayFactor float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	changed := false
	for id, p := range s.patterns {
		if p.LastSeen.Before(cutoff) && p.Confidence > 0.1 {
			p.Confidence *= decayFactor
			s.patterns[id] = p
			changed = true
		}
	}

	if changed {
		return s.saveLocked()
	}
	return nil
}

// BoostConfidence increases a pattern's confidence after a successful use.
func (s *JSONPatternStore) BoostConfidence(ctx context.Context, id string, boost float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.patterns[id]
	if !ok {
		return fmt.Errorf("pattern not found: %s", id)
	}
	p.Confidence += boost
	if p.Confidence > 1.0 {
		p.Confidence = 1.0
	}
	p.LastSeen = time.Now()
	s.patterns[id] = p
	return s.saveLocked()
}

// ScoringConfig holds tunable weights for FindBestMatch scoring.
type ScoringConfig struct {
	SimilarityWeight  float64       // Weight for structural similarity (default 0.7)
	ConfidenceWeight  float64       // Weight for stored confidence (default 0.3)
	BaseThreshold     float64       // Base matching threshold (default 0.6)
	MinThreshold      float64       // Floor for adaptive threshold (default 0.45)
	TimeDecayHalfLife time.Duration // After this duration since LastSeen, confidence decays by half (0 = disabled)
}

// DefaultScoringConfig returns production defaults matching the original fixed weights.
func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		SimilarityWeight:  0.7,
		ConfidenceWeight:  0.3,
		BaseThreshold:     0.6,
		MinThreshold:      0.45,
		TimeDecayHalfLife: 0,
	}
}

// PatternTracker combines drift detection with pattern storage.
type PatternTracker struct {
	store     PatternStorage
	detector  *domheal.DriftDetector
	matcher   *domheal.FingerprintMatcher
	repairLog *domheal.RepairLog
	scoring   ScoringConfig
}

// NewPatternTracker creates a new pattern tracker.
func NewPatternTracker(storePath string, driftDir string) (*PatternTracker, error) {
	store, err := NewPatternStore(storePath)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(driftDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create drift dir: %w", err)
	}
	detector := domheal.NewDriftDetector(driftDir)
	// Try to load existing drift state
	_ = detector.Load()

	repairLogPath := filepath.Join(driftDir, "repair_log.jsonl")

	return &PatternTracker{
		store:     store,
		detector:  detector,
		matcher:   domheal.NewFingerprintMatcher(0.7, slog.New(slog.NewTextHandler(io.Discard, nil))),
		repairLog: domheal.NewRepairLog(repairLogPath),
		scoring:   DefaultScoringConfig(),
	}, nil
}

// NewPatternTrackerWithStore creates a tracker with a custom storage backend.
func NewPatternTrackerWithStore(store PatternStorage, driftDir string) *PatternTracker {
	detector := domheal.NewDriftDetector(driftDir)
	_ = detector.Load()

	repairPath := filepath.Join(driftDir, "repair_log.jsonl")
	repairLog := domheal.NewRepairLog(repairPath)

	return &PatternTracker{
		store:     store,
		detector:  detector,
		matcher:   domheal.NewFingerprintMatcher(0.6, slog.New(slog.NewTextHandler(io.Discard, nil))),
		repairLog: repairLog,
		scoring:   DefaultScoringConfig(),
	}
}

// WithScoringConfig replaces the default scoring weights.
func (t *PatternTracker) WithScoringConfig(cfg ScoringConfig) {
	t.scoring = cfg
}

// CheckDrift checks if the HTML for a given page ID has drifted.
func (t *PatternTracker) CheckDrift(pageID, html string) (bool, error) {
	drifted, err := t.detector.CheckAndUpdate(pageID, html)
	if err != nil {
		return false, err
	}
	if err := t.detector.Save(); err != nil {
		// Log error but don't fail the drift check
		slog.Warn("failed to save drift state", "error", err)
	}
	return drifted, nil
}

// FindBestMatch tries to find the best matching pattern for a given HTML snippet.
// Delegates to FindBestMatchV2 using the tracker's configured scoring weights.
func (t *PatternTracker) FindBestMatch(ctx context.Context, targetID string, currentHTML string) (UIPattern, float64, bool) {
	return t.FindBestMatchV2(ctx, targetID, currentHTML)
}

// FindBestMatchV2 uses configurable weights, adaptive thresholds, and optional
// time-based confidence decay to score pattern matches.
func (t *PatternTracker) FindBestMatchV2(ctx context.Context, targetID string, currentHTML string) (UIPattern, float64, bool) {
	targetPattern, ok := t.store.Get(ctx, targetID)
	if !ok {
		return UIPattern{}, 0, false
	}

	currentFingerprint := domheal.ParseDOMFingerprint(currentHTML)
	rawSimilarity := domheal.DOMFingerprintSimilarity(targetPattern.Fingerprint, currentFingerprint)

	confidence := targetPattern.Confidence
	if confidence <= 0 {
		confidence = 0.5
	}

	// Time-based decay: halve the effective confidence for each half-life
	// elapsed since the pattern was last seen.
	if t.scoring.TimeDecayHalfLife > 0 && !targetPattern.LastSeen.IsZero() {
		elapsed := time.Since(targetPattern.LastSeen)
		if elapsed > 0 {
			halfLives := float64(elapsed) / float64(t.scoring.TimeDecayHalfLife)
			confidence *= math.Pow(0.5, halfLives)
			if confidence < 0.05 {
				confidence = 0.05
			}
		}
	}

	simW := t.scoring.SimilarityWeight
	confW := t.scoring.ConfidenceWeight
	effectiveScore := simW*rawSimilarity + confW*confidence

	threshold := t.scoring.BaseThreshold - 0.1*(confidence-0.5)
	if threshold < t.scoring.MinThreshold {
		threshold = t.scoring.MinThreshold
	}

	if effectiveScore >= threshold {
		return targetPattern, effectiveScore, true
	}

	return UIPattern{}, effectiveScore, false
}

// PenalizeConfidence reduces confidence of a pattern after a failed use.
// The penalty is subtracted from the current confidence, floored at 0.0.
func (t *PatternTracker) PenalizeConfidence(ctx context.Context, id string, penalty float64) error {
	p, ok := t.store.Get(ctx, id)
	if !ok {
		return fmt.Errorf("pattern not found: %s", id)
	}
	p.Confidence -= penalty
	if p.Confidence < 0 {
		p.Confidence = 0
	}
	return t.store.Set(ctx, p)
}

// RegisterPattern learns a new pattern or updates an existing one.
func (t *PatternTracker) RegisterPattern(ctx context.Context, id, selector, description, html string) error {
	fingerprint := domheal.ParseDOMFingerprint(html)

	pattern := UIPattern{
		ID:          id,
		Selector:    selector,
		Description: description,
		Fingerprint: fingerprint,
		LastSeen:    time.Now(),
		Confidence:  1.0, // Newly registered patterns have high confidence
		Metadata:    make(map[string]string),
	}

	// Log the repair/discovery
	suggestion := domheal.RepairSuggestion{
		ElementType: id,
		OldSelector: "", // Or previous selector if updating
		NewSelector: selector,
		Confidence:  1.0,
		Method:      "smart_discovery",
	}
	_ = t.repairLog.Append(suggestion)

	return t.store.Set(ctx, pattern)
}

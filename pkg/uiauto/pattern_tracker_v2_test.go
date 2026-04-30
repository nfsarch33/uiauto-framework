package uiauto

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPatternTracker_PenalizeConfidence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()

	if err := tracker.RegisterPattern(ctx, "btn-login", "button.login", "Login button", "<button class='login'>Sign In</button>"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	p, ok := tracker.store.Get(ctx, "btn-login")
	if !ok {
		t.Fatal("pattern not found after registration")
	}
	if p.Confidence != 1.0 {
		t.Errorf("initial confidence = %f, want 1.0", p.Confidence)
	}

	if err := tracker.PenalizeConfidence(ctx, "btn-login", 0.2); err != nil {
		t.Fatalf("PenalizeConfidence: %v", err)
	}

	p, _ = tracker.store.Get(ctx, "btn-login")
	if p.Confidence != 0.8 {
		t.Errorf("confidence after penalty = %f, want 0.8", p.Confidence)
	}

	// Multiple penalties
	for i := 0; i < 5; i++ {
		_ = tracker.PenalizeConfidence(ctx, "btn-login", 0.2)
	}

	p, _ = tracker.store.Get(ctx, "btn-login")
	if p.Confidence < 0 {
		t.Errorf("confidence should not go below 0, got %f", p.Confidence)
	}
}

func TestPatternTracker_PenalizeConfidence_NotFound(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.PenalizeConfidence(ctx, "nonexistent", 0.1)
	if err == nil {
		t.Error("expected error for nonexistent pattern")
	}
}

func TestPatternTracker_BoostAndPenalizeCycle(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()

	if err := tracker.RegisterPattern(ctx, "nav-link", "a.nav", "Navigation link", "<a class='nav'>Home</a>"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Penalize to 0.7
	_ = tracker.PenalizeConfidence(ctx, "nav-link", 0.3)
	p, _ := tracker.store.Get(ctx, "nav-link")
	if p.Confidence < 0.69 || p.Confidence > 0.71 {
		t.Errorf("confidence = %f, want ~0.7", p.Confidence)
	}

	// Boost back
	_ = tracker.store.BoostConfidence(ctx, "nav-link", 0.2)
	p, _ = tracker.store.Get(ctx, "nav-link")
	if p.Confidence < 0.89 || p.Confidence > 0.91 {
		t.Errorf("confidence after boost = %f, want ~0.9", p.Confidence)
	}

	// Boost should cap at 1.0
	_ = tracker.store.BoostConfidence(ctx, "nav-link", 0.5)
	p, _ = tracker.store.Get(ctx, "nav-link")
	if p.Confidence != 1.0 {
		t.Errorf("confidence after over-boost = %f, want 1.0", p.Confidence)
	}
}

func TestDefaultScoringConfig(t *testing.T) {
	cfg := DefaultScoringConfig()
	if cfg.SimilarityWeight != 0.7 {
		t.Errorf("SimilarityWeight = %f, want 0.7", cfg.SimilarityWeight)
	}
	if cfg.ConfidenceWeight != 0.3 {
		t.Errorf("ConfidenceWeight = %f, want 0.3", cfg.ConfidenceWeight)
	}
	if cfg.BaseThreshold != 0.6 {
		t.Errorf("BaseThreshold = %f, want 0.6", cfg.BaseThreshold)
	}
	if cfg.MinThreshold != 0.45 {
		t.Errorf("MinThreshold = %f, want 0.45", cfg.MinThreshold)
	}
	if cfg.TimeDecayHalfLife != 0 {
		t.Errorf("TimeDecayHalfLife = %v, want 0", cfg.TimeDecayHalfLife)
	}
}

func TestPatternTracker_WithScoringConfig(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	custom := ScoringConfig{
		SimilarityWeight:  0.5,
		ConfidenceWeight:  0.5,
		BaseThreshold:     0.5,
		MinThreshold:      0.3,
		TimeDecayHalfLife: 24 * time.Hour,
	}
	tracker.WithScoringConfig(custom)

	if tracker.scoring.SimilarityWeight != 0.5 {
		t.Errorf("SimilarityWeight = %f, want 0.5", tracker.scoring.SimilarityWeight)
	}
	if tracker.scoring.TimeDecayHalfLife != 24*time.Hour {
		t.Errorf("TimeDecayHalfLife = %v, want 24h", tracker.scoring.TimeDecayHalfLife)
	}
}

func TestFindBestMatchV2_WithTimeDecay(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()
	html := "<div class='main'><h1>Content</h1></div>"
	if err := tracker.RegisterPattern(ctx, "main-div", "div.main", "Main content", html); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Fresh pattern with no decay should match
	_, score1, ok1 := tracker.FindBestMatchV2(ctx, "main-div", html)
	if !ok1 {
		t.Fatal("expected match for fresh pattern")
	}

	// Enable time decay with 1-hour half-life and backdate LastSeen
	tracker.WithScoringConfig(ScoringConfig{
		SimilarityWeight:  0.7,
		ConfidenceWeight:  0.3,
		BaseThreshold:     0.6,
		MinThreshold:      0.45,
		TimeDecayHalfLife: 1 * time.Hour,
	})

	// Manually backdate the pattern
	p, _ := tracker.store.Get(ctx, "main-div")
	p.LastSeen = time.Now().Add(-48 * time.Hour)
	_ = tracker.store.Set(ctx, p)

	_, score2, _ := tracker.FindBestMatchV2(ctx, "main-div", html)

	// Score with 48h elapsed (48 half-lives of 1h) should be much lower
	// because confidence is decayed heavily
	if score2 >= score1 {
		t.Errorf("decayed score %f should be less than fresh score %f", score2, score1)
	}
}

func TestFindBestMatchV2_CustomWeights(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()
	html := "<form id='login'><input name='user'/></form>"
	if err := tracker.RegisterPattern(ctx, "login-form", "form#login", "Login form", html); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Default weights
	_, scoreDefault, _ := tracker.FindBestMatchV2(ctx, "login-form", html)

	// Confidence-heavy weights: 0.3 sim + 0.7 conf
	tracker.WithScoringConfig(ScoringConfig{
		SimilarityWeight:  0.3,
		ConfidenceWeight:  0.7,
		BaseThreshold:     0.6,
		MinThreshold:      0.45,
		TimeDecayHalfLife: 0,
	})
	_, scoreConfHeavy, _ := tracker.FindBestMatchV2(ctx, "login-form", html)

	// With confidence=1.0 and similarity=1.0 (same HTML), both should produce
	// the same raw total but different compositions; here we verify the config
	// is actually applied by checking the tracker holds the new weights.
	if tracker.scoring.SimilarityWeight != 0.3 {
		t.Errorf("expected SimilarityWeight 0.3, got %f", tracker.scoring.SimilarityWeight)
	}
	// Both scores should be high since HTML matches exactly
	if scoreDefault < 0.5 || scoreConfHeavy < 0.5 {
		t.Errorf("expected high scores for exact match: default=%f, confHeavy=%f", scoreDefault, scoreConfHeavy)
	}
}

func TestFindBestMatchV2_NotFound(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	if err := os.MkdirAll(driftDir, 0755); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()
	_, _, ok := tracker.FindBestMatchV2(ctx, "nonexistent", "<div>test</div>")
	if ok {
		t.Error("expected no match for nonexistent pattern")
	}
}

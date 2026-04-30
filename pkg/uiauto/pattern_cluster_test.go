package uiauto

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
)

func TestClusterEngine_BasicClustering(t *testing.T) {
	ce := NewClusterEngine(0.5)

	patterns := map[string]UIPattern{
		"login_button": {
			ID: "login_button", Selector: "button#login",
			Fingerprint: domheal.ParseDOMFingerprint(`<div class="auth-form"><button id="login">Sign In</button></div>`),
			Confidence:  0.9, LastSeen: time.Now(),
		},
		"signup_button": {
			ID: "signup_button", Selector: "button#signup",
			Fingerprint: domheal.ParseDOMFingerprint(`<div class="auth-form"><button id="signup">Sign Up</button></div>`),
			Confidence:  0.85, LastSeen: time.Now(),
		},
		"grade_table": {
			ID: "grade_table", Selector: "table.grades",
			Fingerprint: domheal.ParseDOMFingerprint(`<table class="grades"><tr><td>Score</td></tr></table>`),
			Confidence:  0.7, LastSeen: time.Now(),
		},
	}

	clusters := ce.Cluster(patterns)
	if len(clusters) == 0 {
		t.Fatal("expected at least one cluster")
	}

	totalMembers := 0
	for _, c := range clusters {
		totalMembers += len(c.Members)
		if c.AvgConfidence <= 0 {
			t.Errorf("cluster %s has zero avg confidence", c.ID)
		}
	}
	if totalMembers != 3 {
		t.Errorf("expected 3 total members across clusters, got %d", totalMembers)
	}
}

func TestClusterEngine_AllSimilar(t *testing.T) {
	ce := NewClusterEngine(0.3)

	html := `<div class="card"><h3>Title</h3><p>Content</p></div>`
	fp := domheal.ParseDOMFingerprint(html)

	patterns := map[string]UIPattern{
		"a": {ID: "a", Fingerprint: fp, Confidence: 0.8, LastSeen: time.Now()},
		"b": {ID: "b", Fingerprint: fp, Confidence: 0.9, LastSeen: time.Now()},
		"c": {ID: "c", Fingerprint: fp, Confidence: 0.7, LastSeen: time.Now()},
	}

	clusters := ce.Cluster(patterns)
	if len(clusters) != 1 {
		t.Errorf("identical fingerprints should form 1 cluster, got %d", len(clusters))
	}
	if len(clusters) > 0 && len(clusters[0].Members) != 3 {
		t.Errorf("expected 3 members in single cluster, got %d", len(clusters[0].Members))
	}
}

func TestClusterEngine_EmptyPatterns(t *testing.T) {
	ce := NewClusterEngine(0.6)
	clusters := ce.Cluster(map[string]UIPattern{})
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters for empty input, got %d", len(clusters))
	}
}

func TestClusterEngine_DefaultThreshold(t *testing.T) {
	ce := NewClusterEngine(0)
	if ce.threshold != 0.6 {
		t.Errorf("expected default threshold 0.6, got %f", ce.threshold)
	}
	ce2 := NewClusterEngine(1.5)
	if ce2.threshold != 0.6 {
		t.Errorf("expected clamped threshold 0.6, got %f", ce2.threshold)
	}
}

func TestDriftScorer_RecentEventsWeighMore(t *testing.T) {
	ds := NewDriftScorer(24 * time.Hour)

	now := time.Now()
	events := []DriftEvent{
		{PageID: "p1", Timestamp: now.Add(-48 * time.Hour), Magnitude: 0.2},
		{PageID: "p1", Timestamp: now.Add(-1 * time.Hour), Magnitude: 0.8},
	}

	score := ds.Score(events)
	if score < 0.5 {
		t.Errorf("expected score > 0.5 due to recent high-magnitude event, got %.2f", score)
	}
}

func TestDriftScorer_NoEvents(t *testing.T) {
	ds := NewDriftScorer(24 * time.Hour)
	score := ds.Score(nil)
	if score != 0 {
		t.Errorf("expected 0 for empty events, got %.2f", score)
	}
}

func TestDriftScorer_SingleRecentEvent(t *testing.T) {
	ds := NewDriftScorer(24 * time.Hour)
	events := []DriftEvent{
		{PageID: "p1", Timestamp: time.Now(), Magnitude: 1.0},
	}
	score := ds.Score(events)
	if math.Abs(score-1.0) > 0.01 {
		t.Errorf("expected score ~1.0 for immediate high-magnitude event, got %.2f", score)
	}
}

func TestDriftScorer_OldEventsDecay(t *testing.T) {
	ds := NewDriftScorer(1 * time.Hour)
	events := []DriftEvent{
		{PageID: "p1", Timestamp: time.Now().Add(-720 * time.Hour), Magnitude: 1.0},
	}
	score := ds.Score(events)
	if score > 0.01 {
		t.Errorf("expected near-zero score for very old event, got %.4f", score)
	}
}

func TestConfidenceScorer_BoostAndDecay(t *testing.T) {
	cs := NewConfidenceScorer(0.1, 0.2)

	conf := 0.5
	conf = cs.Update(conf, true)
	if conf != 0.6 {
		t.Errorf("expected 0.6 after boost, got %.2f", conf)
	}

	conf = cs.Update(conf, false)
	if math.Abs(conf-0.4) > 0.001 {
		t.Errorf("expected ~0.4 after decay, got %.4f", conf)
	}
}

func TestConfidenceScorer_Clamping(t *testing.T) {
	cs := NewConfidenceScorer(0.5, 0.5)

	high := cs.Update(0.9, true)
	if high > 1.0 {
		t.Errorf("confidence should clamp at 1.0, got %.2f", high)
	}

	low := cs.Update(0.1, false)
	if low < 0.05 {
		t.Errorf("confidence should not go below min, got %.4f", low)
	}
}

func TestConfidenceScorer_AdaptiveThreshold(t *testing.T) {
	cs := NewConfidenceScorer(0.05, 0.15)

	highConfThreshold := cs.AdaptiveThreshold(0.95)
	lowConfThreshold := cs.AdaptiveThreshold(0.1)

	if highConfThreshold >= lowConfThreshold {
		t.Errorf("high-confidence threshold (%.2f) should be lower than low-confidence (%.2f)",
			highConfThreshold, lowConfThreshold)
	}

	if highConfThreshold < 0.40 || highConfThreshold > 0.80 {
		t.Errorf("high-confidence threshold %.2f out of range [0.40, 0.80]", highConfThreshold)
	}
	if lowConfThreshold < 0.40 || lowConfThreshold > 0.80 {
		t.Errorf("low-confidence threshold %.2f out of range [0.40, 0.80]", lowConfThreshold)
	}
}

func TestPatternMLPipeline_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")

	store, err := NewPatternStore(storePath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	ctx := context.Background()

	_ = store.Set(ctx, UIPattern{
		ID: "nav_menu", Selector: "nav.d2l-nav",
		Description: "D2L navigation menu",
		Fingerprint: domheal.ParseDOMFingerprint(`<nav class="d2l-nav" role="navigation"><a href="/home">Home</a><a href="/courses">Courses</a></nav>`),
		Confidence:  0.9, LastSeen: time.Now(),
	})
	_ = store.Set(ctx, UIPattern{
		ID: "nav_sidebar", Selector: "nav.d2l-sidebar",
		Description: "D2L sidebar navigation",
		Fingerprint: domheal.ParseDOMFingerprint(`<nav class="d2l-sidebar" role="navigation"><a href="/home">Home</a><a href="/grades">Grades</a></nav>`),
		Confidence:  0.85, LastSeen: time.Now(),
	})
	_ = store.Set(ctx, UIPattern{
		ID: "grade_grid", Selector: "div.grade-grid",
		Description: "D2L grade grid",
		Fingerprint: domheal.ParseDOMFingerprint(`<div class="grade-grid" role="grid"><div class="row">A1: 85</div></div>`),
		Confidence:  0.7, LastSeen: time.Now(),
	})

	pipeline := NewPatternMLPipeline(store, 0.4, 7*24*time.Hour)

	// 1. Run clustering
	result, err := pipeline.RunClustering(ctx)
	if err != nil {
		t.Fatalf("clustering: %v", err)
	}
	if result.TotalPatterns != 3 {
		t.Errorf("expected 3 patterns, got %d", result.TotalPatterns)
	}
	if result.NumClusters == 0 {
		t.Error("expected at least 1 cluster")
	}

	// 2. Score drift
	now := time.Now()
	events := []DriftEvent{
		{PageID: "nav_menu", Timestamp: now.Add(-2 * time.Hour), Magnitude: 0.3},
		{PageID: "nav_menu", Timestamp: now.Add(-10 * time.Minute), Magnitude: 0.7},
	}
	driftScore := pipeline.ScoreDrift(events)
	if driftScore < 0.4 {
		t.Errorf("expected drift score > 0.4 with recent high event, got %.2f", driftScore)
	}

	// 3. Update confidence after success/failure
	err = pipeline.UpdateConfidence(ctx, "nav_menu", true)
	if err != nil {
		t.Fatalf("update confidence: %v", err)
	}
	p, _ := store.Get(ctx, "nav_menu")
	if p.Confidence <= 0.9 {
		t.Errorf("expected confidence > 0.9 after success, got %.2f", p.Confidence)
	}

	err = pipeline.UpdateConfidence(ctx, "grade_grid", false)
	if err != nil {
		t.Fatalf("update confidence: %v", err)
	}
	p2, _ := store.Get(ctx, "grade_grid")
	if p2.Confidence >= 0.7 {
		t.Errorf("expected confidence < 0.7 after failure, got %.2f", p2.Confidence)
	}

	// 4. Adaptive threshold
	if !pipeline.AcceptableMatch(0.60, 0.95) {
		t.Error("high-confidence pattern with 0.60 similarity should be acceptable")
	}
	if pipeline.AcceptableMatch(0.45, 0.1) {
		t.Error("low-confidence pattern with 0.45 similarity should be rejected")
	}
}

func TestPatternMLPipeline_DriftTriggersConfidenceDecay(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")

	store, err := NewPatternStore(storePath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	ctx := context.Background()
	_ = store.Set(ctx, UIPattern{
		ID: "submit_btn", Selector: "button.submit",
		Fingerprint: domheal.ParseDOMFingerprint(`<button class="submit">Submit</button>`),
		Confidence:  0.8, LastSeen: time.Now(),
	})

	pipeline := NewPatternMLPipeline(store, 0.5, 7*24*time.Hour)

	for i := 0; i < 5; i++ {
		_ = pipeline.UpdateConfidence(ctx, "submit_btn", false)
	}

	p, _ := store.Get(ctx, "submit_btn")
	if p.Confidence >= 0.3 {
		t.Errorf("expected confidence < 0.3 after 5 failures, got %.2f", p.Confidence)
	}
	if p.Confidence < 0.05 {
		t.Errorf("confidence should not drop below min threshold, got %.4f", p.Confidence)
	}
}

func TestPatternMLPipeline_ClusterStability(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "patterns.json")

	store, err := NewPatternStore(storePath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	ctx := context.Background()
	baseHTML := `<div class="d2l-content"><a href="/mod1">Module 1</a></div>`
	fp := domheal.ParseDOMFingerprint(baseHTML)

	for i := 0; i < 10; i++ {
		_ = store.Set(ctx, UIPattern{
			ID:          "module_" + string(rune('a'+i)),
			Fingerprint: fp,
			Confidence:  0.8,
			LastSeen:    time.Now(),
		})
	}

	pipeline := NewPatternMLPipeline(store, 0.5, 7*24*time.Hour)
	result, err := pipeline.RunClustering(ctx)
	if err != nil {
		t.Fatalf("clustering: %v", err)
	}

	if result.NumClusters != 1 {
		t.Errorf("10 identical fingerprints should form 1 cluster, got %d", result.NumClusters)
	}
	if len(result.Clusters[0].Members) != 10 {
		t.Errorf("expected 10 members in cluster, got %d", len(result.Clusters[0].Members))
	}
}

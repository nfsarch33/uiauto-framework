package uiauto

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPatternTracker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "uitest-pattern-tracker-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storePath := filepath.Join(tmpDir, "patterns.json")
	driftDir := filepath.Join(tmpDir, "drift")

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	html1 := `<div class="login-container"><input type="text" id="username" data-testid="user-input"><button class="btn primary" role="button">Login</button></div>`

	err = tracker.RegisterPattern(context.Background(), "login_form", ".login-container", "Main login form", html1)
	if err != nil {
		t.Fatalf("Failed to register pattern: %v", err)
	}

	// Verify it was saved
	pattern, ok := tracker.store.Get(context.Background(), "login_form")
	if !ok {
		t.Fatalf("Pattern not found in store")
	}
	if pattern.Selector != ".login-container" {
		t.Errorf("Expected selector '.login-container', got '%s'", pattern.Selector)
	}

	// Check drift
	drifted, err := tracker.CheckDrift("login_page", html1)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if drifted {
		t.Errorf("Expected no drift on first check")
	}

	// Change HTML slightly
	html2 := `<div class="login-wrapper"><input type="text" id="username" data-testid="user-input"><button class="btn primary" role="button">Sign In</button></div>`

	drifted, err = tracker.CheckDrift("login_page", html2)
	if err != nil {
		t.Fatalf("CheckDrift failed: %v", err)
	}
	if !drifted {
		t.Errorf("Expected drift to be detected")
	}

	// Find best match in new HTML
	match, similarity, found := tracker.FindBestMatch(context.Background(), "login_form", html2)
	if !found {
		t.Fatalf("Expected to find a match despite drift")
	}
	if similarity < 0.6 {
		t.Errorf("Expected similarity >= 0.6, got %f", similarity)
	}
	if match.ID != "login_form" {
		t.Errorf("Expected match ID 'login_form', got '%s'", match.ID)
	}
}

// --- Sprint 4: Pattern pipeline with drift alerts and multi-model handoff ---

func TestPatternPipeline_DriftAlertCreation(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "patterns.json")
	driftDir := filepath.Join(tmpDir, "drift")

	tracker, err := NewPatternTracker(storePath, driftDir)
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}

	pipeline := NewPatternPipeline(tracker, nil)

	html1 := `<div class="d2l-content"><nav class="d2l-nav">Modules</nav></div>`
	_ = tracker.RegisterPattern(context.Background(), "nav_menu", "nav.d2l-nav", "D2L navigation", html1)

	// First check: no drift (same page)
	drifted, err := pipeline.CheckAndAlert(context.Background(), "d2l-home", "nav_menu", html1)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if drifted {
		t.Error("should not drift on identical page")
	}
	if len(pipeline.UnresolvedAlerts()) != 0 {
		t.Error("expected 0 unresolved alerts")
	}

	// Second check: drift detected (different page)
	html2 := `<div class="d2l-new-layout"><aside class="d2l-sidebar">Modules</aside></div>`
	drifted, err = pipeline.CheckAndAlert(context.Background(), "d2l-home", "nav_menu", html2)
	if err != nil {
		t.Fatalf("check drift: %v", err)
	}
	if !drifted {
		t.Error("should detect drift on changed page")
	}

	alerts := pipeline.UnresolvedAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].PageID != "d2l-home" {
		t.Errorf("expected page_id d2l-home, got %s", alerts[0].PageID)
	}
	if alerts[0].PatternID != "nav_menu" {
		t.Errorf("expected pattern_id nav_menu, got %s", alerts[0].PatternID)
	}
}

func TestPatternPipeline_AlertResolution(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}

	pipeline := NewPatternPipeline(tracker, nil)

	html1 := `<div>original</div>`
	html2 := `<div>changed completely</div>`
	_ = tracker.RegisterPattern(context.Background(), "test", "div", "test elem", html1)

	// Baseline call so drift detector records the page
	_, _ = pipeline.CheckAndAlert(context.Background(), "page1", "test", html1)
	// Now change triggers drift
	_, _ = pipeline.CheckAndAlert(context.Background(), "page1", "test", html2)

	alerts := pipeline.UnresolvedAlerts()
	if len(alerts) == 0 {
		t.Fatal("expected at least 1 alert")
	}

	pipeline.ResolveAlert(alerts[0].ID)

	if len(pipeline.UnresolvedAlerts()) != 0 {
		t.Error("expected 0 unresolved after resolve")
	}
}

func TestPatternPipeline_ModelHandoffTracking(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}

	pipeline := NewPatternPipeline(tracker, nil)

	pipeline.RecordHandoff("login_btn", "light", "smart", "light_failure", true)
	pipeline.RecordHandoff("grade_table", "smart", "vlm", "smart_failure", true)
	pipeline.RecordHandoff("nav_menu", "vlm", "light", "pattern_converged", true)

	handoffs := pipeline.RecentHandoffs(10)
	if len(handoffs) != 3 {
		t.Fatalf("expected 3 handoffs, got %d", len(handoffs))
	}

	// Most recent first
	if handoffs[0].PatternID != "nav_menu" {
		t.Errorf("expected most recent handoff pattern_id=nav_menu, got %s", handoffs[0].PatternID)
	}
	if handoffs[0].FromTier != "vlm" || handoffs[0].ToTier != "light" {
		t.Errorf("unexpected tier transition: %s -> %s", handoffs[0].FromTier, handoffs[0].ToTier)
	}
}

func TestPatternPipeline_AlertHandler(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}

	pipeline := NewPatternPipeline(tracker, nil)

	var received []DriftAlert
	pipeline.WithAlertHandler(func(alert DriftAlert) {
		received = append(received, alert)
	})

	html1 := `<div>v1</div>`
	html2 := `<span>completely different</span>`
	_ = tracker.RegisterPattern(context.Background(), "elem", "div", "test", html1)

	// Baseline call to establish page fingerprint
	_, _ = pipeline.CheckAndAlert(context.Background(), "page", "elem", html1)
	// Drift call triggers alert
	_, _ = pipeline.CheckAndAlert(context.Background(), "page", "elem", html2)

	if len(received) != 1 {
		t.Fatalf("expected 1 alert callback, got %d", len(received))
	}
	if received[0].PageID != "page" {
		t.Errorf("expected page_id=page, got %s", received[0].PageID)
	}
}

func TestClassifyDriftSeverity(t *testing.T) {
	tests := []struct {
		similarity float64
		expected   DriftSeverity
	}{
		{0.9, DriftSeverityLow},
		{0.8, DriftSeverityLow},
		{0.6, DriftSeverityMedium},
		{0.5, DriftSeverityMedium},
		{0.3, DriftSeverityHigh},
		{0.2, DriftSeverityHigh},
		{0.1, DriftSeverityCritical},
		{0.0, DriftSeverityCritical},
	}

	for _, tc := range tests {
		got := ClassifyDriftSeverity(tc.similarity)
		if got != tc.expected {
			t.Errorf("ClassifyDriftSeverity(%.1f) = %s, want %s", tc.similarity, got, tc.expected)
		}
	}
}

func TestInMemoryStores(t *testing.T) {
	// Drift alerts
	alertStore := NewInMemoryDriftAlertStore(nil)
	alertStore.Insert(DriftAlert{PageID: "p1", PatternID: "x", Severity: DriftSeverityHigh})
	alertStore.Insert(DriftAlert{PageID: "p2", PatternID: "y", Severity: DriftSeverityLow})

	unresolved := alertStore.Unresolved()
	if len(unresolved) != 2 {
		t.Fatalf("expected 2, got %d", len(unresolved))
	}

	alertStore.Resolve(1)
	unresolved = alertStore.Unresolved()
	if len(unresolved) != 1 {
		t.Fatalf("expected 1 after resolve, got %d", len(unresolved))
	}

	// Handoff store
	handoffStore := NewInMemoryHandoffStore()
	handoffStore.Insert(ModelHandoff{PatternID: "a", FromTier: "light", ToTier: "smart", Success: true})
	handoffStore.Insert(ModelHandoff{PatternID: "b", FromTier: "smart", ToTier: "vlm", Success: false})

	recent := handoffStore.Recent(5)
	if len(recent) != 2 {
		t.Fatalf("expected 2, got %d", len(recent))
	}
	if recent[0].PatternID != "b" {
		t.Errorf("expected most recent=b, got %s", recent[0].PatternID)
	}
}

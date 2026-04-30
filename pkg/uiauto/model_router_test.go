package uiauto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRouter_SmartDiscovery(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="new-btn">Click Me</button></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping test, could not start browser: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "patterns.json"), filepath.Join(tmpDir, "drift"))
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	light := NewLightExecutor(tracker, agent)
	mockProvider := &MockProvider{Response: "```css\n#new-btn\n```"}
	smart := NewSmartDiscoverer(mockProvider, "test-model")

	router := NewModelRouter(light, smart, tracker, agent)

	err = agent.Navigate(ts.URL)
	if err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	action := Action{
		Type:        "read",
		TargetID:    "submit_btn",
		Description: "The submit button",
	}

	err = router.ExecuteAction(context.Background(), action)
	if err != nil {
		t.Errorf("Expected execution to succeed via smart discovery, got: %v", err)
	}

	pattern, ok := tracker.store.Get(context.Background(), "submit_btn")
	if !ok {
		t.Fatalf("Pattern not found in store after smart discovery")
	}
	if pattern.Selector != "#new-btn" {
		t.Errorf("Expected selector '#new-btn', got '%s'", pattern.Selector)
	}
}

func TestModelRouter_Convergence(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="d">text</div></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping test, could not start browser: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	light := NewLightExecutor(tracker, agent)
	smart := NewSmartDiscoverer(&MockProvider{Response: "#d"}, "m")
	router := NewModelRouter(light, smart, tracker, agent, WithConvergenceThreshold(3))

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "d", "#d", "div", html)

	if router.IsConverged() {
		t.Error("Should not be converged initially")
	}

	for i := 0; i < 3; i++ {
		_ = router.ExecuteAction(ctx, Action{Type: "read", TargetID: "d"})
	}

	if !router.IsConverged() {
		t.Error("Expected convergence after 3 hits")
	}
}

func TestModelRouter_Metrics(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="m">data</div></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping test, could not start browser: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	light := NewLightExecutor(tracker, agent)
	smart := NewSmartDiscoverer(&MockProvider{Response: "#m"}, "m")
	router := NewModelRouter(light, smart, tracker, agent)

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "m", "#m", "m", html)

	_ = router.ExecuteAction(ctx, Action{Type: "read", TargetID: "m"})
	_ = router.ExecuteAction(ctx, Action{Type: "read", TargetID: "missing", Description: "missing element"})

	snap := router.Metrics.Snapshot()
	if snap.ActionCount != 2 {
		t.Errorf("Expected 2 actions, got %d", snap.ActionCount)
	}
	if snap.LightAttempts < 1 {
		t.Errorf("Expected at least 1 light attempt, got %d", snap.LightAttempts)
	}
}

func TestPatternPhase_String(t *testing.T) {
	tests := []struct {
		phase PatternPhase
		want  string
	}{
		{PhaseDiscovery, "discovery"},
		{PhaseCruise, "cruise"},
		{PhaseEscalation, "escalation"},
		{PatternPhase(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("PatternPhase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestModelRouter_TierString(t *testing.T) {
	tests := []struct {
		tier ModelTier
		want string
	}{
		{TierLight, "light"},
		{TierSmart, "smart"},
		{TierVLM, "vlm"},
		{ModelTier(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("ModelTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestModelRouter_BatchExecution(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="x">x</div><div id="y">y</div></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping test, could not start browser: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	light := NewLightExecutor(tracker, agent)
	smart := NewSmartDiscoverer(&MockProvider{Response: "#x"}, "m")
	router := NewModelRouter(light, smart, tracker, agent)

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "x", "#x", "x", html)
	_ = tracker.RegisterPattern(ctx, "y", "#y", "y", html)

	errs := router.ExecuteBatch(ctx, []Action{
		{Type: "read", TargetID: "x"},
		{Type: "read", TargetID: "y"},
	})

	for i, err := range errs {
		if err != nil {
			t.Errorf("Batch action %d failed: %v", i, err)
		}
	}
}

func TestConvergenceState(t *testing.T) {
	c := newConvergenceState(3)
	if c.IsConverged() {
		t.Error("Should not be converged initially")
	}

	c.RecordHit()
	c.RecordHit()
	if c.IsConverged() {
		t.Error("Should not be converged with 2 hits")
	}
	if c.ConsecutiveHits() != 2 {
		t.Errorf("Expected 2 consecutive hits, got %d", c.ConsecutiveHits())
	}

	c.RecordHit()
	if !c.IsConverged() {
		t.Error("Should be converged with 3 hits")
	}

	c.RecordMiss()
	if c.IsConverged() {
		t.Error("Should not be converged after miss")
	}
	if c.ConsecutiveHits() != 0 {
		t.Errorf("Expected 0 consecutive hits after miss, got %d", c.ConsecutiveHits())
	}

	// Edge: threshold of 0 defaults to 5
	c2 := newConvergenceState(0)
	if c2.threshold != 5 {
		t.Errorf("Expected default threshold 5, got %d", c2.threshold)
	}
}

func TestPhaseTracker_InitialState(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 2)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())
}

func TestPhaseTracker_DiscoveryToCruise(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 2)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())

	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())
	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())
	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())
}

func TestPhaseTracker_CruiseToEscalation(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 2)
	for i := 0; i < 3; i++ {
		pt.RecordSuccess(TierSmart)
	}
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())

	pt.RecordFailure(TierLight)
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())
	pt.RecordFailure(TierLight)
	assert.Equal(t, PhaseEscalation, pt.CurrentPhase())
}

func TestPhaseTracker_EscalationToDiscovery(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 2)
	for i := 0; i < 3; i++ {
		pt.RecordSuccess(TierSmart)
	}
	for i := 0; i < 2; i++ {
		pt.RecordFailure(TierLight)
	}
	assert.Equal(t, PhaseEscalation, pt.CurrentPhase())

	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())

	// Also via VLM
	for i := 0; i < 3; i++ {
		pt.RecordSuccess(TierSmart)
	}
	for i := 0; i < 2; i++ {
		pt.RecordFailure(TierLight)
	}
	assert.Equal(t, PhaseEscalation, pt.CurrentPhase())
	pt.RecordSuccess(TierVLM)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())
}

func TestPhaseTracker_CruiseToDiscovery(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 2)
	for i := 0; i < 3; i++ {
		pt.RecordSuccess(TierSmart)
	}
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())

	pt.RecordFailure(TierLight)
	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())
}

func TestPhaseTracker_TransitionHistory(t *testing.T) {
	pt := NewPhaseTracker(2, 5, 2)

	pt.RecordSuccess(TierSmart)
	pt.RecordSuccess(TierSmart)
	hist := pt.History()
	require.Len(t, hist, 1)
	assert.Equal(t, PhaseDiscovery, hist[0].From)
	assert.Equal(t, PhaseCruise, hist[0].To)

	pt.RecordFailure(TierLight)
	pt.RecordFailure(TierLight)
	hist = pt.History()
	require.Len(t, hist, 2)
	assert.Equal(t, PhaseCruise, hist[1].From)
	assert.Equal(t, PhaseEscalation, hist[1].To)

	pt.RecordSuccess(TierSmart)
	hist = pt.History()
	require.Len(t, hist, 3)
	assert.Equal(t, PhaseEscalation, hist[2].From)
	assert.Equal(t, PhaseDiscovery, hist[2].To)
}

func TestPhaseTracker_Stats(t *testing.T) {
	pt := NewPhaseTracker(2, 5, 2)

	pt.RecordSuccess(TierSmart)
	time.Sleep(2 * time.Millisecond)
	pt.RecordSuccess(TierSmart)
	stats := pt.Stats()
	assert.GreaterOrEqual(t, stats.TransitionCount, 1)
	assert.NotNil(t, stats.PhaseDurations)
	assert.GreaterOrEqual(t, stats.StableEntries, int64(1))

	time.Sleep(2 * time.Millisecond)
	pt.RecordFailure(TierLight)
	pt.RecordFailure(TierLight)
	stats = pt.Stats()
	assert.GreaterOrEqual(t, stats.EscalationCount, int64(1))
}

func TestPhaseTracker_ConcurrentAccess(t *testing.T) {
	pt := NewPhaseTracker(100, 50, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pt.RecordSuccess(TierSmart)
			pt.RecordSuccess(TierLight)
			pt.RecordFailure(TierLight)
			_ = pt.CurrentPhase()
			_ = pt.History()
			_ = pt.Stats()
		}()
	}
	wg.Wait()
	// Should not panic; phase should be in a valid state
	phase := pt.CurrentPhase()
	assert.Contains(t, []PatternPhase{PhaseDiscovery, PhaseCruise, PhaseEscalation}, phase)
}

func TestModelRouter_WithPhaseThresholds(t *testing.T) {
	pt := NewPhaseTracker(2, 5, 3)
	assert.Equal(t, PhaseDiscovery, pt.CurrentPhase())
	// Verify transition works with custom thresholds
	pt.RecordSuccess(TierSmart)
	pt.RecordSuccess(TierSmart)
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())
}

func TestPhaseTracker_NoFalseTransitions(t *testing.T) {
	pt := NewPhaseTracker(3, 5, 2)
	for i := 0; i < 3; i++ {
		pt.RecordSuccess(TierSmart)
	}
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())
	histLen := len(pt.History())

	for i := 0; i < 10; i++ {
		pt.RecordSuccess(TierLight)
	}
	assert.Equal(t, PhaseCruise, pt.CurrentPhase())
	assert.Len(t, pt.History(), histLen, "light successes in cruise should not cause transitions")
}

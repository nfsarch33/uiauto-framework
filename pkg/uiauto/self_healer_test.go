package uiauto

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSelfHealer_Heal_SmartLLM(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="submit">Submit</button></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()

	// Register with a now-broken selector
	_ = tracker.RegisterPattern(ctx, "submit_btn", "#old-submit", "Submit button", html)

	mock := &MockProvider{Response: "#submit"}
	smart := NewSmartDiscoverer(mock, "test-model")

	healer := NewSelfHealer(tracker, smart, nil, agent, WithHealStrategy(HealSmartLLM))

	result := healer.Heal(ctx, "submit_btn")
	if !result.Success {
		t.Errorf("Expected healing to succeed, got error: %v", result.Error)
	}
	if result.Method != "smart_llm" {
		t.Errorf("Expected method smart_llm, got %s", result.Method)
	}
	if result.NewSelector != "#submit" {
		t.Errorf("Expected new selector #submit, got %s", result.NewSelector)
	}
}

func TestSelfHealer_Heal_PatternNotFound(t *testing.T) {
	skipWithoutBrowser(t)
	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	mock := &MockProvider{Response: "ok"}
	smart := NewSmartDiscoverer(mock, "m")
	healer := NewSelfHealer(tracker, smart, nil, agent)

	result := healer.Heal(context.Background(), "nonexistent")
	if result.Success {
		t.Error("Expected failure for nonexistent pattern")
	}
	if result.Error == nil {
		t.Error("Expected error for nonexistent pattern")
	}
}

func TestSelfHealer_Heal_AllFail(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div>completely changed</div></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	ctx := context.Background()
	originalHTML := `<html><body><form id="login"><button id="gone" class="primary">Submit</button></form></body></html>`
	_ = tracker.RegisterPattern(ctx, "btn", "#gone", "button", originalHTML)

	_ = agent.Navigate(ts.URL)

	mock := &MockProvider{Err: fmt.Errorf("LLM unavailable")}
	smart := NewSmartDiscoverer(mock, "m")
	healer := NewSelfHealer(tracker, smart, nil, agent)

	result := healer.Heal(ctx, "btn")
	if result.Success {
		t.Error("Expected failure when all strategies fail")
	}
}

func TestSelfHealer_HealBatch(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="a">A</button><button id="b">B</button></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "a", "#old-a", "Button A", html)
	_ = tracker.RegisterPattern(ctx, "b", "#old-b", "Button B", html)

	mock := &MockProvider{Response: "#a"}
	smart := NewSmartDiscoverer(mock, "m")
	healer := NewSelfHealer(tracker, smart, nil, agent, WithHealStrategy(HealSmartLLM))

	results := healer.HealBatch(ctx, []string{"a", "b"})
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
}

func TestSelfHealer_Metrics(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="m">test</div></body></html>`))
	}))
	defer ts.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}
	defer agent.Close()

	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("NewPatternTracker: %v", err)
	}

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "m", "#old-m", "div", html)

	mock := &MockProvider{Response: "#m"}
	smart := NewSmartDiscoverer(mock, "m")
	healer := NewSelfHealer(tracker, smart, nil, agent, WithHealStrategy(HealSmartLLM))

	_ = healer.Heal(ctx, "m")

	snap := healer.Metrics.Snapshot()
	if snap.TotalAttempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", snap.TotalAttempts)
	}
	if snap.SuccessRate() < 0 {
		t.Error("Success rate should not be negative")
	}
}

func TestHealerMetrics_SuccessRate(t *testing.T) {
	m := &HealerMetrics{}
	if m.SuccessRate() != 0 {
		t.Error("Expected 0 rate with no attempts")
	}

	m.TotalAttempts = 10
	m.SuccessfulHeals = 7
	rate := m.SuccessRate()
	if rate < 0.69 || rate > 0.71 {
		t.Errorf("Expected ~0.7, got %f", rate)
	}
}

func TestHealStrategy_Flags(t *testing.T) {
	s := HealFingerprint | HealSmartLLM
	if s&HealFingerprint == 0 {
		t.Error("HealFingerprint should be set")
	}
	if s&HealStructural != 0 {
		t.Error("HealStructural should not be set")
	}
	if s&HealSmartLLM == 0 {
		t.Error("HealSmartLLM should be set")
	}
	if s&HealVLM != 0 {
		t.Error("HealVLM should not be set")
	}
	if s&HealVLMJudge != 0 {
		t.Error("HealVLMJudge should not be set")
	}
	if HealAll&HealFingerprint == 0 || HealAll&HealStructural == 0 || HealAll&HealSmartLLM == 0 || HealAll&HealVLM == 0 || HealAll&HealVLMJudge == 0 {
		t.Error("HealAll should include all strategies")
	}
}

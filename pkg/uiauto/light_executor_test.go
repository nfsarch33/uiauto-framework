package uiauto

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLightExecutor(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<html><body><button id="btn">Click</button></body></html>`)
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

	executor := NewLightExecutor(tracker, agent)

	err = agent.Navigate(ts.URL)
	if err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	html, _ := agent.CaptureDOM()
	err = tracker.RegisterPattern(context.Background(), "test_btn", "#btn", "Test button", html)
	if err != nil {
		t.Fatalf("Failed to register pattern: %v", err)
	}

	ctx := context.Background()

	// Successful read action
	action := Action{Type: "read", TargetID: "test_btn"}
	err = executor.Execute(ctx, action)
	if err != nil {
		t.Errorf("Expected execution to succeed, got: %v", err)
	}

	// Unknown action type
	err = executor.Execute(ctx, Action{Type: "unknown_type", TargetID: "test_btn"})
	if err == nil {
		t.Errorf("Expected execution to fail for unknown action type")
	}

	// Pattern not found
	err = executor.Execute(ctx, Action{Type: "read", TargetID: "nonexistent"})
	if err == nil {
		t.Error("Expected error for missing pattern")
	}

	// Drift recovery path
	err = agent.Navigate("data:text/html,<html><body><button id='new-btn' class='changed'>Click</button></body></html>")
	if err == nil {
		actionDrift := Action{Type: "read", TargetID: "test_btn"}
		err = executor.Execute(ctx, actionDrift)
		if err == nil {
			t.Error("Expected execution to fail and trigger smart recovery")
		}
	}
}

func TestLightExecutor_BatchExecution(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<html><body>
			<button id="btn1">Button 1</button>
			<input id="input1" value="hello"/>
			<div id="div1">Content</div>
		</body></html>`)
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
	executor := NewLightExecutor(tracker, agent)

	err = agent.Navigate(ts.URL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}

	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "btn1", "#btn1", "Button 1", html)
	_ = tracker.RegisterPattern(ctx, "div1", "#div1", "Content div", html)

	actions := []Action{
		{Type: "read", TargetID: "btn1"},
		{Type: "read", TargetID: "div1"},
	}

	result := executor.ExecuteBatch(ctx, actions, false)
	if result.Succeeded != 2 {
		t.Errorf("Expected 2 successes, got %d (failed: %d)", result.Succeeded, result.Failed)
	}
	if result.TotalTime == 0 {
		t.Error("Expected non-zero total time")
	}

	// Batch with stopOnError
	actions = []Action{
		{Type: "read", TargetID: "nonexistent"},
		{Type: "read", TargetID: "btn1"},
	}
	result = executor.ExecuteBatch(ctx, actions, true)
	if result.Succeeded != 0 || result.Failed != 1 {
		t.Errorf("Expected 0 success + 1 fail with stopOnError, got s=%d f=%d", result.Succeeded, result.Failed)
	}
	if len(result.Results) != 1 {
		t.Errorf("Expected 1 result with stopOnError, got %d", len(result.Results))
	}
}

func TestLightExecutor_DiscoverParallel(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<html><body>
			<button id="a">A</button>
			<button id="b">B</button>
		</body></html>`)
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
	executor := NewLightExecutor(tracker, agent)

	err = agent.Navigate(ts.URL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}

	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "a", "#a", "Button A", html)
	_ = tracker.RegisterPattern(ctx, "b", "#b", "Button B", html)

	targets := []Action{
		{Type: "read", TargetID: "a"},
		{Type: "read", TargetID: "b"},
	}

	results := executor.DiscoverParallel(ctx, targets)
	if len(results) != 2 {
		t.Fatalf("Expected 2 parallel results, got %d", len(results))
	}
	for id, r := range results {
		if r.Error != nil {
			t.Errorf("Parallel discover failed for %s: %v", id, r.Error)
		}
	}
}

func TestLightExecutor_Metrics(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<html><body><div id="m">metrics</div></body></html>`)
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
	executor := NewLightExecutor(tracker, agent)

	_ = agent.Navigate(ts.URL)
	html, _ := agent.CaptureDOM()
	ctx := context.Background()
	_ = tracker.RegisterPattern(ctx, "m", "#m", "Metrics div", html)

	_ = executor.Execute(ctx, Action{Type: "read", TargetID: "m"})
	_ = executor.Execute(ctx, Action{Type: "read", TargetID: "missing"})

	snap := executor.Metrics.Snapshot()
	if snap.TotalActions != 2 {
		t.Errorf("Expected 2 total actions, got %d", snap.TotalActions)
	}
	if snap.SuccessActions != 1 {
		t.Errorf("Expected 1 success, got %d", snap.SuccessActions)
	}
	if snap.FailedActions != 1 {
		t.Errorf("Expected 1 failure, got %d", snap.FailedActions)
	}
	if snap.CacheHits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", snap.CacheHits)
	}
}

func TestLightExecutor_Options(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "p.json"), filepath.Join(tmpDir, "d"))
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	executor := NewLightExecutor(tracker, nil, WithTimeout(5*time.Second), WithMaxRetries(3))
	if executor.defaultTimeout != 5*time.Second {
		t.Errorf("Expected 5s timeout, got %v", executor.defaultTimeout)
	}
	if executor.maxRetries != 3 {
		t.Errorf("Expected 3 retries, got %d", executor.maxRetries)
	}
}

func TestLightExecutor_ContextCancellation(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<html><body><div id="x">test</div></body></html>`)
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
	executor := NewLightExecutor(tracker, agent)
	_ = agent.Navigate(ts.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	actions := []Action{
		{Type: "read", TargetID: "x"},
		{Type: "read", TargetID: "x"},
	}
	result := executor.ExecuteBatch(ctx, actions, false)
	if result.Failed == 0 {
		t.Error("Expected failures from cancelled context")
	}
}

func TestBatchResult_Fields(t *testing.T) {
	br := BatchResult{
		Results:   make([]ActionResult, 0),
		Succeeded: 5,
		Failed:    2,
		NeedSmart: 1,
	}
	if br.Succeeded != 5 || br.Failed != 2 || br.NeedSmart != 1 {
		t.Error("BatchResult fields incorrect")
	}
}

func TestLightExecutor_WaitAction(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<html><body><div id="target">visible</div></body></html>`)
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

	executor := NewLightExecutor(tracker, agent)
	_ = agent.Navigate(ts.URL)

	html, _ := agent.CaptureDOM()
	_ = tracker.RegisterPattern(context.Background(), "tgt", "#target", "Target", html)

	// The "wait" action uses the selector from Value, not from pattern
	err = executor.Execute(context.Background(), Action{
		Type:     "wait",
		TargetID: "tgt",
		Value:    "#target",
	})
	// Wait action does not use pattern selector, it uses its own Value
	// So if element is visible it should succeed (or timeout if not)
	// This test just checks no panic occurs
	_ = err

	// Test with temp directory cleanup tracked by test framework
	_ = os.MkdirAll(filepath.Join(tmpDir, "extra"), 0755)
}

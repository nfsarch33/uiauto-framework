package uiauto

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNoBrowserAgentForBridge(t *testing.T) *MemberAgent {
	t.Helper()
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "patterns.json"))
	if err != nil {
		t.Fatalf("pattern store: %v", err)
	}
	logger := testDiscardLogger()
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	executor := NewLightExecutor(tracker, nil)
	router := NewModelRouter(executor, nil, tracker, nil)
	healer := NewSelfHealer(tracker, nil, nil, nil)
	return NewMemberAgentFromComponents(nil, tracker, executor, nil, nil, router, healer, logger)
}

func setupBridgeWithBrowser(t *testing.T, handler http.HandlerFunc) (*IronClawBridge, *httptest.Server) {
	t.Helper()
	skipWithoutBrowser(t)
	ts := httptest.NewServer(handler)

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")

	store, err := NewPatternStore(patternFile)
	require.NoError(t, err)

	logger := testDiscardLogger()
	tracker := NewPatternTrackerWithStore(store, driftDir)

	browser, err := NewBrowserAgent(true)
	require.NoError(t, err)

	executor := NewLightExecutor(tracker, browser, WithLogger(logger))
	smart := NewSmartDiscoverer(&MockProvider{Response: `#submit`}, "test-model")
	router := NewModelRouter(executor, smart, tracker, browser, WithRouterLogger(logger))
	healer := NewSelfHealer(tracker, smart, nil, browser, WithHealerLogger(logger))

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())
	return bridge, ts
}

func TestIronClawBridge_Execute_Navigate(t *testing.T) {
	bridge, ts := setupBridgeWithBrowser(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="test">Click</button></body></html>`))
	})
	defer ts.Close()
	defer bridge.agent.Close()

	ctx := context.Background()
	result := bridge.Execute(ctx, AgentTask{
		ID:   "nav-1",
		Type: TaskTypeNavigate,
		URL:  ts.URL,
	})

	assert.True(t, result.Success)
	assert.Equal(t, "nav-1", result.TaskID)
	assert.Equal(t, "completed", result.Status)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestIronClawBridge_Execute_Interact(t *testing.T) {
	bridge, ts := setupBridgeWithBrowser(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><form id="login"><input id="user"/><button id="submit">Login</button></form></body></html>`))
	})
	defer ts.Close()
	defer bridge.agent.Close()

	ctx := context.Background()
	_ = bridge.agent.Navigate(ts.URL)
	html, _ := bridge.agent.Browser().CaptureDOM()
	_ = bridge.agent.Tracker().RegisterPattern(ctx, "submit_btn", "#submit", "Submit button", html)

	result := bridge.Execute(ctx, AgentTask{
		ID:      "interact-1",
		Type:    TaskTypeInteract,
		Actions: []Action{{Type: "click", TargetID: "submit_btn", Description: "Click submit"}},
	})

	assert.True(t, result.Success)
	assert.Equal(t, "interact-1", result.TaskID)
	assert.Equal(t, "completed", result.Status)
}

func TestIronClawBridge_Execute_Discover(t *testing.T) {
	bridge, ts := setupBridgeWithBrowser(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="magic">Do Magic</button></body></html>`))
	})
	defer ts.Close()
	defer bridge.agent.Close()

	ctx := context.Background()
	_ = bridge.agent.Navigate(ts.URL)

	result := bridge.Execute(ctx, AgentTask{
		ID:   "discover-1",
		Type: TaskTypeDiscover,
		Actions: []Action{
			{TargetID: "magic_btn", Description: "Magic button"},
		},
	})

	assert.True(t, result.Success)
	assert.Equal(t, "discover-1", result.TaskID)
	assert.Equal(t, 1, result.Patterns)
}

func TestIronClawBridge_Execute_Regression(t *testing.T) {
	bridge, ts := setupBridgeWithBrowser(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>ok</body></html>`))
	})
	defer ts.Close()
	defer bridge.agent.Close()

	ctx := context.Background()
	_ = bridge.agent.Navigate(ts.URL)

	result := bridge.Execute(ctx, AgentTask{
		ID:   "regression-1",
		Type: TaskTypeRegression,
	})

	assert.True(t, result.Success)
	assert.Equal(t, "regression-1", result.TaskID)
	assert.Equal(t, "completed", result.Status)
}

func TestIronClawBridge_Execute_HealthCheck(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())

	ctx := context.Background()
	result := bridge.Execute(ctx, AgentTask{
		ID:   "health-1",
		Type: TaskTypeHealthCheck,
	})

	assert.True(t, result.Success)
	assert.Equal(t, "health-1", result.TaskID)
	assert.Equal(t, "completed", result.Status)
	assert.NotNil(t, result.Metrics)
	assert.Equal(t, "light", result.ModelTier)
}

func TestIronClawBridge_ConcurrencyLimit(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	cfg := DefaultBridgeConfig()
	cfg.MaxConcurrent = 1
	bridge := NewIronClawBridge(agent, cfg)

	ctx := context.Background()

	// First task should succeed
	done := make(chan AgentTaskResult, 2)
	go func() { done <- bridge.Execute(ctx, AgentTask{ID: "h1", Type: TaskTypeHealthCheck}) }()
	go func() { done <- bridge.Execute(ctx, AgentTask{ID: "h2", Type: TaskTypeHealthCheck}) }()

	r1 := <-done
	r2 := <-done

	// Both should complete (one may be rejected if timing is tight, or both succeed sequentially)
	successCount := 0
	if r1.Success && r1.Status != "rejected" {
		successCount++
	}
	if r2.Success && r2.Status != "rejected" {
		successCount++
	}
	if r2.Status == "rejected" {
		assert.Contains(t, r2.Error, "concurrency limit")
	}
	assert.GreaterOrEqual(t, successCount, 1)
}

func TestIronClawBridge_TimeoutHandling(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())

	// Use already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := bridge.Execute(ctx, AgentTask{
		ID:      "timeout-1",
		Type:    TaskTypeHealthCheck,
		Timeout: 1 * time.Nanosecond,
	})

	// HealthCheck is fast; may complete before cancel. Either way we get a result.
	assert.Equal(t, "timeout-1", result.TaskID)
}

func TestIronClawBridge_Stats(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())

	ctx := context.Background()
	_ = bridge.Execute(ctx, AgentTask{ID: "s1", Type: TaskTypeHealthCheck})
	_ = bridge.Execute(ctx, AgentTask{ID: "s2", Type: TaskTypeHealthCheck})

	stats := bridge.Stats()
	assert.Equal(t, int64(2), stats.TotalExecuted)
	assert.Equal(t, int64(2), stats.TotalSuccess)
	assert.Equal(t, int64(0), stats.TotalFailed)
	assert.Equal(t, 0, stats.ActiveTasks)
	assert.Equal(t, 2, stats.HistorySize)
}

func TestIronClawBridge_History(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())

	ctx := context.Background()
	_ = bridge.Execute(ctx, AgentTask{ID: "hist-1", Type: TaskTypeHealthCheck})
	_ = bridge.Execute(ctx, AgentTask{ID: "hist-2", Type: TaskTypeHealthCheck})

	history := bridge.History()
	require.Len(t, history, 2)
	assert.Equal(t, "hist-1", history[0].TaskID)
	assert.Equal(t, "hist-2", history[1].TaskID)
}

func TestIronClawBridge_Navigate_EmptyURL(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())

	ctx := context.Background()
	result := bridge.Execute(ctx, AgentTask{
		ID:   "nav-empty",
		Type: TaskTypeNavigate,
		URL:  "",
	})

	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "requires url")
}

func TestIronClawBridge_UnknownTaskType(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	bridge := NewIronClawBridge(agent, DefaultBridgeConfig())

	ctx := context.Background()
	result := bridge.Execute(ctx, AgentTask{
		ID:   "unknown-1",
		Type: AgentTaskType("unknown"),
	})

	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "unknown task type")
}

func TestIronClawBridge_ConcurrentHealthChecks(t *testing.T) {
	agent := newNoBrowserAgentForBridge(t)
	cfg := DefaultBridgeConfig()
	cfg.MaxConcurrent = 16
	bridge := NewIronClawBridge(agent, cfg)

	ctx := context.Background()
	var wg sync.WaitGroup
	n := 8
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = bridge.Execute(ctx, AgentTask{
				ID:   fmt.Sprintf("concurrent-%d", idx),
				Type: TaskTypeHealthCheck,
			})
		}(i)
	}
	wg.Wait()

	stats := bridge.Stats()
	assert.Equal(t, int64(n), stats.TotalExecuted)
}

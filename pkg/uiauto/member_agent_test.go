package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

func setupTestMemberAgent(t *testing.T, handler http.HandlerFunc) (*MemberAgent, *httptest.Server) {
	t.Helper()
	skipWithoutBrowser(t)
	ts := httptest.NewServer(handler)

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")

	store, err := NewPatternStore(patternFile)
	if err != nil {
		t.Fatalf("failed to create pattern store: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, driftDir)

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("failed to create browser: %v", err)
	}

	executor := NewLightExecutor(tracker, browser, WithLogger(logger))
	mock := &MockProvider{Response: `#submit`}
	smart := NewSmartDiscoverer(mock, "test-model")

	router := NewModelRouter(executor, smart, tracker, browser, WithRouterLogger(logger))
	healer := NewSelfHealer(tracker, smart, nil, browser, WithHealerLogger(logger))

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	return agent, ts
}

func TestMemberAgent_NewFromComponents(t *testing.T) {
	agent, ts := setupTestMemberAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="test">Click</button></body></html>`))
	})
	defer ts.Close()
	defer agent.Close()

	if agent.Browser() == nil {
		t.Fatal("Browser() should not be nil")
	}
	if agent.Tracker() == nil {
		t.Fatal("Tracker() should not be nil")
	}
	if agent.Router() == nil {
		t.Fatal("Router() should not be nil")
	}
	if agent.Healer() == nil {
		t.Fatal("Healer() should not be nil")
	}
	if agent.TaskCount() != 0 {
		t.Errorf("expected TaskCount 0, got %d", agent.TaskCount())
	}
	if agent.CurrentTier() != TierLight {
		t.Errorf("expected TierLight, got %s", agent.CurrentTier())
	}
}

func TestMemberAgent_NavigateAndRegister(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><form id="login"><input id="user" type="text"/><button id="submit">Login</button></form></body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	driftDir := filepath.Join(dir, "drift")
	store, err := NewPatternStore(patternFile)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, driftDir)
	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatal(err)
	}
	executor := NewLightExecutor(tracker, browser, WithLogger(logger))
	smart := NewSmartDiscoverer(&MockProvider{Response: `#submit`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	if err := agent.Navigate(ts.URL); err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}

	ctx := context.Background()
	if err := agent.RegisterPattern(ctx, "submit_btn", "#submit", "Submit button"); err != nil {
		t.Fatalf("RegisterPattern failed: %v", err)
	}

	pattern, ok := tracker.store.Get(ctx, "submit_btn")
	if !ok {
		t.Fatal("pattern not registered")
	}
	if pattern.Selector != "#submit" {
		t.Errorf("expected selector #submit, got %s", pattern.Selector)
	}
}

func TestMemberAgent_RunTask_Success(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><form id="login"><input id="user" type="text"/><button id="submit">Login</button></form></body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, err := NewPatternStore(patternFile)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatal(err)
	}
	executor := NewLightExecutor(tracker, browser, WithLogger(logger))
	smart := NewSmartDiscoverer(&MockProvider{Response: `#submit`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	if err := agent.Navigate(ts.URL); err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}

	ctx := context.Background()
	_ = agent.RegisterPattern(ctx, "submit_btn", "#submit", "Submit button")

	result := agent.RunTask(ctx, "test-login", []Action{
		{Type: "click", TargetID: "submit_btn", Description: "Click submit"},
	})

	if result.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s (err: %v)", result.Status, result.Error)
	}
	if result.TaskID != "test-login" {
		t.Errorf("expected task ID test-login, got %s", result.TaskID)
	}
	if agent.TaskCount() != 1 {
		t.Errorf("expected TaskCount 1, got %d", agent.TaskCount())
	}
}

func TestMemberAgent_RunTask_FailAndHeal(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><form id="login"><button id="new-submit" class="primary">Login</button></form></body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, err := NewPatternStore(patternFile)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatal(err)
	}
	executor := NewLightExecutor(tracker, browser, WithLogger(logger), WithMaxRetries(0))
	smart := NewSmartDiscoverer(&MockProvider{Response: `#new-submit`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	if err := agent.Navigate(ts.URL); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	originalHTML := `<html><body><form id="login"><button id="gone-submit">Login</button></form></body></html>`
	_ = tracker.RegisterPattern(ctx, "submit_btn", "#gone-submit", "Submit button", originalHTML)

	result := agent.RunTask(ctx, "test-heal", []Action{
		{Type: "click", TargetID: "submit_btn", Description: "Click submit"},
	})

	// The action will fail (selector gone), then either the router's smart path
	// or the healer should find the new selector via LLM
	if len(result.Actions) == 0 {
		t.Fatal("expected at least one action result")
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestMemberAgent_Metrics(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="ok">OK</button></body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	smart := NewSmartDiscoverer(&MockProvider{Response: `#ok`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	_ = agent.Navigate(ts.URL)
	ctx := context.Background()
	_ = agent.RegisterPattern(ctx, "ok_btn", "#ok", "OK button")

	agent.RunTask(ctx, "metrics-test", []Action{
		{Type: "click", TargetID: "ok_btn", Description: "Click OK"},
	})

	metrics := agent.Metrics()
	if metrics.VLM != nil {
		t.Error("VLM metrics should be nil when VLM not configured")
	}
	// Router should have recorded at least one action
	if metrics.Router.ActionCount == 0 {
		t.Error("expected at least 1 router action")
	}
}

func TestMemberAgent_DiscoverAndRegister(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><button id="magic">Do Magic</button></body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	smart := NewSmartDiscoverer(&MockProvider{Response: `#magic`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	_ = agent.Navigate(ts.URL)

	ctx := context.Background()
	selector, err := agent.DiscoverAndRegister(ctx, "magic_btn", "Magic button")
	if err != nil {
		t.Fatalf("DiscoverAndRegister failed: %v", err)
	}
	if selector != "#magic" {
		t.Errorf("expected #magic, got %s", selector)
	}

	pattern, ok := tracker.store.Get(ctx, "magic_btn")
	if !ok {
		t.Fatal("pattern not found after discover+register")
	}
	if pattern.Selector != "#magic" {
		t.Errorf("expected selector #magic, got %s", pattern.Selector)
	}
}

func TestMemberAgent_DiscoverAndRegister_NoLLM(t *testing.T) {
	skipWithoutBrowser(t)
	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	router := NewModelRouter(executor, nil, tracker, browser)
	healer := NewSelfHealer(tracker, nil, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, nil, nil, router, healer, logger)
	defer agent.Close()

	ctx := context.Background()
	_, err := agent.DiscoverAndRegister(ctx, "x", "x")
	if err == nil {
		t.Error("expected error when smart discoverer is nil")
	}
}

func TestMemberAgent_WithVLMMetrics(t *testing.T) {
	skipWithoutBrowser(t)
	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	smart := NewSmartDiscoverer(&MockProvider{Response: `#ok`}, "m")
	vlm := NewVLMBridge(&MockProvider{Response: `ok`}, []string{"v1"})
	router := NewModelRouter(executor, smart, tracker, browser, WithVLMBridge(vlm))
	healer := NewSelfHealer(tracker, smart, vlm, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, vlm, router, healer, logger)
	defer agent.Close()

	metrics := agent.Metrics()
	if metrics.VLM == nil {
		t.Error("VLM metrics should not be nil when VLM is configured")
	}
}

func TestMemberAgent_Lifecycle(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div>Page 1</div></body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	smart := NewSmartDiscoverer(&MockProvider{Response: `div`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)

	err := agent.Navigate(ts.URL)
	if err != nil {
		t.Fatalf("Navigate failed: %v", err)
	}

	// Close should not panic
	agent.Close()
}

func TestMemberAgent_DetectDriftAndHeal_EmptyPatterns(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	smart := NewSmartDiscoverer(&MockProvider{Response: `ok`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	_ = agent.Navigate(ts.URL)

	ctx := context.Background()
	results := agent.DetectDriftAndHeal(ctx)
	if len(results) != 0 {
		t.Errorf("expected 0 drift results with empty patterns, got %d", len(results))
	}
}

func TestMemberAgent_TaskStatus_Strings(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		expected string
	}{
		{TaskPending, "pending"},
		{TaskRunning, "running"},
		{TaskCompleted, "completed"},
		{TaskFailed, "failed"},
		{TaskHealing, "healing"},
		{TaskStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.expected {
			t.Errorf("TaskStatus(%d).String() = %s, want %s", tt.status, got, tt.expected)
		}
	}
}

func TestNewMemberAgent_Config(t *testing.T) {
	skipWithoutBrowser(t)

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	cfg := MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		LLMProvider: &MockProvider{Response: `#test`},
		SmartModels: []string{"m1"},
		VLMModels:   []string{"v1"},
	}

	agent, err := NewMemberAgent(cfg)
	if err != nil {
		t.Fatalf("NewMemberAgent failed: %v", err)
	}
	defer agent.Close()

	if agent.Browser() == nil {
		t.Error("Browser() should not be nil")
	}
	metrics := agent.Metrics()
	if metrics.VLM == nil {
		t.Error("VLM metrics should be configured")
	}
}

// MockProvider is defined in smart_discovery_test.go for the package.
// We reuse it here. If this file is compiled independently, it will use
// the package-level MockProvider.
var _ llm.Provider = (*MockProvider)(nil)

func TestMemberAgent_RunTask_MultipleActions(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
			<input id="user" type="text" value="" />
			<input id="pass" type="password" value="" />
			<button id="submit">Login</button>
		</body></html>`))
	}))
	defer ts.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser, WithLogger(logger))
	smart := NewSmartDiscoverer(&MockProvider{Response: `#submit`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	_ = agent.Navigate(ts.URL)
	ctx := context.Background()

	html, _ := browser.CaptureDOM()
	_ = tracker.RegisterPattern(ctx, "user_input", "#user", "Username input", html)
	_ = tracker.RegisterPattern(ctx, "pass_input", "#pass", "Password input", html)
	_ = tracker.RegisterPattern(ctx, "submit_btn", "#submit", "Submit button", html)

	result := agent.RunTask(ctx, "multi-step-login", []Action{
		{Type: "type", TargetID: "user_input", Description: "Enter username", Value: "testuser"},
		{Type: "type", TargetID: "pass_input", Description: "Enter password", Value: "secret"},
		{Type: "click", TargetID: "submit_btn", Description: "Click submit"},
	})

	if result.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s (err: %v)", result.Status, result.Error)
	}
	if len(result.Actions) != 3 {
		t.Errorf("expected 3 action results, got %d", len(result.Actions))
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestMemberAgent_ConcurrentTaskCounting(t *testing.T) {
	skipWithoutBrowser(t)
	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(patternFile)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	browser, _ := NewBrowserAgent(true)
	executor := NewLightExecutor(tracker, browser)
	smart := NewSmartDiscoverer(&MockProvider{Response: `div`}, "m")
	router := NewModelRouter(executor, smart, tracker, browser)
	healer := NewSelfHealer(tracker, smart, nil, browser)

	agent := NewMemberAgentFromComponents(browser, tracker, executor, smart, nil, router, healer, logger)
	defer agent.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		agent.RunTask(ctx, fmt.Sprintf("task-%d", i), nil)
	}

	if got := agent.TaskCount(); got != 5 {
		t.Errorf("expected TaskCount 5, got %d", got)
	}
}

// --- Non-browser gap-fill tests (nil browser MemberAgent) ---

func newNoBrowserAgent(t *testing.T) *MemberAgent {
	t.Helper()
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "patterns.json"))
	if err != nil {
		t.Fatalf("pattern store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	executor := NewLightExecutor(tracker, nil)
	router := NewModelRouter(executor, nil, tracker, nil)
	healer := NewSelfHealer(tracker, nil, nil, nil)

	return NewMemberAgentFromComponents(nil, tracker, executor, nil, nil, router, healer, logger)
}

func TestMemberAgent_NoBrowser_Accessors(t *testing.T) {
	agent := newNoBrowserAgent(t)

	if agent.Browser() != nil {
		t.Error("expected nil Browser")
	}
	if agent.Tracker() == nil {
		t.Fatal("Tracker should not be nil")
	}
	if agent.Router() == nil {
		t.Fatal("Router should not be nil")
	}
	if agent.Healer() == nil {
		t.Fatal("Healer should not be nil")
	}
	if agent.CurrentTier() != TierLight {
		t.Errorf("expected TierLight, got %s", agent.CurrentTier())
	}
	if agent.IsConverged() {
		t.Error("should not be converged initially")
	}
	if agent.TaskCount() != 0 {
		t.Errorf("expected 0 task count, got %d", agent.TaskCount())
	}
}

func TestMemberAgent_NoBrowser_Metrics(t *testing.T) {
	agent := newNoBrowserAgent(t)

	metrics := agent.Metrics()
	if metrics.Executor.TotalActions != 0 {
		t.Errorf("expected 0 total actions, got %d", metrics.Executor.TotalActions)
	}
	if metrics.Router.LightAttempts != 0 {
		t.Errorf("expected 0 light attempts, got %d", metrics.Router.LightAttempts)
	}
	if metrics.Healer.TotalAttempts != 0 {
		t.Errorf("expected 0 heal attempts, got %d", metrics.Healer.TotalAttempts)
	}
	if metrics.VLM != nil {
		t.Error("VLM metrics should be nil when VLM bridge is nil")
	}
}

func TestMemberAgent_NoBrowser_RegisterPatternViaTracker(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx := context.Background()

	if err := agent.Tracker().RegisterPattern(ctx, "test-pat", ".test", "Test pattern", ""); err != nil {
		t.Fatalf("RegisterPattern failed: %v", err)
	}

	p, ok := agent.Tracker().store.Get(ctx, "test-pat")
	if !ok {
		t.Fatal("pattern not found")
	}
	if p.Selector != ".test" {
		t.Errorf("expected .test, got %s", p.Selector)
	}
}

func TestMemberAgent_NoBrowser_RunTask_EmptyActions(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx := context.Background()

	result := agent.RunTask(ctx, "empty-task", nil)
	if result.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted for nil actions, got %s", result.Status)
	}
	if agent.TaskCount() != 1 {
		t.Errorf("expected task count 1, got %d", agent.TaskCount())
	}
}

func TestMemberAgent_NoBrowser_DetectDriftAndHeal(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx := context.Background()

	results := agent.DetectDriftAndHeal(ctx)
	if len(results) != 0 {
		t.Errorf("expected 0 heal results with no patterns, got %d", len(results))
	}
}

// --- V3: Concurrent Exploration Tests ---

func TestConcurrentExplore_EmptyTargets(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx := context.Background()

	results := agent.ConcurrentExplore(ctx, nil, ConcurrentExploreConfig{})
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil targets, got %d", len(results))
	}
}

func TestConcurrentExplore_NavigateFailsWithoutBrowser(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx := context.Background()

	targets := []ExploreTarget{
		{URL: "http://example.com", ElementIDs: []string{"btn"}, Description: "test page"},
		{URL: "http://example.org", ElementIDs: []string{"input"}, Description: "test page 2"},
	}

	results := agent.ConcurrentExplore(ctx, targets, ConcurrentExploreConfig{
		MaxConcurrency: 2,
		PageTimeout:    5 * time.Second,
	})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Error == nil {
			t.Error("expected navigate error for nil browser")
		}
	}
}

func TestConcurrentExplore_ContextCancellation(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	targets := []ExploreTarget{
		{URL: "http://example.com", ElementIDs: []string{"a"}, Description: "cancelled"},
	}

	results := agent.ConcurrentExplore(ctx, targets, ConcurrentExploreConfig{
		MaxConcurrency: 1,
		PageTimeout:    1 * time.Second,
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Error("expected context cancellation error")
	}
}

func TestConcurrentExplore_DefaultConfig(t *testing.T) {
	agent := newNoBrowserAgent(t)
	ctx := context.Background()

	targets := []ExploreTarget{
		{URL: "http://example.com", ElementIDs: nil, Description: "empty elements"},
	}

	results := agent.ConcurrentExplore(ctx, targets, ConcurrentExploreConfig{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Even with empty element IDs, navigate error is expected (nil browser)
	if results[0].Error == nil {
		t.Error("expected error from nil browser navigate")
	}
}

func TestSummariseExplore(t *testing.T) {
	results := []ExploreResult{
		{URL: "a.com", Discovered: 3, Failed: 1, HealAttempts: 1, Duration: time.Second},
		{URL: "b.com", Discovered: 2, Failed: 0, HealAttempts: 0, Duration: 2 * time.Second},
		{URL: "c.com", Error: fmt.Errorf("timeout"), Duration: 500 * time.Millisecond},
	}

	rpt := SummariseExplore(results)
	if rpt.TotalTargets != 3 {
		t.Errorf("expected 3 targets, got %d", rpt.TotalTargets)
	}
	if rpt.TotalDiscovered != 5 {
		t.Errorf("expected 5 discovered, got %d", rpt.TotalDiscovered)
	}
	if rpt.TotalFailed != 1 {
		t.Errorf("expected 1 failed, got %d", rpt.TotalFailed)
	}
	if rpt.TotalHealAttempts != 1 {
		t.Errorf("expected 1 heal attempt, got %d", rpt.TotalHealAttempts)
	}
	if len(rpt.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(rpt.Errors))
	}
}

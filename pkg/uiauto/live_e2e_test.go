package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireLiveE2E skips unless UIAUTO_LIVE_E2E=1 is set. These tests run
// against real Chrome (headless or debug-port) rather than mocked fixtures.
func requireLiveE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("UIAUTO_LIVE_E2E") != "1" {
		t.Skip("set UIAUTO_LIVE_E2E=1 to run live E2E tests")
	}
}

// chromeDebugURL returns the Chrome debug WebSocket URL from env or empty.
func chromeDebugURL() string { return os.Getenv("CHROME_DEBUG_URL") }

// --- Live E2E Test 1: Chrome Debug Port Connection ---

func TestLiveE2E_ChromeDebugPortConnection(t *testing.T) {
	requireLiveE2E(t)

	debugURL := chromeDebugURL()
	if debugURL == "" {
		agent, err := NewBrowserAgent(true)
		if err != nil {
			t.Fatalf("NewBrowserAgent headless: %v", err)
		}
		defer agent.Close()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `<!DOCTYPE html><html><body><h1 id="title">Connected</h1></body></html>`)
		}))
		defer ts.Close()

		if err := agent.Navigate(ts.URL); err != nil {
			t.Fatalf("Navigate: %v", err)
		}

		html, err := agent.CaptureDOM()
		if err != nil {
			t.Fatalf("CaptureDOM: %v", err)
		}
		if !strings.Contains(html, "Connected") {
			t.Error("DOM does not contain expected text")
		}
		return
	}

	agent, err := NewBrowserAgentWithRemote(debugURL)
	if err != nil {
		t.Fatalf("NewBrowserAgentWithRemote(%s): %v", debugURL, err)
	}
	defer agent.Close()

	html, err := agent.CaptureDOM()
	if err != nil {
		t.Fatalf("CaptureDOM via remote: %v", err)
	}
	if len(html) == 0 {
		t.Error("empty DOM from remote Chrome")
	}
	t.Logf("remote Chrome DOM length: %d bytes", len(html))
}

// --- Live E2E Test 2: MemberAgent Full Task Lifecycle ---

func TestLiveE2E_MemberAgentTaskLifecycle(t *testing.T) {
	requireLiveE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	cfg := MemberAgentConfig{
		Headless:       true,
		RemoteDebugURL: chromeDebugURL(),
		PatternFile:    patternFile,
		Logger:         testDiscardLogger(),
	}

	agent, err := NewMemberAgent(cfg)
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Navigate and register patterns
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	for _, p := range []struct{ id, sel, desc string }{
		{"username", "#username", "username input"},
		{"password", "#password", "password input"},
		{"submit", "#submit-btn", "submit button"},
	} {
		if err := agent.RegisterPattern(ctx, p.id, p.sel, p.desc); err != nil {
			t.Fatalf("RegisterPattern %s: %v", p.id, err)
		}
	}

	result := agent.RunTask(ctx, "live_e2e_login", []Action{
		{TargetID: "username", Type: "click", Description: "focus username"},
		{TargetID: "password", Type: "click", Description: "focus password"},
		{TargetID: "submit", Type: "click", Description: "click submit"},
	})

	if result.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s; error=%v", result.Status, result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}

	m := agent.Metrics()
	if m.Executor.TotalActions < 3 {
		t.Errorf("expected >= 3 actions, got %d", m.Executor.TotalActions)
	}
	if m.Executor.SuccessActions < 3 {
		t.Errorf("expected >= 3 successes, got %d", m.Executor.SuccessActions)
	}
}

// --- Live E2E Test 3: Drift Detection and Heal Cycle ---

func TestLiveE2E_DriftDetectionAndHealCycle(t *testing.T) {
	requireLiveE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:       true,
		RemoteDebugURL: chromeDebugURL(),
		PatternFile:    patternFile,
		Logger:         testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate clean: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "username", "#username", "username field"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Switch to drifted
	fs.SetScenario(ScenarioDrifted)
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate drifted: %v", err)
	}

	heals := agent.DetectDriftAndHeal(ctx)
	if len(heals) == 0 {
		t.Error("expected heal results after drift, got none")
	}

	var successCount int
	for _, h := range heals {
		t.Logf("heal: target=%s method=%s success=%v old=%s new=%s",
			h.TargetID, h.Method, h.Success, h.OldSelector, h.NewSelector)
		if h.Success {
			successCount++
		}
	}

	m := agent.Metrics()
	if m.Healer.TotalAttempts == 0 {
		t.Error("expected healer attempts > 0")
	}
	t.Logf("heal summary: total=%d successes=%d healer_attempts=%d",
		len(heals), successCount, m.Healer.TotalAttempts)
}

// --- Live E2E Test 4: PageWaiter on Multiple Page Types ---

func TestLiveE2E_PageWaiterMultiPageType(t *testing.T) {
	requireLiveE2E(t)

	pages := map[string]string{
		"static": `<!DOCTYPE html><html><body><h1>Static</h1></body></html>`,
		"delayed_content": `<!DOCTYPE html><html><body>
			<div id="root"></div>
			<script>
				setTimeout(function() {
					document.getElementById('root').innerHTML = '<p id="loaded">Loaded</p>';
				}, 500);
			</script>
		</body></html>`,
		"spa_like": `<!DOCTYPE html><html><body>
			<div id="app"><p>Initial</p></div>
			<script>
				setTimeout(function() {
					document.getElementById('app').innerHTML = '<p id="updated">Updated</p>';
				}, 200);
				setTimeout(function() {
					document.getElementById('app').innerHTML += '<p id="final">Final</p>';
				}, 600);
			</script>
		</body></html>`,
	}

	mux := http.NewServeMux()
	for name, html := range pages {
		content := html
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, content)
		})
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	var browser *BrowserAgent
	var err error
	if du := chromeDebugURL(); du != "" {
		browser, err = NewBrowserAgentWithRemote(du)
	} else {
		browser, err = NewBrowserAgent(true)
	}
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer browser.Close()

	type waitResult struct {
		Page     string        `json:"page"`
		Duration time.Duration `json:"duration_ns"`
		Success  bool          `json:"success"`
		Error    string        `json:"error,omitempty"`
	}

	var results []waitResult
	for name := range pages {
		waiter := NewPageWaiter(10*time.Second, WaitNetworkIdle|WaitDOMStable)
		start := time.Now()
		err := waiter.NavigateAndWait(browser.ctx, ts.URL+"/"+name)
		dur := time.Since(start)

		r := waitResult{Page: name, Duration: dur, Success: err == nil}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
		t.Logf("page=%s duration=%v success=%v", name, dur, err == nil)
	}

	for _, r := range results {
		if !r.Success {
			t.Errorf("PageWaiter failed on %s: %s", r.Page, r.Error)
		}
	}
}

// --- Live E2E Test 5: KPI Baseline Report ---

// KPIBaseline is the structured output for baseline KPI tracking.
type KPIBaseline struct {
	Timestamp         time.Time `json:"timestamp"`
	ActionSuccessRate float64   `json:"action_success_rate"`
	HealSuccessRate   float64   `json:"heal_success_rate"`
	PatternCount      int       `json:"pattern_count"`
	AvgActionLatency  float64   `json:"avg_action_latency_ms"`
	PagesTestedCount  int       `json:"pages_tested_count"`
	DegradedMode      bool      `json:"degraded_mode"`
}

func TestLiveE2E_KPIBaselineReport(t *testing.T) {
	requireLiveE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:       true,
		RemoteDebugURL: chromeDebugURL(),
		PatternFile:    patternFile,
		Logger:         testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Register patterns on clean
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	for _, p := range []struct{ id, sel, desc string }{
		{"username", "#username", "username input"},
		{"password", "#password", "password input"},
		{"submit", "#submit-btn", "submit button"},
	} {
		if err := agent.RegisterPattern(ctx, p.id, p.sel, p.desc); err != nil {
			t.Fatalf("RegisterPattern %s: %v", p.id, err)
		}
	}

	// Run clean actions
	agent.RunTask(ctx, "kpi_clean", []Action{
		{TargetID: "username", Type: "click", Description: "focus username"},
		{TargetID: "password", Type: "click", Description: "focus password"},
		{TargetID: "submit", Type: "click", Description: "click submit"},
	})

	// Run with drift
	fs.SetScenario(ScenarioDrifted)
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate drifted: %v", err)
	}
	agent.DetectDriftAndHeal(ctx)

	// Collect KPI baseline
	m := agent.Metrics()

	var actionSuccessRate, healSuccessRate float64
	if m.Executor.TotalActions > 0 {
		actionSuccessRate = float64(m.Executor.SuccessActions) / float64(m.Executor.TotalActions)
	}
	if m.Healer.TotalAttempts > 0 {
		healSuccessRate = float64(m.Healer.SuccessfulHeals) / float64(m.Healer.TotalAttempts)
	}

	baseline := KPIBaseline{
		Timestamp:         time.Now(),
		ActionSuccessRate: actionSuccessRate,
		HealSuccessRate:   healSuccessRate,
		PatternCount:      3,
		AvgActionLatency:  float64(m.Executor.AvgLatencyMs),
		PagesTestedCount:  2,
		DegradedMode:      m.Degraded,
	}

	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}

	reportPath := filepath.Join(dir, "kpi_baseline.json")
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		t.Fatalf("write baseline report: %v", err)
	}

	t.Logf("KPI Baseline Report:\n%s", string(data))

	if baseline.ActionSuccessRate < 0.5 {
		t.Errorf("action success rate too low: %.2f", baseline.ActionSuccessRate)
	}
}

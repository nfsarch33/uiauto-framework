//go:build integration

package uiauto

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireBrowserE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("UIAUTO_BROWSER_E2E") != "1" {
		t.Skip("set UIAUTO_BROWSER_E2E=1 to run real browser E2E tests")
	}
}

func getDebugURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("CHROME_DEBUG_URL")
	if url == "" {
		url = "ws://localhost:9222"
	}
	return url
}

// --- Real Browser Tests ---

func TestBrowserE2E_ChromeConnection_FixtureNavigation(t *testing.T) {
	requireBrowserE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("Skipping: cannot create headless browser: %v", err)
	}
	defer agent.Close()

	require.NoError(t, agent.Navigate(fs.URL()+"/login"))

	dom, err := agent.CaptureDOM()
	require.NoError(t, err)
	assert.Contains(t, dom, "username", "DOM should contain login form")
	assert.Greater(t, len(dom), 100)
	t.Logf("Captured DOM: %d bytes, contains form=%v", len(dom), strings.Contains(dom, "form"))
}

func TestBrowserE2E_PatternRegistration_AndExecution(t *testing.T) {
	requireBrowserE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Skipf("Skipping: cannot create MemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, agent.Navigate(fs.URL()+"/login"))

	patterns := []struct{ id, sel, desc string }{
		{"username", "#username", "username input"},
		{"password", "#password", "password input"},
		{"submit", "#submit-btn", "submit button"},
	}
	for _, p := range patterns {
		require.NoError(t, agent.RegisterPattern(ctx, p.id, p.sel, p.desc))
	}

	result := agent.RunTask(ctx, "browser_e2e_login", []Action{
		{TargetID: "username", Type: "click", Description: "focus username"},
		{TargetID: "password", Type: "click", Description: "focus password"},
		{TargetID: "submit", Type: "click", Description: "submit form"},
	})

	assert.Equal(t, TaskCompleted, result.Status, "task should complete")
	assert.Nil(t, result.Error)
	assert.Greater(t, result.Duration, time.Duration(0))

	m := agent.Metrics()
	assert.GreaterOrEqual(t, m.Executor.TotalActions, int64(3))
	assert.GreaterOrEqual(t, m.Executor.SuccessActions, int64(3))
	t.Logf("Task completed: actions=%d, successes=%d, duration=%s",
		m.Executor.TotalActions, m.Executor.SuccessActions, result.Duration)
}

func TestBrowserE2E_SelfHealing_DriftedDOM(t *testing.T) {
	requireBrowserE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Skipf("Skipping: cannot create MemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, agent.Navigate(fs.URL()+"/login"))
	require.NoError(t, agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"))
	require.NoError(t, agent.RegisterPattern(ctx, "username", "#username", "username input"))

	fs.SetScenario(ScenarioDrifted)
	require.NoError(t, agent.Navigate(fs.URL()+"/login"))

	heals := agent.DetectDriftAndHeal(ctx)
	require.Greater(t, len(heals), 0, "should detect drift and attempt healing")

	var successCount int
	for _, h := range heals {
		if h.Success {
			successCount++
		}
		t.Logf("Heal: target=%s method=%s success=%v old=%q new=%q duration=%s",
			h.TargetID, h.Method, h.Success, h.OldSelector, h.NewSelector, h.Duration)
	}

	m := agent.Metrics()
	assert.Greater(t, m.Healer.TotalAttempts, int64(0))
	t.Logf("Self-healing: %d heals, %d successes, healer_attempts=%d",
		len(heals), successCount, m.Healer.TotalAttempts)
}

func TestBrowserE2E_ConcurrentExplore_MultiPage(t *testing.T) {
	requireBrowserE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Skipf("Skipping: cannot create MemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targets := []ExploreTarget{
		{URL: fs.URL() + "/login", Description: "login page"},
		{URL: fs.URL() + "/dashboard", Description: "dashboard page"},
	}

	results := agent.ConcurrentExplore(ctx, targets, ConcurrentExploreConfig{
		MaxConcurrency: 2,
		PageTimeout:    15 * time.Second,
	})

	assert.Len(t, results, 2, "should explore both targets")
	for _, r := range results {
		t.Logf("Explore: url=%s discovered=%d failed=%d heal_attempts=%d duration=%s error=%v",
			r.URL, r.Discovered, r.Failed, r.HealAttempts, r.Duration, r.Error)
	}
}

func TestBrowserE2E_SelfHealing_BrokenDOM_Recovery(t *testing.T) {
	requireBrowserE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Skipf("Skipping: cannot create MemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, agent.Navigate(fs.URL()+"/login"))
	require.NoError(t, agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"))

	fs.SetScenario(ScenarioBroken)
	require.NoError(t, agent.Navigate(fs.URL()+"/login"))

	result := agent.RunTask(ctx, "broken_dom_test", []Action{
		{TargetID: "submit", Type: "click", Description: "click broken submit"},
	})

	t.Logf("Broken DOM result: status=%s, heal_results=%d, error=%v",
		result.Status, len(result.HealResults), result.Error)
	assert.Contains(t, []TaskStatus{TaskFailed, TaskCompleted}, result.Status)
}

func TestBrowserE2E_MetricsCollection(t *testing.T) {
	requireBrowserE2E(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: filepath.Join(dir, "patterns.json"),
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Skipf("Skipping: cannot create MemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, agent.Navigate(fs.URL()+"/login"))
	require.NoError(t, agent.RegisterPattern(ctx, "username", "#username", "input"))

	agent.RunTask(ctx, "metrics_test", []Action{
		{TargetID: "username", Type: "click", Description: "click"},
	})

	m := agent.Metrics()
	assert.GreaterOrEqual(t, m.Executor.TotalActions, int64(1))
	assert.Equal(t, int64(0), m.Healer.TotalAttempts, "no healing on clean DOM")
	assert.GreaterOrEqual(t, agent.TaskCount(), 1)
	t.Logf("Metrics: actions=%d, successes=%d, task_count=%d, tier=%s",
		m.Executor.TotalActions, m.Executor.SuccessActions,
		agent.TaskCount(), agent.CurrentTier())
}

package uiauto

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Sprint 7: SelfHealer + PatternPipeline end-to-end integration tests.

func setupPipelineIntegration(t *testing.T) (*PatternPipeline, *SelfHealer, *PatternTracker) {
	t.Helper()

	dir := t.TempDir()
	tracker, err := NewPatternTracker(dir+"/patterns.json", dir)
	require.NoError(t, err)

	pp := NewPatternPipeline(tracker, testDiscardLogger())

	healer := NewSelfHealer(tracker, nil, nil, nil,
		WithHealerLogger(testDiscardLogger()),
		WithHealStrategy(HealFingerprint|HealStructural),
	)

	return pp, healer, tracker
}

func TestPipelineIntegration_DriftDetectionTriggersAlert(t *testing.T) {
	pp, _, tracker := setupPipelineIntegration(t)
	ctx := context.Background()

	origHTML := `<html><body><div id="content"><button id="submit">Submit</button></div></body></html>`
	err := tracker.RegisterPattern(ctx, "submit_btn", "#submit", "submit button", origHTML)
	require.NoError(t, err)

	// First call establishes the baseline fingerprint for this pageID
	drifted, err := pp.CheckAndAlert(ctx, "login_page", "submit_btn", origHTML)
	require.NoError(t, err)
	assert.False(t, drifted, "first call should establish baseline, not report drift")

	// Second call with changed HTML should detect drift
	driftedHTML := `<html><body><div id="content"><button class="new-submit" data-action="submit">Go</button></div></body></html>`
	drifted, err = pp.CheckAndAlert(ctx, "login_page", "submit_btn", driftedHTML)
	require.NoError(t, err)
	assert.True(t, drifted, "should detect drift from original to changed HTML")

	alerts := pp.UnresolvedAlerts()
	assert.GreaterOrEqual(t, len(alerts), 1, "should have at least 1 unresolved alert")
	assert.Equal(t, "login_page", alerts[0].PageID)
	assert.Equal(t, "submit_btn", alerts[0].PatternID)
}

func TestPipelineIntegration_AlertHandlerFires(t *testing.T) {
	pp, _, tracker := setupPipelineIntegration(t)
	ctx := context.Background()

	var handlerCalled int32
	pp.WithAlertHandler(func(alert DriftAlert) {
		atomic.AddInt32(&handlerCalled, 1)
	})

	origHTML := `<html><body><a href="/login" id="login-link">Login</a></body></html>`
	err := tracker.RegisterPattern(ctx, "login_link", "#login-link", "login link", origHTML)
	require.NoError(t, err)

	// Establish baseline
	_, _ = pp.CheckAndAlert(ctx, "home_page", "login_link", origHTML)

	driftedHTML := `<html><body><a href="/auth" class="signin-btn">Sign In</a></body></html>`
	_, _ = pp.CheckAndAlert(ctx, "home_page", "login_link", driftedHTML)

	assert.Greater(t, atomic.LoadInt32(&handlerCalled), int32(0), "alert handler should have been called")
}

func TestPipelineIntegration_ModelHandoffTracking(t *testing.T) {
	pp, _, _ := setupPipelineIntegration(t)

	pp.RecordHandoff("submit_btn", "light", "smart", "cache_miss", true)
	pp.RecordHandoff("login_form", "smart", "vlm", "structural_miss", false)
	pp.RecordHandoff("nav_menu", "light", "smart", "drift_detected", true)

	handoffs := pp.RecentHandoffs(10)
	assert.Equal(t, 3, len(handoffs))
	assert.Equal(t, "nav_menu", handoffs[0].PatternID)
	assert.Equal(t, "login_form", handoffs[1].PatternID)
	assert.Equal(t, "submit_btn", handoffs[2].PatternID)
}

func TestPipelineIntegration_AlertResolution(t *testing.T) {
	pp, _, tracker := setupPipelineIntegration(t)
	ctx := context.Background()

	origHTML := `<html><body><input id="email" type="email" /></body></html>`
	err := tracker.RegisterPattern(ctx, "email_input", "#email", "email input", origHTML)
	require.NoError(t, err)

	// Establish baseline
	_, _ = pp.CheckAndAlert(ctx, "signup_page", "email_input", origHTML)

	driftedHTML := `<html><body><input name="user-email" type="email" /></body></html>`
	_, _ = pp.CheckAndAlert(ctx, "signup_page", "email_input", driftedHTML)

	alerts := pp.UnresolvedAlerts()
	require.GreaterOrEqual(t, len(alerts), 1)
	alertID := alerts[0].ID

	pp.ResolveAlert(alertID)

	remaining := pp.UnresolvedAlerts()
	for _, a := range remaining {
		assert.NotEqual(t, alertID, a.ID, "resolved alert should not appear in unresolved list")
	}
}

func TestPipelineIntegration_DriftSeverityClassification(t *testing.T) {
	tests := []struct {
		similarity float64
		expected   DriftSeverity
	}{
		{0.9, DriftSeverityLow},
		{0.8, DriftSeverityLow},
		{0.7, DriftSeverityMedium},
		{0.5, DriftSeverityMedium},
		{0.3, DriftSeverityHigh},
		{0.2, DriftSeverityHigh},
		{0.1, DriftSeverityCritical},
		{0.0, DriftSeverityCritical},
	}

	for _, tc := range tests {
		got := ClassifyDriftSeverity(tc.similarity)
		assert.Equal(t, tc.expected, got, "similarity=%.2f", tc.similarity)
	}
}

func TestPipelineIntegration_HealerMetricsAccumulate(t *testing.T) {
	_, healer, _ := setupPipelineIntegration(t)

	assert.Equal(t, int64(0), healer.Metrics.TotalAttempts)
	assert.Equal(t, float64(0), healer.Metrics.SuccessRate())

	atomic.AddInt64(&healer.Metrics.TotalAttempts, 10)
	atomic.AddInt64(&healer.Metrics.SuccessfulHeals, 9)
	atomic.AddInt64(&healer.Metrics.FingerprintHeals, 5)
	atomic.AddInt64(&healer.Metrics.StructuralHeals, 4)

	snap := healer.Metrics.Snapshot()
	assert.Equal(t, int64(10), snap.TotalAttempts)
	assert.Equal(t, int64(9), snap.SuccessfulHeals)
	assert.InDelta(t, 0.9, healer.Metrics.SuccessRate(), 0.01)
}

func TestPipelineIntegration_NoDriftNonAlert(t *testing.T) {
	pp, _, tracker := setupPipelineIntegration(t)
	ctx := context.Background()

	html := `<html><body><div id="stable">Content</div></body></html>`
	err := tracker.RegisterPattern(ctx, "stable_div", "#stable", "stable content", html)
	require.NoError(t, err)

	drifted, err := pp.CheckAndAlert(ctx, "test_page", "stable_div", html)
	require.NoError(t, err)
	assert.False(t, drifted, "identical HTML should not trigger drift")

	alerts := pp.UnresolvedAlerts()
	assert.Empty(t, alerts, "no alerts should exist for non-drifted content")
}

func TestPipelineIntegration_MultiplePageDriftTracking(t *testing.T) {
	pp, _, tracker := setupPipelineIntegration(t)
	ctx := context.Background()

	pages := []struct {
		pageID    string
		patternID string
		origHTML  string
		driftHTML string
	}{
		{
			"login_page", "login_btn",
			`<html><body><button id="login">Login</button></body></html>`,
			`<html><body><button class="auth-btn" data-qa="login">Sign In</button></body></html>`,
		},
		{
			"dashboard", "nav_menu",
			`<html><body><nav id="main-nav"><a href="/home">Home</a></nav></body></html>`,
			`<html><body><aside class="sidebar"><a href="/dashboard">Dashboard</a></aside></body></html>`,
		},
		{
			"profile", "avatar_img",
			`<html><body><img id="avatar" src="/img/avatar.png" /></body></html>`,
			`<html><body><div class="avatar-wrapper"><img src="/img/profile.webp" /></div></body></html>`,
		},
	}

	for _, p := range pages {
		err := tracker.RegisterPattern(ctx, p.patternID, "#"+p.patternID, "test element", p.origHTML)
		require.NoError(t, err)
	}

	// Establish baselines first
	for _, p := range pages {
		_, _ = pp.CheckAndAlert(ctx, p.pageID, p.patternID, p.origHTML)
	}

	// Now check for drift
	for _, p := range pages {
		_, _ = pp.CheckAndAlert(ctx, p.pageID, p.patternID, p.driftHTML)
	}

	alerts := pp.UnresolvedAlerts()
	assert.GreaterOrEqual(t, len(alerts), 2, "multiple pages drifting should produce multiple alerts")
}

func TestPipelineIntegration_HandoffRecordRoundTrip(t *testing.T) {
	pp, _, _ := setupPipelineIntegration(t)

	for i := 0; i < 20; i++ {
		pp.RecordHandoff(
			"pattern_"+string(rune('a'+i%5)),
			"light", "smart",
			"test_reason",
			i%3 != 0,
		)
	}

	recent := pp.RecentHandoffs(5)
	assert.Equal(t, 5, len(recent))

	all := pp.RecentHandoffs(100)
	assert.Equal(t, 20, len(all))

	successCount := 0
	for _, h := range all {
		if h.Success {
			successCount++
		}
	}
	// Every 3rd record is a failure: 20 records, 7 failures, 13 successes
	assert.Equal(t, 13, successCount)
}

func TestPipelineIntegration_HealWithNoPattern(t *testing.T) {
	_, healer, _ := setupPipelineIntegration(t)
	ctx := context.Background()

	result := healer.Heal(ctx, "nonexistent_target")
	assert.False(t, result.Success)
	assert.Contains(t, result.Error.Error(), "pattern not found")
	assert.Equal(t, int64(1), healer.Metrics.TotalAttempts)
	assert.Equal(t, int64(1), healer.Metrics.FailedHeals)
}

func TestPipelineIntegration_ConcurrentAlertInsertion(t *testing.T) {
	pp, _, tracker := setupPipelineIntegration(t)
	ctx := context.Background()

	origHTML := `<html><body><div id="target">data</div></body></html>`
	_ = tracker.RegisterPattern(ctx, "target_el", "#target", "target", origHTML)

	// Establish baselines for each page
	for i := 0; i < 10; i++ {
		_, _ = pp.CheckAndAlert(ctx, "page_"+string(rune('0'+i)), "target_el", origHTML)
	}

	driftHTML := `<html><body><span class="new-target">data</span></body></html>`

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			pp.CheckAndAlert(ctx, "page_"+string(rune('0'+idx)), "target_el", driftHTML)
		}(i)
	}

	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent alert insertion timed out")
		}
	}
}

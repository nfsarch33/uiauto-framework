package uiauto

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/aiwright"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/bandits"
	"github.com/prometheus/client_golang/prometheus"
)

// newPrometheusRegistry returns a fresh registry so each test starts clean.
func newPrometheusRegistry() prometheus.Registerer {
	return prometheus.NewRegistry()
}

// uncovered_paths_test.go targets functions that previously sat at 0% coverage
// and don't require a real Chrome instance. Each test uses the existing
// in-package fakes (verifyMockBrowser, MockProvider) plus narrow nil-checks
// on MemberAgent so we can validate the agent's defensive paths cheaply.

// --- RouterMetrics ---------------------------------------------------------

func TestRouterMetrics_LightSuccessRate(t *testing.T) {
	m := &RouterMetrics{}
	if got := m.LightSuccessRate(); got != 0 {
		t.Errorf("rate before any attempts = %v, want 0", got)
	}
	atomic.StoreInt64(&m.LightAttempts, 4)
	atomic.StoreInt64(&m.LightSuccesses, 3)
	if got := m.LightSuccessRate(); got != 0.75 {
		t.Errorf("rate after 3/4 = %v, want 0.75", got)
	}
}

// --- ConvergenceState ------------------------------------------------------

func TestConvergenceState_HitsAndMisses(t *testing.T) {
	c := newConvergenceState(2)
	if c.IsConverged() {
		t.Error("fresh state should not be converged")
	}
	c.RecordHit()
	if c.IsConverged() {
		t.Error("after 1 hit (threshold 2) should not be converged yet")
	}
	c.RecordHit()
	if !c.IsConverged() {
		t.Error("after threshold hits should be converged")
	}
	c.RecordMiss()
	if c.IsConverged() {
		t.Error("after a miss the state must reset")
	}
}

// --- WithConvergenceThreshold ---------------------------------------------

func TestWithConvergenceThreshold_AppliesToRouter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	exec := NewLightExecutor(tracker, fakeOptBrowser{})
	router := NewModelRouter(exec, nil, tracker, nil, WithConvergenceThreshold(7))
	if router.convergence == nil || router.convergence.threshold != 7 {
		t.Errorf("WithConvergenceThreshold did not propagate; threshold=%d", router.convergence.threshold)
	}
}

// --- actionFeatures + Bandit accessor -------------------------------------

func TestModelRouter_ActionFeaturesAndBandit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	exec := NewLightExecutor(tracker, fakeOptBrowser{})
	bandit := bandits.NewContextualBandit(bandits.DefaultConfig(), 1)
	router := NewModelRouter(exec, nil, tracker, nil, WithBandit(bandit))

	if router.Bandit() != bandit {
		t.Error("Bandit() did not return configured bandit")
	}

	feats := router.actionFeatures(Action{TargetID: "btn-id"})
	if !feats.HasDataTestID {
		t.Error("actionFeatures should set HasDataTestID when TargetID present")
	}
	if feats.SelectorCount != 1 {
		t.Errorf("SelectorCount = %d, want 1", feats.SelectorCount)
	}

	feats = router.actionFeatures(Action{})
	if feats.HasDataTestID {
		t.Error("actionFeatures should clear HasDataTestID when TargetID empty")
	}
}

// --- LightExecutor.ExecuteBatch & DiscoverParallel ------------------------

func TestLightExecutor_ExecuteBatch_HappyAndStop(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	ctx := context.Background()
	actions := []Action{
		{Type: "click", TargetID: "home"},
		{Type: "click", TargetID: "home"},
	}
	res := exec.ExecuteBatch(ctx, actions, false)
	if res.Succeeded != 2 || res.Failed != 0 {
		t.Errorf("ExecuteBatch = %d ok / %d fail, want 2/0", res.Succeeded, res.Failed)
	}
	if res.TotalTime <= 0 {
		t.Error("TotalTime should be set")
	}
}

func TestLightExecutor_ExecuteBatch_StopOnError(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	ctx := context.Background()
	actions := []Action{
		{Type: "click", TargetID: "missing"}, // no pattern → cache_miss
		{Type: "click", TargetID: "home"},
	}
	res := exec.ExecuteBatch(ctx, actions, true)
	if res.Failed == 0 {
		t.Error("expected at least one failure")
	}
	if len(res.Results) > 1 {
		t.Errorf("stopOnError should halt after first failure, got %d results", len(res.Results))
	}
}

func TestLightExecutor_ExecuteBatch_ContextCancelled(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := exec.ExecuteBatch(ctx, []Action{{Type: "click", TargetID: "home"}}, false)
	if res.Failed == 0 {
		t.Error("cancelled ctx should produce a failure")
	}
}

func TestLightExecutor_DiscoverParallel_TwoTargets(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	results := exec.DiscoverParallel(context.Background(), []Action{
		{Type: "click", TargetID: "home"},
		{Type: "click", TargetID: "missing"},
	})
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if _, ok := results["home"]; !ok {
		t.Error("missing 'home' result")
	}
}

func TestLightExecutor_DiscoverParallel_CtxCancelled(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := exec.DiscoverParallel(ctx, []Action{{Type: "click", TargetID: "home"}})
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// --- LightExecutor.performAction error branches ---------------------------

// readErrBrowser fails Evaluate so performAction("read") returns an error.
type readErrBrowser struct{ verifyMockBrowser }

func (r *readErrBrowser) Evaluate(string, interface{}) error {
	return errors.New("eval failed")
}

func TestLightExecutor_PerformAction_ReadError(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &readErrBrowser{}
	exec := NewLightExecutor(tracker, mb)

	res := exec.executeWithMetrics(context.Background(), Action{Type: "read", TargetID: "home"})
	if res.Error == nil {
		t.Error("expected error from read action when Evaluate fails")
	}
}

// --- MemberAgent simple methods (nil browser path) -----------------------

func TestMemberAgent_NilBrowser_ReturnsErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	agent := NewMemberAgentFromComponents(nil, nil, nil, nil, nil, nil, nil, logger)
	if agent == nil {
		t.Fatal("agent must not be nil")
	}

	// WithMemberLogger applied via the option API.
	other := slog.New(slog.NewTextHandler(io.Discard, nil))
	WithMemberLogger(other)(agent)
	if agent.logger != other {
		t.Error("WithMemberLogger did not set logger")
	}

	if err := agent.Navigate("https://example.com"); err == nil {
		t.Error("Navigate with nil browser should error")
	}
	if err := agent.RegisterPattern(context.Background(), "id", "#x", "desc"); err == nil {
		t.Error("RegisterPattern with nil browser should error")
	}
	// DiscoverAndRegister fails on nil smart first.
	if _, err := agent.DiscoverAndRegister(context.Background(), "id", "desc"); err == nil {
		t.Error("DiscoverAndRegister with nil smart should error")
	}

	// targetCircuitBreaker creates and caches per-target breakers.
	cb := agent.targetCircuitBreaker("t1")
	if cb == nil {
		t.Error("targetCircuitBreaker returned nil")
	}
	if agent.targetCircuitBreaker("t1") != cb {
		t.Error("targetCircuitBreaker should reuse cached entry")
	}

	// Close on nil browser is a no-op (must not panic).
	agent.Close()
}

func TestMemberAgent_DiscoverAndRegister_NilBrowserAfterSmart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	smart := NewSmartDiscoverer(&MockProvider{Response: "#x"}, "m")
	agent := NewMemberAgentFromComponents(nil, nil, nil, smart, nil, nil, nil, logger)
	if _, err := agent.DiscoverAndRegister(context.Background(), "id", "desc"); err == nil {
		t.Error("expected error: smart present but browser nil")
	}
}

// --- aiwright option setters ---------------------------------------------

func TestAIWrightOptionSetters(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	c := aiwright.NewClient("http://localhost:9999", aiwright.WithLogger(logger))
	if c == nil {
		t.Fatal("NewClient returned nil")
	}

	// Bridge with logger option must not panic and must construct successfully.
	b := aiwright.NewBridge(c, nil, aiwright.WithBridgeLogger(logger))
	if b == nil {
		t.Fatal("NewBridge returned nil")
	}
}

// --- LearningLoop.persistBanditState --------------------------------------

func TestLearningLoop_PersistBanditState_NoBanditEntries(t *testing.T) {
	dir := t.TempDir()
	bandit := bandits.NewContextualBandit(bandits.DefaultConfig(), 1)

	loop := &LearningLoop{
		bandit:         bandit,
		banditStateDir: filepath.Join(dir, "state"),
	}

	if err := loop.persistBanditState(); err != nil {
		t.Fatalf("persistBanditState: %v", err)
	}

	// File should exist (even with no entries the snapshot is written).
	statePath := filepath.Join(loop.banditStateDir, "bandit-state.json")
	if _, err := os.ReadFile(statePath); err != nil {
		t.Errorf("expected snapshot at %s: %v", statePath, err)
	}
}

func TestLearningLoop_PersistBanditState_WithEntries(t *testing.T) {
	dir := t.TempDir()
	bandit := bandits.NewContextualBandit(bandits.DefaultConfig(), 1)
	bandit.Update(bandits.Features{PageComplexity: 0.1, HasDataTestID: true}, bandits.ArmLight, 1.0)
	bandit.Update(bandits.Features{PageComplexity: 0.4, HasDataTestID: true}, bandits.ArmSmart, 1.0)

	loop := &LearningLoop{
		bandit:         bandit,
		banditStateDir: filepath.Join(dir, "state"),
	}
	if err := loop.persistBanditState(); err != nil {
		t.Fatalf("persistBanditState: %v", err)
	}
	statePath := filepath.Join(loop.banditStateDir, "bandit-state.json")
	if _, err := os.ReadFile(statePath); err != nil {
		t.Errorf("expected snapshot file: %v", err)
	}
}

// --- ModelRouter.checkPromotion / checkDemotion ---------------------------

func TestModelRouter_CheckPromotionAndDemotion(t *testing.T) {
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	exec := NewLightExecutor(tracker, fakeOptBrowser{})
	router := NewModelRouter(exec, nil, tracker, nil)

	// Force tier to Smart so checkPromotion can move it down to Light.
	router.tierMu.Lock()
	router.currentTier = TierSmart
	router.tierMu.Unlock()

	atomic.StoreInt64(&router.Metrics.LightAttempts, 10)
	atomic.StoreInt64(&router.Metrics.LightSuccesses, 10)
	router.checkPromotion()
	if router.CurrentTier() != TierLight {
		t.Errorf("checkPromotion: tier = %v, want Light", router.CurrentTier())
	}

	// Drop success rate below demotion threshold (0.5) with attempts > 5.
	atomic.StoreInt64(&router.Metrics.LightAttempts, 10)
	atomic.StoreInt64(&router.Metrics.LightSuccesses, 1)
	router.checkDemotion()
	if router.CurrentTier() == TierLight {
		t.Errorf("checkDemotion: tier should advance from Light, got %v", router.CurrentTier())
	}
}

func TestModelRouter_ExecuteBatch_DispatchesAll(t *testing.T) {
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	tracker.store.Set(context.Background(), UIPattern{
		ID:         "home",
		Selector:   "#home",
		Confidence: 0.9,
		LastSeen:   time.Now(),
	})

	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)
	router := NewModelRouter(exec, nil, tracker, nil)

	errs := router.ExecuteBatch(context.Background(), []Action{
		{Type: "click", TargetID: "home"},
		{Type: "click", TargetID: "home"},
	})
	if len(errs) != 2 {
		t.Fatalf("ExecuteBatch returned %d errors, want 2 slots", len(errs))
	}
	for i, e := range errs {
		if e != nil {
			t.Errorf("err[%d] = %v, want nil", i, e)
		}
	}
}

// banditGuidedExecution is exercised when a bandit is wired in; warmup
// rounds force ArmLight, which routes through cascadeExecution → light path.
func TestModelRouter_BanditGuided_LightSuccess(t *testing.T) {
	dir := t.TempDir()
	store, err := NewPatternStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tracker := NewPatternTrackerWithStore(store, filepath.Join(dir, "drift"))
	tracker.store.Set(context.Background(), UIPattern{
		ID:         "home",
		Selector:   "#home",
		Confidence: 0.9,
		LastSeen:   time.Now(),
	})

	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)
	bandit := bandits.NewContextualBandit(bandits.DefaultConfig(), 1)
	router := NewModelRouter(exec, nil, tracker, nil, WithBandit(bandit))

	if e := router.ExecuteAction(context.Background(), Action{Type: "click", TargetID: "home"}); e != nil {
		t.Errorf("ExecuteAction error: %v", e)
	}
}

// --- MetricsCollector wired against a real MemberAgent -------------------

func TestMetricsCollector_Collect_AndJudgeReport(t *testing.T) {
	cfg := MemberAgentConfig{
		Headless:    true,
		PatternFile: filepath.Join(t.TempDir(), "p.json"),
	}
	agent, err := NewMemberAgent(cfg)
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	prom := NewMetrics(prometheus.NewRegistry())
	col := NewMetricsCollector(prom, agent)

	atomic.StoreInt64(&agent.executor.Metrics.TotalActions, 5)
	atomic.StoreInt64(&agent.executor.Metrics.SuccessActions, 3)
	atomic.StoreInt64(&agent.executor.Metrics.FailedActions, 2)
	atomic.StoreInt64(&agent.executor.Metrics.CacheHits, 4)
	atomic.StoreInt64(&agent.executor.Metrics.CacheMisses, 1)
	atomic.StoreInt64(&agent.executor.Metrics.StructuralMatches, 2)

	atomic.StoreInt64(&agent.router.Metrics.LightAttempts, 1)
	atomic.StoreInt64(&agent.router.Metrics.SmartAttempts, 1)
	atomic.StoreInt64(&agent.router.Metrics.VLMAttempts, 1)

	col.Collect()

	report := &JudgeEvalReport{
		F1Score:      0.9,
		Precision:    0.95,
		Recall:       0.85,
		Accuracy:     0.92,
		AvgLatencyMs: 250,
		Results: []JudgeCaseResult{
			{Correct: true},
			{Correct: false},
		},
	}
	col.CollectJudgeReport(report)
	// Calling with nil must not panic.
	col.CollectJudgeReport(nil)
}

// --- PageWaiter construction + WithMetrics -------------------------------

func TestPageWaiter_Construction_AndOptions(t *testing.T) {
	w := NewPageWaiter(2*time.Second, WaitElementVisible)
	if w == nil {
		t.Fatal("NewPageWaiter returned nil")
	}
	if got := w.WithTargetSelector("#x"); got.targetSelector != "#x" {
		t.Errorf("WithTargetSelector did not set selector")
	}

	m := NewMetrics(newPrometheusRegistry())
	if got := w.WithMetrics(m); got.metrics != m {
		t.Errorf("WithMetrics did not attach metrics")
	}

	cfg := WaitConfig{
		Timeout:        time.Second,
		Strategy:       WaitDOMStable,
		StableFor:      0,
		PollInterval:   0,
		ContinueOnErr:  true,
		TargetSelector: "#root",
	}
	w2 := NewPageWaiterFromConfig(cfg)
	if w2 == nil || w2.targetSelector != "#root" {
		t.Errorf("NewPageWaiterFromConfig did not propagate config: %+v", w2)
	}
	if w2.stableFor == 0 || w2.pollInterval == 0 {
		t.Error("zero StableFor/PollInterval should be replaced with defaults")
	}

	if cfg2 := DefaultWaitConfig(); cfg2.Timeout == 0 {
		t.Error("DefaultWaitConfig should populate Timeout")
	}
}

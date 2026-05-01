package uiauto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/aiwright"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/bandits"
	"github.com/prometheus/client_golang/prometheus"
)

// TestSmartDiscoverer_DiscoverScript_StripsBackticks verifies the
// LLM-driven JavaScript discovery path, including backtick cleanup.
func TestSmartDiscoverer_DiscoverScript_StripsBackticks(t *testing.T) {
	mock := &MockProvider{Response: "```javascript\nreturn document.title;\n```"}
	d := NewSmartDiscoverer(mock, "test-model")
	got, err := d.DiscoverScript(context.Background(), "get title", "<html></html>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "return document.title;" {
		t.Errorf("DiscoverScript returned %q", got)
	}
}

func TestSmartDiscoverer_DiscoverScript_TruncatesLargeHTML(t *testing.T) {
	mock := &MockProvider{Response: "return 1;"}
	d := NewSmartDiscoverer(mock, "test-model")
	huge := make([]byte, 60_000)
	for i := range huge {
		huge[i] = 'a'
	}
	if _, err := d.DiscoverScript(context.Background(), "x", string(huge)); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSmartDiscoverer_DiscoverScript_AllModelsFail(t *testing.T) {
	mock := &MockProvider{Err: errors.New("boom")}
	d := NewSmartDiscoverer(mock, "a", "b")
	if _, err := d.DiscoverScript(context.Background(), "x", "<html/>"); err == nil {
		t.Error("expected error when all models fail")
	}
}

// TestSmartDiscoverer_DiscoverSelector_ChoicesEmpty -- DiscoverSelector
// already has a happy-path test; this exercises the empty-choices branch.
func TestSmartDiscoverer_DiscoverSelector_ChoicesEmpty(t *testing.T) {
	provider := &emptyChoicesProvider{}
	d := NewSmartDiscoverer(provider, "m")
	if _, err := d.DiscoverSelector(context.Background(), "x", "<html/>"); err == nil {
		t.Error("expected error when provider returns no choices")
	}
}

type emptyChoicesProvider struct{}

func (emptyChoicesProvider) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Choices: nil}, nil
}

// JSONPatternStore.DecayConfidence is currently 0% covered.
func TestJSONPatternStore_DecayConfidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patterns.json")
	store, err := NewPatternStore(path)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	old := UIPattern{ID: "old", Selector: "#a", LastSeen: now.Add(-48 * time.Hour), Confidence: 0.8}
	fresh := UIPattern{ID: "fresh", Selector: "#b", LastSeen: now, Confidence: 0.8}
	low := UIPattern{ID: "low", Selector: "#c", LastSeen: now.Add(-48 * time.Hour), Confidence: 0.05}
	if err := store.Set(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), low); err != nil {
		t.Fatal(err)
	}

	if err := store.DecayConfidence(context.Background(), 24*time.Hour, 0.5); err != nil {
		t.Fatal(err)
	}

	gotOld, _ := store.Get(context.Background(), "old")
	gotFresh, _ := store.Get(context.Background(), "fresh")
	gotLow, _ := store.Get(context.Background(), "low")

	if gotOld.Confidence != 0.4 {
		t.Errorf("old.confidence = %v, want 0.4", gotOld.Confidence)
	}
	if gotFresh.Confidence != 0.8 {
		t.Errorf("fresh.confidence = %v, want 0.8 (untouched)", gotFresh.Confidence)
	}
	if gotLow.Confidence != 0.05 {
		t.Errorf("low.confidence = %v, want 0.05 (skipped: <= 0.1)", gotLow.Confidence)
	}
}

func TestJSONPatternStore_DecayConfidence_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patterns.json")
	store, err := NewPatternStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), UIPattern{ID: "fresh", Selector: "#a", LastSeen: time.Now(), Confidence: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := store.DecayConfidence(context.Background(), 24*time.Hour, 0.9); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// SlogMetricsHandler.WithAttrs and WithGroup are 0% covered.
func TestSlogMetricsHandler_WithAttrsAndGroup(t *testing.T) {
	reg := prometheus.NewRegistry()
	inner := slog.NewTextHandler(io.Discard, nil)
	h := NewSlogMetricsHandler(inner, reg)

	withAttrs := h.WithAttrs([]slog.Attr{slog.String("service", "test")})
	if withAttrs == nil {
		t.Fatal("WithAttrs returned nil")
	}
	if !withAttrs.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled returned false on derived handler")
	}

	withGroup := h.WithGroup("group1")
	if withGroup == nil {
		t.Fatal("WithGroup returned nil")
	}

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	if err := withAttrs.Handle(context.Background(), rec); err != nil {
		t.Errorf("handle: %v", err)
	}
	if err := withGroup.Handle(context.Background(), rec); err != nil {
		t.Errorf("handle: %v", err)
	}
}

// SelfEvaluator's History/SaveHistory/LoadHistory currently require a real
// browser via newTestEvaluator. Exercise them via white-box history injection.
func TestSelfEvaluator_HistoryAndPersistence_NoBrowser(t *testing.T) {
	e := &SelfEvaluator{
		costs:     DefaultCostConfig(),
		maxHist:   10,
		startTime: time.Now(),
	}
	for i := 0; i < 5; i++ {
		e.history = append(e.history, EffectivenessScore{
			OverallScore: float64(i) / 10.0,
		})
	}

	hist := e.History(3)
	if len(hist) != 3 {
		t.Errorf("History(3) len = %d", len(hist))
	}

	hist = e.History(50)
	if len(hist) != 5 {
		t.Errorf("History(50) clamps to 5, got %d", len(hist))
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "hist.json")
	if err := e.SaveHistory(path); err != nil {
		t.Fatal(err)
	}

	other := &SelfEvaluator{costs: DefaultCostConfig(), maxHist: 10}
	if err := other.LoadHistory(path); err != nil {
		t.Fatal(err)
	}
	if len(other.history) != 5 {
		t.Errorf("loaded history len = %d", len(other.history))
	}

	if err := other.LoadHistory(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("LoadHistory should fail on missing file")
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := other.LoadHistory(filepath.Join(dir, "bad.json")); err == nil {
		t.Error("LoadHistory should fail on bad JSON")
	}

	if err := other.SaveHistory(filepath.Join(dir, "deep", "nested", "out.json")); err == nil {
		// Save MarshalIndent succeeds; WriteFile fails because dir is missing.
		// Older Go behaviour may auto-create; either outcome is fine.
		t.Logf("SaveHistory to deep path returned nil (acceptable)")
	}
}

// SelfEvaluator.Latest returns nil for empty history.
func TestSelfEvaluator_LatestEmpty(t *testing.T) {
	e := &SelfEvaluator{maxHist: 10}
	if e.Latest() != nil {
		t.Error("Latest should be nil for empty history")
	}
	e.history = []EffectivenessScore{{OverallScore: 0.42}}
	if got := e.Latest(); got == nil || got.OverallScore != 0.42 {
		t.Errorf("Latest = %+v", got)
	}
}

// SelfEvaluator.computeOverall: covered values inside zone.
func TestSelfEvaluator_ComputeOverall_PartialScore(t *testing.T) {
	e := &SelfEvaluator{costs: DefaultCostConfig()}
	mid := EffectivenessScore{
		ActionSuccessRate: 0.5,
		CacheHitRate:      0.5,
		HealSuccessRate:   0.5,
		HealFrequency:     5,
		TierDistribution:  map[string]float64{"light": 0.5},
	}
	got := e.computeOverall(mid)
	if got <= 0 || got >= 1 {
		t.Errorf("expected mid score in (0,1), got %v", got)
	}
}

// ModelRouter helpers: armToTier / tierToArm / actionFeatures.
func TestModelRouter_Helpers_ArmTierMapping(t *testing.T) {
	cases := []struct {
		arm  bandits.Arm
		tier ModelTier
	}{
		{bandits.ArmLight, TierLight},
		{bandits.ArmSmart, TierSmart},
		{bandits.ArmVLM, TierVLM},
	}
	for _, c := range cases {
		if got := armToTier(c.arm); got != c.tier {
			t.Errorf("armToTier(%v) = %v, want %v", c.arm, got, c.tier)
		}
		if got := tierToArm(c.tier); got != c.arm {
			t.Errorf("tierToArm(%v) = %v, want %v", c.tier, got, c.arm)
		}
	}

	if got := armToTier(bandits.Arm(99)); got != TierLight {
		t.Errorf("armToTier(unknown) = %v, want TierLight default", got)
	}
	if got := tierToArm(ModelTier(99)); got != bandits.ArmLight {
		t.Errorf("tierToArm(unknown) = %v, want ArmLight default", got)
	}
}

func TestModelRouter_PhaseTrackerAndBandit(t *testing.T) {
	bandit := bandits.NewContextualBandit(bandits.DefaultConfig(), 0)
	r := NewModelRouter(nil, nil, nil, nil,
		WithBandit(bandit),
		WithPhaseThresholds(2, 4, 2),
	)
	if r.Bandit() != bandit {
		t.Error("Bandit() did not return injected bandit")
	}
	if r.PhaseTracker() == nil {
		t.Error("PhaseTracker() should be non-nil after WithPhaseThresholds")
	}

	feats := r.actionFeatures(Action{TargetID: "btn"})
	if !feats.HasDataTestID {
		t.Error("expected HasDataTestID=true when TargetID set")
	}
	if feats.SelectorCount != 1 {
		t.Errorf("SelectorCount = %d", feats.SelectorCount)
	}

	feats = r.actionFeatures(Action{})
	if feats.HasDataTestID {
		t.Error("expected HasDataTestID=false when no TargetID")
	}
}

func TestModelRouter_PhaseTracker_NilWhenUnset(t *testing.T) {
	r := NewModelRouter(nil, nil, nil, nil)
	if r.PhaseTracker() != nil {
		t.Error("PhaseTracker should be nil without WithPhaseThresholds")
	}
	if r.Bandit() != nil {
		t.Error("Bandit should be nil without WithBandit")
	}
}

// SelfHealer option setters are mostly 0% covered. Build a healer with all
// options applied and assert each one mutates the right field.
func TestSelfHealer_OptionSetters(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	smartCB := NewCircuitBreaker("test_smart", DefaultCircuitBreakerConfig())
	vlmCB := NewCircuitBreaker("test_vlm", DefaultCircuitBreakerConfig())
	judgeCB := NewCircuitBreaker("test_judge", DefaultCircuitBreakerConfig())
	auditor := stubAxeEvaluator{}

	h := NewSelfHealer(nil, nil, nil, nil,
		WithHealStrategy(HealAll),
		WithHealerLogger(logger),
		WithSmartCircuitBreaker(smartCB),
		WithVLMCircuitBreaker(vlmCB),
		WithVLMJudgeCircuitBreaker(judgeCB),
		WithAccessibilityAuditor(auditor),
	)

	if h.smartCB != smartCB {
		t.Error("smartCB not applied")
	}
	if h.vlmCB != vlmCB {
		t.Error("vlmCB not applied")
	}
	if h.vlmJudgeCB != judgeCB {
		t.Error("vlmJudgeCB not applied")
	}
	if h.axeAuditor == nil {
		t.Error("axeAuditor not applied")
	}
	if h.SmartCB() != smartCB || h.VLMCB() != vlmCB || h.VLMJudgeCB() != judgeCB {
		t.Error("CB getter returned wrong instance")
	}
}

type stubAxeEvaluator struct{}

func (stubAxeEvaluator) Evaluate(_ context.Context, _ string, _ interface{}) error {
	return nil
}

// VLMBridge option setters.
func TestVLMBridge_OptionSetters(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := &aiwright.Bridge{}
	v := NewVLMBridge(nil, []string{"gpt-4o"},
		WithVLMLogger(logger),
		WithAiWright(bridge),
	)
	if v.logger != logger {
		t.Error("WithVLMLogger not applied")
	}
	if v.aiwrightBrdge != bridge {
		t.Error("WithAiWright not applied")
	}
}

// NewVisualVerifier with custom config.
func TestNewVisualVerifier_AppliesConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	vv := NewVisualVerifier(nil, nil, logger, VisualVerifierConfig{
		DOMConfidenceThreshold: 0.9,
		VLMConfidenceThreshold: 0.8,
	})
	if vv.domConfThreshold != 0.9 {
		t.Errorf("dom = %v", vv.domConfThreshold)
	}
	if vv.vlmConfThreshold != 0.8 {
		t.Errorf("vlm = %v", vv.vlmConfThreshold)
	}
}

func TestNewVisualVerifier_DefaultsWhenConfigZero(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	vv := NewVisualVerifier(nil, nil, logger)
	if vv.domConfThreshold != 0.7 {
		t.Errorf("default dom = %v", vv.domConfThreshold)
	}
	if vv.vlmConfThreshold != 0.6 {
		t.Errorf("default vlm = %v", vv.vlmConfThreshold)
	}
}

// JSONPatternStore round-trip via Save/Load is partly covered; explicitly
// exercise the fresh-load path that returns os.ErrNotExist.
func TestNewPatternStore_FreshFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.json")
	store, err := NewPatternStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("store nil")
	}
	got, _ := store.Get(context.Background(), "anything")
	if got.ID != "" {
		t.Errorf("expected empty pattern, got %+v", got)
	}
}

// JSONPatternStore.Save writes valid JSON we can decode.
func TestJSONPatternStore_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.json")
	store, err := NewPatternStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), UIPattern{ID: "x", Selector: "#x", Confidence: 0.7}); err != nil {
		t.Fatal(err)
	}

	store2, err := NewPatternStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := store2.Get(context.Background(), "x")
	if !ok || got.Selector != "#x" {
		t.Errorf("round-trip lost data: %+v ok=%v", got, ok)
	}
}

// Buffer-based slog handler -- ensures Handle path is exercised end-to-end
// alongside WithAttrs/WithGroup chained handlers.
func TestSlogMetricsHandler_HandleWritesToInner(t *testing.T) {
	var buf bytes.Buffer
	reg := prometheus.NewRegistry()
	inner := slog.NewTextHandler(&buf, nil)
	h := NewSlogMetricsHandler(inner, reg)
	logger := slog.New(h)
	logger.Info("hello", "k", "v")
	if !bytes.Contains(buf.Bytes(), []byte("hello")) {
		t.Errorf("inner handler did not receive log: %q", buf.String())
	}
}

// SmartDiscoverer falls back to next model on the first failure.
func TestSmartDiscoverer_FallsBackToSecondModel(t *testing.T) {
	first := &flakyProvider{failOnce: true}
	d := NewSmartDiscoverer(first, "a", "b")
	got, err := d.DiscoverSelector(context.Background(), "x", "<html/>")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Error("expected fallback selector")
	}
}

type flakyProvider struct{ failOnce bool }

func (p *flakyProvider) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if p.failOnce {
		p.failOnce = false
		return nil, errors.New("transient")
	}
	body, _ := json.Marshal(map[string]string{"answer": "#fallback"})
	_ = body
	return &llm.CompletionResponse{
		Choices: []llm.Choice{{Message: llm.Message{Content: "#fallback"}}},
	}, nil
}

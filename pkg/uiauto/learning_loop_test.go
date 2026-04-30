package uiauto

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/evolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Learning Loop Tests ---

func TestLearningLoop_Tick(t *testing.T) {
	agent := newNoBrowserAgent(t)
	evaluator := NewSelfEvaluator(agent, DefaultCostConfig())
	miner := evolver.NewSignalMiner(evolver.DefaultSignalMinerConfig())
	engine := evolver.NewEvolutionEngine(evolver.DefaultEngineConfig(), nil)

	loop := NewLearningLoop(LearningLoopConfig{
		Agent:     agent,
		Evaluator: evaluator,
		Miner:     miner,
		Engine:    engine,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx := context.Background()
	iter, err := loop.Tick(ctx)
	require.NoError(t, err)
	assert.False(t, iter.Timestamp.IsZero())
	assert.Greater(t, iter.Duration, time.Duration(0))
}

func TestLearningLoop_HistoryAccumulates(t *testing.T) {
	agent := newNoBrowserAgent(t)
	evaluator := NewSelfEvaluator(agent, DefaultCostConfig())
	miner := evolver.NewSignalMiner(evolver.DefaultSignalMinerConfig())
	engine := evolver.NewEvolutionEngine(evolver.DefaultEngineConfig(), nil)

	loop := NewLearningLoop(LearningLoopConfig{
		Agent:     agent,
		Evaluator: evaluator,
		Miner:     miner,
		Engine:    engine,
		MaxHist:   5,
	})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := loop.Tick(ctx)
		require.NoError(t, err)
	}

	hist := loop.History(10)
	assert.Len(t, hist, 3)
}

func TestLearningLoop_HistoryTruncated(t *testing.T) {
	agent := newNoBrowserAgent(t)
	evaluator := NewSelfEvaluator(agent, DefaultCostConfig())
	miner := evolver.NewSignalMiner(evolver.DefaultSignalMinerConfig())
	engine := evolver.NewEvolutionEngine(evolver.DefaultEngineConfig(), nil)

	loop := NewLearningLoop(LearningLoopConfig{
		Agent:     agent,
		Evaluator: evaluator,
		Miner:     miner,
		Engine:    engine,
		MaxHist:   2,
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		loop.Tick(ctx)
	}

	hist := loop.History(10)
	assert.Len(t, hist, 2, "should be truncated to maxHist")
}

func TestLearningLoop_TraceBridgeForwarding(t *testing.T) {
	agent := newNoBrowserAgent(t)
	evaluator := NewSelfEvaluator(agent, DefaultCostConfig())
	miner := evolver.NewSignalMiner(evolver.DefaultSignalMinerConfig())
	engine := evolver.NewEvolutionEngine(evolver.DefaultEngineConfig(), nil)

	traceFile := filepath.Join(t.TempDir(), "traces.jsonl")
	collector := evolver.NewTraceCollector(traceFile, 100)
	bridge := evolver.NewTraceBridge(collector, miner, evolver.TraceBridgeConfig{})

	loop := NewLearningLoop(LearningLoopConfig{
		Agent:       agent,
		Evaluator:   evaluator,
		Miner:       miner,
		Engine:      engine,
		TraceBridge: bridge,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx := context.Background()
	iter, err := loop.Tick(ctx)
	require.NoError(t, err)
	assert.False(t, iter.Timestamp.IsZero())

	stats := bridge.Stats()
	assert.GreaterOrEqual(t, stats[evolver.TraceSourceUIAuto], 0)
}

func TestLearningLoop_NilTraceBridge(t *testing.T) {
	agent := newNoBrowserAgent(t)
	evaluator := NewSelfEvaluator(agent, DefaultCostConfig())
	miner := evolver.NewSignalMiner(evolver.DefaultSignalMinerConfig())
	engine := evolver.NewEvolutionEngine(evolver.DefaultEngineConfig(), nil)

	loop := NewLearningLoop(LearningLoopConfig{
		Agent:     agent,
		Evaluator: evaluator,
		Miner:     miner,
		Engine:    engine,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx := context.Background()
	_, err := loop.Tick(ctx)
	require.NoError(t, err, "nil TraceBridge should not cause errors")
}

// --- Pattern Export/Import Tests ---

func TestPatternExport_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	storeFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(storeFile)

	ctx := context.Background()
	p1 := UIPattern{ID: "btn-submit", Selector: "#submit", Confidence: 0.9, LastSeen: time.Now()}
	p2 := UIPattern{ID: "input-email", Selector: "#email", Confidence: 0.7, LastSeen: time.Now()}
	store.Set(ctx, p1)
	store.Set(ctx, p2)

	export, err := ExportPatterns("agent-alpha", store, ctx)
	require.NoError(t, err)
	assert.Equal(t, "agent-alpha", export.AgentID)
	assert.Len(t, export.Patterns, 2)
	assert.Equal(t, "2.0", export.Metadata["version"])

	exportFile := filepath.Join(dir, "export.json")
	err = SavePatternExport(export, exportFile)
	require.NoError(t, err)

	loaded, err := LoadPatternExport(exportFile)
	require.NoError(t, err)
	assert.Equal(t, "agent-alpha", loaded.AgentID)
	assert.Len(t, loaded.Patterns, 2)
}

func TestPatternImport_MergeHigherConfidence(t *testing.T) {
	dir := t.TempDir()
	storeFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(storeFile)

	ctx := context.Background()
	localPattern := UIPattern{ID: "btn-submit", Selector: "#local", Confidence: 0.5, LastSeen: time.Now()}
	store.Set(ctx, localPattern)

	imported := &PatternExport{
		Patterns: []UIPattern{
			{ID: "btn-submit", Selector: "#imported", Confidence: 0.8, LastSeen: time.Now()},
			{ID: "new-btn", Selector: "#new", Confidence: 0.6, LastSeen: time.Now()},
		},
	}

	merged, err := ImportPatterns(store, imported, ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, merged, "should merge both (higher confidence + new)")

	p, ok := store.Get(ctx, "btn-submit")
	assert.True(t, ok)
	assert.Equal(t, "#imported", p.Selector, "should use imported higher-confidence pattern")
}

func TestPatternImport_SkipLowerConfidence(t *testing.T) {
	dir := t.TempDir()
	storeFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(storeFile)

	ctx := context.Background()
	localPattern := UIPattern{ID: "btn-submit", Selector: "#local", Confidence: 0.9, LastSeen: time.Now()}
	store.Set(ctx, localPattern)

	imported := &PatternExport{
		Patterns: []UIPattern{
			{ID: "btn-submit", Selector: "#imported", Confidence: 0.5, LastSeen: time.Now()},
		},
	}

	merged, err := ImportPatterns(store, imported, ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, merged, "should skip lower-confidence import")

	p, ok := store.Get(ctx, "btn-submit")
	assert.True(t, ok)
	assert.Equal(t, "#local", p.Selector, "should keep local higher-confidence pattern")
}

func TestPatternExport_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	storeFile := filepath.Join(dir, "patterns.json")
	store, _ := NewPatternStore(storeFile)

	export, err := ExportPatterns("agent-empty", store, context.Background())
	require.NoError(t, err)
	assert.Len(t, export.Patterns, 0)
}

func TestPatternExport_JSONSerializable(t *testing.T) {
	export := &PatternExport{
		AgentID:    "test",
		ExportedAt: time.Now(),
		Patterns:   []UIPattern{{ID: "p1", Selector: "#p", Confidence: 0.8}},
		Metadata:   map[string]string{"source": "test"},
	}
	data, err := json.Marshal(export)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"agent_id":"test"`)
}

func TestLoadPatternExport_FileNotFound(t *testing.T) {
	_, err := LoadPatternExport("/nonexistent/path.json")
	assert.Error(t, err)
}

// --- KPI Framework Tests ---

func TestKPIFramework_DefaultKPIs(t *testing.T) {
	kf := NewKPIFramework()
	all := kf.AllKPIs()
	assert.Len(t, all, 6)

	kpi, ok := kf.GetKPI("action_success_rate")
	assert.True(t, ok)
	assert.Equal(t, 0.95, kpi.Target)
	assert.Equal(t, "ratio", kpi.Unit)
}

func TestKPIFramework_UpdateFromScore(t *testing.T) {
	kf := NewKPIFramework()

	score := EffectivenessScore{
		ActionSuccessRate: 0.92,
		CacheHitRate:      0.75,
		HealSuccessRate:   0.85,
		EstimatedCostUSD:  0.002,
		OverallScore:      0.82,
		HealFrequency:     1.5,
	}
	kf.UpdateFromScore(score)

	kpi, _ := kf.GetKPI("action_success_rate")
	assert.Equal(t, 0.92, kpi.Current)

	kpi, _ = kf.GetKPI("cache_hit_rate")
	assert.Equal(t, 0.75, kpi.Current)

	kpi, _ = kf.GetKPI("overall_score")
	assert.Equal(t, 0.82, kpi.Current)
}

func TestKPIFramework_AlertsBelowThreshold(t *testing.T) {
	kf := NewKPIFramework()

	score := EffectivenessScore{
		ActionSuccessRate: 0.70,
		CacheHitRate:      0.30,
		HealSuccessRate:   0.40,
		OverallScore:      0.40,
		HealFrequency:     10,
	}
	kf.UpdateFromScore(score)

	alerts := kf.Alerts()
	assert.Greater(t, len(alerts), 0, "should have alerts for low scores")

	alertNames := make(map[string]bool)
	for _, a := range alerts {
		alertNames[a.Name] = true
	}
	assert.True(t, alertNames["action_success_rate"], "should alert on low action success")
	assert.True(t, alertNames["cache_hit_rate"], "should alert on low cache hit rate")
	assert.True(t, alertNames["overall_score"], "should alert on low overall score")
}

func TestKPIFramework_NoAlertsWhenHealthy(t *testing.T) {
	kf := NewKPIFramework()

	score := EffectivenessScore{
		ActionSuccessRate: 0.98,
		CacheHitRate:      0.85,
		HealSuccessRate:   0.90,
		OverallScore:      0.90,
		HealFrequency:     0.5,
	}
	kf.UpdateFromScore(score)

	alerts := kf.Alerts()
	assert.Len(t, alerts, 0, "healthy scores should produce no alerts")
}

func TestKPIFramework_OnTarget(t *testing.T) {
	kf := NewKPIFramework()

	score := EffectivenessScore{
		ActionSuccessRate: 0.98,
		CacheHitRate:      0.85,
		HealSuccessRate:   0.90,
		EstimatedCostUSD:  0.0005,
		OverallScore:      0.90,
		HealFrequency:     1.0,
	}
	kf.UpdateFromScore(score)
	assert.True(t, kf.OnTarget(), "all KPIs above target should return true")
}

func TestKPIFramework_NotOnTarget(t *testing.T) {
	kf := NewKPIFramework()
	assert.False(t, kf.OnTarget(), "zero-valued KPIs should not be on target")
}

func TestKPIFramework_GetUnknownKPI(t *testing.T) {
	kf := NewKPIFramework()
	_, ok := kf.GetKPI("nonexistent")
	assert.False(t, ok)
}

// --- Integration: Learning Loop + KPI Framework ---

func TestLearningLoop_IntegrationWithKPI(t *testing.T) {
	agent := newNoBrowserAgent(t)
	evaluator := NewSelfEvaluator(agent, DefaultCostConfig())
	miner := evolver.NewSignalMiner(evolver.DefaultSignalMinerConfig())
	engine := evolver.NewEvolutionEngine(evolver.DefaultEngineConfig(), nil)
	kf := NewKPIFramework()

	loop := NewLearningLoop(LearningLoopConfig{
		Agent:     agent,
		Evaluator: evaluator,
		Miner:     miner,
		Engine:    engine,
	})

	ctx := context.Background()
	iter, _ := loop.Tick(ctx)
	kf.UpdateFromScore(iter.Score)

	kpi, ok := kf.GetKPI("overall_score")
	assert.True(t, ok)
	assert.Equal(t, iter.Score.OverallScore, kpi.Current)
}

// --- Pattern sharing file I/O ---

func TestPatternExportImport_FullCycle(t *testing.T) {
	dir := t.TempDir()

	// Agent Alpha creates patterns
	alphaStore, _ := NewPatternStore(filepath.Join(dir, "alpha.json"))
	ctx := context.Background()
	alphaStore.Set(ctx, UIPattern{ID: "login-btn", Selector: "#login", Confidence: 0.8, LastSeen: time.Now()})
	alphaStore.Set(ctx, UIPattern{ID: "search-input", Selector: "#search", Confidence: 0.9, LastSeen: time.Now()})

	// Alpha exports
	export, _ := ExportPatterns("alpha", alphaStore, ctx)
	exportPath := filepath.Join(dir, "fleet-export.json")
	SavePatternExport(export, exportPath)

	// Agent Beta imports
	betaStore, _ := NewPatternStore(filepath.Join(dir, "beta.json"))
	loaded, _ := LoadPatternExport(exportPath)
	merged, err := ImportPatterns(betaStore, loaded, ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, merged)

	// Verify Beta has the patterns
	p, ok := betaStore.Get(ctx, "login-btn")
	assert.True(t, ok)
	assert.Equal(t, "#login", p.Selector)
}

// --- LoopIteration JSON roundtrip ---

func TestLoopIteration_JSONRoundTrip(t *testing.T) {
	iter := LoopIteration{
		Timestamp: time.Now(),
		Score:     EffectivenessScore{OverallScore: 0.85},
		Signals:   []evolver.Signal{{ID: "sig-1", Type: evolver.SignalHighLatency}},
		Duration:  500 * time.Millisecond,
	}

	data, err := json.Marshal(iter)
	require.NoError(t, err)

	var decoded LoopIteration
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, 0.85, decoded.Score.OverallScore)
}

// --- SavePatternExport creates valid JSON file ---

func TestSavePatternExport_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")

	export := &PatternExport{
		AgentID:    "test-agent",
		ExportedAt: time.Now(),
		Patterns:   []UIPattern{{ID: "p1", Confidence: 0.5}},
	}
	err := SavePatternExport(export, path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"agent_id": "test-agent"`)
}

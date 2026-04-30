//go:build integration

package evolver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealE2E_TraceBridge_ResearchTraces_Through_Pipeline(t *testing.T) {
	traceFile := filepath.Join(t.TempDir(), "traces.jsonl")
	collector := NewTraceCollector(traceFile, 100)
	miner := NewSignalMiner(SignalMinerConfig{
		RepeatedFailureThreshold: 2,
		HighLatencyThresholdMs:   1000,
		CostSpikeMultiplier:      2.0,
		LowSuccessRateThreshold:  0.6,
		WindowSize:               50,
	})
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	for i := 0; i < 5; i++ {
		err := bridge.RecordResearchTrace(
			"scrape_page",
			i%3 != 0,
			float64(200+i*100),
			func() string {
				if i%3 == 0 {
					return "connection timeout"
				}
				return ""
			}(),
			map[string]string{"url": "https://example.com/page", "stage": "scrape"},
		)
		require.NoError(t, err)
	}

	err := bridge.RecordResearchTrace("llm_completion", true, 3500, "", map[string]string{"model": "qwen3.5:0.8b"})
	require.NoError(t, err)
	err = bridge.RecordResearchTrace("llm_completion", true, 4200, "", map[string]string{"model": "qwen3.5:0.8b"})
	require.NoError(t, err)

	stats := bridge.Stats()
	assert.Equal(t, 7, stats[TraceSourceResearch])

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	assert.Greater(t, len(signals), 0, "should detect at least one signal from repeated failures or high latency")

	hasRepeatedFailure := false
	hasHighLatency := false
	for _, sig := range signals {
		t.Logf("Signal: type=%s severity=%s desc=%q", sig.Type, sig.Severity, sig.Description)
		if sig.Type == SignalRepeatedFailure {
			hasRepeatedFailure = true
		}
		if sig.Type == SignalHighLatency {
			hasHighLatency = true
		}
	}
	assert.True(t, hasRepeatedFailure, "should detect repeated 'connection timeout' failures")
	assert.True(t, hasHighLatency, "should detect high latency from LLM calls >1000ms")
}

func TestRealE2E_FullEvolutionCycle_TracesToCapsules(t *testing.T) {
	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "traces.jsonl")
	storeDir := filepath.Join(tmpDir, "store")
	kbDir := filepath.Join(tmpDir, "kb")

	collector := NewTraceCollector(traceFile, 100)
	miner := NewSignalMiner(SignalMinerConfig{
		RepeatedFailureThreshold: 2,
		HighLatencyThresholdMs:   500,
		LowSuccessRateThreshold:  0.7,
		WindowSize:               50,
	})
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	for i := 0; i < 4; i++ {
		_ = bridge.RecordResearchTrace("pdf_extract", false, 300, "pdf parse error: invalid header", map[string]string{"file": "test.pdf"})
	}
	_ = bridge.RecordResearchTrace("pdf_extract", true, 100, "", map[string]string{"file": "good.pdf"})

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	require.Greater(t, len(signals), 0)

	engine := NewEvolutionEngine(EvolutionEngineConfig{
		Mode:               ModeBalanced,
		MaxMutationsPerRun: 10,
		DryRun:             true,
	}, nil)

	gene := Gene{
		ID:       "gene-pdf-retry",
		Name:     "pdf-retry-with-fallback",
		Category: GeneCategoryResilience,
		Tags:     []string{string(SignalRepeatedFailure)},
		Payload:  json.RawMessage(`{"strategy":"retry-then-fallback","max_retries":3}`),
		Validation: []ValidationStep{
			{Name: "unit-test", Command: "go test ./internal/pdfproc/..."},
		},
		BlastRadius: BlastRadius{Level: RiskLow, AffectedModules: []string{"pdfproc"}, Reversible: true},
		CreatedAt:   time.Now(),
	}
	require.NoError(t, engine.RegisterGene(gene))

	ctx := context.Background()
	mutations, err := engine.Evolve(ctx, signals)
	require.NoError(t, err)
	require.Greater(t, len(mutations), 0, "should produce mutations from signals")

	for _, m := range mutations {
		t.Logf("Mutation: id=%s signal=%s gene=%s risk=%s reasoning=%q", m.ID, m.SignalID, m.GeneID, m.RiskEstimate, m.Reasoning)
	}

	matchedGene := false
	for _, m := range mutations {
		if m.GeneID == "gene-pdf-retry" {
			matchedGene = true
		}
	}
	assert.True(t, matchedGene, "should match the pdf-retry gene for repeated failures")

	store, err := NewCapsuleStore(storeDir)
	require.NoError(t, err)

	capsule := &Capsule{
		ID:          "cap-pdf-retry-001",
		Name:        "PDF retry with fallback",
		Description: "Automatically retry failed PDF extractions with pdfcpu fallback",
		GeneIDs:     []string{"gene-pdf-retry"},
		Status:      CapsuleStatusDraft,
		Metrics: CapsuleMetrics{
			SuccessRate:  0.80,
			AvgLatencyMs: 250,
			SampleCount:  5,
		},
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.SaveCapsule(ctx, capsule))

	for _, ev := range engine.Events() {
		require.NoError(t, store.SaveEvent(ctx, &ev))
	}

	require.NoError(t, store.SaveGene(ctx, &gene))

	kbSync, err := NewGitKBSync(store, GitKBSyncConfig{KBDir: kbDir, AgentID: "test-evolver"})
	require.NoError(t, err)

	syncResult, err := kbSync.SyncAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, syncResult.CapsulesSynced)
	assert.Greater(t, syncResult.EventsSynced, 0)
	assert.Equal(t, 1, syncResult.GenesSynced)

	capFile := filepath.Join(kbDir, "capsules", "cap-pdf-retry-001.json")
	data, err := os.ReadFile(capFile)
	require.NoError(t, err)

	var loadedCap Capsule
	require.NoError(t, json.Unmarshal(data, &loadedCap))
	assert.Equal(t, "PDF retry with fallback", loadedCap.Name)
	assert.Equal(t, CapsuleStatusDraft, loadedCap.Status)

	summaryFile := filepath.Join(kbDir, "summary.json")
	summaryData, err := os.ReadFile(summaryFile)
	require.NoError(t, err)
	assert.Contains(t, string(summaryData), "test-evolver")

	t.Logf("Full cycle: %d traces → %d signals → %d mutations → 1 capsule → KB synced to %s", 5, len(signals), len(mutations), kbDir)
}

func TestRealE2E_UIAutoTraces_SelfHealingSignals(t *testing.T) {
	traceFile := filepath.Join(t.TempDir(), "ui-traces.jsonl")
	collector := NewTraceCollector(traceFile, 100)
	miner := NewSignalMiner(SignalMinerConfig{
		RepeatedFailureThreshold: 2,
		HighLatencyThresholdMs:   2000,
		LowSuccessRateThreshold:  0.5,
		WindowSize:               50,
	})
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	for i := 0; i < 3; i++ {
		err := bridge.RecordUIAutoTrace(
			"click_submit_button",
			false,
			float64(500+i*200),
			[]ToolCall{
				{Name: "chromedp.Click", LatencyMs: 100, Success: false, ErrorMsg: "selector not found: #submit-btn"},
			},
			map[string]string{"model_tier": "light", "page": "checkout"},
		)
		require.NoError(t, err)
	}

	err := bridge.RecordUIAutoTrace(
		"click_submit_button",
		true,
		1200,
		[]ToolCall{
			{Name: "chromedp.Click", LatencyMs: 50, Success: true},
			{Name: "SelectorRepairV2.Repair", LatencyMs: 800, Success: true},
		},
		map[string]string{"model_tier": "smart", "page": "checkout", "healed": "true"},
	)
	require.NoError(t, err)

	stats := bridge.Stats()
	assert.Equal(t, 4, stats[TraceSourceUIAuto])

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	assert.Greater(t, len(signals), 0)

	for _, sig := range signals {
		t.Logf("UI Signal: type=%s severity=%s desc=%q", sig.Type, sig.Severity, sig.Description)
	}
}

func TestRealE2E_MixedTraces_CrossPackageSignals(t *testing.T) {
	traceFile := filepath.Join(t.TempDir(), "mixed-traces.jsonl")
	collector := NewTraceCollector(traceFile, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	for i := 0; i < 5; i++ {
		_ = bridge.RecordResearchTrace("essay_generate", true, float64(2000+i*500), "", map[string]string{"stage": "write"})
	}

	for i := 0; i < 3; i++ {
		_ = bridge.RecordUIAutoTrace("fill_form", true, float64(300+i*100), nil, map[string]string{"page": "registration"})
	}

	for i := 0; i < 4; i++ {
		_ = bridge.RecordResearchTrace("web_scrape", false, 100, "403 forbidden", map[string]string{"url": "https://blocked.example.com"})
	}

	stats := bridge.Stats()
	assert.Equal(t, 9, stats[TraceSourceResearch])
	assert.Equal(t, 3, stats[TraceSourceUIAuto])

	signals, err := bridge.MineSignals()
	require.NoError(t, err)

	hasFailure := false
	for _, sig := range signals {
		if sig.Type == SignalRepeatedFailure {
			hasFailure = true
		}
		t.Logf("Mixed Signal: type=%s severity=%s desc=%q", sig.Type, sig.Severity, sig.Description)
	}
	assert.True(t, hasFailure, "should detect repeated 403 failures from web_scrape")
}

package evolver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIronEvolver(t *testing.T, dryRun bool) (*IronEvolver, *TraceBridge) {
	t.Helper()
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")

	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	promoCfg := DefaultPromotionConfig()
	promoCfg.RequireHITL = false
	pipeline := NewPromotionPipeline(promoCfg, store, harness)

	sandbox := NewEvolutionSandbox(DefaultSandboxConfig(), NewHITLGate(true))

	cfg := DefaultIronEvolverConfig()
	cfg.DryRun = dryRun

	evolver := NewIronEvolver(cfg, bridge, engine, pipeline, sandbox)
	return evolver, bridge
}

func TestIronEvolver_RunCycle_NoSignals(t *testing.T) {
	evolver, _ := newTestIronEvolver(t, true)

	err := evolver.RunCycle(context.Background())
	require.NoError(t, err)

	stats := evolver.Stats()
	assert.Equal(t, 1, stats.CyclesRun)
	assert.Equal(t, 0, stats.SignalsFound)
	assert.Equal(t, 0, stats.MutationsCreated)
}

func TestIronEvolver_RunCycle_WithSignals_DryRun(t *testing.T) {
	evolver, bridge := newTestIronEvolver(t, true)

	for i := 0; i < 5; i++ {
		err := bridge.RecordResearchTrace("scrape-test", false, 100, "timeout", nil)
		require.NoError(t, err)
	}

	err := evolver.RunCycle(context.Background())
	require.NoError(t, err)

	stats := evolver.Stats()
	assert.Equal(t, 1, stats.CyclesRun)
	assert.Greater(t, stats.SignalsFound, 0)
	assert.Equal(t, 0, stats.MutationsApplied, "dry run should not apply mutations")
}

func TestIronEvolver_RunCycle_WithSignals_Live(t *testing.T) {
	evolver, bridge := newTestIronEvolver(t, false)

	for i := 0; i < 5; i++ {
		err := bridge.RecordResearchTrace("scrape-test", false, 100, "timeout", nil)
		require.NoError(t, err)
	}

	err := evolver.RunCycle(context.Background())
	require.NoError(t, err)

	stats := evolver.Stats()
	assert.Equal(t, 1, stats.CyclesRun)
	assert.Greater(t, stats.SignalsFound, 0)
}

func TestIronEvolver_MultipleCycles(t *testing.T) {
	evolver, bridge := newTestIronEvolver(t, true)

	for i := 0; i < 3; i++ {
		_ = bridge.RecordUIAutoTrace("click", true, 50, nil, nil)
		err := evolver.RunCycle(context.Background())
		require.NoError(t, err)
	}

	stats := evolver.Stats()
	assert.Equal(t, 3, stats.CyclesRun)
}

func TestIronEvolver_StartStop(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	cfg := DefaultIronEvolverConfig()
	cfg.AutoEvolve = true
	cfg.EvolveInterval = 50 * time.Millisecond
	cfg.DryRun = true

	evolver := NewIronEvolver(cfg, bridge, engine, nil, nil)
	evolver.Start(context.Background())

	time.Sleep(150 * time.Millisecond)
	evolver.Stop()

	stats := evolver.Stats()
	assert.Greater(t, stats.CyclesRun, 0)
}

func TestIronEvolver_StartIdempotent(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	cfg := DefaultIronEvolverConfig()
	cfg.AutoEvolve = true
	cfg.EvolveInterval = 100 * time.Millisecond

	evolver := NewIronEvolver(cfg, bridge, engine, nil, nil)
	evolver.Start(context.Background())
	evolver.Start(context.Background())
	evolver.Stop()
}

func TestDefaultIronEvolverConfig(t *testing.T) {
	cfg := DefaultIronEvolverConfig()
	assert.False(t, cfg.AutoEvolve)
	assert.Equal(t, 10*time.Minute, cfg.EvolveInterval)
	assert.Equal(t, 1, cfg.MaxMutations)
	assert.False(t, cfg.DryRun)
	assert.True(t, cfg.SafetyChecks)
	assert.NotNil(t, cfg.Logger)
}

func TestIronEvolver_SafetyChecks_SkipsHighRisk(t *testing.T) {
	evolver, bridge := newTestIronEvolver(t, false)
	evolver.cfg.SafetyChecks = true

	for i := 0; i < 5; i++ {
		err := bridge.RecordResearchTrace("scrape-test", false, 100, "timeout", nil)
		require.NoError(t, err)
	}

	err := evolver.RunCycle(context.Background())
	require.NoError(t, err)

	stats := evolver.Stats()
	assert.Equal(t, 1, stats.CyclesRun)
}

func TestIsHighRisk(t *testing.T) {
	assert.False(t, isHighRisk(RiskLow))
	assert.False(t, isHighRisk(RiskMedium))
	assert.True(t, isHighRisk(RiskHigh))
	assert.True(t, isHighRisk(RiskCritical))
	assert.False(t, isHighRisk(""))
}

func TestTruncateReason(t *testing.T) {
	assert.Equal(t, "short", truncateReason("short", 10))
	assert.Equal(t, "0123456789...", truncateReason("0123456789abcdef", 10))
	assert.Equal(t, "", truncateReason("", 10))
}

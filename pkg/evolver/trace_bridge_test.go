package evolver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/internal/doctor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceBridge_RecordUIAutoTrace(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	err := bridge.RecordUIAutoTrace("click-login", true, 150.0, []ToolCall{
		{Name: "chromedp.Click", LatencyMs: 50, Success: true},
	}, map[string]string{"model_tier": "light"})
	require.NoError(t, err)

	stats := bridge.Stats()
	assert.Equal(t, 1, stats[TraceSourceUIAuto])
	assert.Equal(t, 1, collector.Len())
}

func TestTraceBridge_RecordResearchTrace(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	err := bridge.RecordResearchTrace("scrape-d2l", false, 3000.0, "selector not found",
		map[string]string{"url": "https://d2l.deakin.edu.au"})
	require.NoError(t, err)

	stats := bridge.Stats()
	assert.Equal(t, 1, stats[TraceSourceResearch])
}

func TestTraceBridge_MineSignals(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	for i := 0; i < 5; i++ {
		err := bridge.RecordResearchTrace("scrape-fail", false, 100.0, "connection timeout", nil)
		require.NoError(t, err)
	}

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	assert.NotEmpty(t, signals, "repeated failures should trigger signals")
}

func TestTraceBridge_MineSignals_EmptyTraces(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	assert.Nil(t, signals)
}

func TestTraceBridge_StartStop(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	cfg := DefaultTraceBridgeConfig()
	cfg.FlushInterval = 50 * time.Millisecond
	bridge := NewTraceBridge(collector, miner, cfg)

	bridge.Start(context.Background())
	_ = bridge.RecordUIAutoTrace("test", true, 10, nil, nil)

	time.Sleep(100 * time.Millisecond)
	err := bridge.Stop()
	require.NoError(t, err)
}

func TestTraceBridge_StartIdempotent(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())
	bridge.Start(context.Background())
	bridge.Start(context.Background())
	err := bridge.Stop()
	require.NoError(t, err)
}

func TestTraceBridge_MultiSource(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())

	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	require.NoError(t, bridge.RecordUIAutoTrace("click", true, 50, nil, nil))
	require.NoError(t, bridge.RecordResearchTrace("scrape", true, 200, "", nil))
	require.NoError(t, bridge.RecordUIAutoTrace("scroll", true, 30, nil, nil))

	stats := bridge.Stats()
	assert.Equal(t, 2, stats[TraceSourceUIAuto])
	assert.Equal(t, 1, stats[TraceSourceResearch])
	assert.Equal(t, 3, collector.Len())
}

func TestTraceBridge_RecordDoctorReport(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	report := &doctor.Report{
		Suites: []doctor.Suite{
			{
				Name: "evolver",
				Checks: []doctor.Check{
					{Name: "docker", Status: doctor.StatusPass, Message: "ok", Duration: 10 * time.Millisecond},
					{Name: "llm-router", Status: doctor.StatusFail, Message: "unreachable", Duration: 5 * time.Millisecond},
				},
			},
			{
				Name: "research",
				Checks: []doctor.Check{
					{Name: "browser", Status: doctor.StatusPass, Message: "ok", Duration: 1 * time.Millisecond},
				},
			},
		},
		Overall:   "unhealthy",
		Timestamp: time.Now(),
	}

	err := bridge.RecordDoctorReport(report)
	require.NoError(t, err)

	stats := bridge.Stats()
	assert.Equal(t, 3, stats[TraceSourceDoctor])
	assert.Equal(t, 3, collector.Len())

	require.NoError(t, bridge.RecordDoctorReport(nil))
}

func TestTraceBridge_RecordWCMetrics(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	snap := WCMetricsSnapshot{
		Imports:            42,
		Orders:             150,
		Anomalies:          3,
		Descriptions:       28,
		Deploys:            5,
		Errors:             0,
		AvgLatencyMs:       230.5,
		EventsTracked:      900,
		ReportsGenerated:   12,
		StoresSynced:       4,
		OrdersRouted:       88,
		LeadsScored:        200,
		LeadsQualified:     45,
		PricesCalculated:   120,
		DynamicAdjustments: 7,
		ChecksTotal:        500,
		AlertsTotal:        9,
		SequencesRun:       30,
		MessagesSent:       400,
		CampaignsRun:       6,
		InteractionsTotal:  1500,
	}

	err := bridge.RecordWCMetrics(snap)
	require.NoError(t, err)

	stats := bridge.Stats()
	assert.Equal(t, 1, stats[TraceSourceWooCommerce])
	assert.Equal(t, 1, collector.Len())

	require.NoError(t, collector.Flush())
	traces, err := LoadTraces(tracePath)
	require.NoError(t, err)
	require.Len(t, traces, 1)
	assert.True(t, traces[0].Success)
	assert.Equal(t, "woocommerce/metrics_snapshot", traces[0].TaskName)
	assert.InDelta(t, 230.5, traces[0].LatencyMs, 1e-9)

	md := traces[0].Metadata
	// Primary WC operational fields (trace_bridge.go Metadata map)
	assert.Equal(t, "42", md["imports"])
	assert.Equal(t, "150", md["orders"])
	assert.Equal(t, "3", md["anomalies"])
	assert.Equal(t, "28", md["descriptions"])
	assert.Equal(t, "5", md["deploys"])
	assert.Equal(t, "0", md["errors"])
	// Secondary package aggregates
	assert.Equal(t, "900", md["events_tracked"])
	assert.Equal(t, "12", md["reports_generated"])
	assert.Equal(t, "4", md["stores_synced"])
	assert.Equal(t, "88", md["orders_routed"])
	assert.Equal(t, "200", md["leads_scored"])
	assert.Equal(t, "45", md["leads_qualified"])
	assert.Equal(t, "120", md["prices_calculated"])
	assert.Equal(t, "7", md["dynamic_adjustments"])
	assert.Equal(t, "500", md["checks_total"])
	assert.Equal(t, "9", md["alerts_total"])
	assert.Equal(t, "30", md["sequences_run"])
	assert.Equal(t, "400", md["messages_sent"])
	assert.Equal(t, "6", md["campaigns_run"])
	assert.Equal(t, "1500", md["interactions_total"])

	// Every WCMetricsSnapshot field must appear in persisted metadata (avg latency is on ExecutionTrace.LatencyMs).
	expectedKeys := []string{
		"imports", "orders", "anomalies", "descriptions", "deploys", "errors",
		"events_tracked", "reports_generated", "stores_synced", "orders_routed",
		"leads_scored", "leads_qualified", "prices_calculated", "dynamic_adjustments",
		"checks_total", "alerts_total", "sequences_run", "messages_sent",
		"campaigns_run", "interactions_total",
	}
	for _, k := range expectedKeys {
		assert.Contains(t, md, k, "metadata must include %q for evolver WC parity", k)
	}
}

func TestTraceBridge_RecordWCMetrics_WithErrors(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")
	collector := NewTraceCollector(tracePath, 100)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, DefaultTraceBridgeConfig())

	snap := WCMetricsSnapshot{
		Imports: 10,
		Orders:  5,
		Errors:  3,
	}

	err := bridge.RecordWCMetrics(snap)
	require.NoError(t, err)

	err = collector.Flush()
	require.NoError(t, err)

	traces, err := LoadTraces(tracePath)
	require.NoError(t, err)
	require.Len(t, traces, 1)

	assert.False(t, traces[0].Success)
	assert.Contains(t, traces[0].ErrorMsg, "3 errors")
	assert.Equal(t, "woocommerce/metrics_snapshot", traces[0].TaskName)

	md := traces[0].Metadata
	assert.Equal(t, "10", md["imports"])
	assert.Equal(t, "5", md["orders"])
	assert.Equal(t, "0", md["anomalies"])
	assert.Equal(t, "0", md["descriptions"])
	assert.Equal(t, "0", md["deploys"])
	assert.Equal(t, "3", md["errors"])
	assert.Equal(t, "0", md["events_tracked"])
	assert.Equal(t, "0", md["reports_generated"])
	assert.Equal(t, "0", md["stores_synced"])
	assert.Equal(t, "0", md["orders_routed"])
	assert.Equal(t, "0", md["leads_scored"])
	assert.Equal(t, "0", md["leads_qualified"])
	assert.Equal(t, "0", md["prices_calculated"])
	assert.Equal(t, "0", md["dynamic_adjustments"])
	assert.Equal(t, "0", md["checks_total"])
	assert.Equal(t, "0", md["alerts_total"])
	assert.Equal(t, "0", md["sequences_run"])
	assert.Equal(t, "0", md["messages_sent"])
	assert.Equal(t, "0", md["campaigns_run"])
	assert.Equal(t, "0", md["interactions_total"])
}

func TestDefaultTraceBridgeConfig(t *testing.T) {
	cfg := DefaultTraceBridgeConfig()
	assert.Equal(t, 30*time.Second, cfg.FlushInterval)
	assert.True(t, cfg.AutoFlush)
	assert.NotNil(t, cfg.Logger)
}

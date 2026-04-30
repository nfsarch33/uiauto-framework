//go:build integration

package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Phase 3.1: TraceBridge → TraceCollector → SignalMiner with real filesystem ---

func TestIntegration_FullTraceToSignalPipeline(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")

	collector := NewTraceCollector(tracePath, 1000)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, TraceBridgeConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridge.Start(ctx)

	for i := 0; i < 5; i++ {
		err := bridge.RecordResearchTrace(
			"research-pipeline-run",
			i >= 2, // first 2 fail, rest succeed
			float64(1000+i*500),
			func() string {
				if i < 2 {
					return "connection timeout"
				}
				return ""
			}(),
			map[string]string{"unit": "SIT771", "stage": "scrape"},
		)
		require.NoError(t, err, "RecordResearchTrace should succeed")
	}

	err := bridge.RecordUIAutoTrace(
		"login-automation",
		true,
		250.0,
		[]ToolCall{{Name: "click", LatencyMs: 50, Success: true}},
		map[string]string{"page": "/login"},
	)
	require.NoError(t, err)

	require.NoError(t, bridge.Stop())

	stats := bridge.Stats()
	assert.Equal(t, 5, stats[TraceSourceResearch])
	assert.Equal(t, 1, stats[TraceSourceUIAuto])

	persisted, err := LoadTraces(tracePath)
	require.NoError(t, err)
	assert.Len(t, persisted, 6, "6 traces should be persisted to disk")

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	t.Logf("Mined %d signals from %d traces", len(signals), len(persisted))
	for _, s := range signals {
		t.Logf("  signal: type=%s severity=%s desc=%s", s.Type, s.Severity, s.Description)
	}
}

// --- Phase 3.2: CapsuleStore persistence to filesystem ---

func TestIntegration_CapsuleStore_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	capsule := &Capsule{
		ID:          "cap-integration-001",
		Name:        "retry-scraper-capsule",
		Description: "Add retry logic for scraper timeouts",
		Status:      CapsuleStatusDraft,
		CreatedAt:   time.Now(),
		Metadata:    map[string]string{"mutation_id": "mut-001"},
	}

	require.NoError(t, store.SaveCapsule(ctx, capsule))

	loaded, err := store.LoadCapsule(ctx, "cap-integration-001")
	require.NoError(t, err)
	assert.Equal(t, "cap-integration-001", loaded.ID)
	assert.Equal(t, "Add retry logic for scraper timeouts", loaded.Description)

	gene := &Gene{
		ID:          "gene-integration-001",
		Name:        "retry-scraper",
		Description: "Adds exponential backoff retry to scraper",
		Category:    GeneCategoryResilience,
		Tags:        []string{"scraper"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, store.SaveGene(ctx, gene))

	loadedGene, err := store.LoadGene(ctx, "gene-integration-001")
	require.NoError(t, err)
	assert.Equal(t, "retry-scraper", loadedGene.Name)

	event := &EvolutionEvent{
		ID:        "evt-integration-001",
		Type:      EventCapsuleCreated,
		ActorID:   "integration-test",
		ParentID:  "cap-integration-001",
		Payload:   json.RawMessage(`{"message":"Capsule created from scraper timeout signal"}`),
		Outcome:   EventOutcome{Success: true, Reason: "integration test"},
		Timestamp: time.Now(),
	}
	require.NoError(t, store.SaveEvent(ctx, event))

	capsules, err := store.ListCapsules(ctx)
	require.NoError(t, err)
	assert.Len(t, capsules, 1)

	genes, err := store.ListGenes(ctx)
	require.NoError(t, err)
	assert.Len(t, genes, 1)

	events, err := store.ListEvents(ctx)
	require.NoError(t, err)
	assert.Len(t, events, 1)

	t.Logf("Store lifecycle: %d capsules, %d genes, %d events on disk at %s",
		len(capsules), len(genes), len(events), dir)
}

// --- Phase 3.3: PromotionPipeline with real CapsuleStore ---

func TestIntegration_PromotionPipeline_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	harness := NewEvaluationHarness(EvaluationConfig{
		PassThreshold: 0.6,
		MaxScore:      1.0,
	}, nil)

	pipeline := NewPromotionPipeline(PromotionPipelineConfig{
		RequireHITL:        true,
		AutoPromoteLowRisk: false,
		PassThreshold:      0.6,
	}, store, harness)

	ctx := context.Background()

	signal := Signal{
		ID:                "sig-int-001",
		Type:              SignalRepeatedFailure,
		Severity:          SeverityWarning,
		Description:       "Scraper fails 3x on D2L",
		SuggestedMutation: "Add retry with exponential backoff",
		TraceIDs:          []string{"t1", "t2", "t3"},
		DetectedAt:        time.Now(),
	}

	mutation := Mutation{
		ID:           "mut-int-001",
		SignalID:     "sig-int-001",
		Reasoning:    "Add retry with exponential backoff to scraper",
		RiskEstimate: RiskLow,
		Status:       MutationStatusPending,
		CreatedAt:    time.Now(),
	}

	record, err := pipeline.Submit(ctx, mutation, signal)
	require.NoError(t, err)
	assert.Equal(t, "mut-int-001", record.MutationID)
	t.Logf("Submit status: %s (score=%.2f)", record.Status, func() float64 {
		if record.Evaluation != nil {
			return record.Evaluation.Score
		}
		return -1
	}())

	// Rule-based evaluation may pass or reject; verify the pipeline processed it
	retrieved, found := pipeline.GetRecord("mut-int-001")
	require.True(t, found)
	assert.Contains(t, []PromotionStatus{PromotionEvaluated, PromotionRejected, PromotionApproved}, retrieved.Status)

	metrics := pipeline.Metrics()
	t.Logf("Promotion metrics: submitted=%d evaluated=%d approved=%d rejected=%d",
		metrics.TotalSubmitted, metrics.TotalEvaluated, metrics.TotalApproved, metrics.TotalRejected)
	assert.Equal(t, 1, metrics.TotalSubmitted)
}

// --- Phase 3.4: Prometheus Metrics End-to-End ---

func TestIntegration_PrometheusMetrics_FullCycle(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	require.NotNil(t, m)

	m.RecordSignals([]Signal{
		{Type: SignalRepeatedFailure, Severity: SeverityWarning},
		{Type: SignalHighLatency, Severity: SeverityCritical},
	})

	m.RecordMutations([]Mutation{
		{ID: "m1", Status: MutationStatusPending, CreatedAt: time.Now()},
		{ID: "m2", Status: MutationStatusApplied, CreatedAt: time.Now()},
	})

	m.CapsulesCreated.Inc()
	m.GenesAppliedTotal.Inc()

	m.EngineRunsTotal.Inc()
	m.EngineRunDuration.Observe(1.5)

	m.RecordTrace(string(TraceSourceResearch))
	m.RecordTrace(string(TraceSourceUIAuto))
	m.RecordTrace(string(TraceSourceResearch))

	m.RecordPromotion("approved")

	families, err := reg.Gather()
	require.NoError(t, err)

	metricNames := make(map[string]bool)
	for _, f := range families {
		metricNames[f.GetName()] = true
		t.Logf("metric: %s (%d samples)", f.GetName(), len(f.GetMetric()))
	}

	assert.True(t, metricNames["evolver_signals_detected_total"], "signals counter should exist")
	assert.True(t, metricNames["evolver_mutations_total"], "mutations counter should exist")
	assert.True(t, metricNames["evolver_traces_recorded_total"], "traces counter should exist")
	assert.True(t, metricNames["evolver_engine_runs_total"], "engine runs counter should exist")
	assert.True(t, metricNames["evolver_capsules_created_total"], "capsules counter should exist")
}

// --- Phase 3.5: HITL Promotion Gate (simulated) ---

func TestIntegration_HITL_SubmitRejectResubmit(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	harness := NewEvaluationHarness(EvaluationConfig{
		PassThreshold: 0.6,
		MaxScore:      1.0,
	}, nil)

	pipeline := NewPromotionPipeline(PromotionPipelineConfig{
		RequireHITL:        true,
		AutoPromoteLowRisk: false,
		PassThreshold:      0.6,
	}, store, harness)

	ctx := context.Background()

	signal := Signal{
		ID:          "sig-hitl-001",
		Type:        SignalRepeatedFailure,
		Severity:    SeverityCritical,
		Description: "Critical scraper failure",
		DetectedAt:  time.Now(),
	}

	mut1 := Mutation{
		ID:           "mut-hitl-001",
		SignalID:     "sig-hitl-001",
		Reasoning:    "First attempt: naive retry",
		RiskEstimate: RiskLow,
		Status:       MutationStatusPending,
		CreatedAt:    time.Now(),
	}

	rec1, err := pipeline.Submit(ctx, mut1, signal)
	require.NoError(t, err)
	t.Logf("mut-hitl-001 submit status: %s", rec1.Status)

	rec1After, found := pipeline.GetRecord("mut-hitl-001")
	require.True(t, found)

	// The evaluation may reject it; either way, pipeline processed it
	t.Logf("mut-hitl-001 final status: %s", rec1After.Status)

	mut2 := Mutation{
		ID:           "mut-hitl-002",
		SignalID:     "sig-hitl-001",
		Reasoning:    "Second attempt: exponential backoff with jitter",
		RiskEstimate: RiskLow,
		Status:       MutationStatusPending,
		CreatedAt:    time.Now(),
	}

	rec2, err := pipeline.Submit(ctx, mut2, signal)
	require.NoError(t, err)
	t.Logf("mut-hitl-002 submit status: %s", rec2.Status)

	rec2After, found := pipeline.GetRecord("mut-hitl-002")
	require.True(t, found)
	t.Logf("mut-hitl-002 final status: %s", rec2After.Status)

	metrics := pipeline.Metrics()
	t.Logf("HITL metrics: submitted=%d evaluated=%d approved=%d rejected=%d",
		metrics.TotalSubmitted, metrics.TotalEvaluated, metrics.TotalApproved, metrics.TotalRejected)
	assert.Equal(t, 2, metrics.TotalSubmitted)
}

// --- Phase 3.3 (opt): Mem0Bridge with mock client ---

func TestIntegration_Mem0Bridge_SyncCapsules(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, store.SaveCapsule(ctx, &Capsule{
			ID:          capsuleID(i),
			Name:        "sync-capsule-" + capsuleID(i),
			Description: "Test capsule for Mem0 sync",
			Status:      CapsuleStatusActive,
			CreatedAt:   time.Now(),
		}))
	}

	client := &integMem0Client{memories: make(map[string]string)}
	mem0Bridge := NewMem0Bridge(store, client, Mem0BridgeConfig{
		EventLogPath: filepath.Join(dir, "events.jsonl"),
		AgentID:      "integration-test",
	})

	synced, err := mem0Bridge.SyncCapsules(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, synced, "should sync 3 capsules")

	assert.Equal(t, 3, len(client.memories), "mock Mem0 should have 3 entries")

	results, err := mem0Bridge.SearchRelated(ctx, "test capsule", 5)
	require.NoError(t, err)
	t.Logf("Search returned %d related memories", len(results))

	total, failed := mem0Bridge.Stats()
	assert.Equal(t, 3, total)
	assert.Equal(t, 0, failed)
}

func capsuleID(i int) string {
	return "cap-mem0-" + string(rune('a'+i))
}

type integMem0Client struct {
	memories map[string]string
}

func (m *integMem0Client) Add(_ context.Context, content string, _ map[string]string) (string, error) {
	id := fmt.Sprintf("mem-%d", len(m.memories)+1)
	m.memories[id] = content
	return id, nil
}

func (m *integMem0Client) Search(_ context.Context, _ string, limit int) ([]Mem0Memory, error) {
	var results []Mem0Memory
	for id, content := range m.memories {
		results = append(results, Mem0Memory{
			ID:      id,
			Content: content,
			Score:   0.85,
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// --- Full end-to-end: Trace → Signal → Mutation → Promotion → Capsule ---

func TestIntegration_FullEvolutionCycle(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")

	collector := NewTraceCollector(tracePath, 1000)
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	bridge := NewTraceBridge(collector, miner, TraceBridgeConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridge.Start(ctx)

	for i := 0; i < 5; i++ {
		_ = bridge.RecordResearchTrace("research-scrape", false, 2000, "timeout", map[string]string{"stage": "scrape"})
	}
	_ = bridge.RecordResearchTrace("research-scrape", true, 500, "", map[string]string{"stage": "scrape"})

	require.NoError(t, bridge.Stop())

	signals, err := bridge.MineSignals()
	require.NoError(t, err)
	require.NotEmpty(t, signals, "should detect at least one signal")

	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	gene := Gene{
		ID:          "gene-retry-v2",
		Name:        "retry-with-backoff",
		Description: "Adds retry with exponential backoff",
		Category:    GeneCategoryResilience,
		Tags:        []string{"repeated_failure", "scraper"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, engine.RegisterGene(gene))

	mutations, err := engine.Evolve(ctx, signals)
	require.NoError(t, err)
	t.Logf("Generated %d mutations from %d signals", len(mutations), len(signals))

	store, err := NewCapsuleStore(filepath.Join(dir, "capsules"))
	require.NoError(t, err)

	harness := NewEvaluationHarness(EvaluationConfig{
		PassThreshold: 0.6,
		MaxScore:      1.0,
	}, nil)

	pipeline := NewPromotionPipeline(PromotionPipelineConfig{
		RequireHITL:        false,
		AutoPromoteLowRisk: false,
		PassThreshold:      0.6,
	}, store, harness)

	for _, mut := range mutations {
		record, err := pipeline.Submit(ctx, mut, signals[0])
		require.NoError(t, err)
		t.Logf("Submitted mutation: %s → status=%s", record.MutationID, record.Status)
	}

	metrics := pipeline.Metrics()
	t.Logf("Full cycle: %d submitted, %d auto-approved",
		metrics.TotalSubmitted, metrics.TotalApproved)

	persisted, _ := LoadTraces(tracePath)
	assert.Len(t, persisted, 6, "6 traces on disk")

	t.Logf("Full evolution cycle completed: traces=%d signals=%d mutations=%d",
		len(persisted), len(signals), len(mutations))
}

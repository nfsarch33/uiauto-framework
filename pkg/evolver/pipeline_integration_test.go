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

func TestPipelineIntegration_TraceToSignal(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "traces.jsonl")

	tc := NewTraceCollector(tracePath, 100)

	// Record traces that should trigger repeated_failure and high_latency signals
	for i := 0; i < 4; i++ {
		require.NoError(t, tc.Record(newFailedTrace("t"+string(rune('0'+i)), "scrape", "connection timeout")))
	}
	require.NoError(t, tc.Record(newTrace("t4", "slow-task", true, 6000, 0.01)))
	require.NoError(t, tc.Flush())

	traces, err := LoadTraces(tracePath)
	require.NoError(t, err)
	require.Len(t, traces, 5)

	miner := NewSignalMiner(DefaultSignalMinerConfig())
	signals := miner.Mine(traces)

	require.NotEmpty(t, signals, "expected at least one signal from traces")

	var foundFailure, foundLatency bool
	for _, s := range signals {
		if s.Type == SignalRepeatedFailure {
			foundFailure = true
			assert.NotEmpty(t, s.ID)
			assert.NotEmpty(t, s.Description)
			assert.NotEmpty(t, s.TraceIDs)
			assert.NotEmpty(t, s.SuggestedMutation)
		}
		if s.Type == SignalHighLatency {
			foundLatency = true
			assert.NotEmpty(t, s.ID)
			assert.NotEmpty(t, s.Description)
		}
	}
	assert.True(t, foundFailure, "expected repeated_failure signal")
	assert.True(t, foundLatency, "expected high_latency signal")
}

func TestPipelineIntegration_SignalToMutation(t *testing.T) {
	ctx := context.Background()
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	// Register a gene with validation for matching
	gene := Gene{
		ID:          "gene-retry",
		Name:        "retry-handler",
		Description: "Adds retry logic for transient failures",
		Category:    GeneCategoryResilience,
		Tags:        []string{"repeated_failure"},
		Validation: []ValidationStep{
			{Name: "smoke", Command: "go test ./...", Timeout: "60s"},
		},
		BlastRadius: BlastRadius{Level: RiskLow, Reversible: true},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, engine.RegisterGene(gene))

	signals := []Signal{
		{
			ID:                "sig-001",
			Type:              SignalRepeatedFailure,
			Severity:          SeverityWarning,
			Description:       "timeout repeated 4 times",
			TraceIDs:          []string{"t1", "t2", "t3", "t4"},
			SuggestedMutation: "add retry with backoff",
			DetectedAt:        time.Now().UTC(),
		},
	}

	muts, err := engine.Evolve(ctx, signals)
	require.NoError(t, err)
	require.Len(t, muts, 1)

	mut := muts[0]
	assert.NotEmpty(t, mut.ID, "mutation must have ID")
	assert.NotEmpty(t, mut.SignalID, "mutation must reference signal")
	assert.NotEmpty(t, mut.Reasoning, "mutation must have description/reasoning")
	assert.Equal(t, "gene-retry", mut.GeneID, "mutation should match registered gene")
	assert.NotEmpty(t, mut.RiskEstimate)
	assert.Equal(t, MutationStatusPending, mut.Status)

	// Verify the matched gene has validation
	genes := engine.Genes()
	require.Contains(t, genes, "gene-retry")
	assert.Len(t, genes["gene-retry"].Validation, 1)
	assert.Equal(t, "smoke", genes["gene-retry"].Validation[0].Name)
	assert.NotEmpty(t, genes["gene-retry"].Validation[0].Command)
}

func TestPipelineIntegration_MutationToPromotion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	cfg := DefaultPromotionConfig()
	cfg.RequireHITL = true
	pipeline := NewPromotionPipeline(cfg, store, harness)

	mut := Mutation{
		ID:           "mut-promo",
		SignalID:     "sig-promo",
		GeneID:       "gene-fix",
		RiskEstimate: RiskLow,
		Reasoning:    "matched gene for selector repair with structural matching and cached patterns",
		Strategy:     ModeBalanced,
		Status:       MutationStatusPending,
		CreatedAt:    time.Now().UTC(),
	}
	sig := Signal{
		ID:          "sig-promo",
		Type:        SignalRepeatedFailure,
		Severity:    SeverityCritical,
		Description: "selector drift detected",
		DetectedAt:  time.Now().UTC(),
	}

	rec, err := pipeline.Submit(ctx, mut, sig)
	require.NoError(t, err)
	require.Equal(t, PromotionEvaluated, rec.Status)
	require.True(t, rec.Evaluation.Pass, "mutation should pass evaluation")

	require.NoError(t, pipeline.Approve("mut-promo", "reviewer@test.com", "verified"))

	capsule := &Capsule{
		ID:          "cap-promo",
		Name:        "selector-fix-promo",
		Description: "Fix login selector drift",
		GeneIDs:     []string{"gene-fix"},
		Environment: EnvFingerprint{OS: "darwin", Arch: "arm64"},
		Metrics:     CapsuleMetrics{SuccessRate: 0.95, SampleCount: 20},
		Status:      CapsuleStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	require.NoError(t, pipeline.Promote(ctx, "mut-promo", capsule))

	saved, err := store.LoadCapsule(ctx, "cap-promo")
	require.NoError(t, err)
	assert.Equal(t, "selector-fix-promo", saved.Name)
	assert.Equal(t, []string{"gene-fix"}, saved.GeneIDs)
	assert.Equal(t, CapsuleStatusActive, saved.Status)
}

func TestPipelineIntegration_FullCycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	tracePath := filepath.Join(dir, "traces.jsonl")
	tc := NewTraceCollector(tracePath, 100)

	// 1. Collect traces
	for i := 0; i < 4; i++ {
		require.NoError(t, tc.Record(newFailedTrace("trace-"+string(rune('a'+i)), "scrape", "rate limit exceeded")))
	}
	require.NoError(t, tc.Flush())

	traces, err := LoadTraces(tracePath)
	require.NoError(t, err)
	require.Len(t, traces, 4)

	// 2. Mine signals
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	signals := miner.Mine(traces)
	require.NotEmpty(t, signals)

	// 3. Synthesise mutations
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)
	muts, err := engine.Evolve(ctx, signals)
	require.NoError(t, err)
	require.NotEmpty(t, muts)

	mut := muts[0]
	sig := signals[0]
	for _, s := range signals {
		if s.ID == mut.SignalID {
			sig = s
			break
		}
	}

	// 4. Evaluate -> Approve -> Promote
	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	cfg := DefaultPromotionConfig()
	cfg.RequireHITL = true
	pipeline := NewPromotionPipeline(cfg, store, harness)

	rec, err := pipeline.Submit(ctx, mut, sig)
	require.NoError(t, err)

	if rec.Status == PromotionEvaluated && rec.Evaluation.Pass {
		require.NoError(t, pipeline.Approve(mut.ID, "integration-test", "full cycle"))

		capsule := &Capsule{
			ID:          "cap-full-" + mut.ID,
			Name:        "full-cycle-capsule",
			Description: "Capsule from full trace->signal->mutate->promote cycle",
			GeneIDs:     []string{mut.GeneID},
			Environment: EnvFingerprint{OS: "darwin", Arch: "arm64"},
			Metrics:     CapsuleMetrics{SuccessRate: 0.9, SampleCount: 10},
			Status:      CapsuleStatusActive,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if mut.GeneID == "" {
			capsule.GeneIDs = []string{"synthetic"}
		}

		require.NoError(t, pipeline.Promote(ctx, mut.ID, capsule))

		saved, err := store.LoadCapsule(ctx, capsule.ID)
		require.NoError(t, err)
		assert.Equal(t, capsule.Name, saved.Name)
		assert.NotEmpty(t, saved.GeneIDs)
	}
}

func TestPipelineIntegration_CapsuleToGlobalKB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	capsule := &Capsule{
		ID:          "cap-kb-001",
		Name:        "kb-capsule",
		Description: "Capsule for Global KB integration test",
		GeneIDs:     []string{"gene-a", "gene-b"},
		Environment: EnvFingerprint{OS: "darwin", Arch: "arm64", GoVer: "1.24"},
		Metrics:     CapsuleMetrics{SuccessRate: 0.92, AvgLatencyMs: 150, SampleCount: 50},
		Status:      CapsuleStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	require.NoError(t, store.SaveCapsule(ctx, capsule))

	capsulePath := filepath.Join(dir, "capsules", "cap-kb-001.json")
	require.FileExists(t, capsulePath)

	data, err := os.ReadFile(capsulePath)
	require.NoError(t, err)

	var decoded Capsule
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "cap-kb-001", decoded.ID)
	assert.Equal(t, "kb-capsule", decoded.Name)
	assert.Equal(t, []string{"gene-a", "gene-b"}, decoded.GeneIDs)
	assert.Equal(t, "darwin", decoded.Environment.OS)
	assert.Equal(t, 0.92, decoded.Metrics.SuccessRate)

	// Verify valid JSON structure (committable)
	require.True(t, json.Valid(data), "expected valid JSON")
}

func TestPipelineIntegration_WorkflowEvolution(t *testing.T) {
	ctx := context.Background()

	g := NewWorkflowGraph("wf-evolve", "Evolution Test Workflow")
	require.NoError(t, g.AddNode(WorkflowNode{ID: "a", Name: "Node A", AgentType: "scraper", ModelTier: "fast"}))
	require.NoError(t, g.AddNode(WorkflowNode{ID: "b", Name: "Node B", AgentType: "processor", ModelTier: "balanced"}))
	require.NoError(t, g.AddNode(WorkflowNode{ID: "c", Name: "Node C", AgentType: "evaluator", ModelTier: "powerful"}))
	require.NoError(t, g.AddEdge(WorkflowEdge{From: "a", To: "b"}))
	require.NoError(t, g.AddEdge(WorkflowEdge{From: "b", To: "c"}))
	g.EntryNodeID = "a"

	require.NoError(t, g.Validate())

	// Record runs to produce metrics
	g.RecordRun(true, 100, 0.01)
	g.RecordRun(true, 120, 0.012)
	g.RecordRun(false, 200, 0.02)

	assert.Equal(t, int64(3), g.Metrics.TotalRuns)
	assert.Equal(t, int64(2), g.Metrics.SuccessRuns)
	assert.InDelta(t, 140.0, g.Metrics.AvgLatencyMs, 1.0)
	assert.True(t, g.Metrics.AvgCost > 0)
	assert.NotNil(t, g.Metrics.LastRunAt)

	// Evaluate workflow
	evaluator := NewWorkflowEvaluator(DefaultWorkflowEvalConfig(), nil)
	result, err := evaluator.EvaluateFull(ctx, g)
	require.NoError(t, err)

	assert.NotNil(t, result)
	assert.Equal(t, "wf-evolve", result.GraphID)
	assert.Greater(t, result.OverallScore, 0.0)
	assert.NotEmpty(t, result.CriterionScores)
	assert.False(t, result.EvaluatedAt.IsZero())

	// Simulate workflow mutation: add node
	require.NoError(t, g.AddNode(WorkflowNode{ID: "d", Name: "Node D", AgentType: "aggregator", ModelTier: "balanced"}))
	require.NoError(t, g.AddEdge(WorkflowEdge{From: "c", To: "d"}))
	assert.Len(t, g.Nodes, 4)
	assert.Len(t, g.Edges, 3)

	// Simulate remove node
	require.NoError(t, g.RemoveNode("d"))
	assert.Len(t, g.Nodes, 3)
}

func TestPipelineIntegration_RollbackOnFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := NewCapsuleStore(dir)
	require.NoError(t, err)

	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	cfg := DefaultPromotionConfig()
	cfg.RequireHITL = true
	pipeline := NewPromotionPipeline(cfg, store, harness)

	// Mutation that will fail evaluation: high risk, minimal reasoning, no gene
	mut := Mutation{
		ID:           "mut-fail",
		SignalID:     "sig-fail",
		GeneID:       "",
		RiskEstimate: RiskHigh,
		Reasoning:    "x",
		Strategy:     ModeBalanced,
		Status:       MutationStatusPending,
		CreatedAt:    time.Now().UTC(),
	}
	sig := Signal{
		ID:          "sig-fail",
		Type:        SignalRepeatedFailure,
		Severity:    SeverityCritical,
		Description: "critical failure pattern",
		DetectedAt:  time.Now().UTC(),
	}

	rec, err := pipeline.Submit(ctx, mut, sig)
	require.NoError(t, err)

	assert.Equal(t, PromotionRejected, rec.Status, "failing mutation should be rejected")
	assert.Contains(t, rec.Reason, "below threshold")

	// No capsule should be persisted
	capsulesDir := filepath.Join(dir, "capsules")
	entries, err := os.ReadDir(capsulesDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no capsule should be saved when evaluation rejects")
}

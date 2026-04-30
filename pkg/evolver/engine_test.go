package evolver

import (
	"context"
	"testing"
	"time"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestEvolutionEngine_EvolveNoLLM(t *testing.T) {
	cfg := DefaultEngineConfig()
	engine := NewEvolutionEngine(cfg, nil)

	signals := []Signal{
		{
			ID:                "sig-001",
			Type:              SignalRepeatedFailure,
			Severity:          SeverityWarning,
			Description:       "timeout repeated 3 times",
			SuggestedMutation: "add retry logic",
			DetectedAt:        time.Now().UTC(),
		},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	if len(muts) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(muts))
	}
	if muts[0].SignalID != "sig-001" {
		t.Errorf("mutation signal ID mismatch")
	}
	if muts[0].Reasoning != "add retry logic" {
		t.Errorf("expected suggested mutation as reasoning, got: %s", muts[0].Reasoning)
	}
	if muts[0].RiskEstimate != RiskMedium {
		t.Errorf("expected medium risk for warning severity, got %s", muts[0].RiskEstimate)
	}
}

func TestEvolutionEngine_EvolveWithLLM(t *testing.T) {
	llm := &mockLLM{response: `{"reasoning":"add exponential backoff","risk":"medium"}`}
	cfg := DefaultEngineConfig()
	engine := NewEvolutionEngine(cfg, llm)

	signals := []Signal{
		{
			ID:       "sig-002",
			Type:     SignalHighLatency,
			Severity: SeverityInfo,
		},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	if len(muts) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(muts))
	}
	if muts[0].Reasoning == "" {
		t.Error("expected LLM-generated reasoning")
	}
}

func TestEvolutionEngine_GeneMatching(t *testing.T) {
	cfg := DefaultEngineConfig()
	engine := NewEvolutionEngine(cfg, nil)

	gene := Gene{
		ID:       "gene-retry",
		Name:     "retry-handler",
		Category: GeneCategoryResilience,
		Tags:     []string{"repeated_failure"},
	}
	if err := engine.RegisterGene(gene); err != nil {
		t.Fatalf("RegisterGene: %v", err)
	}

	signals := []Signal{
		{
			ID:       "sig-003",
			Type:     SignalRepeatedFailure,
			Severity: SeverityCritical,
		},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	if len(muts) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(muts))
	}
	if muts[0].GeneID != "gene-retry" {
		t.Errorf("expected gene match, got gene_id=%s", muts[0].GeneID)
	}
}

func TestEvolutionEngine_RepairOnlyMode(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.Mode = ModeRepairOnly
	engine := NewEvolutionEngine(cfg, nil)

	signals := []Signal{
		{ID: "s1", Type: SignalHighLatency, Severity: SeverityInfo, SuggestedMutation: "x"},
		{ID: "s2", Type: SignalRepeatedFailure, Severity: SeverityCritical, SuggestedMutation: "y"},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	if len(muts) != 1 {
		t.Fatalf("repair-only should process only critical, got %d mutations", len(muts))
	}
	if muts[0].SignalID != "s2" {
		t.Errorf("expected critical signal, got %s", muts[0].SignalID)
	}
}

func TestEvolutionEngine_MaxMutationsPerRun(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.MaxMutationsPerRun = 2
	engine := NewEvolutionEngine(cfg, nil)

	var signals []Signal
	for i := 0; i < 5; i++ {
		signals = append(signals, Signal{
			ID:                "s" + string(rune('0'+i)),
			Type:              SignalRepeatedFailure,
			Severity:          SeverityWarning,
			SuggestedMutation: "fix",
		})
	}

	muts, _ := engine.Evolve(context.Background(), signals)
	if len(muts) != 2 {
		t.Errorf("expected max 2 mutations, got %d", len(muts))
	}
}

func TestEvolutionEngine_EventsRecorded(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)
	signals := []Signal{
		{ID: "s1", Type: SignalCostSpike, Severity: SeverityWarning, SuggestedMutation: "fix"},
	}

	_, _ = engine.Evolve(context.Background(), signals)

	events := engine.Events()
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (signal_detected + mutation_proposed), got %d", len(events))
	}
	if events[0].Type != EventSignalDetected {
		t.Errorf("first event should be signal_detected, got %s", events[0].Type)
	}
}

func TestEvolutionEngine_RegisterInvalidGene(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)
	err := engine.RegisterGene(Gene{})
	if err == nil {
		t.Fatal("expected error for invalid gene")
	}
}

func TestEvolutionEngine_RegisterCapsule(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)
	err := engine.RegisterCapsule(Capsule{
		ID:      "cap-001",
		Name:    "test-capsule",
		GeneIDs: []string{"g1"},
	})
	if err != nil {
		t.Fatalf("RegisterCapsule: %v", err)
	}
}

func TestEvolutionEngine_Mutations(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	// Before evolve, mutations should be empty.
	if m := engine.Mutations(); len(m) != 0 {
		t.Fatalf("expected 0 mutations initially, got %d", len(m))
	}

	signals := []Signal{
		{ID: "s1", Type: SignalRepeatedFailure, Severity: SeverityWarning, SuggestedMutation: "retry"},
		{ID: "s2", Type: SignalCostSpike, Severity: SeverityCritical, SuggestedMutation: "cache"},
	}
	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}

	stored := engine.Mutations()
	if len(stored) != len(muts) {
		t.Fatalf("expected %d mutations stored, got %d", len(muts), len(stored))
	}
	// Verify it returns a copy, not a reference.
	stored[0].Reasoning = "MODIFIED"
	original := engine.Mutations()
	if original[0].Reasoning == "MODIFIED" {
		t.Error("Mutations() should return a copy, not a reference")
	}
}

func TestEvolutionEngine_Genes(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	if g := engine.Genes(); len(g) != 0 {
		t.Fatalf("expected 0 genes initially, got %d", len(g))
	}

	gene := Gene{
		ID:       "gene-test",
		Name:     "test-gene",
		Category: GeneCategoryTool,
		Tags:     []string{"test"},
	}
	if err := engine.RegisterGene(gene); err != nil {
		t.Fatalf("RegisterGene: %v", err)
	}

	genes := engine.Genes()
	if len(genes) != 1 {
		t.Fatalf("expected 1 gene, got %d", len(genes))
	}
	if genes["gene-test"] == nil {
		t.Fatal("expected gene-test in map")
	}
	if genes["gene-test"].Name != "test-gene" {
		t.Errorf("unexpected gene name: %s", genes["gene-test"].Name)
	}
}

func TestEvolutionEngine_ShouldProcess_HardenMode(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.Mode = ModeHarden
	engine := NewEvolutionEngine(cfg, nil)

	signals := []Signal{
		{ID: "s1", Type: SignalHighLatency, Severity: SeverityInfo, SuggestedMutation: "x"},
		{ID: "s2", Type: SignalRepeatedFailure, Severity: SeverityWarning, SuggestedMutation: "y"},
		{ID: "s3", Type: SignalCostSpike, Severity: SeverityCritical, SuggestedMutation: "z"},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	// Harden mode should skip info severity, process warning and critical.
	if len(muts) != 2 {
		t.Errorf("harden mode should process warning+critical only, got %d mutations", len(muts))
	}
}

func TestEvolutionEngine_ShouldProcess_InnovateMode(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.Mode = ModeInnovate
	engine := NewEvolutionEngine(cfg, nil)

	signals := []Signal{
		{ID: "s1", Type: SignalHighLatency, Severity: SeverityInfo, SuggestedMutation: "x"},
		{ID: "s2", Type: SignalRepeatedFailure, Severity: SeverityWarning, SuggestedMutation: "y"},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	// Innovate mode should process all signals.
	if len(muts) != 2 {
		t.Errorf("innovate mode should process all, got %d mutations", len(muts))
	}
}

func TestEvolutionEngine_RegisterCapsuleInvalid(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)
	err := engine.RegisterCapsule(Capsule{})
	if err == nil {
		t.Fatal("expected error for invalid capsule")
	}
}

func TestEvolutionEngine_ShouldProcess_RepairOnlyMode(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.Mode = ModeRepairOnly
	engine := NewEvolutionEngine(cfg, nil)

	signals := []Signal{
		{ID: "s1", Type: SignalHighLatency, Severity: SeverityInfo, SuggestedMutation: "x"},
		{ID: "s2", Type: SignalRepeatedFailure, Severity: SeverityWarning, SuggestedMutation: "y"},
		{ID: "s3", Type: SignalCostSpike, Severity: SeverityCritical, SuggestedMutation: "z"},
	}

	muts, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}
	// RepairOnly should only process critical signals.
	if len(muts) != 1 {
		t.Errorf("repair-only mode should process only critical, got %d mutations", len(muts))
	}
}

func TestEvolutionEngine_Events(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	if events := engine.Events(); len(events) != 0 {
		t.Fatalf("expected 0 events initially, got %d", len(events))
	}

	signals := []Signal{
		{ID: "s1", Type: SignalRepeatedFailure, Severity: SeverityWarning, SuggestedMutation: "retry"},
	}
	_, err := engine.Evolve(context.Background(), signals)
	if err != nil {
		t.Fatalf("Evolve: %v", err)
	}

	events := engine.Events()
	if len(events) == 0 {
		t.Fatal("expected at least 1 event after Evolve")
	}

	// Verify events returns a copy.
	events[0].ActorID = "MODIFIED"
	original := engine.Events()
	if original[0].ActorID == "MODIFIED" {
		t.Error("Events() should return a copy")
	}
}

func TestEvolutionEngine_RegisterDuplicateGene(t *testing.T) {
	engine := NewEvolutionEngine(DefaultEngineConfig(), nil)

	gene := Gene{
		ID:       "g1",
		Name:     "gene-one",
		Category: GeneCategoryTool,
	}
	if err := engine.RegisterGene(gene); err != nil {
		t.Fatalf("first RegisterGene: %v", err)
	}

	gene2 := Gene{
		ID:       "g1",
		Name:     "gene-one-updated",
		Category: GeneCategoryPrompt,
	}
	if err := engine.RegisterGene(gene2); err != nil {
		t.Fatalf("second RegisterGene: %v", err)
	}

	genes := engine.Genes()
	if genes["g1"].Name != "gene-one-updated" {
		t.Errorf("expected updated name, got %s", genes["g1"].Name)
	}
}

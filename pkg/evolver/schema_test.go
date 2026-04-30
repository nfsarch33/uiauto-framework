package evolver

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGene_Validate(t *testing.T) {
	tests := []struct {
		name    string
		gene    Gene
		wantErr bool
	}{
		{
			name: "valid gene",
			gene: Gene{
				ID:       "gene-001",
				Name:     "retry-on-timeout",
				Category: GeneCategoryResilience,
			},
		},
		{
			name:    "missing id",
			gene:    Gene{Name: "x", Category: GeneCategoryTool},
			wantErr: true,
		},
		{
			name:    "missing name",
			gene:    Gene{ID: "g1", Category: GeneCategoryTool},
			wantErr: true,
		},
		{
			name:    "missing category",
			gene:    Gene{ID: "g1", Name: "x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gene.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCapsule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		capsule Capsule
		wantErr bool
	}{
		{
			name: "valid capsule",
			capsule: Capsule{
				ID:      "cap-001",
				Name:    "login-flow-v2",
				GeneIDs: []string{"gene-001"},
			},
		},
		{
			name:    "missing id",
			capsule: Capsule{Name: "x", GeneIDs: []string{"g1"}},
			wantErr: true,
		},
		{
			name:    "no gene ids",
			capsule: Capsule{ID: "c1", Name: "x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.capsule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMutation_Validate(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
		wantErr  bool
	}{
		{
			name: "valid mutation",
			mutation: Mutation{
				ID:        "mut-001",
				SignalID:  "sig-001",
				Reasoning: "retry reduces timeout failures by 60%",
			},
		},
		{
			name:     "missing signal id",
			mutation: Mutation{ID: "m1", Reasoning: "x"},
			wantErr:  true,
		},
		{
			name:     "missing reasoning",
			mutation: Mutation{ID: "m1", SignalID: "s1"},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mutation.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvolutionEvent_Validate(t *testing.T) {
	valid := EvolutionEvent{
		ID:      "evt-001",
		Type:    EventSignalDetected,
		ActorID: "agent-1",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	noID := valid
	noID.ID = ""
	if err := noID.Validate(); err == nil {
		t.Fatal("expected error for missing id")
	}

	noType := valid
	noType.Type = ""
	if err := noType.Validate(); err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestGene_JSONRoundTrip(t *testing.T) {
	g := Gene{
		ID:          "gene-rt-001",
		Name:        "roundtrip-test",
		Description: "tests JSON marshal/unmarshal",
		Category:    GeneCategoryWorkflow,
		Tags:        []string{"test", "ci"},
		Payload:     json.RawMessage(`{"command":"echo ok"}`),
		Validation:  []ValidationStep{{Name: "check", Command: "echo ok", Timeout: "5s"}},
		BlastRadius: BlastRadius{Level: RiskLow, AffectedModules: []string{"evolver"}, Reversible: true},
		Origin:      "test",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
		Version:     1,
		Metadata:    map[string]string{"author": "test"},
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Gene
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ID != g.ID {
		t.Errorf("ID mismatch: got %s, want %s", decoded.ID, g.ID)
	}
	if decoded.Category != g.Category {
		t.Errorf("Category mismatch: got %s, want %s", decoded.Category, g.Category)
	}
	if len(decoded.Tags) != len(g.Tags) {
		t.Errorf("Tags length mismatch: got %d, want %d", len(decoded.Tags), len(g.Tags))
	}
	if decoded.BlastRadius.Level != g.BlastRadius.Level {
		t.Errorf("BlastRadius.Level mismatch: got %s, want %s", decoded.BlastRadius.Level, g.BlastRadius.Level)
	}
}

func TestCapsule_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	c := Capsule{
		ID:          "cap-rt-001",
		Name:        "roundtrip-capsule",
		Description: "tests capsule JSON",
		GeneIDs:     []string{"g1", "g2"},
		Environment: EnvFingerprint{OS: "darwin", Arch: "arm64", GoVer: "1.23"},
		Metrics:     CapsuleMetrics{SuccessRate: 0.95, AvgLatencyMs: 120, SampleCount: 50},
		Status:      CapsuleStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Capsule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Metrics.SuccessRate != c.Metrics.SuccessRate {
		t.Errorf("SuccessRate mismatch")
	}
	if decoded.Status != c.Status {
		t.Errorf("Status mismatch")
	}
}

func TestEvolutionEvent_JSONRoundTrip(t *testing.T) {
	e := EvolutionEvent{
		ID:        "evt-rt-001",
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Type:      EventMutationProposed,
		ActorID:   "engine-1",
		ParentID:  "evt-000",
		Payload:   json.RawMessage(`{"mutation_id":"mut-001"}`),
		Outcome:   EventOutcome{Success: true, Reason: "validated"},
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EvolutionEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != e.Type {
		t.Errorf("Type mismatch")
	}
	if decoded.ParentID != e.ParentID {
		t.Errorf("ParentID mismatch")
	}
}

func TestEvolutionMode_Constants(t *testing.T) {
	modes := []EvolutionMode{ModeRepairOnly, ModeHarden, ModeBalanced, ModeInnovate}
	for _, m := range modes {
		if m == "" {
			t.Errorf("empty mode constant")
		}
	}
}

func TestGeneCategory_Constants(t *testing.T) {
	cats := []GeneCategory{
		GeneCategoryPrompt, GeneCategoryTool, GeneCategoryWorkflow,
		GeneCategoryConfig, GeneCategorySelector, GeneCategoryPattern,
		GeneCategoryResilience,
	}
	seen := make(map[GeneCategory]bool)
	for _, c := range cats {
		if seen[c] {
			t.Errorf("duplicate category: %s", c)
		}
		seen[c] = true
	}
}

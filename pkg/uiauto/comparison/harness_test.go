package comparison

import (
	"math"
	"testing"
)

func TestMcNemar_KnownValues(t *testing.T) {
	tests := []struct {
		name      string
		b, c      int
		wantSig   bool
		wantPLess float64
	}{
		{
			name:      "strong treatment advantage",
			b:         5,
			c:         45,
			wantSig:   true,
			wantPLess: 0.001,
		},
		{
			name:      "no difference",
			b:         10,
			c:         10,
			wantSig:   false,
			wantPLess: 1.0,
		},
		{
			name:      "zero discordant",
			b:         0,
			c:         0,
			wantSig:   false,
			wantPLess: 1.1,
		},
		{
			name:      "marginal significance",
			b:         3,
			c:         15,
			wantSig:   true,
			wantPLess: 0.05,
		},
		{
			name:      "small sample with correction",
			b:         2,
			c:         8,
			wantSig:   false,
			wantPLess: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chi, p := McNemar(tt.b, tt.c)
			sig := p < 0.05

			if sig != tt.wantSig {
				t.Errorf("McNemar(%d,%d): significant=%v (p=%.6f, chi2=%.4f), want significant=%v",
					tt.b, tt.c, sig, p, chi, tt.wantSig)
			}
			if tt.wantPLess < 1.0 && p >= tt.wantPLess {
				t.Errorf("McNemar(%d,%d): p=%.6f, wanted p < %.4f", tt.b, tt.c, p, tt.wantPLess)
			}
		})
	}
}

func TestMcNemar_Symmetry(t *testing.T) {
	chi1, p1 := McNemar(10, 30)
	chi2, p2 := McNemar(30, 10)
	if math.Abs(chi1-chi2) > 0.001 || math.Abs(p1-p2) > 0.001 {
		t.Errorf("McNemar should be symmetric: (10,30)=(%.4f,%.6f) vs (30,10)=(%.4f,%.6f)",
			chi1, p1, chi2, p2)
	}
}

func TestHarness_RunPaired(t *testing.T) {
	h := NewHarness(
		Strategy{Name: "static-selectors", Description: "Hardcoded CSS selectors"},
		Strategy{Name: "self-healing", Description: "3-tier cascade with pattern learning"},
	)

	// Simulate 100 paired steps where treatment wins 30 discordant, control wins 5
	pairs := make([]PairedOutcome, 0, 100)
	for i := 0; i < 60; i++ {
		pairs = append(pairs, PairedOutcome{StepID: "both-pass", Control: OutcomePass, Treatment: OutcomePass})
	}
	for i := 0; i < 5; i++ {
		pairs = append(pairs, PairedOutcome{StepID: "both-fail", Control: OutcomeFail, Treatment: OutcomeFail})
	}
	for i := 0; i < 5; i++ {
		pairs = append(pairs, PairedOutcome{StepID: "ctrl-only", Control: OutcomePass, Treatment: OutcomeFail})
	}
	for i := 0; i < 30; i++ {
		pairs = append(pairs, PairedOutcome{StepID: "treat-only", Control: OutcomeFail, Treatment: OutcomePass})
	}

	result := h.RunPaired(pairs)

	if result.TotalPairs != 100 {
		t.Errorf("total pairs = %d, want 100", result.TotalPairs)
	}
	if result.BothPass != 60 {
		t.Errorf("both pass = %d, want 60", result.BothPass)
	}
	if result.BothFail != 5 {
		t.Errorf("both fail = %d, want 5", result.BothFail)
	}
	if result.OnlyCtrl != 5 {
		t.Errorf("only ctrl = %d, want 5", result.OnlyCtrl)
	}
	if result.OnlyTreat != 30 {
		t.Errorf("only treat = %d, want 30", result.OnlyTreat)
	}
	if !result.Significant {
		t.Errorf("expected significant result, got p=%.6f", result.PValue)
	}
	if result.TreatmentAdvantage() <= 0 {
		t.Errorf("expected positive treatment advantage, got %.4f", result.TreatmentAdvantage())
	}
	if result.DiscordantPairs() != 35 {
		t.Errorf("discordant pairs = %d, want 35", result.DiscordantPairs())
	}
}

func TestHarness_NoDiscordant(t *testing.T) {
	h := NewHarness(
		Strategy{Name: "A"},
		Strategy{Name: "B"},
	)

	pairs := []PairedOutcome{
		{StepID: "1", Control: OutcomePass, Treatment: OutcomePass},
		{StepID: "2", Control: OutcomePass, Treatment: OutcomePass},
	}

	result := h.RunPaired(pairs)
	if result.Significant {
		t.Error("should not be significant with no discordant pairs")
	}
	if result.PValue != 1.0 {
		t.Errorf("p-value = %f, want 1.0", result.PValue)
	}
}

func TestComparisonResult_Summary(t *testing.T) {
	r := ComparisonResult{Significant: true}
	if r.Summary() != "SIGNIFICANT" {
		t.Errorf("got %q, want SIGNIFICANT", r.Summary())
	}

	r.Significant = false
	if r.Summary() != "NOT significant" {
		t.Errorf("got %q, want NOT significant", r.Summary())
	}
}

func TestTreatmentAdvantage_Zero(t *testing.T) {
	r := ComparisonResult{OnlyCtrl: 0, OnlyTreat: 0}
	if r.TreatmentAdvantage() != 0 {
		t.Errorf("got %f, want 0", r.TreatmentAdvantage())
	}
}

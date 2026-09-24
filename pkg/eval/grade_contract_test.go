package eval

// Contract cases pinning two rules an earlier review found broken:
//   1. accuracy divides by GRADED decisions only (no-golden decisions are
//      not wrong), and the counts expose all/graded/correct separately;
//   2. choice (multiclass, 0..2) and noul (binary, 0..1) Brier are
//      reported separately, the multiclass term includes the golden class
//      even when the distribution omits it, and a nil distribution is
//      scored as one-hot on Got (0 correct, 2 wrong).

import (
	"math"
	"testing"
)

func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }

func recDecisions(ds []DecisionRecord) []RunRecord {
	return []RunRecord{{ScenarioID: "s", Executor: "laya-decide", Decisions: ds}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestAccuracyDividesByGradedOnly: an ungraded decision must not count as
// wrong — 1 correct of 2 graded is 0.5 even with a third ungraded one.
func TestAccuracyDividesByGradedOnly(t *testing.T) {
	m := Aggregate(recDecisions([]DecisionRecord{
		{Type: "choice", Got: "a", Want: "a", Dist: map[string]float64{"a": 0.9, "b": 0.1}, Confidence: 0.9},
		{Type: "choice", Got: "b", Confidence: 0.3}, // ungraded
		{Type: "noul", NoulValue: fp(0.8), WantBool: bp(false), Confidence: 0.8},
	}))
	if m.DecisionCount != 3 {
		t.Fatalf("DecisionCount = %d, want 3 (ALL decisions)", m.DecisionCount)
	}
	if m.GradedDecisions != 2 || m.CorrectDecisions != 1 {
		t.Fatalf("Graded/Correct = %d/%d, want 2/1", m.GradedDecisions, m.CorrectDecisions)
	}
	if !near(m.DecisionAccuracy, 0.5) {
		t.Fatalf("DecisionAccuracy = %v, want 0.5", m.DecisionAccuracy)
	}
	if !near(m.BrierChoice, 0.02) {
		t.Fatalf("BrierChoice = %v, want 0.02", m.BrierChoice)
	}
	if !near(m.BrierNoul, 0.64) {
		t.Fatalf("BrierNoul = %v, want 0.64", m.BrierNoul)
	}
	if !near(m.LowConfidenceRate, 1.0/3.0) {
		t.Fatalf("LowConfidenceRate = %v, want 1/3", m.LowConfidenceRate)
	}
}

// TestMulticlassBrierIncludesAnAbsentGoldenClass: golden "c" carries p=0.
func TestMulticlassBrierIncludesAnAbsentGoldenClass(t *testing.T) {
	m := Aggregate(recDecisions([]DecisionRecord{
		{Type: "choice", Got: "a", Want: "c", Dist: map[string]float64{"a": 0.7, "b": 0.3}},
	}))
	if !near(m.BrierChoice, 1.58) {
		t.Fatalf("BrierChoice = %v, want 1.58 (0.49 + 0.09 + 1.0)", m.BrierChoice)
	}
	if m.CorrectDecisions != 0 || m.GradedDecisions != 1 {
		t.Fatalf("Graded/Correct = %d/%d, want 1/0", m.GradedDecisions, m.CorrectDecisions)
	}
}

func TestMulticlassBrierWhenGoldenIsInTheDistribution(t *testing.T) {
	m := Aggregate(recDecisions([]DecisionRecord{
		{Type: "choice", Got: "a", Want: "a", Dist: map[string]float64{"a": 0.7, "b": 0.3}},
	}))
	if !near(m.BrierChoice, 0.18) {
		t.Fatalf("BrierChoice = %v, want 0.18", m.BrierChoice)
	}
}

// TestChoiceWithoutDistributionIsOneHotOnGot: right scores 0, wrong 2.
func TestChoiceWithoutDistributionIsOneHotOnGot(t *testing.T) {
	right := Aggregate(recDecisions([]DecisionRecord{{Type: "choice", Got: "a", Want: "a"}}))
	wrong := Aggregate(recDecisions([]DecisionRecord{{Type: "choice", Got: "a", Want: "b"}}))
	if !near(right.BrierChoice, 0) || !near(wrong.BrierChoice, 2) {
		t.Fatalf("one-hot Brier right/wrong = %v/%v, want 0/2", right.BrierChoice, wrong.BrierChoice)
	}
}

// TestNoulBrierIsAMeanOverGradedNouls: ungraded nouls are excluded.
func TestNoulBrierIsAMeanOverGradedNouls(t *testing.T) {
	m := Aggregate(recDecisions([]DecisionRecord{
		{Type: "noul", NoulValue: fp(0.8), WantBool: bp(true)},
		{Type: "noul", NoulValue: fp(0.8), WantBool: bp(false)},
		{Type: "noul", NoulValue: fp(0.1)}, // ungraded
	}))
	if !near(m.BrierNoul, 0.34) {
		t.Fatalf("BrierNoul = %v, want 0.34 (mean of 0.04 and 0.64)", m.BrierNoul)
	}
	if m.GradedDecisions != 2 || m.CorrectDecisions != 1 {
		t.Fatalf("Graded/Correct = %d/%d, want 2/1", m.GradedDecisions, m.CorrectDecisions)
	}
}

// TestNoulThresholdIsInclusive: exactly 0.5 predicts true.
func TestNoulThresholdIsInclusive(t *testing.T) {
	m := Aggregate(recDecisions([]DecisionRecord{{Type: "noul", NoulValue: fp(0.5), WantBool: bp(true)}}))
	if m.CorrectDecisions != 1 {
		t.Fatalf("noul of exactly 0.5 must predict true: Correct = %d", m.CorrectDecisions)
	}
}

func TestEmptyDecisionsAreZeroNotNaN(t *testing.T) {
	m := Aggregate(nil)
	for name, v := range map[string]float64{
		"accuracy": m.DecisionAccuracy, "brier_choice": m.BrierChoice, "brier_noul": m.BrierNoul,
	} {
		if v != 0 || math.IsNaN(v) {
			t.Fatalf("%s = %v on empty input, want 0", name, v)
		}
	}
}

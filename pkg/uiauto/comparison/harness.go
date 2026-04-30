package comparison

// Harness runs paired A/B comparisons between two strategies.
type Harness struct {
	Control   Strategy
	Treatment Strategy
}

// NewHarness creates a comparison harness.
func NewHarness(control, treatment Strategy) *Harness {
	return &Harness{
		Control:   control,
		Treatment: treatment,
	}
}

// RunPaired takes pre-collected paired outcomes and computes the comparison.
func (h *Harness) RunPaired(pairs []PairedOutcome) ComparisonResult {
	result := ComparisonResult{
		Control:    h.Control,
		Treatment:  h.Treatment,
		Pairs:      pairs,
		TotalPairs: len(pairs),
	}

	for _, p := range pairs {
		switch {
		case p.Control == OutcomePass && p.Treatment == OutcomePass:
			result.BothPass++
		case p.Control == OutcomeFail && p.Treatment == OutcomeFail:
			result.BothFail++
		case p.Control == OutcomePass && p.Treatment == OutcomeFail:
			result.OnlyCtrl++
		case p.Control == OutcomeFail && p.Treatment == OutcomePass:
			result.OnlyTreat++
		}
	}

	result.ChiSquared, result.PValue = McNemar(result.OnlyCtrl, result.OnlyTreat)
	result.Significant = result.PValue < 0.05

	return result
}

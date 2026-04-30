package bandits

// Features represents the context vector used by the contextual bandit
// to decide which tier should handle a given UI interaction.
type Features struct {
	PageComplexity    float64 // normalized [0,1]: DOM node count / max expected
	SelectorCount     int     // number of candidate selectors available
	PreviousFailures  int     // consecutive failures in the current healing attempt
	MutationIntensity float64 // [0,1] fraction of DOM elements mutated
	HasDataTestID     bool    // stable test-id selector is available
	IframeDepth       int     // nesting depth (0 = top-level)
}

// ContextKey produces a coarse bucketing key so the bandit maintains separate
// posteriors for qualitatively different page contexts.
func (f Features) ContextKey() string {
	complexity := "low"
	if f.PageComplexity > 0.6 {
		complexity = "high"
	} else if f.PageComplexity > 0.3 {
		complexity = "mid"
	}

	stability := "stable"
	if f.MutationIntensity > 0.2 || f.PreviousFailures > 1 {
		stability = "unstable"
	}
	if !f.HasDataTestID {
		stability = "unstable"
	}

	return complexity + ":" + stability
}

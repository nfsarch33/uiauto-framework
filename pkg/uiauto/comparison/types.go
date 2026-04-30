package comparison

// Outcome represents the result of a single test step.
type Outcome int

const (
	OutcomePass Outcome = iota
	OutcomeFail
)

// PairedOutcome captures one test step run under both strategies.
type PairedOutcome struct {
	StepID    string
	Control   Outcome
	Treatment Outcome
}

// Strategy identifies a test approach (e.g., "static-selectors", "self-healing-cascade").
type Strategy struct {
	Name        string
	Description string
}

// ComparisonResult holds the aggregate output of an A/B comparison run.
type ComparisonResult struct {
	Control   Strategy
	Treatment Strategy

	Pairs      []PairedOutcome
	TotalPairs int

	// Contingency table cells for McNemar's test
	BothPass  int // control pass, treatment pass
	BothFail  int // control fail, treatment fail
	OnlyCtrl  int // control pass, treatment fail (discordant)
	OnlyTreat int // control fail, treatment pass (discordant)

	PValue      float64
	ChiSquared  float64
	Significant bool // p < 0.05
}

// Summary returns a human-readable summary string.
func (r ComparisonResult) Summary() string {
	sig := "NOT significant"
	if r.Significant {
		sig = "SIGNIFICANT"
	}
	return sig
}

// DiscordantPairs returns the total number of discordant pairs (b + c).
func (r ComparisonResult) DiscordantPairs() int {
	return r.OnlyCtrl + r.OnlyTreat
}

// TreatmentAdvantage returns (OnlyTreat - OnlyCtrl) / total discordant.
// Positive means treatment is better.
func (r ComparisonResult) TreatmentAdvantage() float64 {
	d := r.DiscordantPairs()
	if d == 0 {
		return 0
	}
	return float64(r.OnlyTreat-r.OnlyCtrl) / float64(d)
}

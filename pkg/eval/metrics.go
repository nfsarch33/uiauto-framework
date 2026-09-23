package eval

import (
	"sort"
)

// SuiteMetrics aggregates RunRecords into the outcome metrics the rubric
// grades. Every field is a plain number so the rubric can reference any
// of them by name via Snapshot().
type SuiteMetrics struct {
	Scenarios          int                         `json:"scenarios"`
	Runs               int                         `json:"runs"`
	SuccessfulRuns     int                         `json:"successful_runs"`
	TaskSuccessRate    float64                     `json:"task_success_rate"`    // successful runs / runs
	StrictScenarioRate float64                     `json:"strict_scenario_rate"` // scenarios with ALL repeats ok
	MeanSteps          float64                     `json:"mean_steps"`
	MeanDurationSec    float64                     `json:"mean_duration_s"`
	P50DurationSec     float64                     `json:"p50_duration_s"`
	P95DurationSec     float64                     `json:"p95_duration_s"`
	FlakeRate          float64                     `json:"flake_rate"`          // scenarios with mixed outcomes / scenarios with >1 repeat... see Aggregate
	DecisionCount      int                         `json:"decision_count"`      // ALL decisions, graded or not
	GradedDecisions    int                         `json:"graded_decisions"`    // decisions carrying a golden
	CorrectDecisions   int                         `json:"correct_decisions"`   // graded decisions answered correctly
	DecisionAccuracy   float64                     `json:"decision_accuracy"`   // correct / graded (0 when none graded)
	BrierChoice        float64                     `json:"brier_choice"`        // mean multiclass Brier over graded choices (0..2)
	BrierNoul          float64                     `json:"brier_noul"`          // mean binary Brier over graded nouls (0..1)
	LowConfidenceRate  float64                     `json:"low_confidence_rate"` // decisions under the probation floor (0.5) / ALL decisions
	PerExecutor        map[string]*ExecutorMetrics `json:"per_executor"`
}

// ExecutorMetrics is the per-lane slice of the same outcome numbers.
type ExecutorMetrics struct {
	Runs            int     `json:"runs"`
	SuccessfulRuns  int     `json:"successful_runs"`
	SuccessRate     float64 `json:"success_rate"`
	MeanSteps       float64 `json:"mean_steps"`
	MeanDurationSec float64 `json:"mean_duration_s"`
}

// Aggregate computes SuiteMetrics from run records. Flake rate is the
// share of repeated scenarios (repeats > 1) whose outcomes were mixed —
// the number a deterministic harness drives to zero.
func Aggregate(records []RunRecord) SuiteMetrics {
	m := SuiteMetrics{PerExecutor: map[string]*ExecutorMetrics{}}
	if len(records) == 0 {
		return m
	}
	byScenario := map[string][]RunRecord{}
	var durations []float64
	var steps []int
	decisions := 0
	lowConfidence := 0
	for _, r := range records {
		m.Runs++
		m.SuccessfulRuns += b2i(r.OK)
		steps = append(steps, r.Steps)
		durations = append(durations, r.DurationSec)
		em, ok := m.PerExecutor[r.Executor]
		if !ok {
			em = &ExecutorMetrics{}
			m.PerExecutor[r.Executor] = em
		}
		em.Runs++
		em.SuccessfulRuns += b2i(r.OK)
		em.MeanSteps += float64(r.Steps)
		em.MeanDurationSec += r.DurationSec

		byScenario[r.ScenarioID] = append(byScenario[r.ScenarioID], r)
		for _, d := range r.Decisions {
			decisions++
			m.DecisionCount++
			if graded(d) {
				m.GradedDecisions++
				if d.isCorrect() {
					m.CorrectDecisions++
				}
			}
			if d.Confidence < 0.5 {
				lowConfidence++
			}
		}
	}
	m.Scenarios = len(byScenario)
	m.TaskSuccessRate = float64(m.SuccessfulRuns) / float64(m.Runs)

	strict, repeated, mixed := 0, 0, 0
	for _, rs := range byScenario {
		allOK := true
		anyOK := false
		for _, r := range rs {
			allOK = allOK && r.OK
			anyOK = anyOK || r.OK
		}
		if allOK {
			strict++
		}
		if len(rs) > 1 {
			repeated++
			if !allOK && anyOK {
				mixed++
			}
		}
	}
	m.StrictScenarioRate = float64(strict) / float64(m.Scenarios)
	if repeated > 0 {
		m.FlakeRate = float64(mixed) / float64(repeated)
	}

	mean := func(f func(int) float64) float64 {
		sum := 0.0
		for i := range records {
			sum += f(i)
		}
		return sum / float64(len(records))
	}
	m.MeanSteps = mean(func(i int) float64 { return float64(steps[i]) })
	m.MeanDurationSec = mean(func(i int) float64 { return durations[i] })
	m.P50DurationSec = percentile(durations, 50)
	m.P95DurationSec = percentile(durations, 95)

	for _, em := range m.PerExecutor {
		em.SuccessRate = float64(em.SuccessfulRuns) / float64(em.Runs)
		em.MeanSteps /= float64(em.Runs)
		em.MeanDurationSec /= float64(em.Runs)
	}

	if decisions > 0 {
		m.LowConfidenceRate = float64(lowConfidence) / float64(decisions)
	}
	if m.GradedDecisions > 0 {
		m.DecisionAccuracy = float64(m.CorrectDecisions) / float64(m.GradedDecisions)
	}
	m.BrierChoice, m.BrierNoul = MeanBriers(records)
	return m
}

// graded mirrors the reference contract: a choice is graded when it
// carries a golden label; a noul when it carries both a golden boolean
// and a value. Ungraded decisions are excluded from accuracy and both
// Brier means -- they are not wrong, they are unevidenced.
func graded(d DecisionRecord) bool {
	switch d.Type {
	case "choice":
		return d.Want != ""
	case "noul":
		return d.WantBool != nil && d.NoulValue != nil
	}
	return false
}

// isCorrect derives the verdict from the decision's own fields (the
// Correct field is evidence output; aggregation never trusts it):
// a choice is correct when Got == Want; a noul when (p >= 0.5) equals
// the golden boolean -- the threshold is inclusive.
func (d DecisionRecord) isCorrect() bool {
	if !graded(d) {
		return false
	}
	switch d.Type {
	case "choice":
		return d.Got == d.Want
	case "noul":
		return (*d.NoulValue >= 0.5) == *d.WantBool
	}
	return false
}

// Snapshot flattens metrics into the name->value map the rubric scores
// against. Executor-scoped metrics get "<executor>.<field>" keys
// (e.g. "browser-use.success_rate").
func (m SuiteMetrics) Snapshot() map[string]float64 {
	s := map[string]float64{
		"scenarios":            float64(m.Scenarios),
		"runs":                 float64(m.Runs),
		"task_success_rate":    m.TaskSuccessRate,
		"strict_scenario_rate": m.StrictScenarioRate,
		"mean_steps":           m.MeanSteps,
		"mean_duration_s":      m.MeanDurationSec,
		"p50_duration_s":       m.P50DurationSec,
		"p95_duration_s":       m.P95DurationSec,
		"flake_rate":           m.FlakeRate,
		"decision_count":       float64(m.DecisionCount),
		"graded_decisions":     float64(m.GradedDecisions),
		"correct_decisions":    float64(m.CorrectDecisions),
		"decision_accuracy":    m.DecisionAccuracy,
		"brier_choice":         m.BrierChoice,
		"brier_noul":           m.BrierNoul,
		"low_confidence_rate":  m.LowConfidenceRate,
	}
	for name, em := range m.PerExecutor {
		s[name+".runs"] = float64(em.Runs)
		s[name+".success_rate"] = em.SuccessRate
		s[name+".mean_steps"] = em.MeanSteps
		s[name+".mean_duration_s"] = em.MeanDurationSec
	}
	return s
}

// percentile returns the nearest-rank percentile of xs (0-100). Nil for
// empty input is the caller's problem; Aggregate only calls it non-empty.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	rank := p / 100 * float64(len(sorted)-1)
	idx := int(rank + 0.5) // nearest rank
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

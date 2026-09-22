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
	FlakeRate          float64                     `json:"flake_rate"`        // scenarios with mixed outcomes / scenarios with >1 repeat... see Aggregate
	DecisionAccuracy   float64                     `json:"decision_accuracy"` // golden decisions correct / total (0 when none)
	DecisionCount      int                         `json:"decision_count"`
	BrierScore         float64                     `json:"brier_score"`         // mean Brier over golden decisions (lower is better)
	LowConfidenceRate  float64                     `json:"low_confidence_rate"` // decisions under the probation floor (0.5) / decisions
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
			if d.Correct {
				m.DecisionCount++
			}
			if d.Confidence < 0.5 {
				m.LowConfidenceRate++
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
		m.DecisionAccuracy = float64(m.DecisionCount) / float64(decisions)
		m.LowConfidenceRate /= float64(decisions)
		m.BrierScore = MeanBrier(records)
	}
	return m
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
		"decision_accuracy":    m.DecisionAccuracy,
		"decision_count":       float64(m.DecisionCount),
		"brier_score":          m.BrierScore,
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

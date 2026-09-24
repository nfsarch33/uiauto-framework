package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Rubric is the weighted scoring sheet for a suite run. Criteria name a
// metric from SuiteMetrics.Snapshot(), an operator, and a threshold; a
// Required criterion doubles as a gate — the overall verdict is PASS only
// when every required criterion passes.
type Rubric struct {
	Name     string      `yaml:"name"`
	Criteria []Criterion `yaml:"criteria"`
}

// Criterion is one scored line of the rubric.
type Criterion struct {
	ID          string  `yaml:"id"`
	Description string  `yaml:"description"`
	Metric      string  `yaml:"metric"`
	Operator    string  `yaml:"operator"` // >= | <=
	Threshold   float64 `yaml:"threshold"`
	Weight      float64 `yaml:"weight"`
	Required    bool    `yaml:"required"`
}

// CriterionResult is the observed outcome for one criterion.
type CriterionResult struct {
	ID        string  `json:"id"`
	Metric    string  `json:"metric"`
	Observed  float64 `json:"observed"`
	Threshold float64 `json:"threshold"`
	Operator  string  `json:"operator"`
	Passed    bool    `json:"passed"`
	Weight    float64 `json:"weight"`
	Required  bool    `json:"required"`
}

// RubricResult is the scored rubric: per-criterion outcomes, a 0..1
// weighted score, and the gate verdict.
type RubricResult struct {
	Rubric   string            `json:"rubric"`
	Criteria []CriterionResult `json:"criteria"`
	Score    float64           `json:"score"`   // 0..1 weighted
	Verdict  string            `json:"verdict"` // PASS | FAIL
}

// LoadRubric reads and validates a rubric file.
func LoadRubric(path string) (*Rubric, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read rubric: %w", err)
	}
	var r Rubric
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("eval: parse rubric %s: %w", path, err)
	}
	if r.Name == "" {
		return nil, fmt.Errorf("eval: rubric %s has no name", path)
	}
	if len(r.Criteria) == 0 {
		return nil, fmt.Errorf("eval: rubric %s has no criteria", path)
	}
	total := 0.0
	for i := range r.Criteria {
		c := &r.Criteria[i]
		if c.ID == "" || c.Metric == "" {
			return nil, fmt.Errorf("eval: rubric criterion %d needs id and metric", i)
		}
		if c.Operator != ">=" && c.Operator != "<=" {
			return nil, fmt.Errorf("eval: criterion %q operator must be >= or <=", c.ID)
		}
		if c.Weight <= 0 {
			c.Weight = 1
		}
		total += c.Weight
	}
	if total == 0 {
		return nil, fmt.Errorf("eval: rubric %s has zero total weight", path)
	}
	return &r, nil
}

// Score evaluates the rubric against a metrics snapshot. An unknown
// metric name fails that criterion with the observed value NaN-safe at 0
// — a rubric naming a metric the harness does not produce must not pass.
func (r *Rubric) Score(snapshot map[string]float64) RubricResult {
	res := RubricResult{Rubric: r.Name, Verdict: "PASS"}
	weightSum, weightEarned := 0.0, 0.0
	for _, c := range r.Criteria {
		observed, ok := snapshot[c.Metric]
		passed := ok && compare(observed, c.Operator, c.Threshold)
		if !ok {
			observed = 0
		}
		weightSum += c.Weight
		if passed {
			weightEarned += c.Weight
		}
		if c.Required && !passed {
			res.Verdict = "FAIL"
		}
		res.Criteria = append(res.Criteria, CriterionResult{
			ID: c.ID, Metric: c.Metric, Observed: observed, Threshold: c.Threshold,
			Operator: c.Operator, Passed: passed, Weight: c.Weight, Required: c.Required,
		})
	}
	if weightSum > 0 {
		res.Score = weightEarned / weightSum
	}
	return res
}

func compare(observed float64, op string, threshold float64) bool {
	if op == "<=" {
		return observed <= threshold
	}
	return observed >= threshold
}

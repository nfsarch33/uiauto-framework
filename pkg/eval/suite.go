// Package eval is the outcome-evaluation harness: it runs a suite of
// scenarios through the framework's execution lanes, aggregates outcome
// metrics (task/step success, latency percentiles, flake rate, laya
// decision accuracy and Brier calibration), scores them against a
// weighted rubric, and emits a JSON + markdown report.
//
// The harness is deterministic-by-construction in CI: scenarios point at
// fixture pages and stubbed services (WireMock LLM + laya stubs in the
// integration stack), so metric math and rubric verdicts are reproducible.
// Live-model quality runs are opt-in via suite URLs pointing at real
// services; the weekly laya probation ledger consumes those reports.
package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Suite is a named collection of scenarios plus run defaults.
type Suite struct {
	Name      string     `yaml:"name"`
	Defaults  Defaults   `yaml:"defaults"`
	Scenarios []Scenario `yaml:"scenarios"`
}

// Defaults are overridable per scenario.
type Defaults struct {
	Repeats  int `yaml:"repeats"`
	TimeoutS int `yaml:"timeout_s"`
}

// Scenario is one evaluable unit. Exactly one executor lane is named;
// executor-specific fields are read by that lane's Executor.
type Scenario struct {
	ID          string            `yaml:"id"`
	Executor    string            `yaml:"executor"` // browser-use | laya-decide
	Repeats     int               `yaml:"repeats"`  // 0 = suite default
	TimeoutS    int               `yaml:"timeout_s"`
	Task        string            `yaml:"task"` // browser-use: NL task
	URL         string            `yaml:"url"`  // page under test
	MaxSteps    int               `yaml:"max_steps"`
	CDPURL      string            `yaml:"cdp_url"`      // optional CDP override
	QuestionSet string            `yaml:"question_set"` // laya-decide: schema name
	Golden      map[string]Golden `yaml:"golden"`       // laya-decide: expected answers
	// FinalContains: browser-use success additionally requires the final
	// result to contain this substring.
	FinalContains string `yaml:"final_contains"`
}

// Golden is the expected decision for one question. For choice: Label.
// For noul: True (expected yes/no).
type Golden struct {
	Label string `yaml:"label"`
	True  *bool  `yaml:"true"`
}

// LoadSuite reads and validates a suite file. Validation failures are
// errors, not warnings: a suite that cannot run cleanly must not produce
// a report that looks like it ran.
func LoadSuite(path string) (*Suite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read suite: %w", err)
	}
	var s Suite
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("eval: parse suite %s: %w", path, err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("eval: suite %s has no name", path)
	}
	if len(s.Scenarios) == 0 {
		return nil, fmt.Errorf("eval: suite %s has no scenarios", path)
	}
	if s.Defaults.Repeats < 1 {
		s.Defaults.Repeats = 1
	}
	if s.Defaults.TimeoutS < 1 {
		s.Defaults.TimeoutS = 120
	}
	seen := map[string]bool{}
	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		if sc.ID == "" {
			return nil, fmt.Errorf("eval: scenario %d has no id", i)
		}
		if seen[sc.ID] {
			return nil, fmt.Errorf("eval: duplicate scenario id %q", sc.ID)
		}
		seen[sc.ID] = true
		if sc.Executor == "" {
			return nil, fmt.Errorf("eval: scenario %q has no executor", sc.ID)
		}
		if sc.Repeats == 0 {
			sc.Repeats = s.Defaults.Repeats
		}
		if sc.TimeoutS == 0 {
			sc.TimeoutS = s.Defaults.TimeoutS
		}
		if sc.Repeats < 1 || sc.Repeats > 50 {
			return nil, fmt.Errorf("eval: scenario %q repeats=%d out of range [1,50]", sc.ID, sc.Repeats)
		}
	}
	return &s, nil
}

// EffectiveTimeout returns the scenario timeout in seconds.
func (sc Scenario) seconds() int { return sc.TimeoutS }

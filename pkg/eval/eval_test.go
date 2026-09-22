package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- suite loading ---

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "suite.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSuiteAppliesDefaultsAndValidates(t *testing.T) {
	p := writeTemp(t, `
name: smoke
defaults: {repeats: 2, timeout_s: 30}
scenarios:
  - id: a
    executor: browser-use
    task: do the thing
  - id: b
    executor: laya-decide
    question_set: checkout_recovery
    url: http://x.test
    repeats: 3
`)
	s, err := LoadSuite(p)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if s.Scenarios[0].Repeats != 2 || s.Scenarios[0].TimeoutS != 30 {
		t.Errorf("defaults not applied: %+v", s.Scenarios[0])
	}
	if s.Scenarios[1].Repeats != 3 {
		t.Errorf("explicit repeat overridden: %d", s.Scenarios[1].Repeats)
	}

	for name, body := range map[string]string{
		"dup ids":     "name: x\nscenarios:\n  - {id: a, executor: browser-use}\n  - {id: a, executor: browser-use}\n",
		"no executor": "name: x\nscenarios:\n  - {id: a}\n",
		"empty":       "name: x\nscenarios: []\n",
	} {
		if _, err := LoadSuite(writeTemp(t, body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// --- metrics aggregation ---

func rec(id, ex string, ok bool, steps int, dur float64) RunRecord {
	return RunRecord{ScenarioID: id, Executor: ex, OK: ok, Steps: steps, DurationSec: dur}
}

func TestAggregateRatesAndPercentiles(t *testing.T) {
	records := []RunRecord{
		rec("a", "browser-use", true, 3, 1.0),
		rec("a", "browser-use", true, 5, 2.0),
		rec("a", "browser-use", false, 7, 9.0), // flake: mixed
		rec("b", "laya-decide", true, 1, 4.0),
	}
	m := Aggregate(records)
	if m.Runs != 4 || m.SuccessfulRuns != 3 {
		t.Fatalf("runs=%d ok=%d", m.Runs, m.SuccessfulRuns)
	}
	if want := 0.75; m.TaskSuccessRate != want {
		t.Errorf("task_success_rate = %v, want %v", m.TaskSuccessRate, want)
	}
	if m.StrictScenarioRate != 0.5 { // only b strictly passes
		t.Errorf("strict_scenario_rate = %v, want 0.5", m.StrictScenarioRate)
	}
	if m.FlakeRate != 1 { // a is the only repeated scenario and it is mixed
		t.Errorf("flake_rate = %v, want 1", m.FlakeRate)
	}
	if m.MeanSteps != 4 {
		t.Errorf("mean_steps = %v", m.MeanSteps)
	}
	// durations sorted: 1,2,4,9 -> p50 nearest-rank 4-ish, p95 9
	if m.P50DurationSec != 2 && m.P50DurationSec != 4 {
		t.Errorf("p50 = %v (nearest-rank of {1,2,4,9})", m.P50DurationSec)
	}
	if m.P95DurationSec != 9 {
		t.Errorf("p95 = %v, want 9", m.P95DurationSec)
	}
	if m.PerExecutor["browser-use"].SuccessRate != 2.0/3.0 {
		t.Errorf("browser-use success = %v", m.PerExecutor["browser-use"].SuccessRate)
	}
	snap := m.Snapshot()
	if snap["browser-use.success_rate"] != 2.0/3.0 {
		t.Errorf("snapshot executor metric = %v", snap["browser-use.success_rate"])
	}
}

// --- calibration ---

func TestMeanBrierKnownValues(t *testing.T) {
	no := 0.1
	t9 := 0.9
	records := []RunRecord{{
		Decisions: []DecisionRecord{
			// full dist: golden a, p(a)=0.8,p(b)=0.2 -> (0.2)^2+(0.2)^2 = 0.08
			{Type: "choice", Got: "a", Want: "a", Correct: true, Dist: map[string]float64{"a": 0.8, "b": 0.2}},
			// noul 0.9, golden true -> (0.9-1)^2 = 0.01
			{Type: "noul", NoulValue: &t9, WantBool: boolPtr(true), Correct: true},
			// no golden on purpose: skipped by the math
			{Type: "choice", Got: "z", Dist: map[string]float64{"z": 1}},
			{Type: "noul", NoulValue: &no},
		},
	}}
	if got := MeanBrier(records); got < 0.0449 || got > 0.0451 {
		t.Fatalf("MeanBrier = %v, want 0.045", got)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestDecisionAccuracyFromRecords(t *testing.T) {
	records := []RunRecord{{
		Decisions: []DecisionRecord{
			{Type: "choice", Correct: true, Confidence: 0.9},
			{Type: "choice", Correct: false, Confidence: 0.9},
			{Type: "choice", Correct: true, Confidence: 0.3}, // low confidence
		},
	}}
	m := Aggregate(records)
	if m.DecisionAccuracy != 2.0/3.0 {
		t.Errorf("accuracy = %v", m.DecisionAccuracy)
	}
	if m.LowConfidenceRate != 1.0/3.0 {
		t.Errorf("low_confidence_rate = %v", m.LowConfidenceRate)
	}
}

// --- rubric ---

func TestRubricScoringAndGates(t *testing.T) {
	body := `
name: default
criteria:
  - {id: success, metric: task_success_rate, operator: ">=", threshold: 0.99, weight: 3, required: true}
  - {id: fast, metric: p95_duration_s, operator: "<=", threshold: 10, weight: 1}
`
	p := filepath.Join(t.TempDir(), "rubric.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRubric(p)
	if err != nil {
		t.Fatalf("LoadRubric: %v", err)
	}

	res := r.Score(map[string]float64{"task_success_rate": 1.0, "p95_duration_s": 12})
	if res.Verdict != "PASS" {
		t.Errorf("verdict = %s, want PASS (non-required miss only)", res.Verdict)
	}
	if res.Score != 0.75 {
		t.Errorf("score = %v, want 0.75", res.Score)
	}

	res = r.Score(map[string]float64{"task_success_rate": 0.9, "p95_duration_s": 5})
	if res.Verdict != "FAIL" {
		t.Errorf("verdict = %s, want FAIL (required gate missed)", res.Verdict)
	}

	// A rubric naming an unknown metric must never pass it.
	res = r.Score(map[string]float64{"p95_duration_s": 5})
	for _, c := range res.Criteria {
		if c.Metric == "task_success_rate" && c.Passed {
			t.Error("unknown metric counted as passed")
		}
	}

	if _, err := LoadRubric(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing rubric file: expected error")
	}
	if _, err := LoadRubric(writeRubric(t, "name: x\ncriteria:\n  - {id: a, metric: m, operator: ==, threshold: 1}\n")); err == nil {
		t.Error("bad operator: expected error")
	}
}

func writeRubric(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "r.yaml")
	_ = os.WriteFile(p, []byte(body), 0o644)
	return p
}

// --- runner ---

type fakeExec struct {
	name string
	ok   bool
}

func (f fakeExec) Name() string { return f.name }
func (f fakeExec) Run(_ context.Context, sc Scenario, attempt int) RunRecord {
	return RunRecord{ScenarioID: sc.ID, Attempt: attempt, OK: f.ok, Steps: 1, DurationSec: 0.1}
}

func TestRunnerRepeatsAndUnknownExecutor(t *testing.T) {
	suite := &Suite{Name: "t", Scenarios: []Scenario{
		{ID: "a", Executor: "fake", Repeats: 3},
		{ID: "b", Executor: "ghost", Repeats: 1},
	}}
	r := &Runner{Executors: map[string]Executor{"fake": fakeExec{name: "fake", ok: true}}}
	records, err := r.RunSuite(context.Background(), suite)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want unknown-executor error", err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3 before the unknown executor stops the run", len(records))
	}
}

// --- report ---

func TestWriteReportEmitsJSONAndMarkdown(t *testing.T) {
	dir := t.TempDir()
	metrics := Aggregate([]RunRecord{rec("a", "browser-use", true, 2, 1.5), rec("b", "browser-use", false, 2, 1.5)})
	res := (&Rubric{Name: "r"}).Score(map[string]float64{"task_success_rate": 1})
	jsonPath, mdPath, err := WriteReport(filepath.Join(dir, "run"), "smoke", metrics, &res, nil)
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	for _, p := range []string{jsonPath, mdPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	raw, _ := os.ReadFile(mdPath)
	if !strings.Contains(string(raw), "PASS") || !strings.Contains(string(raw), "task success rate") {
		t.Errorf("markdown missing verdict/metrics:\n%s", raw)
	}
}

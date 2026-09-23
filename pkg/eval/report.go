package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report is the full eval artefact: what ran, the aggregated metrics, the
// scored rubric, and the per-run records a reviewer re-traces failures
// from. It is the machine-readable input to the laya probation ledger.
type Report struct {
	Suite     string        `json:"suite"`
	Rubric    *RubricResult `json:"rubric,omitempty"`
	Metrics   SuiteMetrics  `json:"metrics"`
	Records   []RunRecord   `json:"records"`
	Generated time.Time     `json:"generated"`
}

// WriteReport writes report.json + report.md into dir (created if
// needed) and returns their paths.
func WriteReport(dir, suiteName string, metrics SuiteMetrics, rubric *RubricResult, records []RunRecord) (string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("eval: create report dir: %w", err)
	}
	rep := Report{
		Suite: suiteName, Rubric: rubric, Metrics: metrics,
		Records: records, Generated: time.Now().UTC(),
	}
	jsonPath := filepath.Join(dir, "report.json")
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("eval: marshal report: %w", err)
	}
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		return "", "", fmt.Errorf("eval: write report.json: %w", err)
	}

	mdPath := filepath.Join(dir, "report.md")
	if err := os.WriteFile(mdPath, []byte(renderMarkdown(rep)), 0o644); err != nil {
		return "", "", fmt.Errorf("eval: write report.md: %w", err)
	}
	return jsonPath, mdPath, nil
}

func renderMarkdown(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Eval report — %s\n\nGenerated %s\n\n", rep.Suite, rep.Generated.Format(time.RFC3339))

	if rep.Rubric != nil {
		fmt.Fprintf(&b, "## Rubric verdict: **%s** (score %.3f)\n\n", rep.Rubric.Verdict, rep.Rubric.Score)
		b.WriteString("| criterion | metric | observed | threshold | passed | weight | required |\n|---|---|---|---|---|---|---|\n")
		for _, c := range rep.Rubric.Criteria {
			fmt.Fprintf(&b, "| %s | `%s` | %.4f | %s %.4f | %s | %.1f | %v |\n",
				c.ID, c.Metric, c.Observed, c.Operator, c.Threshold, tick(c.Passed), c.Weight, c.Required)
		}
		b.WriteString("\n")
	}

	m := rep.Metrics
	fmt.Fprintf(&b, "## Metrics\n\n- scenarios: %d, runs: %d (%d ok)\n", m.Scenarios, m.Runs, m.SuccessfulRuns)
	fmt.Fprintf(&b, "- task success rate: %.4f, strict scenario rate: %.4f, flake rate: %.4f\n", m.TaskSuccessRate, m.StrictScenarioRate, m.FlakeRate)
	fmt.Fprintf(&b, "- steps: mean %.2f | duration: mean %.2fs p50 %.2fs p95 %.2fs\n", m.MeanSteps, m.MeanDurationSec, m.P50DurationSec, m.P95DurationSec)
	fmt.Fprintf(&b, "- decisions: %d total, %d graded, %d correct (accuracy %.4f) | Brier choice %.4f, noul %.4f | low-confidence rate %.4f\n",
		m.DecisionCount, m.GradedDecisions, m.CorrectDecisions, m.DecisionAccuracy, m.BrierChoice, m.BrierNoul, m.LowConfidenceRate)

	b.WriteString("\n### Per executor\n\n| executor | runs | success | mean steps | mean duration s |\n|---|---|---|---|---|\n")
	for name, em := range m.PerExecutor {
		fmt.Fprintf(&b, "| %s | %d | %.4f | %.2f | %.2f |\n", name, em.Runs, em.SuccessRate, em.MeanSteps, em.MeanDurationSec)
	}

	if len(rep.Records) > 0 {
		b.WriteString("\n### Runs\n\n| scenario | attempt | ok | steps | duration s | errors |\n|---|---|---|---|---|---|\n")
		for _, r := range rep.Records {
			errs := strings.Join(r.Errors, "; ")
			if len(errs) > 80 {
				errs = errs[:80] + "…"
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %d | %.2f | %s |\n", r.ScenarioID, r.Attempt, tick(r.OK), r.Steps, r.DurationSec, errs)
		}
	}
	return b.String()
}

func tick(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}

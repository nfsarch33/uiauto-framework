package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// The nightly regression lane: a health-gated suite verdict and a
// Prometheus textfile. The health gate exists so an executor outage is
// NEVER recorded as "0 failures" — a down lane is NOT_RUN, a run lane
// that failed tasks is a regression, and the two look different in
// every artifact this package emits.

// Verdict is the suite-level outcome class.
type Verdict string

const (
	// VerdictOK: the suite ran to completion (individual runs may have
	// failed — that is a regression, visible in the metrics).
	VerdictOK Verdict = "ok"
	// VerdictNotRun: a needed executor was unhealthy before the first
	// run. No task counts were recorded.
	VerdictNotRun Verdict = "not_run"
)

// Healthy is implemented by executors that can report readiness. The
// pre-flight gate probes only executors a suite actually names.
type Healthy interface {
	Healthy(ctx context.Context) error
}

// PreFlight probes every executor the suite names. The first failure
// returns VerdictNotRun with the reason; the suite must not start
// half-healthy.
func PreFlight(ctx context.Context, s *Suite, executors map[string]Executor) (Verdict, string) {
	seen := map[string]bool{}
	for _, sc := range s.Scenarios {
		if seen[sc.Executor] {
			continue
		}
		seen[sc.Executor] = true
		ex, ok := executors[sc.Executor]
		if !ok {
			return VerdictNotRun, fmt.Sprintf("executor %q is not registered", sc.Executor)
		}
		h, ok := ex.(Healthy)
		if !ok {
			continue
		}
		if err := h.Healthy(ctx); err != nil {
			return VerdictNotRun, fmt.Sprintf("executor %q failed its health probe: %v", sc.Executor, err)
		}
	}
	return VerdictOK, ""
}

// AllowlistViolations counts runs whose errors name an allowlist breach.
func AllowlistViolations(records []RunRecord) int {
	n := 0
	for _, r := range records {
		for _, e := range r.Errors {
			if strings.HasPrefix(e, "allowlist_violation:") {
				n++
				break
			}
		}
	}
	return n
}

// WriteNightlyTextfile emits the browser-nightly gauges under the
// historical prefix. See WritePrefixedTextfile.
func WriteNightlyTextfile(path string, verdict Verdict, m SuiteMetrics, records []RunRecord) error {
	return WritePrefixedTextfile(path, "uiauto_eval_nightly", verdict, m, records)
}

// WritePrefixedTextfile emits the suite gauges in Prometheus text
// format under a caller-chosen prefix, atomically (temp file + rename)
// so a collector never reads a half-written file. Two nightlies (the
// browser suite and the laya probation suite) share one textfile
// directory; without distinct prefixes the collector would see the
// same series name twice. The verdict gauge is 1 only for a suite that
// RAN — a dead executor can never look like a clean run. Decision
// accuracy and mean run latency ride along: they are the probation
// numbers the weekly review reads.
func WritePrefixedTextfile(path, prefix string, verdict Verdict, m SuiteMetrics, records []RunRecord) error {
	verdictValue := 0.0
	if verdict == VerdictOK {
		verdictValue = 1
	}
	var latencyMs float64
	if len(records) > 0 {
		var total float64
		for _, r := range records {
			total += r.DurationSec
		}
		latencyMs = total / float64(len(records)) * 1000
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP %s_ok The suite's verdict as a number: 1 = ran, 0 = not_run.\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_ok gauge\n", prefix)
	fmt.Fprintf(&b, "%s_ok %g\n\n", prefix, verdictValue)
	fmt.Fprintf(&b, "# HELP %s_runs Total task runs recorded.\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_runs gauge\n", prefix)
	fmt.Fprintf(&b, "%s_runs %d\n\n", prefix, m.Runs)
	fmt.Fprintf(&b, "# HELP %s_successful_runs Successful task runs.\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_successful_runs gauge\n", prefix)
	fmt.Fprintf(&b, "%s_successful_runs %d\n\n", prefix, m.SuccessfulRuns)
	fmt.Fprintf(&b, "# HELP %s_pass_all_repeats Share of scenarios whose every repeat succeeded (pass@N, 0..1).\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_pass_all_repeats gauge\n", prefix)
	fmt.Fprintf(&b, "%s_pass_all_repeats %g\n\n", prefix, m.StrictScenarioRate)
	fmt.Fprintf(&b, "# HELP %s_allowlist_violations Runs that navigated off the scenario allowlist.\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_allowlist_violations gauge\n", prefix)
	fmt.Fprintf(&b, "%s_allowlist_violations %d\n\n", prefix, AllowlistViolations(records))
	fmt.Fprintf(&b, "# HELP %s_decision_accuracy Graded decide answers that matched golden (0..1; 0 when none graded).\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_decision_accuracy gauge\n", prefix)
	fmt.Fprintf(&b, "%s_decision_accuracy %g\n\n", prefix, m.DecisionAccuracy)
	fmt.Fprintf(&b, "# HELP %s_run_latency_ms Mean wall-clock duration of a task run, milliseconds.\n", prefix)
	fmt.Fprintf(&b, "# TYPE %s_run_latency_ms gauge\n", prefix)
	fmt.Fprintf(&b, "%s_run_latency_ms %.1f\n", prefix, latencyMs)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

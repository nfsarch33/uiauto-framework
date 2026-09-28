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

// WriteNightlyTextfile emits the nightly gauges in Prometheus text
// format, atomically (temp file + rename) so a collector never reads a
// half-written file. The verdict gauge is 1 only for a suite that RAN —
// a dead executor can never look like a clean run.
func WriteNightlyTextfile(path string, verdict Verdict, m SuiteMetrics, records []RunRecord) error {
	verdictValue := 0.0
	if verdict == VerdictOK {
		verdictValue = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP uiauto_eval_nightly_ok The suite's verdict as a number: 1 = ran, 0 = not_run.\n")
	fmt.Fprintf(&b, "# TYPE uiauto_eval_nightly_ok gauge\n")
	fmt.Fprintf(&b, "uiauto_eval_nightly_ok %g\n\n", verdictValue)
	fmt.Fprintf(&b, "# HELP uiauto_eval_nightly_runs Total task runs recorded.\n")
	fmt.Fprintf(&b, "# TYPE uiauto_eval_nightly_runs gauge\n")
	fmt.Fprintf(&b, "uiauto_eval_nightly_runs %d\n\n", m.Runs)
	fmt.Fprintf(&b, "# HELP uiauto_eval_nightly_successful_runs Successful task runs.\n")
	fmt.Fprintf(&b, "# TYPE uiauto_eval_nightly_successful_runs gauge\n")
	fmt.Fprintf(&b, "uiauto_eval_nightly_successful_runs %d\n\n", m.SuccessfulRuns)
	fmt.Fprintf(&b, "# HELP uiauto_eval_nightly_pass_all_repeats Share of scenarios whose every repeat succeeded (pass@N, 0..1).\n")
	fmt.Fprintf(&b, "# TYPE uiauto_eval_nightly_pass_all_repeats gauge\n")
	fmt.Fprintf(&b, "uiauto_eval_nightly_pass_all_repeats %g\n\n", m.StrictScenarioRate)
	fmt.Fprintf(&b, "# HELP uiauto_eval_nightly_allowlist_violations Runs that navigated off the scenario allowlist.\n")
	fmt.Fprintf(&b, "# TYPE uiauto_eval_nightly_allowlist_violations gauge\n")
	fmt.Fprintf(&b, "uiauto_eval_nightly_allowlist_violations %d\n", AllowlistViolations(records))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

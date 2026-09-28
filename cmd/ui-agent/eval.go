package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/nfsarch33/uiauto-framework/pkg/eval"
)

// errNotRun is the NOT_RUN verdict as an exit path: exit code 3, so a
// dead executor is distinguishable from a failing suite (exit 1).
var errNotRun = errors.New("eval: suite not run (executor unhealthy)")

// evalCmd runs an eval suite through the execution lanes and scores the
// aggregated outcome metrics against a rubric. A FAIL verdict exits
// non-zero so CI can gate on it; the report carries the evidence.
func evalCmd() *cobra.Command {
	var suitePath, rubricPath, outDir, browserUseURL, layaURL, chromeDebug, omniParserURL string
	var fusionThreshold float64
	var repeatOverride int
	var promTextfile string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run an eval suite through the execution lanes, aggregate outcome metrics, and score them against a rubric",
		RunE: func(cmd *cobra.Command, _ []string) error {
			suite, err := eval.LoadSuite(suitePath)
			if err != nil {
				return err
			}
			var rubric *eval.Rubric
			if rubricPath != "" {
				rubric, err = eval.LoadRubric(rubricPath)
				if err != nil {
					return err
				}
			}
			if repeatOverride > 0 {
				for i := range suite.Scenarios {
					suite.Scenarios[i].Repeats = repeatOverride
				}
			}

			// A fusion scenario with no OmniParser endpoint would run the
			// plain lane under the fusion label — a misconfigured eval
			// must refuse to run rather than report mislabeled results.
			fusionInSuite := false
			for _, sc := range suite.Scenarios {
				if sc.Executor == eval.FusionExecutorName {
					fusionInSuite = true
					break
				}
			}
			if fusionInSuite && omniParserURL == "" {
				return fmt.Errorf("suite names executor browser-use-fusion but --omniparser is empty; point it at the OmniParser service or drop the fusion scenarios")
			}

			runner := &eval.Runner{
				Executors: map[string]eval.Executor{
					"browser-use":           &eval.BrowserUseExecutor{ServiceURL: browserUseURL},
					eval.FusionExecutorName: eval.NewFusionExecutor(browserUseURL, omniParserURL, chromeDebug, fusionThreshold),
					"laya-decide":           &eval.LayaDecideExecutor{ServiceURL: layaURL, ChromeDebug: chromeDebug},
				},
				OnRecord: func(r eval.RunRecord) {
					fmt.Fprintf(cmd.ErrOrStderr(), "  [%s] %s attempt %d ok=%v steps=%d %.2fs\n",
						r.Executor, r.ScenarioID, r.Attempt, r.OK, r.Steps, r.DurationSec)
				},
			}
			verdict, reason := eval.PreFlight(cmd.Context(), suite, runner.Executors)
			if verdict != eval.VerdictOK {
				// NOT_RUN, never "0 failures": no records exist, the
				// textfile says so, and the exit code is distinct.
				fmt.Fprintf(cmd.ErrOrStderr(), "verdict: %s (%s)\n", verdict, reason)
				if promTextfile != "" {
					if err := eval.WriteNightlyTextfile(promTextfile, verdict, eval.SuiteMetrics{}, nil); err != nil {
						return err
					}
				}
				return errNotRun
			}
			records, err := runner.RunSuite(cmd.Context(), suite)
			if err != nil {
				return err
			}
			metrics := eval.Aggregate(records)
			if promTextfile != "" {
				if err := eval.WriteNightlyTextfile(promTextfile, eval.VerdictOK, metrics, records); err != nil {
					return err
				}
			}
			var rubricResult *eval.RubricResult
			if rubric != nil {
				res := rubric.Score(metrics.Snapshot())
				rubricResult = &res
			}

			dir := filepath.Join(outDir, time.Now().UTC().Format("20060102-150405"))
			jsonPath, mdPath, err := eval.WriteReport(dir, suite.Name, metrics, rubricResult, records)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "suite: %s\n", suite.Name)
			fmt.Fprintf(out, "runs: %d (%d ok) | task success %.4f | flake %.4f | decisions %d graded %d correct %d (accuracy %.4f) | brier choice %.4f noul %.4f\n",
				metrics.Runs, metrics.SuccessfulRuns, metrics.TaskSuccessRate, metrics.FlakeRate,
				metrics.DecisionCount, metrics.GradedDecisions, metrics.CorrectDecisions,
				metrics.DecisionAccuracy, metrics.BrierChoice, metrics.BrierNoul)
			if rubricResult != nil {
				fmt.Fprintf(out, "rubric %s: verdict %s (score %.3f)\n", rubricResult.Rubric, rubricResult.Verdict, rubricResult.Score)
			}
			fmt.Fprintf(out, "report: %s\n            %s\n", jsonPath, mdPath)

			if rubricResult != nil && rubricResult.Verdict != "PASS" {
				return errors.New("eval: rubric verdict FAIL")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&suitePath, "suite", "", "path to the eval suite YAML (required)")
	cmd.Flags().StringVar(&rubricPath, "rubric", "", "path to the rubric YAML (optional; without it the run reports metrics only)")
	cmd.Flags().StringVar(&outDir, "out", "eval-out", "output directory (a timestamped subdirectory is created per run)")
	cmd.Flags().StringVar(&browserUseURL, "browser-use", "http://127.0.0.1:8091", "browser-use executor service base URL")
	cmd.Flags().StringVar(&layaURL, "laya", "http://127.0.0.1:8090", "laya decision service base URL")
	cmd.Flags().StringVar(&chromeDebug, "chrome-debug", "", "attach the shared Chrome CDP session at this debug URL for page capture")
	cmd.Flags().StringVar(&omniParserURL, "omniparser", "", "OmniParser service base URL (grounding for the browser-use-fusion executor)")
	cmd.Flags().Float64Var(&fusionThreshold, "fusion-threshold", 0, "fusion grounding confidence threshold (0 = package default 0.35)")
	cmd.Flags().IntVar(&repeatOverride, "repeat", 0, "override every scenario's repeat count (flake measurement)")
	cmd.Flags().StringVar(&promTextfile, "prom-textfile", "", "write nightly Prometheus textfile gauges to this path (verdict, runs, pass@N, allowlist violations)")
	_ = cmd.MarkFlagRequired("suite")
	return cmd
}

package eval

import (
	"context"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/fusion"
)

// fusionAdapter adapts the fusion executor to the eval harness's
// executor interface, so a suite names "browser-use-fusion" and the
// runner records grounding evidence alongside the standard outcome
// fields. The lane itself stays behind its flag: nothing changes until
// a scenario names the executor.
type fusionAdapter struct {
	exec *fusion.Executor
}

func (f *fusionAdapter) Name() string { return FusionExecutorName }

func (f *fusionAdapter) Run(ctx context.Context, sc Scenario, attempt int) RunRecord {
	rec := f.exec.Run(ctx, fusion.Scenario{
		ID: sc.ID, Task: sc.Task, URL: sc.URL,
		MaxSteps: sc.MaxSteps, FinalContains: sc.FinalContains,
	}, attempt)
	return RunRecord{
		ScenarioID:  rec.ScenarioID,
		Executor:    f.Name(),
		Attempt:     rec.Attempt,
		OK:          rec.OK,
		Steps:       rec.Steps,
		DurationSec: rec.DurationSec,
		Errors:      rec.Errors,
		URLs:        rec.URLs,
		// Grounding evidence rides in the record; the report's runs table
		// shows grounding_reason via the evidence fields below.
		Evidence: rec.Evidence,
	}
}

// FusionExecutorName is the single name suites use to select the fusion
// executor (the CLI scan and the runner map key both read it).
const FusionExecutorName = "browser-use-fusion"

// FusionDefaultThreshold re-exports the package default so the CLI's
// flag help (and any future gates) read ONE number, not a copy.
const FusionDefaultThreshold = fusion.DefaultConfidenceThreshold

// NewFusionExecutor returns the fusion executor registered under
// FusionExecutorName for eval suites.
func NewFusionExecutor(browserUseURL, omniParserURL, chromeDebug string, threshold float64) Executor {
	return &fusionAdapter{exec: &fusion.Executor{
		BrowserUseURL: browserUseURL,
		OmniParserURL: omniParserURL,
		ChromeDebug:   chromeDebug,
		Threshold:     threshold,
	}}
}

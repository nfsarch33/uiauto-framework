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

func (f *fusionAdapter) Name() string { return f.exec.Name() }

func (f *fusionAdapter) Run(ctx context.Context, sc Scenario, attempt int) RunRecord {
	rec := f.exec.Run(ctx, fusion.Scenario{
		ID: sc.ID, Task: sc.Task, URL: sc.URL,
		MaxSteps: sc.MaxSteps, FinalContains: sc.FinalContains,
	}, attempt)
	out := RunRecord{
		ScenarioID:  rec.ScenarioID,
		Executor:    f.Name(),
		Attempt:     rec.Attempt,
		OK:          rec.OK,
		Steps:       rec.Steps,
		DurationSec: rec.DurationSec,
		Errors:      rec.Errors,
	}
	// Grounding evidence rides in the record; the report renders it via
	// the evidence the CLI prints per run.
	if len(rec.Evidence) > 0 {
		out.Decisions = nil // no typed decisions in this lane
	}
	out.Evidence = rec.Evidence
	return out
}

// NewFusionExecutor returns the fusion executor registered under
// "browser-use-fusion" for eval suites.
func NewFusionExecutor(browserUseURL, omniParserURL, chromeDebug string, threshold float64) Executor {
	return &fusionAdapter{exec: &fusion.Executor{
		BrowserUseURL: browserUseURL,
		OmniParserURL: omniParserURL,
		ChromeDebug:   chromeDebug,
		Threshold:     threshold,
	}}
}

package eval

import (
	"context"
	"fmt"
	"time"
)

// Executor is one lane able to run a scenario. Implementations live in
// executors.go (browser-use, laya-decide) and in tests (fakes); the
// runner itself is lane-agnostic.
type Executor interface {
	Name() string
	// Run executes the scenario once and returns its record. Errors are
	// carried in the record (a failing scenario is data, not a harness
	// crash); an error return is reserved for harness-level faults.
	Run(ctx context.Context, sc Scenario, attempt int) RunRecord
}

// Runner executes every scenario of a suite with its executor, repeating
// each scenario per its Repeats.
type Runner struct {
	Executors map[string]Executor
	// OnRecord, when set, is called after every record (live progress).
	OnRecord func(RunRecord)
}

// RunSuite runs all scenarios serially: deterministic ordering, no lane
// interference, and repeats measure flake rather than parallelism noise.
func (r *Runner) RunSuite(ctx context.Context, s *Suite) ([]RunRecord, error) {
	var records []RunRecord
	for _, sc := range s.Scenarios {
		ex, ok := r.Executors[sc.Executor]
		if !ok {
			return records, fmt.Errorf("eval: scenario %q names executor %q which is not registered", sc.ID, sc.Executor)
		}
		for attempt := 1; attempt <= sc.Repeats; attempt++ {
			runCtx, cancel := context.WithTimeout(ctx, time.Duration(sc.seconds())*time.Second)
			rec := ex.Run(runCtx, sc, attempt)
			cancel()
			rec.ScenarioID = sc.ID
			rec.Executor = ex.Name()
			rec.Attempt = attempt
			records = append(records, rec)
			if r.OnRecord != nil {
				r.OnRecord(rec)
			}
			if ctx.Err() != nil {
				return records, ctx.Err()
			}
		}
	}
	return records, nil
}

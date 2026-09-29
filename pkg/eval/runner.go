package eval

import (
	"context"
	"fmt"
	"net/url"
	"strings"
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

// applyHostAllowlist fails the record when the lane's page history left
// the scenario's host list. DETECTION AFTER NAVIGATION: the executor
// already visited the page; this converts an off-list visit into a
// failed run with a named host. Hostnames are normalised (url.Parse,
// lowercased, trailing dot stripped) and matched exactly; an empty or
// unparseable host is a violation; and when hosts are configured but
// the executor reported no URL history the run fails CLOSED — no
// history is not permission.
func applyHostAllowlist(rec *RunRecord, allowed []string) {
	allowedSet := make(map[string]bool, len(allowed))
	for _, h := range allowed {
		allowedSet[normaliseHost(h)] = true
	}
	if len(allowedSet) > 0 && len(rec.URLs) == 0 {
		rec.OK = false
		rec.Errors = append(rec.Errors,
			fmt.Sprintf("allowlist_violation: executor reported no page history (allowed: %v)", allowed))
		return
	}
	for _, u := range rec.URLs {
		parsed, err := url.Parse(u)
		host := ""
		if err == nil {
			host = normaliseHost(parsed.Hostname())
		}
		if host == "" {
			rec.OK = false
			rec.Errors = append(rec.Errors, fmt.Sprintf("allowlist_violation: empty or unparsable url %q", u))
			continue
		}
		if !allowedSet[host] {
			rec.OK = false
			rec.Errors = append(rec.Errors, fmt.Sprintf("allowlist_violation: visited %q (allowed: %v)", host, allowed))
		}
	}
}

func normaliseHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(h, "."))
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
			if len(sc.AllowedHosts) > 0 {
				applyHostAllowlist(&rec, sc.AllowedHosts)
			}
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

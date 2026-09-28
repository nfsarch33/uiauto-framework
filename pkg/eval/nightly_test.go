package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeExecutor returns a canned record; with unhealthy=true it also
// implements Healthy with a failing probe.
type fakeExecutor struct {
	name      string
	record    RunRecord
	unhealthy bool
}

func (f *fakeExecutor) Name() string { return f.name }
func (f *fakeExecutor) Run(_ context.Context, sc Scenario, attempt int) RunRecord {
	r := f.record
	r.ScenarioID = sc.ID
	r.Attempt = attempt
	return r
}
func (f *fakeExecutor) Healthy(_ context.Context) error {
	if f.unhealthy {
		return errors.New("connection refused")
	}
	return nil
}

func TestPreFlightNotRunOnUnhealthyExecutor(t *testing.T) {
	s := &Suite{Name: "nightly", Scenarios: []Scenario{{ID: "a", Executor: "browser-use"}}}
	ex := map[string]Executor{"browser-use": &fakeExecutor{name: "browser-use", unhealthy: true}}
	verdict, reason := PreFlight(context.Background(), s, ex)
	// Mutant this kills: the PreFlight gate deleted — the verdict is ok
	// and a down lane records task failures instead of NOT_RUN.
	if verdict != VerdictNotRun {
		t.Fatalf("verdict = %q, want not_run", verdict)
	}
	if !strings.Contains(reason, "health probe") {
		t.Errorf("reason = %q, want the probe failure named", reason)
	}
}

func TestPreFlightOKWhenHealthy(t *testing.T) {
	s := &Suite{Name: "nightly", Scenarios: []Scenario{{ID: "a", Executor: "browser-use"}}}
	ex := map[string]Executor{"browser-use": &fakeExecutor{name: "browser-use"}}
	if v, _ := PreFlight(context.Background(), s, ex); v != VerdictOK {
		t.Fatalf("verdict = %q, want ok", v)
	}
}

func TestPreFlightUnknownExecutorIsNotRun(t *testing.T) {
	s := &Suite{Name: "nightly", Scenarios: []Scenario{{ID: "a", Executor: "ghost"}}}
	v, reason := PreFlight(context.Background(), s, map[string]Executor{})
	if v != VerdictNotRun || !strings.Contains(reason, "not registered") {
		t.Fatalf("verdict=%q reason=%q, want not_run with the missing executor named", v, reason)
	}
}

func TestApplyHostAllowlistFailsOffListNavigation(t *testing.T) {
	rec := RunRecord{OK: true, URLs: []string{
		"http://fixtures:8018/products.html",
		"https://evil.example/steal",
	}}
	applyHostAllowlist(&rec, []string{"fixtures"})
	// Mutant this kills: the allowlist check deleted (or the off-list
	// branch skips setting OK=false) — an otherwise-successful walk off
	// the reservation reports success.
	if rec.OK {
		t.Fatal("off-list navigation must fail the run")
	}
	if len(rec.Errors) != 1 || !strings.HasPrefix(rec.Errors[0], "allowlist_violation:") {
		t.Fatalf("errors = %v, want one allowlist_violation entry", rec.Errors)
	}
}

func TestApplyHostAllowlistPassesOnListWalk(t *testing.T) {
	rec := RunRecord{OK: true, URLs: []string{"http://fixtures:8018/a.html", "http://fixtures:8018/b.html"}}
	applyHostAllowlist(&rec, []string{"fixtures"})
	if !rec.OK || len(rec.Errors) != 0 {
		t.Fatalf("on-list walk must stay ok: %+v", rec)
	}
}

func TestRunnerEnforcesScenarioAllowlist(t *testing.T) {
	s := &Suite{Name: "nightly", Defaults: Defaults{Repeats: 1, TimeoutS: 5}, Scenarios: []Scenario{{
		ID: "fenced", Executor: "fake", Repeats: 1, AllowedHosts: []string{"fixtures"},
	}}}
	ex := &fakeExecutor{
		name:   "fake",
		record: RunRecord{OK: true, URLs: []string{"https://elsewhere.example/x"}},
	}
	r := &Runner{Executors: map[string]Executor{"fake": ex}}
	records, err := r.RunSuite(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	// Mutant this kills: the runner-side applyHostAllowlist call deleted
	// — the executor's own OK verdict survives the off-list URL.
	if records[0].OK {
		t.Fatal("runner must fail a record that left the allowlist")
	}
}

func TestWriteNightlyTextfileNotRunNeverLooksClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nightly.prom")
	if err := WriteNightlyTextfile(path, VerdictNotRun, SuiteMetrics{}, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// Mutant this kills: the verdict gauge inverted (or hardcoded 1) —
	// a dead executor would read as a clean nightly run.
	if !strings.Contains(text, "hlxn_uiauto_nightly_ok 0") {
		t.Fatalf("textfile must carry nightly_ok 0 for not_run:\n%s", text)
	}
	if strings.Contains(text, "hlxn_uiauto_nightly_runs 1") {
		t.Fatalf("not_run must not record runs:\n%s", text)
	}
}

func TestWriteNightlyTextfileCountsViolationsAndPassAtN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nightly.prom")
	m := Aggregate([]RunRecord{
		{ScenarioID: "a", OK: true, DurationSec: 1},
		{ScenarioID: "a", OK: true, DurationSec: 2},
		{ScenarioID: "b", OK: false, Errors: []string{"allowlist_violation: visited \"evil\""}},
	})
	if err := WriteNightlyTextfile(path, VerdictOK, m, []RunRecord{
		{Errors: []string{"allowlist_violation: visited \"evil\""}},
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	for _, want := range []string{
		"hlxn_uiauto_nightly_ok 1",
		"hlxn_uiauto_nightly_runs 3",
		"hlxn_uiauto_nightly_successful_runs 2",
		"hlxn_uiauto_nightly_allowlist_violations 1",
	} {
		// Mutant this kills: any of the four gauges dropped or
		// miscounted in the emit.
		if !strings.Contains(text, want) {
			t.Errorf("textfile missing %q:\n%s", want, text)
		}
	}
}

func TestNightlySuiteFileLoads(t *testing.T) {
	s, err := LoadSuite(filepath.Join("..", "..", "eval", "suites", "nightly-regression.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Mutant this kills: the suite YAML malformed (a scenario lost its
	// final-state assertion or allowlist) — validation counts catch it.
	if len(s.Scenarios) != 10 {
		t.Fatalf("scenarios = %d, want 10", len(s.Scenarios))
	}
	if s.Defaults.Repeats != 3 {
		t.Fatalf("default repeats = %d, want 3 (pass@3 epochs)", s.Defaults.Repeats)
	}
	withoutAssertion, withoutAllowlist := 0, 0
	for _, sc := range s.Scenarios {
		if sc.Executor == "browser-use" && sc.FinalContains == "" {
			withoutAssertion++
		}
		if sc.Executor == "browser-use" && len(sc.AllowedHosts) == 0 {
			withoutAllowlist++
		}
	}
	if withoutAssertion != 0 || withoutAllowlist != 0 {
		t.Fatalf("browser-use scenarios missing assertions=%d allowlists=%d", withoutAssertion, withoutAllowlist)
	}
}

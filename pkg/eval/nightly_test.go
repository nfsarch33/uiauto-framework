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
	// Mutant this kills: the Healthy() call deleted (`_ = h`) — the
	// verdict is ok and a down lane records task failures instead of
	// NOT_RUN.
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
	applyHostAllowlist(&rec, []string{"fixtures:8018"})
	// Mutant this kills: the off-list branch no longer sets OK=false —
	// an otherwise-successful walk off the reservation reports success.
	if rec.OK {
		t.Fatal("off-list navigation must fail the run")
	}
	if len(rec.Errors) != 1 || !strings.HasPrefix(rec.Errors[0], "allowlist_violation:") {
		t.Fatalf("errors = %v, want one allowlist_violation entry", rec.Errors)
	}
	if !strings.Contains(rec.Errors[0], "evil.example") {
		t.Errorf("the violation must name the host: %v", rec.Errors)
	}
}

func TestApplyHostAllowlistNormalisesHosts(t *testing.T) {
	rec := RunRecord{OK: true, URLs: []string{
		"http://FIXTURES.example:8018/a.html",
		"http://fixtures.example.:8018/b.html",
	}}
	applyHostAllowlist(&rec, []string{"Fixtures.Example:8018"})
	// Mutant this kills: normaliseHost stops lowercasing or stripping
	// the trailing dot — case or dot variants become violations.
	if !rec.OK || len(rec.Errors) != 0 {
		t.Fatalf("normalised hosts must match the allowlist: ok=%v errors=%v", rec.OK, rec.Errors)
	}
}

func TestApplyHostAllowlistFailsClosedWithoutHistory(t *testing.T) {
	rec := RunRecord{OK: true}
	applyHostAllowlist(&rec, []string{"fixtures:8018"})
	// Mutant this kills: the no-history fail-closed branch deleted — an
	// executor that reports no page history passes an allowlist it has
	// no evidence for.
	if rec.OK {
		t.Fatal("allowlist set + no page history must fail closed")
	}
	if !strings.Contains(rec.Errors[0], "no page history") {
		t.Errorf("errors = %v, want the no-history reason", rec.Errors)
	}
	// Mutant this kills: the reason split collapsed back to one string —
	// triage cannot tell a transport-shaped death from a silent empty
	// history (the 20261002 nightly paged on the first kind).
	if !strings.Contains(rec.Errors[0], "with no run errors") {
		t.Errorf("errors = %v, want the no-run-errors shape named", rec.Errors)
	}
}

func TestApplyHostAllowlistNamesTransportShapedNoHistory(t *testing.T) {
	rec := RunRecord{OK: false, Errors: []string{
		`browseruse: run: Post "http://127.0.0.1:8091/run": context deadline exceeded`,
	}}
	applyHostAllowlist(&rec, []string{"example.com"})
	// Mutant this kills: the after-errors branch reports the silent
	// shape — the report blames the executor's death as a scope breach.
	last := rec.Errors[len(rec.Errors)-1]
	if !strings.Contains(last, "after run errors") {
		t.Fatalf("errors = %v, want the after-run-errors shape named", rec.Errors)
	}
}

func TestAllowlistNoHistorySplitsTransportFromOffScope(t *testing.T) {
	transport := RunRecord{Errors: []string{
		"browseruse: run: deadline exceeded",
		"allowlist_violation: executor reported no page history after run errors (allowed: [example.com])",
	}}
	silent := RunRecord{Errors: []string{
		"allowlist_violation: executor reported no page history with no run errors (allowed: [example.com])",
	}}
	offScope := RunRecord{Errors: []string{
		`allowlist_violation: visited "evil.example" (allowed: [example.com])`,
	}}
	clean := RunRecord{OK: true}
	records := []RunRecord{transport, silent, offScope, clean}
	// Mutant this kills: AllowlistNoHistory counts every violation — the
	// triage gauge can no longer tell an executor death from a scope
	// breach, which is the whole point of the split.
	if got := AllowlistViolations(records); got != 3 {
		t.Fatalf("AllowlistViolations = %d, want 3", got)
	}
	// Mutant this kills: the matcher loosens back to "no page history" —
	// the silent shape moves the transport gauge and triage reads it as
	// an executor death, the exact confusion this split exists to remove.
	if got := AllowlistNoHistory(records); got != 1 {
		t.Fatalf("AllowlistNoHistory = %d, want 1 (transport-shaped only; silent and off-scope must not count)", got)
	}
}

func TestApplyHostAllowlistFailsUnparsableURL(t *testing.T) {
	rec := RunRecord{OK: true, URLs: []string{"http://%zz/bad"}}
	applyHostAllowlist(&rec, []string{"fixtures:8018"})
	if rec.OK {
		t.Fatal("an unparsable URL must be a violation, not permission")
	}
}

func TestApplyHostAllowlistPassesOnListWalk(t *testing.T) {
	rec := RunRecord{OK: true, URLs: []string{"http://fixtures:8018/a.html", "http://fixtures:8018/b.html"}}
	applyHostAllowlist(&rec, []string{"fixtures:8018"})
	if !rec.OK || len(rec.Errors) != 0 {
		t.Fatalf("on-list walk must stay ok: %+v", rec)
	}
}

func TestRunnerEnforcesScenarioAllowlist(t *testing.T) {
	s := &Suite{Name: "nightly", Defaults: Defaults{Repeats: 1, TimeoutS: 5}, Scenarios: []Scenario{{
		ID: "fenced", Executor: "fake", Repeats: 1, AllowedHosts: []string{"fixtures:8018"},
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
	// Mutant this kills: the verdict gauge hardcoded to 1 — a dead
	// executor reads as a clean nightly run.
	if !strings.Contains(text, "uiauto_eval_nightly_ok 0") {
		t.Fatalf("textfile must carry nightly_ok 0 for not_run:\n%s", text)
	}
	if strings.Contains(text, "uiauto_eval_nightly_runs 1") {
		t.Fatalf("not_run must not record runs:\n%s", text)
	}
}

func TestWriteNightlyTextfileAtomicNoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nightly.prom")
	if err := WriteNightlyTextfile(path, VerdictOK, SuiteMetrics{}, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Mutant this kills: the rename dropped (write straight to path) —
	// a collector can observe a half-written file; the check also
	// catches a leftover .tmp.
	if len(entries) != 1 || entries[0].Name() != "nightly.prom" {
		t.Fatalf("exactly the final file must exist, got %d entries", len(entries))
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
		"uiauto_eval_nightly_ok 1",
		"uiauto_eval_nightly_runs 3",
		"uiauto_eval_nightly_successful_runs 2",
		"uiauto_eval_nightly_allowlist_violations 1",
		// off-scope visit, so the transport-shaped split gauge reads 0
		"uiauto_eval_nightly_allowlist_no_history 0",
		"Share of scenarios whose every repeat succeeded",
	} {
		// Mutant this kills: any of the gauges dropped or miscounted,
		// or the pass@N HELP still claims a count for a rate.
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
	// end-state assertion or allowlist) — validation counts catch it.
	if len(s.Scenarios) != 10 {
		t.Fatalf("scenarios = %d, want 10", len(s.Scenarios))
	}
	if s.Defaults.Repeats != 3 {
		t.Fatalf("default repeats = %d, want 3 (pass@3 epochs)", s.Defaults.Repeats)
	}
	seen := map[string]bool{}
	for _, sc := range s.Scenarios {
		if sc.Executor != "browser-use" {
			continue
		}
		if sc.FinalContains == "" {
			t.Errorf("scenario %q has no final-state assertion", sc.ID)
		}
		if len(sc.AllowedHosts) == 0 {
			t.Errorf("scenario %q has no host allowlist", sc.ID)
		}
		// Distinctness: same URL AND same marker is a duplicate task.
		key := sc.URL + "|" + sc.FinalContains
		if seen[key] {
			t.Errorf("duplicate task (same url+marker): %q", sc.ID)
		}
		seen[key] = true
	}
}

// A port-scoped allowlist entry binds the port: with
// allowed_hosts ["localhost:8018"], a visit to localhost on ANY other
// port is a violation. Mutant this kills: the matcher comparing
// Hostname() only — every loopback port (the executor's, a stray CDP
// hop) slips through the fixture allowlist.
func TestAllowlistPortScopedEntryBindsPort(t *testing.T) {
	rec := RunRecord{OK: true, URLs: []string{"http://localhost:8018/woo-admin/products.html", "http://localhost:9999/other"}}
	applyHostAllowlist(&rec, []string{"localhost:8018"})
	if rec.OK {
		t.Fatal("localhost:9999 must violate a localhost:8018 allowlist")
	}
	if len(rec.Errors) != 1 || !strings.Contains(rec.Errors[0], "localhost:9999") {
		t.Fatalf("errors = %v, want exactly the :9999 violation", rec.Errors)
	}

	rec = RunRecord{OK: true, URLs: []string{"http://localhost:8018/a.html"}}
	applyHostAllowlist(&rec, []string{"localhost:8018"})
	if !rec.OK {
		t.Fatalf("the scoped host itself must pass: %v", rec.Errors)
	}

	// Bare-hostname entries match ONLY unported URLs: a bare entry
	// never admits the same host on an explicit port. Mutant this
	// kills: the matcher ignoring the port for bare entries (the old
	// any-port hostname semantics).
	rec = RunRecord{OK: true, URLs: []string{"https://example.com/"}}
	applyHostAllowlist(&rec, []string{"example.com"})
	if !rec.OK {
		t.Fatalf("bare entry must match its unported URL: %v", rec.Errors)
	}

	rec = RunRecord{OK: true, URLs: []string{"https://example.com:443/x"}}
	applyHostAllowlist(&rec, []string{"example.com"})
	if rec.OK {
		t.Fatal("a bare entry must NOT admit the same host on an explicit port (example.com:443)")
	}
	if len(rec.Errors) != 1 || !strings.Contains(rec.Errors[0], "example.com:443") {
		t.Fatalf("errors = %v, want exactly the :443 violation", rec.Errors)
	}
}

// The laya probation textfile must carry its OWN prefix plus the two
// probation gauges (accuracy, mean latency). Mutant this kills: the
// writer ignoring the prefix (series would collide with the browser
// nightly's in the shared textfile directory).
func TestPrefixedTextfileOwnsItsPrefixAndProbationGauges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "laya.prom")
	m := SuiteMetrics{Runs: 2, SuccessfulRuns: 2, DecisionCount: 4, GradedDecisions: 4, CorrectDecisions: 4}
	m.DecisionAccuracy = 1 // as Aggregate would derive 4/4 graded-correct
	records := []RunRecord{{DurationSec: 0.5}, {DurationSec: 1.5}}
	if err := WritePrefixedTextfile(path, "uiauto_eval_laya", VerdictOK, m, records); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"uiauto_eval_laya_ok 1",
		"uiauto_eval_laya_decision_accuracy 1",
		"uiauto_eval_laya_run_latency_ms 1000",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("textfile missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "uiauto_eval_nightly") {
		t.Fatalf("prefixed textfile must not emit the browser-nightly prefix:\n%s", text)
	}
}

// The laya nightly suite itself: every scenario is a decide task on a
// local fixture page, and the healthy-page discriminator exists (a
// suite where every golden is an error state cannot catch a judge that
// cries error). Mutant this kills: the healthy scenario deleted.
func TestLayaNightlySuiteShape(t *testing.T) {
	s, err := LoadSuite(filepath.Join("..", "..", "eval", "suites", "laya-nightly.yaml"))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	executors := map[string]bool{}
	healthy := false
	for _, sc := range s.Scenarios {
		executors[sc.Executor] = true
		if !strings.HasPrefix(sc.URL, "http://localhost:8018/") {
			t.Fatalf("scenario %q must target the local fixture server, got %q", sc.ID, sc.URL)
		}
		if sc.Golden == nil {
			t.Fatalf("scenario %q has no golden answers", sc.ID)
		}
		if sc.QuestionSet == "" {
			t.Fatalf("scenario %q has no question_set", sc.ID)
		}
		if g, ok := sc.Golden["next_action"]; ok && g.Label == "no_error_continue" {
			healthy = true
		}
	}
	if len(executors) != 1 || !executors["laya-decide"] {
		t.Fatalf("laya-nightly must be laya-decide only, got %v", executors)
	}
	if !healthy {
		t.Fatal("suite needs a healthy-page scenario (golden no_error_continue): an all-error suite cannot catch a judge that calls every page an error")
	}
}

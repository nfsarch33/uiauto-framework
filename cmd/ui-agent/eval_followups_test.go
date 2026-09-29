package main

import (
	"strings"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/eval"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/fusion"
)

// A fusion run that fell back must say WHY on the progress line; a
// grounded run (and a plain lane run) must not grow a grounding suffix.
// Mutant this kills: the evidence check deleted or compared against the
// wrong constant — the fallback line loses (or a grounded line gains)
// the grounding= field.
func TestProgressLineReportsFallbackReason(t *testing.T) {
	fallback := eval.RunRecord{
		Executor: eval.FusionExecutorName, ScenarioID: "s", Attempt: 1,
		OK: true, Steps: 2, DurationSec: 3.5,
		Evidence: map[string]any{"grounding_reason": string(fusion.ReasonGroundingUnavail)},
	}
	got := progressLine(fallback)
	if !strings.Contains(got, "grounding=grounding_unavailable") {
		t.Fatalf("fallback line missing grounding reason: %q", got)
	}

	grounded := eval.RunRecord{
		Executor: eval.FusionExecutorName, ScenarioID: "s", Attempt: 1,
		OK: true, Steps: 2, DurationSec: 3.5,
		Evidence: map[string]any{"grounding_reason": string(fusion.Grounded)},
	}
	if got := progressLine(grounded); strings.Contains(got, "grounding=") {
		t.Fatalf("grounded line must not carry a grounding suffix: %q", got)
	}

	plain := eval.RunRecord{Executor: "browser-use", ScenarioID: "s", Attempt: 1, OK: true}
	if got := progressLine(plain); strings.Contains(got, "grounding=") {
		t.Fatalf("plain-lane line must not carry a grounding suffix: %q", got)
	}
}

// The refusal is conditional on the SUITE naming the fusion executor: a
// plain suite with an empty --omniparser is a normal run, not a
// misconfiguration. Mutant this kills: the fusionInSuite condition
// deleted — the plain suite refuses too.
func TestFusionRefusalPlainSuiteNeverRefuses(t *testing.T) {
	plain := &eval.Suite{Scenarios: []eval.Scenario{
		{ID: "a", Executor: "browser-use"},
		{ID: "b", Executor: "laya-decide"},
	}}
	if err := fusionRefusal(plain, ""); err != nil {
		t.Fatalf("plain suite with empty --omniparser must not refuse: %v", err)
	}

	mixed := &eval.Suite{Scenarios: []eval.Scenario{
		{ID: "a", Executor: "browser-use"},
		{ID: "f", Executor: eval.FusionExecutorName},
	}}
	if err := fusionRefusal(mixed, ""); err == nil {
		t.Fatal("fusion suite with empty --omniparser must refuse")
	}
	if err := fusionRefusal(mixed, "http://127.0.0.1:7860"); err != nil {
		t.Fatalf("fusion suite with an endpoint must not refuse: %v", err)
	}
}

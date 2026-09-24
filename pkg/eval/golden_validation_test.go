package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/laya"
)

// layaStub answers /predict with a canned body.
func layaStub(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

// A golden the question set cannot answer must fail the scenario BEFORE
// any browser is attached: an ungradable decision is not a pass, and a
// misspelled key silently ungrading a decision is the failure mode this
// guards against.
func TestLayaDecideExecutorRefusesBadGolden(t *testing.T) {
	captureRan := false
	ex := &LayaDecideExecutor{
		ServiceURL: "http://unused",
		Capture: func(_ context.Context, _ Scenario) (laya.State, error) {
			captureRan = true
			return laya.State{"visible_text": "x"}, nil
		},
	}
	cases := []struct {
		name   string
		golden map[string]Golden
	}{
		{"misspelled key", map[string]Golden{"next_actoin": {Label: "retry_payment"}}},
		{"choice golden without label", map[string]Golden{"next_action": {}}},
		{"choice golden with foreign label", map[string]Golden{"next_action": {Label: "nuke_the_db"}}},
		{"noul golden without true", map[string]Golden{"error_visible": {}}},
		{"golden on wrong-type question", map[string]Golden{"error_visible": {Label: "x"}}},
	}
	for _, tc := range cases {
		rec := ex.Run(context.Background(), Scenario{
			ID: "s", QuestionSet: "checkout_recovery", Golden: tc.golden,
		}, 1)
		if rec.OK || len(rec.Errors) == 0 {
			t.Errorf("%s: rec = %+v, want a failed record with errors", tc.name, rec)
		}
	}
	if captureRan {
		t.Fatal("capture ran for a bad golden; validation must fail before any browser work")
	}
}

// A valid golden still runs and grades normally.
func TestLayaDecideExecutorAcceptsValidGolden(t *testing.T) {
	svc := layaStub(`{"answers":{
		"next_action":{"type":"choice","choice":"retry_payment","probabilities":{"retry_payment":0.9,"escalate":0.1},"confidence":0.9},
		"error_visible":{"type":"noul","noul":0.9,"confidence":0.9}
	}}`)
	defer svc.Close()
	ex := &LayaDecideExecutor{
		ServiceURL: svc.URL,
		Capture: func(_ context.Context, _ Scenario) (laya.State, error) {
			return laya.State{"visible_text": "Payment failed"}, nil
		},
	}
	rec := ex.Run(context.Background(), Scenario{
		ID: "s", QuestionSet: "checkout_recovery",
		Golden: map[string]Golden{"next_action": {Label: "retry_payment"}},
	}, 1)
	if !rec.OK || len(rec.Errors) != 0 {
		t.Fatalf("valid golden rejected: %+v", rec)
	}
	if len(rec.Decisions) != 2 {
		t.Fatalf("decisions = %d, want both questions graded", len(rec.Decisions))
	}
}

// Snapshot omits brier_choice/brier_noul when that type had no graded
// decisions: a required calibration gate then fails on the unknown metric
// instead of passing vacuously against a zero mean.
func TestSnapshotOmitsBrierForUngradedTypes(t *testing.T) {
	onlyChoice := Aggregate([]RunRecord{{ScenarioID: "s", Executor: "laya-decide", Decisions: []DecisionRecord{
		{Type: "choice", Got: "a", Want: "a", Dist: map[string]float64{"a": 1}},
	}}})
	snap := onlyChoice.Snapshot()
	if _, ok := snap["brier_noul"]; ok {
		t.Error("brier_noul present with zero graded nouls; the gate would pass vacuously")
	}
	if _, ok := snap["brier_choice"]; !ok {
		t.Error("brier_choice missing despite a graded choice")
	}

	onlyNoul := Aggregate([]RunRecord{{ScenarioID: "s", Executor: "laya-decide", Decisions: []DecisionRecord{
		{Type: "noul", NoulValue: fp(0.9), WantBool: bp(true)},
	}}})
	snap = onlyNoul.Snapshot()
	if _, ok := snap["brier_choice"]; ok {
		t.Error("brier_choice present with zero graded choices")
	}
	if _, ok := snap["brier_noul"]; !ok {
		t.Error("brier_noul missing despite a graded noul")
	}
}

// An empty distribution map (what a report round-trip turns nil into)
// must score identically to a missing one: one-hot on the pick.
func TestEmptyDistScoresLikeNilDist(t *testing.T) {
	nilDist := Aggregate([]RunRecord{{Decisions: []DecisionRecord{
		{Type: "choice", Got: "a", Want: "b"},
	}}})
	emptyDist := Aggregate([]RunRecord{{Decisions: []DecisionRecord{
		{Type: "choice", Got: "a", Want: "b", Dist: map[string]float64{}},
	}}})
	if nilDist.BrierChoice != emptyDist.BrierChoice {
		t.Fatalf("nil-dist Brier %v != empty-dist Brier %v (live vs reloaded records must agree)", nilDist.BrierChoice, emptyDist.BrierChoice)
	}
	if nilDist.BrierChoice != 2 {
		t.Fatalf("wrong-decision one-hot Brier = %v, want 2", nilDist.BrierChoice)
	}
}

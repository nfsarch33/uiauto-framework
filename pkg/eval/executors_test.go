package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/laya"
)

func TestGradeAnswers(t *testing.T) {
	high, low := 0.9, 0.2
	answers := map[string]laya.Answer{
		"hit":        {Type: "choice", Choice: "retry_payment", Confidence: 0.9, Probabilities: map[string]float64{"retry_payment": 0.9, "escalate": 0.1}},
		"miss":       {Type: "choice", Choice: "escalate", Confidence: 0.3, Probabilities: map[string]float64{"escalate": 0.6, "retry_payment": 0.4}},
		"noul_yes":   {Type: "noul", Noul: &high, Confidence: 0.9},
		"noul_wrong": {Type: "noul", Noul: &low, Confidence: 0.2},
		"ungraded":   {Type: "choice", Choice: "x", Confidence: 0.5},
	}
	golden := map[string]Golden{
		"hit":        {Label: "retry_payment"},
		"miss":       {Label: "retry_payment"},
		"noul_yes":   {True: boolPtr(true)},
		"noul_wrong": {True: boolPtr(true)},
	}
	recs := gradeAnswers(answers, golden)
	byQ := map[string]DecisionRecord{}
	for _, d := range recs {
		byQ[d.Question] = d
	}
	if !byQ["hit"].Correct || byQ["hit"].Probability != 0.9 {
		t.Errorf("hit = %+v", byQ["hit"])
	}
	if byQ["miss"].Correct || byQ["miss"].Want != "retry_payment" {
		t.Errorf("miss = %+v", byQ["miss"])
	}
	if !byQ["noul_yes"].Correct || byQ["noul_wrong"].Correct {
		t.Errorf("noul grading wrong: yes=%+v wrong=%+v", byQ["noul_yes"], byQ["noul_wrong"])
	}
	if byQ["ungraded"].Want != "" || byQ["ungraded"].WantBool != nil || byQ["ungraded"].Correct {
		t.Errorf("ungraded decision must stay ungraded: %+v", byQ["ungraded"])
	}
}

func TestBrowserUseExecutorRun(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"final_result":"fixture done: page loaded","steps":2,"duration_s":0.5,"errors":[]}`))
	}))
	defer svc.Close()
	ex := &BrowserUseExecutor{ServiceURL: svc.URL}

	rec := ex.Run(context.Background(), Scenario{ID: "s1", Task: "t", FinalContains: "fixture done"}, 1)
	if !rec.OK || rec.Steps != 2 {
		t.Fatalf("record = %+v", rec)
	}

	// Outcome mismatch: lane ok but final result lacks the expected text.
	rec = ex.Run(context.Background(), Scenario{ID: "s2", Task: "t", FinalContains: "nothing"}, 1)
	if rec.OK || len(rec.Errors) == 0 {
		t.Fatalf("outcome mismatch must fail the scenario: %+v", rec)
	}
}

func TestBrowserUseExecutorSurfacesLaneError(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"errors":["no CDP endpoint"]}`))
	}))
	defer svc.Close()
	rec := (&BrowserUseExecutor{ServiceURL: svc.URL}).Run(context.Background(), Scenario{ID: "s3", Task: "t"}, 1)
	if rec.OK {
		t.Fatalf("lane failure must not be OK: %+v", rec)
	}
}

func TestLayaDecideExecutorRunWithInjectedCapture(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"ok":true,"laya":"stub"}`))
		case "/predict":
			_, _ = w.Write([]byte(`{"answers":{
				"next_action":{"type":"choice","choice":"retry_payment","probabilities":{"retry_payment":0.86,"escalate":0.14},"confidence":0.86},
				"error_visible":{"type":"noul","noul":0.84,"confidence":0.84}
			}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer svc.Close()

	ex := &LayaDecideExecutor{
		ServiceURL: svc.URL,
		Capture: func(_ context.Context, _ Scenario) (laya.State, error) {
			return laya.State{"title": "Checkout", "visible_text": "Payment failed - card declined"}, nil
		},
	}
	sc := Scenario{
		ID: "checkout", QuestionSet: "checkout_recovery",
		Golden: map[string]Golden{
			"next_action":   {Label: "retry_payment"},
			"error_visible": {True: boolPtr(true)},
		},
	}
	rec := ex.Run(context.Background(), sc, 1)
	if !rec.OK {
		t.Fatalf("record not ok: %+v", rec)
	}
	if len(rec.Decisions) != 2 {
		t.Fatalf("decisions = %d", len(rec.Decisions))
	}
	for _, d := range rec.Decisions {
		if !d.Correct {
			t.Errorf("decision %s graded wrong: %+v", d.Question, d)
		}
	}

	// Empty captured state is refused, never decided on.
	empty := &LayaDecideExecutor{ServiceURL: svc.URL, Capture: func(_ context.Context, _ Scenario) (laya.State, error) {
		return laya.State{}, nil
	}}
	if rec := empty.Run(context.Background(), sc, 1); rec.OK || len(rec.Errors) == 0 {
		t.Fatalf("empty state must be refused: %+v", rec)
	}

	// Unknown question set fails before any capture.
	bad := &LayaDecideExecutor{ServiceURL: svc.URL, Capture: func(_ context.Context, _ Scenario) (laya.State, error) {
		t.Fatal("capture must not run for a bad question set")
		return nil, nil
	}}
	if rec := bad.Run(context.Background(), Scenario{ID: "x", QuestionSet: "nope"}, 1); rec.OK {
		t.Fatalf("bad question set must fail: %+v", rec)
	}
}

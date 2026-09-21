package laya

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func checkoutQuestions() Questions {
	return Questions{
		"next_action": {
			"type":         "choice",
			"instructions": "Pick the next UI automation action.",
			"criteria": map[string]any{
				"retry_payment": "A retry control is visible and the failure is transient.",
				"escalate":      "No recovery path is visible.",
			},
		},
	}
}

// TestDecideParsesTypedAnswers pins the wire contract: POST /predict with
// state and questions, answers parsed with probabilities and noul pointer.
func TestDecideParsesTypedAnswers(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict" {
			t.Errorf("path = %q, want /predict", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %q", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"answers":{
			"next_action":{"type":"choice","choice":"retry_payment","probabilities":{"retry_payment":0.86,"escalate":0.14},"confidence":0.61},
			"error_visible":{"type":"noul","noul":0.84,"confidence":0.84}
		}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	answers, err := c.Decide(context.Background(), State{"visible_text": "payment failed"}, checkoutQuestions())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("answers = %d, want 2", len(answers))
	}
	na := answers["next_action"]
	if na.Choice != "retry_payment" || na.Type != "choice" {
		t.Fatalf("next_action = %+v", na)
	}
	if na.Probabilities["retry_payment"] < 0.85 || na.Probabilities["escalate"] > 0.15 {
		t.Errorf("probabilities off: %+v", na.Probabilities)
	}
	ev := answers["error_visible"]
	if ev.Noul == nil || *ev.Noul < 0.8 {
		t.Errorf("error_visible noul = %v", ev.Noul)
	}
	if gotBody["state"] == nil || gotBody["questions"] == nil {
		t.Errorf("request body missing state/questions: %v", gotBody)
	}
}

// TestDecideSurfacesHTTPError: a 500 from the service is an error naming
// the status, never a silent empty answer set.
func TestDecideSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.Decide(context.Background(), State{}, checkoutQuestions()); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want 500 in message", err)
	}
}

// TestDecideRejectsMalformedJSON: garbage bodies must error, not decode to
// zero-value answers that would read as "no decision".
func TestDecideRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{oops`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.Decide(context.Background(), State{}, checkoutQuestions()); err == nil {
		t.Fatal("err = nil, want decode error")
	}
}

// TestDecideHonorsContextDeadline: a wedged transport must not hang the
// caller past its own deadline. Exercised with a fake HTTPDoer rather
// than a stalled httptest server: the contract under test is that Decide
// passes its ctx into the request and returns when that ctx fires --
// stdlib transport cancellation is not our code and proved
// runner-dependent in CI.
func TestDecideHonorsContextDeadline(t *testing.T) {
	doer := &ctxBlockDoer{}
	c := &Client{BaseURL: "http://laya.test", HTTP: doer}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.Decide(ctx, State{}, checkoutQuestions()); err == nil {
		t.Fatal("err = nil, want deadline exceeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Decide took %s; deadline not honored", elapsed)
	}
}

// ctxBlockDoer blocks until the request's context is done, then returns
// the context error -- the minimal wedge.
type ctxBlockDoer struct{}

func (d *ctxBlockDoer) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

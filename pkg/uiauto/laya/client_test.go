package laya

import (
	"context"
	"encoding/json"
	"errors"
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

// answerServer answers /predict with a canned body.
func answerServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

// TestDecideRejectsOutOfSchemaChoice pins L2 of the adversarial review: a
// service answering with a choice that is not one of the question's own
// criteria must be a named error, never an actionable-looking decision the
// executor could act on.
func TestDecideRejectsOutOfSchemaChoice(t *testing.T) {
	srv := answerServer(`{"answers":{"next_action":{"type":"choice","choice":"format_the_disk","probabilities":{"format_the_disk":0.9},"confidence":0.02}}}`)
	defer srv.Close()
	_, err := New(srv.URL).Decide(context.Background(), State{"visible_text": "x"}, checkoutQuestions())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("err = %v, want ErrSchema", err)
	}
	if !strings.Contains(err.Error(), "format_the_disk") {
		t.Errorf("error does not name the out-of-schema choice: %v", err)
	}
}

// TestDecideRejectsWrongTypedAnswer pins L3: a choice question answered
// with a noul payload decodes cleanly into an empty choice — neither an
// error nor an actionable decision downstream. The type mismatch must be
// caught at the client boundary.
func TestDecideRejectsWrongTypedAnswer(t *testing.T) {
	srv := answerServer(`{"answers":{"next_action":{"type":"noul","noul":0.7,"confidence":0.7}}}`)
	defer srv.Close()
	_, err := New(srv.URL).Decide(context.Background(), State{"visible_text": "x"}, checkoutQuestions())
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("err = %v, want ErrSchema", err)
	}
	if !strings.Contains(err.Error(), "declares") {
		t.Errorf("error does not name the declared type: %v", err)
	}
}

// TestDecideRejectsUnpopulatedValues: the type-appropriate field must be
// populated — an empty choice or a noul without a value is not a verdict.
func TestDecideRejectsUnpopulatedValues(t *testing.T) {
	cases := map[string]string{
		"empty choice": `{"answers":{"next_action":{"type":"choice","choice":"","confidence":0.9}}}`,
		"nil noul":     `{"answers":{"next_action":{"type":"choice","choice":"retry_payment","confidence":0.9},"q2":{"type":"noul","confidence":0.5}}}`,
		"unknown type": `{"answers":{"next_action":{"type":"quantum","confidence":0.5}}}`,
	}
	for name, body := range cases {
		questions := Questions{
			"next_action": checkoutQuestions()["next_action"],
			"q2":          {"type": "noul", "instructions": "q"},
		}
		if name == "empty choice" || name == "unknown type" {
			questions = Questions{"next_action": questions["next_action"]}
		}
		srv := answerServer(body)
		if _, err := New(srv.URL).Decide(context.Background(), State{"visible_text": "x"}, questions); !errors.Is(err, ErrSchema) {
			t.Errorf("%s: err = %v, want ErrSchema", name, err)
		}
		srv.Close()
	}
}

// TestDecideRejectsMissingAnswer: silence about an asked question is an
// error — "no answer" must never read as "no action needed".
func TestDecideRejectsMissingAnswer(t *testing.T) {
	srv := answerServer(`{"answers":{"unrelated":{"type":"choice","choice":"x","confidence":0.9}}}`)
	defer srv.Close()
	_, err := New(srv.URL).Decide(context.Background(), State{"visible_text": "x"}, checkoutQuestions())
	if !errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v, want ErrSchema naming the missing question", err)
	}
}

// TestHealthProbesVersion: /healthz reports the built laya version so
// stamp drift is visible; transport and non-200 failures are errors.
func TestHealthProbesVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true,"laya":"0.3.5"}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	h, err := New(srv.URL).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !h.OK || h.Laya != "0.3.5" {
		t.Fatalf("health = %+v", h)
	}
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv500.Close()
	if _, err := New(srv500.URL).Health(context.Background()); err == nil {
		t.Fatal("err = nil, want 500 surfaced")
	}
}

// TestAnswerBelowFloor: the probation ledger separates unsure from
// wrong-and-confident via the recorded floor flag.
func TestAnswerBelowFloor(t *testing.T) {
	low, high := Answer{Confidence: 0.01}, Answer{Confidence: 0.99}
	if !low.BelowFloor(0.5) || high.BelowFloor(0.5) {
		t.Fatalf("floor check wrong: low=%v high=%v", low.BelowFloor(0.5), high.BelowFloor(0.5))
	}
}

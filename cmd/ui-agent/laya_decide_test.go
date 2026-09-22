package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/laya"
)

// TestLayaDecideLoopAgainstFixturePage drives the full decision loop
// against a local fixture page with a fake laya service: real browser
// navigation and DOM capture, typed decisions returned over HTTP. Skipped
// in CI/short like every other browser test in this repo.
func TestLayaDecideLoopAgainstFixturePage(t *testing.T) {
	if os.Getenv("CI") != "" || testing.Short() {
		t.Skip("skipping browser test in CI/short mode")
	}

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Checkout - Fixture</title></head>
		<body><h1>Checkout</h1><p>Payment failed - card declined</p><button>Retry payment</button></body></html>`))
	}))
	defer page.Close()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true,"laya":"0.3.5"}`))
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		state, _ := req["state"].(map[string]any)
		text, _ := state["visible_text"].(string)
		choice := "escalate"
		if contains(text, "Retry payment") {
			choice = "retry_payment"
		}
		_, _ = w.Write([]byte(`{"answers":{"next_action":{"type":"choice","choice":"` + choice + `","probabilities":{"retry_payment":0.9},"confidence":0.9},"error_visible":{"type":"noul","noul":0.9,"confidence":0.9}}}`))
	}))
	defer fake.Close()

	ev, err := layaDecideRun(context.Background(), page.URL, fake.URL, "checkout_recovery", "", 0.5)
	if err != nil {
		t.Fatalf("laya-decide loop: %v", err)
	}
	if ev["title"] != "Checkout - Fixture" {
		t.Errorf("title = %v", ev["title"])
	}
	// Round-trip the evidence so assertions see the wire shape a consumer
	// (runs export, reviewer) would see, not Go struct types.
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	decisions, _ := wire["decisions"].(map[string]any)
	na, _ := decisions["next_action"].(map[string]any)
	if na["choice"] != "retry_payment" {
		t.Errorf("fixture decision = %v, want retry_payment (loop must carry page text into the decision)", na["choice"])
	}
	if wire["laya_version"] != "0.3.5" {
		t.Errorf("laya_version = %v, want 0.3.5 from the healthz probe", wire["laya_version"])
	}
	below, _ := wire["confidence_below_floor"].(map[string]any)
	if len(below) != 2 {
		t.Errorf("confidence_below_floor = %v, want one flag per decision", below)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestDecideEvidenceWireShape pins the pure evidence builder without a
// browser: the confidence floor is recorded per decision (a 0.01-confidence
// answer must be distinguishable from a 0.99 one), the laya version is
// included only when the probe returned one.
func TestDecideEvidenceWireShape(t *testing.T) {
	noul := 0.84
	answers := map[string]laya.Answer{
		"next_action":   {Type: "choice", Choice: "escalate", Confidence: 0.01},
		"error_visible": {Type: "noul", Noul: &noul, Confidence: 0.99},
	}
	ev := decideEvidence("https://x.test", "http://laya.test", laya.State{"title": "T", "visible_text": "body text"}, answers, "0.3.5", 0.5)
	if ev["min_confidence"] != 0.5 {
		t.Errorf("min_confidence = %v", ev["min_confidence"])
	}
	if ev["laya_version"] != "0.3.5" {
		t.Errorf("laya_version = %v", ev["laya_version"])
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	below, _ := wire["confidence_below_floor"].(map[string]any)
	if below["next_action"] != true || below["error_visible"] != false {
		t.Errorf("confidence_below_floor = %v, want next_action=true error_visible=false", below)
	}

	ev = decideEvidence("https://x.test", "http://laya.test", laya.State{"visible_text": "body"}, answers, "", 0.5)
	if _, ok := ev["laya_version"]; ok {
		t.Error("laya_version present when the probe returned nothing")
	}
}

// TestLayaDecideRefusesEmptyState: a page whose visible text is empty even
// after the settle retry must not be decided on -- an empty state is an
// error, not a verdict, on the input side too.
func TestLayaDecideRefusesEmptyState(t *testing.T) {
	if os.Getenv("CI") != "" || testing.Short() {
		t.Skip("skipping browser test in CI/short mode")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Shell</title></head><body><script>var x = 1;</script></body></html>`))
	}))
	defer page.Close()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{"next_action":{"type":"choice","choice":"escalate","confidence":0.5},"error_visible":{"type":"noul","noul":0.5,"confidence":0.5}}}`))
	}))
	defer fake.Close()
	_, err := layaDecideRun(context.Background(), page.URL, fake.URL, "checkout_recovery", "", 0.5)
	if err == nil {
		t.Fatal("err = nil, want refusal on empty page state")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v, want it to name the empty state", err)
	}
}

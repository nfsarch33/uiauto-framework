package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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

	ev, err := layaDecideRun(context.Background(), page.URL, fake.URL, "checkout_recovery", "")
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

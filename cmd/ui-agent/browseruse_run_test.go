package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/browseruse"
)

// TestBrowserUseRunEvidence pins the evidence wire shape: history from the
// lane plus the advisory service-health block.
func TestBrowserUseRunEvidence(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"ok":true,"browser_use":"0.13.10","cdp_configured":true}`))
		case "/run":
			_, _ = w.Write([]byte(`{"ok":true,"final_result":"form submitted","steps":3,"duration_s":2.5,"errors":[],"urls_visited":1,"browser_use":"0.13.10"}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer svc.Close()

	ev, err := browserUseRun(context.Background(), svc.URL, browseruse.RunRequest{Task: "submit the form", MaxSteps: 5})
	if err != nil {
		t.Fatalf("browserUseRun: %v", err)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["final_result"] != "form submitted" || wire["steps"] != float64(3) {
		t.Errorf("history missing from evidence: %v", wire)
	}
	health, _ := wire["service_health"].(map[string]any)
	if health["browser_use"] != "0.13.10" {
		t.Errorf("service_health = %v, want the pinned version", health)
	}
}

// TestBrowserUseRunMarksDegradedRun: the third done-shape from the live leg.
// The executor lane reports ok=true while the model's final action never
// parsed, so the run reached no structured outcome at all. The verdict must
// say so machine-readably, and the lane's own ok flag must stay comparable.
func TestBrowserUseRunMarksDegradedRun(t *testing.T) {
	body := readBrowserUseFixture(t, "../../pkg/uiauto/browseruse/testdata/run3_invalid_agent_output.json")
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"ok":true,"browser_use":"0.13.10","cdp_configured":true}`))
		case "/run":
			_, _ = w.Write(body)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer svc.Close()

	ev, err := browserUseRun(context.Background(), svc.URL, browseruse.RunRequest{Task: "add a key", MaxSteps: 5})
	if err != nil {
		t.Fatalf("browserUseRun: %v", err)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["ok"] != true {
		t.Errorf("ok = %v, want true: the lane's own flag must stay comparable", wire["ok"])
	}
	if wire["degraded"] != true {
		t.Errorf("degraded = %v, want true: a truncated run whose final action never parsed is degraded", wire["degraded"])
	}
	reasons, _ := wire["degraded_reasons"].([]any)
	if len(reasons) != 2 {
		t.Fatalf("degraded_reasons = %v, want the truncation and the AgentOutput markers", reasons)
	}
}

// readBrowserUseFixture reads a sanitized executor payload from the
// browseruse package's testdata directory.
func readBrowserUseFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

// TestBrowserUseRunSurfacesFailure: a failed task must be an error, never
// evidence that reads as success.
func TestBrowserUseRunSurfacesFailure(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"errors":["no CDP endpoint"]}`))
	}))
	defer svc.Close()
	if _, err := browserUseRun(context.Background(), svc.URL, browseruse.RunRequest{Task: "x"}); err == nil {
		t.Fatal("err = nil, want task failure surfaced")
	}
}

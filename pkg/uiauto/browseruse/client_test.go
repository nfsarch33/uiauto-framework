package browseruse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRunParsesHistory pins the wire contract: POST /run with the task
// payload, typed history back, request body carries the overrides.
func TestRunParsesHistory(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run" {
			t.Errorf("path = %q, want /run", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %q", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{
			"ok": true,
			"final_result": "form submitted",
			"steps": 4,
			"duration_s": 12.5,
			"errors": [],
			"urls_visited": 2,
			"browser_use": "0.13.10"
		}`))
	}))
	defer srv.Close()

	res, err := New(srv.URL).Run(context.Background(), RunRequest{
		Task:     "submit the fixture form",
		URL:      "http://fixtures:8018/form-flow/",
		MaxSteps: 10,
		CDPURL:   "http://chrome:9222",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.OK || res.FinalResult != "form submitted" || res.Steps != 4 {
		t.Fatalf("result = %+v", res)
	}
	if gotBody["task"] != "submit the fixture form" || gotBody["cdp_url"] != "http://chrome:9222" {
		t.Errorf("request body missing overrides: %v", gotBody)
	}
	if gotBody["max_steps"] != float64(10) {
		t.Errorf("max_steps = %v, want 10", gotBody["max_steps"])
	}
}

// TestRunSurfacesTaskFailure: ok=false in a 200 body is an error naming
// the first lane error — never a silent success.
func TestRunSurfacesTaskFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok": false, "errors": ["no CDP endpoint: pass cdp_url"], "steps": 0}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).Run(context.Background(), RunRequest{Task: "x"})
	if err == nil || !strings.Contains(err.Error(), "no CDP endpoint") {
		t.Fatalf("err = %v, want it to name the lane error", err)
	}
}

// TestRunSurfacesHTTPError and malformed bodies must error, not decode to
// zero values that read as success.
func TestRunRejectsBadResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Run(context.Background(), RunRequest{Task: "x"}); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want 500", err)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{oops`))
	}))
	defer srv2.Close()
	if _, err := New(srv2.URL).Run(context.Background(), RunRequest{Task: "x"}); err == nil {
		t.Fatal("err = nil, want decode error")
	}
}

// TestRunHonorsContextDeadline: a wedged transport must not hang the
// caller past its own deadline (Doer wedge, same contract as the laya
// client).
func TestRunHonorsContextDeadline(t *testing.T) {
	doer := &blockDoer{}
	c := &Client{BaseURL: "http://bu.test", HTTP: doer}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.Run(ctx, RunRequest{Task: "x"}); err == nil {
		t.Fatal("err = nil, want deadline exceeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Run took %s; deadline not honored", elapsed)
	}
}

// TestHealthProbesVersion: /healthz reports the pinned version and CDP
// state; non-200 is an error.
func TestHealthProbesVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true,"browser_use":"0.13.10","cdp_configured":true}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	h, err := New(srv.URL).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !h.OK || h.BrowserUse != "0.13.10" || !h.CDPConfigured {
		t.Fatalf("health = %+v", h)
	}
}

type blockDoer struct{}

func (d *blockDoer) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

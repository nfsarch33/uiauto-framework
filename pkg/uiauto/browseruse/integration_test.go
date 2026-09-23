package browseruse

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBrowserUseLaneEndToEnd drives the full executor lane against the
// integration stack: browser-use service -> agent -> headless Chrome over
// CDP -> WireMock LLM stub -> history. Deterministic because the stub's
// scenario answers the first LLM call with a navigate action and every
// later one with done. Skipped unless the integration stack is up
// (BROWSER_USE_URL exported by scripts/run-integration-tests.sh).
func TestBrowserUseLaneEndToEnd(t *testing.T) {
	base := os.Getenv("BROWSER_USE_URL")
	if base == "" {
		t.Skip("BROWSER_USE_URL not set; bring up the integration stack (make test-integration-up)")
	}
	fixture := os.Getenv("BROWSER_USE_FIXTURE_URL")
	if fixture == "" {
		fixture = "http://fixtures:8018/form-flow/index.html"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// The navigate-then-done stub is a stateful WireMock scenario shared
	// by every test against this stack; reset it so this test sees a fresh
	// navigate step regardless of what ran before it.
	if stub := os.Getenv("BROWSER_USE_STUB_URL"); stub != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, stub+"/__admin/scenarios/reset", nil)
		if err == nil {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}
	}

	client := New(base)
	h, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("lane healthz: %v", err)
	}
	if !h.OK || h.BrowserUse == "" {
		t.Fatalf("lane health = %+v", h)
	}

	// fixture (page A) comes from BROWSER_USE_FIXTURE_URL; the stub
	// independently navigates to page B, so both must show up in the
	// distinct page history the service returns.
	res, err := client.Run(ctx, RunRequest{
		Task:     "Open the page, confirm it loaded, then finish the task.",
		URL:      fixture,
		MaxSteps: 5,
	})
	if err != nil {
		t.Fatalf("lane run: %v", err)
	}
	if !res.OK {
		t.Fatalf("task failed: %+v", res)
	}
	if res.Steps < 1 {
		t.Errorf("steps = %d, want >= 1", res.Steps)
	}
	if !strings.Contains(res.FinalResult, "fixture done") {
		t.Errorf("final_result = %q, want the stub's done text", res.FinalResult)
	}
	// Navigation is proven by the page history, not by the step count:
	// the request URL A must appear (deterministic pre-navigation) AND the
	// stub's page B must appear (the model-driven navigate action ran).
	// A missing A means the pre-navigation was dropped; a missing B means
	// the action never executed.
	pageB := os.Getenv("BROWSER_USE_STUB_NAV_URL")
	if pageB == "" {
		pageB = "http://fixtures:8018/checkout-recovery/index.html"
	}
	has := func(u string) bool {
		for _, got := range res.URLs {
			if got == u {
				return true
			}
		}
		return false
	}
	if !has(fixture) {
		t.Errorf("page history %v does not contain the request URL %q (pre-navigation dropped)", res.URLs, fixture)
	}
	if !has(pageB) {
		t.Errorf("page history %v does not contain the stub page %q (navigate action did not run)", res.URLs, pageB)
	}
}

// TestBrowserUseLaneLive is the env-gated real-LLM run: same lane, real
// gateway/model, a task with a checkable outcome. Opt-in because it costs
// tokens and needs credentials; see docs/eval.md.
func TestBrowserUseLaneLive(t *testing.T) {
	base := os.Getenv("BROWSER_USE_LIVE_URL")
	task := os.Getenv("BROWSER_USE_LIVE_TASK")
	if base == "" || task == "" {
		t.Skip("BROWSER_USE_LIVE_URL / BROWSER_USE_LIVE_TASK not set; live lane run is opt-in")
	}
	req := RunRequest{Task: task, MaxSteps: 15}
	if url := os.Getenv("BROWSER_USE_LIVE_PAGE"); url != "" {
		req.URL = url
	}
	if cdp := os.Getenv("BROWSER_USE_LIVE_CDP"); cdp != "" {
		req.CDPURL = cdp
	}
	if model := os.Getenv("BROWSER_USE_LIVE_MODEL"); model != "" {
		req.Model = model
	}
	if bu := os.Getenv("BROWSER_USE_LIVE_BASE_URL"); bu != "" {
		req.BaseURL = bu
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := New(base).Run(ctx, req)
	if err != nil {
		t.Fatalf("live lane run: %v", err)
	}
	t.Logf("live run: final=%q steps=%d duration=%.1fs errors=%v", res.FinalResult, res.Steps, res.DurationSec, res.Errors)
	if !res.OK {
		t.Fatalf("live task failed: %+v", res)
	}
}

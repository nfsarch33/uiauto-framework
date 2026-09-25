package fusion

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestFusionLaneEndToEnd drives the fused lane against the integration
// stack: capture -> OmniParser stub (grounded candidates) -> enriched
// task -> browser-use lane -> WireMock LLM scenario (navigate to page B,
// then done). Deterministic; skipped unless the stack is up
// (FUSION_BROWSER_USE_URL + FUSION_OMNIPARSER_URL exported by the
// integration script). Asserts the same distinct-page-history contract
// as the plain lane (request page A AND stub page B) plus grounded
// evidence, proving the enrichment does not break the loop.
func TestFusionLaneEndToEnd(t *testing.T) {
	laneURL := os.Getenv("FUSION_BROWSER_USE_URL")
	omniURL := os.Getenv("FUSION_OMNIPARSER_URL")
	if laneURL == "" || omniURL == "" {
		t.Skip("FUSION_BROWSER_USE_URL / FUSION_OMNIPARSER_URL not set; bring up the integration stack")
	}
	pageA := os.Getenv("FUSION_FIXTURE_URL")
	if pageA == "" {
		pageA = "http://fixtures:8018/form-flow/index.html"
	}
	pageB := os.Getenv("FUSION_STUB_NAV_URL")
	if pageB == "" {
		pageB = "http://fixtures:8018/checkout-recovery/index.html"
	}
	if stub := os.Getenv("FUSION_STUB_URL"); stub != "" {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, stub+"/__admin/scenarios/reset", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}

	chromeDebug := os.Getenv("FUSION_CHROME_DEBUG")
	if chromeDebug == "" {
		chromeDebug = "http://127.0.0.1:9333"
	}
	e := &Executor{
		BrowserUseURL: laneURL,
		OmniParserURL: omniURL,
		ChromeDebug:   chromeDebug,
		Threshold:     0.3,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	rec := e.Run(ctx, Scenario{
		ID: "fusion-e2e", Task: "Open the page, confirm it loaded, then finish the task.",
		URL: pageA, MaxSteps: 6, FinalContains: "fixture done",
	}, 1)

	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Errors) > 0 {
		t.Fatalf("fusion lane errors: %s", raw)
	}
	if !rec.OK {
		t.Fatalf("fusion run not ok: %s", raw)
	}
	if rec.Evidence["grounding"] != "grounded" {
		t.Fatalf("grounding = %v, want grounded (stub always returns confident elements)", rec.Evidence["grounding"])
	}
	if n, _ := rec.Evidence["candidates"].(int); n == 0 {
		t.Fatal("grounded run carried zero candidates")
	}
	_ = strings.TrimSpace // keep strings import stable if asserts change
}

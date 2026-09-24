// Package browseruse is the executor-lane client for the containerized
// browser-use service (containers/browser-use). browser-use drives an
// existing Chromium over CDP from a natural-language task; this client
// keeps the run contract typed — task in, structured history out — with
// no LLM handling on the Go side.
package browseruse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RunRequest is one task execution. Task is required; everything else
// overrides the service's environment defaults for this run only.
type RunRequest struct {
	Task     string `json:"task"`
	URL      string `json:"url,omitempty"`
	MaxSteps int    `json:"max_steps,omitempty"`
	CDPURL   string `json:"cdp_url,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Headless *bool  `json:"headless,omitempty"`
}

// RunResult is the run history the service returns. FinalResult empty
// with Errors non-empty is a failed task, not a silent success.
type RunResult struct {
	OK          bool     `json:"ok"`
	FinalResult string   `json:"final_result"`
	Steps       int      `json:"steps"`
	DurationSec float64  `json:"duration_s"`
	Errors      []string `json:"errors"`
	// URLs is the distinct, non-null, non-about:blank page history of the
	// run, in first-visit order -- the evidence that navigation (both the
	// deterministic pre-navigation and any model-driven one) actually
	// happened. URLsVisited is len(URLs).
	URLs        []string `json:"urls,omitempty"`
	URLsVisited int      `json:"urls_visited"`
	Model       string   `json:"model,omitempty"`
	BrowserUse  string   `json:"browser_use,omitempty"`
}

// Health is the /healthz payload: the service's ok flag, the pinned
// browser-use version it was built with, and whether a default CDP
// endpoint is configured.
type Health struct {
	OK            bool   `json:"ok"`
	BrowserUse    string `json:"browser_use"`
	CDPConfigured bool   `json:"cdp_configured"`
}

// Doer is the HTTP seam: *http.Client satisfies it, and tests inject a
// wedge without standing up a server.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client talks to the browser-use executor service (POST {base}/run).
type Client struct {
	BaseURL string
	HTTP    Doer
}

// New returns a client with a bounded default HTTP timeout. Task runs can
// be minutes long; callers that need longer pass their own Doer with the
// timeout their scenario deserves.
func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

func (c *Client) doer() Doer {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Run executes one task and returns its history. An HTTP error, a decode
// error, or ok=false in the payload are all errors — a task that failed
// inside the lane must never read as a succeeded one.
func (c *Client) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return RunResult{}, fmt.Errorf("browseruse: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/run", bytes.NewReader(body))
	if err != nil {
		return RunResult{}, fmt.Errorf("browseruse: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.doer().Do(httpReq)
	if err != nil {
		return RunResult{}, fmt.Errorf("browseruse: run: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return RunResult{}, fmt.Errorf("browseruse: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return RunResult{}, fmt.Errorf("browseruse: run returned %d: %.200s", resp.StatusCode, raw)
	}
	var out RunResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return RunResult{}, fmt.Errorf("browseruse: decode result: %w (body: %.120s)", err, raw)
	}
	if !out.OK {
		return out, fmt.Errorf("browseruse: task failed: %s", firstOr(out.Errors, "no final result"))
	}
	return out, nil
}

// Health probes the executor service. Advisory: a mismatched version or a
// missing default CDP endpoint is drift to record, not a reason to fail
// by itself.
func (c *Client) Health(ctx context.Context) (Health, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return Health{}, fmt.Errorf("browseruse: build health request: %w", err)
	}
	resp, err := c.doer().Do(httpReq)
	if err != nil {
		return Health{}, fmt.Errorf("browseruse: healthz: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return Health{}, fmt.Errorf("browseruse: read healthz: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("browseruse: healthz returned %d: %.200s", resp.StatusCode, raw)
	}
	var h Health
	if err := json.Unmarshal(raw, &h); err != nil {
		return Health{}, fmt.Errorf("browseruse: decode healthz: %w", err)
	}
	return h, nil
}

func firstOr(ss []string, or string) string {
	if len(ss) > 0 && ss[0] != "" {
		return ss[0]
	}
	return or
}

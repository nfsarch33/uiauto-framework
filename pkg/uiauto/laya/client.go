// Package laya is the typed-decision client for the containerized laya
// decision service (containers/laya). Laya returns structured decisions —
// choice with a calibrated probability distribution, noul (yes/no
// probability), score (ordinal) — with no text generation, which is why
// the answers parse into fixed shapes and nothing here needs an LLM.
package laya

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// State is the page/world state handed to the decision engine. Keys are
// free-form (visible_text, url, title, ...); the engine reads them as text.
type State map[string]string

// Questions is the question schema keyed by question name. Values mirror
// laya's schema: type, instructions, and criteria (choices, ordinal levels,
// or nothing for noul). Kept as maps so new question types travel without
// a client release.
type Questions map[string]map[string]any

// Answer is one typed decision. Which fields are populated depends on
// Type: Choice+Probabilities for "choice", Noul for "noul", Score for
// "score". Confidence is the engine's own calibration measure.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Confidence    float64            `json:"confidence"`
}

// Doer is the HTTP seam: *http.Client satisfies it, and tests inject a
// wedge without standing up a server.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client talks to the laya decision service (POST {base}/predict).
type Client struct {
	BaseURL string
	HTTP    Doer
}

// New returns a client with a bounded default HTTP timeout.
func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Decide asks the engine for typed decisions about one state. An empty
// answer set is an error, not a verdict: the harness must never read
// "service returned nothing" as "no action needed".
func (c *Client) Decide(ctx context.Context, state State, questions Questions) (map[string]Answer, error) {
	body, err := json.Marshal(map[string]any{"state": state, "questions": questions})
	if err != nil {
		return nil, fmt.Errorf("laya: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/predict", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("laya: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("laya: predict: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("laya: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("laya: predict returned %d: %.200s", resp.StatusCode, raw)
	}
	var out struct {
		Answers map[string]Answer `json:"answers"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("laya: decode answers: %w (body: %.120s)", err, raw)
	}
	if len(out.Answers) == 0 {
		return nil, fmt.Errorf("laya: no answers returned")
	}
	return out.Answers, nil
}

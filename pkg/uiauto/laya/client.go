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
	"errors"
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

// BelowFloor reports whether the engine's own calibration measure sits
// under the configured floor. Recorded, not enforced, while the model is
// on probation: the eval ledger has to separate wrong-and-confident from
// unsure before any verdict can be graded.
func (a Answer) BelowFloor(floor float64) bool { return a.Confidence < floor }

// ErrSchema marks an answer that failed validation against the question
// it answers. The client is the only boundary between a model's output
// and the executor, so an out-of-schema answer is an error, never a
// silently-empty field the executor would ignore.
var ErrSchema = errors.New("laya: answer failed schema validation")

// validateAnswers checks each answer against its question: the type must
// match the declared question type, a choice must be one of the question's
// own criteria, and the type-appropriate value field must be populated.
// Answers to questions that were not asked are tolerated (the service may
// volunteer extras); a missing answer to an asked question is not.
func validateAnswers(questions Questions, answers map[string]Answer) error {
	for name, q := range questions {
		a, ok := answers[name]
		if !ok {
			return fmt.Errorf("%w: no answer for question %q", ErrSchema, name)
		}
		if want, _ := q["type"].(string); want != "" && a.Type != want {
			return fmt.Errorf("%w: answer %q has type %q, question declares %q", ErrSchema, name, a.Type, want)
		}
		switch a.Type {
		case "choice":
			if a.Choice == "" {
				return fmt.Errorf("%w: answer %q has an empty choice", ErrSchema, name)
			}
			if criteria, ok := q["criteria"].(map[string]any); ok && len(criteria) > 0 {
				if _, ok := criteria[a.Choice]; !ok {
					return fmt.Errorf("%w: answer %q choice %q is not one of the question's criteria", ErrSchema, name, a.Choice)
				}
			}
		case "noul":
			if a.Noul == nil {
				return fmt.Errorf("%w: answer %q carries no noul value", ErrSchema, name)
			}
		case "score":
			if a.Score == nil {
				return fmt.Errorf("%w: answer %q carries no score value", ErrSchema, name)
			}
		default:
			return fmt.Errorf("%w: answer %q has unknown type %q", ErrSchema, name, a.Type)
		}
	}
	return nil
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
	if err := validateAnswers(questions, out.Answers); err != nil {
		return nil, err
	}
	return out.Answers, nil
}

// Health is the /healthz payload: the service's ok flag and the laya
// package version the container was built with, so stamp drift between
// the running container and the pinned build is visible to callers and
// run evidence.
type Health struct {
	OK   bool   `json:"ok"`
	Laya string `json:"laya"`
}

// Health probes the decision service. It is advisory: a version mismatch
// is drift to record in evidence, not a reason to fail a run by itself.
func (c *Client) Health(ctx context.Context) (Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return Health{}, fmt.Errorf("laya: build health request: %w", err)
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return Health{}, fmt.Errorf("laya: healthz: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return Health{}, fmt.Errorf("laya: read healthz: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("laya: healthz returned %d: %.200s", resp.StatusCode, raw)
	}
	var h Health
	if err := json.Unmarshal(raw, &h); err != nil {
		return Health{}, fmt.Errorf("laya: decode healthz: %w", err)
	}
	return h, nil
}

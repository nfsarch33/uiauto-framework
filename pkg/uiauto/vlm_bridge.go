package uiauto

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/aiwright"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/omniparser"
)

// UIElement represents a detected UI element from OmniParser or VLM analysis.
type UIElement struct {
	Label       string  `json:"label"`
	Type        string  `json:"type"`         // "button", "input", "link", "icon", "text"
	BoundingBox [4]int  `json:"bounding_box"` // [x, y, width, height]
	Confidence  float64 `json:"confidence"`
	Selector    string  `json:"selector,omitempty"`
}

// JudgmentResult captures the VLM's judgment on whether a described UI element/state is present.
type JudgmentResult struct {
	Present           bool    `json:"present"`
	Confidence        float64 `json:"confidence"`
	Explanation       string  `json:"explanation"`
	SuggestedSelector string  `json:"suggested_selector"`
}

// SelectorResult holds a generated selector and its confidence.
type SelectorResult struct {
	Selector   string  `json:"selector"`
	Confidence float64 `json:"confidence"`
}

// VLMAnalysisResult captures the full output of a VLM analysis pass.
type VLMAnalysisResult struct {
	Elements    []UIElement `json:"elements"`
	Description string      `json:"description"`
	RawResponse string      `json:"raw_response"`
	Model       string      `json:"model"`
	LatencyMs   int64       `json:"latency_ms"`
}

// VLMMetrics tracks VLM usage statistics.
type VLMMetrics struct {
	TotalCalls     int64
	SuccessCalls   int64
	FailedCalls    int64
	TotalLatencyMs int64
}

// Snapshot returns an atomic copy of the current VLM metrics.
func (m *VLMMetrics) Snapshot() VLMMetrics {
	return VLMMetrics{
		TotalCalls:     atomic.LoadInt64(&m.TotalCalls),
		SuccessCalls:   atomic.LoadInt64(&m.SuccessCalls),
		FailedCalls:    atomic.LoadInt64(&m.FailedCalls),
		TotalLatencyMs: atomic.LoadInt64(&m.TotalLatencyMs),
	}
}

// OmniParserConfig holds configuration for OmniParser V2 integration.
type OmniParserConfig struct {
	Endpoint string // HTTP endpoint for OmniParser service
	Timeout  time.Duration
	Enabled  bool
}

// VLMBridge provides an interface to Vision-Language Models for visual UI understanding.
type VLMBridge struct {
	provider      llm.Provider
	models        []string
	omniParser    *OmniParserConfig
	omniClient    *omniparser.Client // wired OmniParser V2 client (created from OmniParserConfig)
	aiwrightBrdge *aiwright.Bridge   // optional ai-wright SOM provider
	Metrics       *VLMMetrics
	logger        *slog.Logger
}

// VLMOption configures VLMBridge behavior.
type VLMOption func(*VLMBridge)

// WithOmniParser attaches OmniParser V2 configuration and creates the HTTP client.
func WithOmniParser(cfg OmniParserConfig) VLMOption {
	return func(v *VLMBridge) {
		v.omniParser = &cfg
		if cfg.Enabled && cfg.Endpoint != "" {
			timeout := cfg.Timeout
			if timeout == 0 {
				timeout = 10 * time.Second
			}
			v.omniClient = omniparser.NewClient(cfg.Endpoint,
				omniparser.WithHTTPClient(&http.Client{Timeout: timeout}))
		}
	}
}

// WithAiWright attaches an ai-wright SOM bridge as an alternative visual provider.
func WithAiWright(bridge *aiwright.Bridge) VLMOption {
	return func(v *VLMBridge) { v.aiwrightBrdge = bridge }
}

// WithVLMLogger sets the VLM logger.
func WithVLMLogger(l *slog.Logger) VLMOption {
	return func(v *VLMBridge) { v.logger = l }
}

// NewVLMBridge creates a new VLMBridge with multi-model fallback support.
func NewVLMBridge(provider llm.Provider, models []string, opts ...VLMOption) *VLMBridge {
	if len(models) == 0 {
		models = []string{"gpt-4o"}
	}
	v := &VLMBridge{
		provider: provider,
		models:   models,
		Metrics:  &VLMMetrics{},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// AnalyzeScreenshot sends a screenshot to the VLM for UI element detection.
func (v *VLMBridge) AnalyzeScreenshot(ctx context.Context, description string, screenshot []byte) (string, error) {
	start := time.Now()
	atomic.AddInt64(&v.Metrics.TotalCalls, 1)

	b64Image := base64.StdEncoding.EncodeToString(screenshot)
	imageURI := fmt.Sprintf("data:image/png;base64,%s", b64Image)

	prompt := buildVLMPrompt(description, imageURI)
	temp := float64(0.1)

	var lastErr error
	for _, model := range v.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp,
		}

		resp, err := v.provider.Complete(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			v.logger.Warn("VLM model failed", "model", model, "error", err)
			continue
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("model %s returned no choices", model)
			continue
		}

		latency := time.Since(start)
		atomic.AddInt64(&v.Metrics.SuccessCalls, 1)
		atomic.AddInt64(&v.Metrics.TotalLatencyMs, int64(latency/time.Millisecond))

		return strings.TrimSpace(resp.Choices[0].Message.Content), nil
	}

	atomic.AddInt64(&v.Metrics.FailedCalls, 1)
	return "", fmt.Errorf("all VLM models failed, last error: %w", lastErr)
}

// DetectElements analyzes a screenshot and returns structured UI elements.
func (v *VLMBridge) DetectElements(ctx context.Context, screenshot []byte) (*VLMAnalysisResult, error) {
	start := time.Now()
	atomic.AddInt64(&v.Metrics.TotalCalls, 1)

	b64Image := base64.StdEncoding.EncodeToString(screenshot)
	imageURI := fmt.Sprintf("data:image/png;base64,%s", b64Image)

	prompt := buildElementDetectionPrompt(imageURI)
	temp := float64(0.1)

	var lastErr error
	for _, model := range v.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp,
		}

		resp, err := v.provider.Complete(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			continue
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("model %s returned no choices", model)
			continue
		}

		raw := strings.TrimSpace(resp.Choices[0].Message.Content)
		elements := parseElementResponse(raw)
		latency := time.Since(start)
		atomic.AddInt64(&v.Metrics.SuccessCalls, 1)
		atomic.AddInt64(&v.Metrics.TotalLatencyMs, int64(latency/time.Millisecond))

		return &VLMAnalysisResult{
			Elements:    elements,
			Description: raw,
			RawResponse: raw,
			Model:       model,
			LatencyMs:   int64(latency / time.Millisecond),
		}, nil
	}

	atomic.AddInt64(&v.Metrics.FailedCalls, 1)
	return nil, fmt.Errorf("all VLM models failed for element detection, last: %w", lastErr)
}

// VerifyElement uses VLM to confirm whether a selector points to the expected element.
func (v *VLMBridge) VerifyElement(ctx context.Context, description string, screenshot []byte, selector string) (matched bool, confidence float64, err error) {
	start := time.Now()
	atomic.AddInt64(&v.Metrics.TotalCalls, 1)

	b64Image := base64.StdEncoding.EncodeToString(screenshot)
	imageURI := fmt.Sprintf("data:image/png;base64,%s", b64Image)

	prompt := fmt.Sprintf(`Analyze this screenshot and determine if the CSS selector "%s" likely points to the element described as: "%s".

Screenshot: %s

Respond with ONLY a JSON object: {"match": true/false, "confidence": 0.0-1.0, "reason": "brief explanation"}`, selector, description, imageURI[:min(len(imageURI), 100)]+"...")

	temp := float64(0.1)
	var lastErr error
	for _, model := range v.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp,
		}

		resp, err := v.provider.Complete(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}

		if len(resp.Choices) == 0 {
			continue
		}

		latency := time.Since(start)
		atomic.AddInt64(&v.Metrics.SuccessCalls, 1)
		atomic.AddInt64(&v.Metrics.TotalLatencyMs, int64(latency/time.Millisecond))

		raw := strings.TrimSpace(resp.Choices[0].Message.Content)
		var result struct {
			Match      bool    `json:"match"`
			Confidence float64 `json:"confidence"`
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return strings.Contains(strings.ToLower(raw), "true"), 0.5, nil
		}
		return result.Match, result.Confidence, nil
	}

	atomic.AddInt64(&v.Metrics.FailedCalls, 1)
	return false, 0, fmt.Errorf("VLM verification failed: %w", lastErr)
}

// VLMAsJudge sends a screenshot and description to the VLM and asks it to judge whether
// the described UI element/state is present. Returns a structured JudgmentResult.
func (v *VLMBridge) VLMAsJudge(ctx context.Context, description string, screenshot []byte) (*JudgmentResult, error) {
	start := time.Now()
	atomic.AddInt64(&v.Metrics.TotalCalls, 1)

	b64Image := base64.StdEncoding.EncodeToString(screenshot)
	imageURI := fmt.Sprintf("data:image/png;base64,%s", b64Image)

	prompt := buildJudgePrompt(description, imageURI)
	temp := float64(0.1)

	var lastErr error
	for _, model := range v.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp,
		}

		resp, err := v.provider.Complete(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			v.logger.Warn("VLM judge model failed", "model", model, "error", err)
			continue
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("model %s returned no choices", model)
			continue
		}

		latency := time.Since(start)
		atomic.AddInt64(&v.Metrics.SuccessCalls, 1)
		atomic.AddInt64(&v.Metrics.TotalLatencyMs, int64(latency/time.Millisecond))

		raw := strings.TrimSpace(resp.Choices[0].Message.Content)
		result := parseJudgeResponse(raw)
		return result, nil
	}

	atomic.AddInt64(&v.Metrics.FailedCalls, 1)
	return nil, fmt.Errorf("VLM judge failed, last error: %w", lastErr)
}

// GenerateSelectorFromVLM sends a screenshot and description to the VLM and asks it to
// generate a CSS selector or XPath for the target element. Returns the selector and confidence.
func (v *VLMBridge) GenerateSelectorFromVLM(ctx context.Context, description string, screenshot []byte) (*SelectorResult, error) {
	start := time.Now()
	atomic.AddInt64(&v.Metrics.TotalCalls, 1)

	b64Image := base64.StdEncoding.EncodeToString(screenshot)
	imageURI := fmt.Sprintf("data:image/png;base64,%s", b64Image)

	prompt := buildSelectorPrompt(description, imageURI)
	temp := float64(0.1)

	var lastErr error
	for _, model := range v.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp,
		}

		resp, err := v.provider.Complete(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			v.logger.Warn("VLM selector model failed", "model", model, "error", err)
			continue
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("model %s returned no choices", model)
			continue
		}

		latency := time.Since(start)
		atomic.AddInt64(&v.Metrics.SuccessCalls, 1)
		atomic.AddInt64(&v.Metrics.TotalLatencyMs, int64(latency/time.Millisecond))

		raw := strings.TrimSpace(resp.Choices[0].Message.Content)
		result := parseSelectorResponse(raw)
		return result, nil
	}

	atomic.AddInt64(&v.Metrics.FailedCalls, 1)
	return nil, fmt.Errorf("VLM selector generation failed, last error: %w", lastErr)
}

// IsOmniParserAvailable checks if the OmniParser endpoint is configured and reachable.
func (v *VLMBridge) IsOmniParserAvailable() bool {
	return v.omniParser != nil && v.omniParser.Enabled && v.omniParser.Endpoint != ""
}

func buildVLMPrompt(description, imageURI string) string {
	previewLen := 100
	if len(imageURI) < previewLen {
		previewLen = len(imageURI)
	}
	return fmt.Sprintf(`You are an expert UI automation agent.
I need to interact with an element described as: "%s"

Analyze the provided screenshot and tell me how to find or interact with this element.
Provide a CSS selector if possible, or describe coordinates and visual characteristics.

[Image Data: %s...]`, description, imageURI[:previewLen])
}

func buildJudgePrompt(description, imageURI string) string {
	previewLen := 100
	if len(imageURI) < previewLen {
		previewLen = len(imageURI)
	}
	return fmt.Sprintf(`You are an expert UI automation judge. Analyze this screenshot and determine whether the described UI element or state is present.

Description: "%s"

Screenshot: %s

Respond with ONLY a JSON object (no markdown, no extra text):
{"present": true/false, "confidence": 0.0-1.0, "explanation": "brief reason", "suggested_selector": "CSS selector or XPath if element found, else empty string"}`, description, imageURI[:previewLen]+"...")
}

func buildSelectorPrompt(description, imageURI string) string {
	previewLen := 100
	if len(imageURI) < previewLen {
		previewLen = len(imageURI)
	}
	return fmt.Sprintf(`You are an expert UI automation agent. Analyze this screenshot and generate a CSS selector or XPath for the target element.

Description: "%s"

Screenshot: %s

Respond with ONLY a JSON object (no markdown, no extra text):
{"selector": "CSS selector or XPath", "confidence": 0.0-1.0}`, description, imageURI[:previewLen]+"...")
}

func parseJudgeResponse(raw string) *JudgmentResult {
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var j struct {
		Present           bool    `json:"present"`
		Confidence        float64 `json:"confidence"`
		Explanation       string  `json:"explanation"`
		SuggestedSelector string  `json:"suggested_selector"`
	}
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		return &JudgmentResult{
			Present:     strings.Contains(strings.ToLower(raw), "true"),
			Confidence:  0.5,
			Explanation: strings.TrimSpace(raw),
		}
	}
	return &JudgmentResult{
		Present:           j.Present,
		Confidence:        j.Confidence,
		Explanation:       j.Explanation,
		SuggestedSelector: j.SuggestedSelector,
	}
}

func parseSelectorResponse(raw string) *SelectorResult {
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var s struct {
		Selector   string  `json:"selector"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return &SelectorResult{Selector: "", Confidence: 0}
	}
	return &SelectorResult{Selector: s.Selector, Confidence: s.Confidence}
}

func buildElementDetectionPrompt(imageURI string) string {
	previewLen := 100
	if len(imageURI) < previewLen {
		previewLen = len(imageURI)
	}
	return fmt.Sprintf(`Analyze the UI screenshot and identify all interactive elements.
For each element, provide:
- label: descriptive name
- type: button|input|link|icon|text
- bounding_box: [x, y, width, height]
- confidence: 0.0-1.0

Respond with a JSON array of elements.

[Image Data: %s...]`, imageURI[:previewLen])
}

func parseElementResponse(raw string) []UIElement {
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var elements []UIElement
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		return nil
	}
	return elements
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

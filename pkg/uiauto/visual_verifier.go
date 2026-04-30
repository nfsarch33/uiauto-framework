package uiauto

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"
)

// JudgeVerificationResult captures the outcome of a VLM-as-judge verification.
type JudgeVerificationResult struct {
	Passed      bool          `json:"passed"`
	Confidence  float64       `json:"confidence"`
	Explanation string        `json:"explanation"`
	Screenshot  string        `json:"screenshot"` // base64-encoded PNG
	Duration    time.Duration `json:"duration_ns"`
}

// VisualVerificationResult captures the outcome of a visual verification.
type VisualVerificationResult struct {
	TargetID    string        `json:"target_id"`
	Selector    string        `json:"selector"`
	DOMMatch    bool          `json:"dom_match"`
	VLMMatch    bool          `json:"vlm_match"`
	VLMConf     float64       `json:"vlm_confidence"`
	Combined    bool          `json:"combined_match"`
	Duration    time.Duration `json:"duration_ns"`
	ElementsVLM int           `json:"elements_detected_vlm"`
	Method      string        `json:"method"`
}

// VisualVerifier composes screenshot capture with VLM analysis for UI verification.
// When text-based DOM detection is uncertain, it falls back to visual analysis
// to confirm element presence and correctness.
type VisualVerifier struct {
	browser *BrowserAgent
	vlm     *VLMBridge
	logger  *slog.Logger

	domConfThreshold float64
	vlmConfThreshold float64
}

// VisualVerifierConfig holds configuration for the verifier.
type VisualVerifierConfig struct {
	DOMConfidenceThreshold float64
	VLMConfidenceThreshold float64
}

// NewVisualVerifier creates a verifier that combines DOM and VLM checks.
func NewVisualVerifier(browser *BrowserAgent, vlm *VLMBridge, logger *slog.Logger, cfg ...VisualVerifierConfig) *VisualVerifier {
	vv := &VisualVerifier{
		browser:          browser,
		vlm:              vlm,
		logger:           logger,
		domConfThreshold: 0.7,
		vlmConfThreshold: 0.6,
	}
	if len(cfg) > 0 {
		if cfg[0].DOMConfidenceThreshold > 0 {
			vv.domConfThreshold = cfg[0].DOMConfidenceThreshold
		}
		if cfg[0].VLMConfidenceThreshold > 0 {
			vv.vlmConfThreshold = cfg[0].VLMConfidenceThreshold
		}
	}
	return vv
}

// VerifyElement checks whether a selector points to the expected element by
// first trying DOM-based verification, then falling back to VLM if uncertain.
func (vv *VisualVerifier) VerifyElement(ctx context.Context, targetID, selector, description string) (*VisualVerificationResult, error) {
	start := time.Now()
	result := &VisualVerificationResult{
		TargetID: targetID,
		Selector: selector,
	}

	// L1: DOM-based check
	domOK := vv.browser.Click(selector) == nil
	result.DOMMatch = domOK

	if domOK {
		result.Combined = true
		result.Method = "dom"
		result.Duration = time.Since(start)
		return result, nil
	}

	// L2: VLM fallback
	if vv.vlm == nil {
		result.Combined = false
		result.Method = "dom_only"
		result.Duration = time.Since(start)
		return result, nil
	}

	screenshot, err := vv.browser.CaptureScreenshot()
	if err != nil {
		vv.logger.Warn("screenshot capture failed for VLM verification", "error", err)
		result.Combined = false
		result.Method = "screenshot_failed"
		result.Duration = time.Since(start)
		return result, nil
	}

	match, conf, err := vv.vlm.VerifyElement(ctx, description, screenshot, selector)
	if err != nil {
		vv.logger.Warn("VLM verification failed", "target", targetID, "error", err)
		result.Combined = false
		result.Method = "vlm_error"
		result.Duration = time.Since(start)
		return result, nil
	}

	result.VLMMatch = match
	result.VLMConf = conf
	result.Combined = match && conf >= vv.vlmConfThreshold
	result.Method = "vlm"
	result.Duration = time.Since(start)
	return result, nil
}

// VerifyWithJudge captures a screenshot, calls VLMAsJudge to verify the described UI state,
// and returns a JudgeVerificationResult with pass/fail, confidence, explanation, and screenshot.
func (vv *VisualVerifier) VerifyWithJudge(ctx context.Context, description string) (*JudgeVerificationResult, error) {
	start := time.Now()
	result := &JudgeVerificationResult{}

	if vv.vlm == nil {
		result.Passed = false
		result.Confidence = 0
		result.Explanation = "VLM not configured"
		result.Duration = time.Since(start)
		return result, nil
	}

	screenshot, err := vv.browser.CaptureScreenshot()
	if err != nil {
		result.Passed = false
		result.Confidence = 0
		result.Explanation = fmt.Sprintf("screenshot capture failed: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	judgment, err := vv.vlm.VLMAsJudge(ctx, description, screenshot)
	if err != nil {
		vv.logger.Warn("VLM judge failed", "description", description, "error", err)
		result.Passed = false
		result.Confidence = 0
		result.Explanation = fmt.Sprintf("VLM judge error: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	result.Passed = judgment.Present && judgment.Confidence >= vv.vlmConfThreshold
	result.Confidence = judgment.Confidence
	result.Explanation = judgment.Explanation
	result.Screenshot = base64.StdEncoding.EncodeToString(screenshot)
	result.Duration = time.Since(start)
	return result, nil
}

// DetectAllElements uses VLM to discover all interactive elements on the page.
func (vv *VisualVerifier) DetectAllElements(ctx context.Context) (*VLMAnalysisResult, error) {
	if vv.vlm == nil {
		return nil, fmt.Errorf("VLM not configured")
	}

	screenshot, err := vv.browser.CaptureScreenshot()
	if err != nil {
		return nil, fmt.Errorf("screenshot capture: %w", err)
	}

	return vv.vlm.DetectElements(ctx, screenshot)
}

// VerifyPageState takes a screenshot and verifies multiple expected elements.
func (vv *VisualVerifier) VerifyPageState(ctx context.Context, expectations []struct {
	TargetID    string
	Selector    string
	Description string
}) ([]VisualVerificationResult, error) {
	var results []VisualVerificationResult
	for _, exp := range expectations {
		r, err := vv.VerifyElement(ctx, exp.TargetID, exp.Selector, exp.Description)
		if err != nil {
			return results, fmt.Errorf("verify %s: %w", exp.TargetID, err)
		}
		results = append(results, *r)
	}
	return results, nil
}

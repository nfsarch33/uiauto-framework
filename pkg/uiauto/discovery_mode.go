package uiauto

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// DiscoveredElement represents a UI element found during discovery.
type DiscoveredElement struct {
	ID          string  `json:"id"`
	Selector    string  `json:"selector"`
	Description string  `json:"description"`
	ElementType string  `json:"element_type"`
	Confidence  float64 `json:"confidence"`
	HTML        string  `json:"html"`
}

// DiscoveryResult holds the outcome of a discovery scan.
type DiscoveryResult struct {
	URL             string              `json:"url"`
	Elements        []DiscoveredElement `json:"elements"`
	Duration        time.Duration       `json:"duration"`
	TotalCandidates int                 `json:"total_candidates"`
	Registered      int                 `json:"registered"`
	Errors          []string            `json:"errors,omitempty"`
}

// DiscoveryConfig controls discovery mode behavior.
type DiscoveryConfig struct {
	ElementTypes  []string      // element types to discover (e.g. "button", "input", "link", "nav")
	MaxElements   int           // max elements to register per scan (0 = unlimited)
	MinConfidence float64       // minimum confidence to register (default 0.5)
	ScanTimeout   time.Duration // timeout for the entire scan
	UseVLM        bool          // whether to use VLM for verification
}

// DefaultDiscoveryConfig returns production defaults.
func DefaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		ElementTypes:  []string{"button", "input", "link", "navigation", "form", "heading"},
		MaxElements:   50,
		MinConfidence: 0.5,
		ScanTimeout:   30 * time.Second,
		UseVLM:        false,
	}
}

// DiscoveryMode performs initial page exploration to pre-populate patterns.
// It uses the smart discoverer (and optionally VLM) to scan a page and
// register all significant interactive elements.
type DiscoveryMode struct {
	smart   *SmartDiscoverer
	vlm     *VLMBridge
	tracker *PatternTracker
	browser *BrowserAgent
	logger  *slog.Logger
	config  DiscoveryConfig
}

// NewDiscoveryMode creates a new DiscoveryMode.
func NewDiscoveryMode(smart *SmartDiscoverer, tracker *PatternTracker, browser *BrowserAgent, cfg DiscoveryConfig, logger *slog.Logger) *DiscoveryMode {
	return &DiscoveryMode{
		smart:   smart,
		tracker: tracker,
		browser: browser,
		logger:  logger,
		config:  cfg,
	}
}

// WithVLM attaches a VLM bridge for verification during discovery.
func (d *DiscoveryMode) WithVLM(vlm *VLMBridge) {
	d.vlm = vlm
}

// Scan performs a full-page discovery scan, registering found elements.
func (d *DiscoveryMode) Scan(ctx context.Context, pageURL string) (*DiscoveryResult, error) {
	start := time.Now()

	scanCtx, cancel := context.WithTimeout(ctx, d.config.ScanTimeout)
	defer cancel()

	result := &DiscoveryResult{
		URL: pageURL,
	}

	html, err := d.browser.CaptureDOM()
	if err != nil {
		return nil, fmt.Errorf("failed to capture DOM for discovery: %w", err)
	}

	for _, elemType := range d.config.ElementTypes {
		if scanCtx.Err() != nil {
			result.Errors = append(result.Errors, "scan timeout reached")
			break
		}

		prompt := fmt.Sprintf("Find all %s elements on this page and return their CSS selectors", elemType)
		selector, discErr := d.smart.DiscoverSelector(scanCtx, prompt, html)
		if discErr != nil {
			d.logger.Debug("discovery failed for element type",
				"type", elemType, "error", discErr)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", elemType, discErr))
			continue
		}

		confidence := 0.7
		if d.vlm != nil && d.config.UseVLM {
			screenshot, ssErr := d.browser.CaptureScreenshot()
			if ssErr == nil {
				matched, vlmConf, verifyErr := d.vlm.VerifyElement(scanCtx, elemType, screenshot, selector)
				if verifyErr == nil && matched {
					confidence = vlmConf
				}
			}
		}

		elem := DiscoveredElement{
			ID:          fmt.Sprintf("%s_%s", elemType, sanitizeID(selector)),
			Selector:    selector,
			Description: fmt.Sprintf("Auto-discovered %s element", elemType),
			ElementType: elemType,
			Confidence:  confidence,
			HTML:        html,
		}
		result.Elements = append(result.Elements, elem)
		result.TotalCandidates++

		if confidence >= d.config.MinConfidence {
			if d.config.MaxElements > 0 && result.Registered >= d.config.MaxElements {
				continue
			}
			regErr := d.tracker.RegisterPattern(scanCtx, elem.ID, elem.Selector, elem.Description, elem.HTML)
			if regErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("register %s: %v", elem.ID, regErr))
				continue
			}
			result.Registered++
		}
	}

	result.Duration = time.Since(start)
	d.logger.Info("discovery scan complete",
		"url", pageURL,
		"elements", result.TotalCandidates,
		"registered", result.Registered,
		"duration", result.Duration,
	)

	return result, nil
}

func sanitizeID(selector string) string {
	out := make([]byte, 0, len(selector))
	for _, c := range selector {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			out = append(out, byte(c))
		} else {
			out = append(out, '_')
		}
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return string(out)
}

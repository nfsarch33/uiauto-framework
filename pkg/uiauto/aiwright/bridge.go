package aiwright

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ScreenshotProvider captures the current browser viewport.
type ScreenshotProvider interface {
	CaptureScreenshot() ([]byte, error)
}

// ElementMapper converts SOM elements into actionable selectors.
type ElementMapper interface {
	MapToSelector(element SOMElement) string
}

// Bridge connects browser screenshot capture to ai-wright SOM annotation,
// producing actionable element maps from visual analysis.
type Bridge struct {
	client     *Client
	screenshot ScreenshotProvider
	mapper     ElementMapper
	logger     *slog.Logger
}

// BridgeOption configures a Bridge.
type BridgeOption func(*Bridge)

// WithBridgeLogger sets a structured logger.
func WithBridgeLogger(l *slog.Logger) BridgeOption {
	return func(b *Bridge) { b.logger = l }
}

// WithElementMapper overrides the default element mapper.
func WithElementMapper(m ElementMapper) BridgeOption {
	return func(b *Bridge) { b.mapper = m }
}

// NewBridge creates a Bridge that pipes browser screenshots through ai-wright.
func NewBridge(client *Client, sp ScreenshotProvider, opts ...BridgeOption) *Bridge {
	b := &Bridge{
		client:     client,
		screenshot: sp,
		mapper:     &DefaultMapper{},
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// ElementMap holds the SOM result with computed selectors for each element.
type ElementMap struct {
	Result    *SOMResult
	Selectors map[int]string
	CaptureAt time.Time
	Latency   time.Duration
}

// Analyze captures a screenshot and runs SOM annotation, producing an element map.
func (b *Bridge) Analyze(ctx context.Context) (*ElementMap, error) {
	start := time.Now()

	screenshot, err := b.screenshot.CaptureScreenshot()
	if err != nil {
		return nil, fmt.Errorf("bridge: capture screenshot: %w", err)
	}
	if len(screenshot) == 0 {
		return nil, fmt.Errorf("bridge: empty screenshot")
	}

	result, err := b.client.Annotate(ctx, screenshot)
	if err != nil {
		return nil, fmt.Errorf("bridge: annotate: %w", err)
	}

	selectors := make(map[int]string, len(result.Elements))
	for _, elem := range result.Elements {
		selectors[elem.ID] = b.mapper.MapToSelector(elem)
	}

	latency := time.Since(start)
	b.logger.Info("bridge analyze",
		slog.Int("elements", len(result.Elements)),
		slog.Duration("capture_latency", result.Latency),
		slog.Duration("total_latency", latency),
	)

	return &ElementMap{
		Result:    result,
		Selectors: selectors,
		CaptureAt: start,
		Latency:   latency,
	}, nil
}

// DefaultMapper generates CSS selectors from SOM element attributes.
type DefaultMapper struct{}

// MapToSelector builds a CSS selector from element attributes and type.
func (d *DefaultMapper) MapToSelector(elem SOMElement) string {
	if id, ok := elem.Attributes["id"]; ok && id != "" {
		return "#" + id
	}
	if testID, ok := elem.Attributes["data-testid"]; ok && testID != "" {
		return "[data-testid=\"" + testID + "\"]"
	}
	if name, ok := elem.Attributes["name"]; ok && name != "" {
		return "[name=\"" + name + "\"]"
	}

	switch elem.Type {
	case ElementButton:
		if elem.Label != "" {
			return "button:has-text(\"" + elem.Label + "\")"
		}
		return "button"
	case ElementInput:
		if ph, ok := elem.Attributes["placeholder"]; ok {
			return "input[placeholder=\"" + ph + "\"]"
		}
		if elem.Attributes["type"] == "password" {
			return "input[type=\"password\"]"
		}
		return "input"
	case ElementLink:
		if elem.Label != "" {
			return "a:has-text(\"" + elem.Label + "\")"
		}
		return "a"
	default:
		return fmt.Sprintf("[data-som-id=\"%d\"]", elem.ID)
	}
}

package omniparser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ParseResult is the OmniParser V2 response.
type ParseResult struct {
	Elements       []UIElement   `json:"elements"`
	Layout         LayoutInfo    `json:"layout"`
	SOMImageB64    string        `json:"som_image_base64,omitempty"`
	Mode           string        `json:"mode,omitempty"`
	OCRTextCount   int           `json:"ocr_text_count,omitempty"`
	FallbackReason string        `json:"fallback_reason,omitempty"`
	Latency        time.Duration `json:"-"`
}

// UIElement represents a parsed UI component.
type UIElement struct {
	ID           int         `json:"id"`
	Type         string      `json:"type"`
	Text         string      `json:"text"`
	Content      string      `json:"content,omitempty"`
	BoundingBox  BoundingBox `json:"bounding_box"`
	Confidence   float64     `json:"confidence"`
	Interactable bool        `json:"interactable"`
}

// BoundingBox is a screen rectangle.
type BoundingBox struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// LayoutInfo captures page-level structure.
type LayoutInfo struct {
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	PageType string `json:"page_type"`
	Regions  int    `json:"regions"`
}

// Client communicates with OmniParser V2.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates an OmniParser client.
func NewClient(baseURL string, opts ...func(*Client)) *Client {
	c := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) func(*Client) {
	return func(c *Client) { c.httpClient = hc }
}

// ParseRequest is the request payload for OmniParser V2.
// The server expects a base64-encoded image string, not raw bytes.
type ParseRequest struct {
	Base64Image string `json:"base64_image"`
	Detail      string `json:"detail,omitempty"`
}

// Parse sends a screenshot for element detection.
func (c *Client) Parse(ctx context.Context, screenshot []byte) (*ParseResult, error) {
	start := time.Now()

	payload, err := json.Marshal(ParseRequest{Base64Image: base64.StdEncoding.EncodeToString(screenshot), Detail: "high"})
	if err != nil {
		return nil, fmt.Errorf("omniparser: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("omniparser: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("omniparser: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("omniparser: status %d: %s", resp.StatusCode, string(body))
	}

	var result ParseResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("omniparser: decode: %w", err)
	}

	result.Latency = time.Since(start)
	result.Mode = "visual"
	c.logger.Info("omniparser parse",
		slog.Int("elements", len(result.Elements)),
		slog.Duration("latency", result.Latency),
	)

	if len(result.Elements) == 0 {
		c.logger.Info("omniparser: zero elements from /parse, trying /parse-ocr fallback")
		if ocrResult, ocrErr := c.parseOCR(ctx, screenshot); ocrErr == nil && len(ocrResult.Elements) > 0 {
			ocrResult.Latency = time.Since(start)
			ocrResult.Mode = "ocr"
			ocrResult.FallbackReason = "visual_zero_elements"
			return ocrResult, nil
		} else if ocrErr != nil {
			result.FallbackReason = "visual_zero_elements; ocr_failed"
		} else {
			result.FallbackReason = "visual_zero_elements; ocr_zero_elements"
		}
	}

	return &result, nil
}

func (c *Client) parseOCR(ctx context.Context, screenshot []byte) (*ParseResult, error) {
	payload, err := json.Marshal(ParseRequest{Base64Image: base64.StdEncoding.EncodeToString(screenshot)})
	if err != nil {
		return nil, fmt.Errorf("omniparser-ocr: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse-ocr", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("omniparser-ocr: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("omniparser-ocr: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("omniparser-ocr: status %d: %s", resp.StatusCode, string(body))
	}

	var result ParseResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("omniparser-ocr: decode: %w", err)
	}
	result.Mode = "ocr"
	result.OCRTextCount = countTextElements(result.Elements)

	c.logger.Info("omniparser-ocr fallback",
		slog.Int("elements", len(result.Elements)),
		slog.Int("ocr_text_count", result.OCRTextCount),
	)
	return &result, nil
}

func countTextElements(elements []UIElement) int {
	count := 0
	for _, element := range elements {
		if element.Text != "" || element.Content != "" {
			count++
		}
	}
	return count
}

// HealthCheck verifies service availability.
// Tries /health first; falls back to /probe/ (used by OmniParser FastAPI server).
func (c *Client) HealthCheck(ctx context.Context) error {
	for _, path := range []string{"/health", "/probe/"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return fmt.Errorf("omniparser: health request: %w", err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("omniparser: health: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		if resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("omniparser: health status %d on %s", resp.StatusCode, path)
		}
	}
	return fmt.Errorf("omniparser: no health endpoint found (tried /health and /probe/)")
}

// FindInteractable returns all interactive elements.
func (r ParseResult) FindInteractable() []UIElement {
	var result []UIElement
	for _, e := range r.Elements {
		if e.Interactable {
			result = append(result, e)
		}
	}
	return result
}

// FindByType returns elements matching the given type.
func (r ParseResult) FindByType(t string) []UIElement {
	var result []UIElement
	for _, e := range r.Elements {
		if e.Type == t {
			result = append(result, e)
		}
	}
	return result
}

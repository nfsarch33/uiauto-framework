package aiwright

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client communicates with an ai-wright SOM annotation service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) { cl.httpClient = c }
}

// WithLogger sets a structured logger.
func WithLogger(l *slog.Logger) ClientOption {
	return func(cl *Client) { cl.logger = l }
}

// NewClient creates a Client pointing at the given SOM service URL.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// AnnotateRequest is the payload sent to the SOM endpoint.
type AnnotateRequest struct {
	Image       []byte `json:"image"`
	ReturnImage bool   `json:"return_image,omitempty"`
}

// Annotate sends a screenshot to the SOM service and returns detected elements.
func (c *Client) Annotate(ctx context.Context, screenshot []byte) (*SOMResult, error) {
	start := time.Now()

	payload, err := json.Marshal(AnnotateRequest{
		Image:       screenshot,
		ReturnImage: true,
	})
	if err != nil {
		return nil, fmt.Errorf("aiwright: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/annotate", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("aiwright: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aiwright: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aiwright: server returned %d: %s", resp.StatusCode, string(body))
	}

	var result SOMResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("aiwright: decode response: %w", err)
	}

	result.Latency = time.Since(start)
	c.logger.Info("aiwright annotate",
		slog.Int("elements", len(result.Elements)),
		slog.Duration("latency", result.Latency),
	)

	return &result, nil
}

// HealthCheck verifies the SOM service is reachable.
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("aiwright: health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("aiwright: health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("aiwright: health status %d", resp.StatusCode)
	}
	return nil
}

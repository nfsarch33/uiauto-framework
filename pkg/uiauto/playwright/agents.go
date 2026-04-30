package playwright

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// AgentsClient wraps the Playwright Agents CLI (npx @playwright/agents).
// It delegates planning and code generation to Playwright's built-in
// planner/generator/healer pipeline, returning structured results.
type AgentsClient struct {
	npxPath string
	timeout time.Duration
	logger  *slog.Logger
}

// Option configures AgentsClient.
type Option func(*AgentsClient)

// WithNpxPath overrides the npx binary path.
func WithNpxPath(path string) Option {
	return func(c *AgentsClient) { c.npxPath = path }
}

// WithTimeout sets the maximum execution time for a single agent run.
func WithTimeout(d time.Duration) Option {
	return func(c *AgentsClient) { c.timeout = d }
}

// WithLogger sets a structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(c *AgentsClient) { c.logger = l }
}

// NewAgentsClient creates a client that invokes Playwright Agents CLI.
func NewAgentsClient(opts ...Option) *AgentsClient {
	c := &AgentsClient{
		npxPath: "npx",
		timeout: 120 * time.Second,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// PlanResult is the structured output from a Playwright Agents plan run.
type PlanResult struct {
	Steps    []PlanStep    `json:"steps"`
	Model    string        `json:"model"`
	Tokens   int           `json:"tokens"`
	Duration time.Duration `json:"-"`
}

// PlanStep is a single planned action.
type PlanStep struct {
	Action      string `json:"action"`
	Selector    string `json:"selector,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description"`
}

// GenerateResult is the structured output from code generation.
type GenerateResult struct {
	Code     string        `json:"code"`
	Language string        `json:"language"`
	Model    string        `json:"model"`
	Duration time.Duration `json:"-"`
}

// Plan uses Playwright Agents to plan a sequence of actions from a natural language instruction.
func (c *AgentsClient) Plan(ctx context.Context, instruction string, pageURL string) (*PlanResult, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{"@playwright/agents", "plan",
		"--instruction", instruction,
		"--url", pageURL,
		"--format", "json",
	}

	cmd := exec.CommandContext(ctx, c.npxPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		c.logger.Warn("playwright agents plan failed",
			"error", err,
			"stderr", stderr.String())
		return nil, fmt.Errorf("playwright agents plan: %w: %s", err, stderr.String())
	}

	var result PlanResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("playwright agents plan: parse output: %w", err)
	}

	result.Duration = time.Since(start)
	c.logger.Info("playwright agents plan",
		slog.Int("steps", len(result.Steps)),
		slog.Duration("duration", result.Duration))

	return &result, nil
}

// Generate uses Playwright Agents to generate test code from a natural language instruction.
func (c *AgentsClient) Generate(ctx context.Context, instruction string, pageURL string) (*GenerateResult, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{"@playwright/agents", "generate",
		"--instruction", instruction,
		"--url", pageURL,
		"--format", "json",
	}

	cmd := exec.CommandContext(ctx, c.npxPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		c.logger.Warn("playwright agents generate failed",
			"error", err,
			"stderr", stderr.String())
		return nil, fmt.Errorf("playwright agents generate: %w: %s", err, stderr.String())
	}

	var result GenerateResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("playwright agents generate: parse output: %w", err)
	}

	result.Duration = time.Since(start)
	c.logger.Info("playwright agents generate",
		slog.String("language", result.Language),
		slog.Duration("duration", result.Duration))

	return &result, nil
}

// Available checks if the Playwright Agents CLI is installed.
func (c *AgentsClient) Available() bool {
	cmd := exec.Command(c.npxPath, "@playwright/agents", "--version")
	return cmd.Run() == nil
}

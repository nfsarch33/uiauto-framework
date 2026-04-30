package playwright

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestNewAgentsClient(t *testing.T) {
	c := NewAgentsClient()
	if c.npxPath != "npx" {
		t.Errorf("expected npx, got %s", c.npxPath)
	}
	if c.timeout != 120*time.Second {
		t.Errorf("expected 120s timeout, got %v", c.timeout)
	}
}

func TestNewAgentsClientWithOptions(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewAgentsClient(
		WithNpxPath("/usr/local/bin/npx"),
		WithTimeout(30*time.Second),
		WithLogger(l),
	)
	if c.npxPath != "/usr/local/bin/npx" {
		t.Errorf("expected custom npx path, got %s", c.npxPath)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", c.timeout)
	}
}

func TestPlanStep(t *testing.T) {
	step := PlanStep{
		Action:      "click",
		Selector:    "[data-testid='reply-btn']",
		Description: "Click the reply button",
	}
	if step.Action != "click" {
		t.Errorf("expected click, got %s", step.Action)
	}
}

func TestPlanResultSteps(t *testing.T) {
	result := PlanResult{
		Steps: []PlanStep{
			{Action: "navigate", Description: "Go to ticket page"},
			{Action: "click", Selector: "#reply", Description: "Click reply"},
		},
		Model:  "gpt-4o",
		Tokens: 1500,
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
}

func TestGenerateResult(t *testing.T) {
	result := GenerateResult{
		Code:     `await page.click('[data-testid="reply"]')`,
		Language: "typescript",
		Model:    "gpt-4o",
	}
	if result.Language != "typescript" {
		t.Errorf("expected typescript, got %s", result.Language)
	}
}

func TestAvailable_NotInstalled(t *testing.T) {
	c := NewAgentsClient(WithNpxPath("/nonexistent/npx"))
	if c.Available() {
		t.Error("expected false for nonexistent npx path")
	}
}

func TestPlan_InvalidNpx(t *testing.T) {
	c := NewAgentsClient(
		WithNpxPath("/nonexistent/npx"),
		WithTimeout(2*time.Second),
	)
	_, err := c.Plan(context.Background(), "click button", "http://localhost")
	if err == nil {
		t.Error("expected error for invalid npx path")
	}
}

func TestGenerate_InvalidNpx(t *testing.T) {
	c := NewAgentsClient(
		WithNpxPath("/nonexistent/npx"),
		WithTimeout(2*time.Second),
	)
	_, err := c.Generate(context.Background(), "click button", "http://localhost")
	if err == nil {
		t.Error("expected error for invalid npx path")
	}
}

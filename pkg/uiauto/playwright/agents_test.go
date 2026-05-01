package playwright

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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

// fakeNpx writes a small shell script to dir that prints body to stdout when
// invoked, so AgentsClient.{Plan,Generate} can exercise their JSON-parsing
// path without requiring a real Node.js + Playwright installation.
func fakeNpx(t *testing.T, dir, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake npx test is unix-only")
	}
	script := filepath.Join(dir, "npx")
	content := "#!/bin/sh\ncat <<'EOF'\n" + body + "\nEOF\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func fakeNpxFailing(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake npx test is unix-only")
	}
	script := filepath.Join(dir, "npx")
	content := "#!/bin/sh\necho boom 1>&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestPlan_HappyPath(t *testing.T) {
	npx := fakeNpx(t, t.TempDir(), `{"steps":[{"action":"click","selector":"#go","description":"go"}],"model":"m","tokens":42}`)
	c := NewAgentsClient(WithNpxPath(npx), WithTimeout(5*time.Second))
	got, err := c.Plan(context.Background(), "click", "http://x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Steps) != 1 || got.Steps[0].Selector != "#go" {
		t.Errorf("unexpected: %+v", got)
	}
	if got.Model != "m" || got.Tokens != 42 {
		t.Errorf("model/tokens: %+v", got)
	}
	if got.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestPlan_BadJSON(t *testing.T) {
	npx := fakeNpx(t, t.TempDir(), `not-json`)
	c := NewAgentsClient(WithNpxPath(npx), WithTimeout(5*time.Second))
	if _, err := c.Plan(context.Background(), "go", "http://x"); err == nil {
		t.Error("expected JSON parse error")
	}
}

func TestPlan_NpxFailureSurfacesStderr(t *testing.T) {
	npx := fakeNpxFailing(t, t.TempDir())
	c := NewAgentsClient(WithNpxPath(npx), WithTimeout(5*time.Second))
	_, err := c.Plan(context.Background(), "click", "http://x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "boom") {
		t.Errorf("error missing stderr: %v", err)
	}
}

func TestGenerate_HappyPath(t *testing.T) {
	npx := fakeNpx(t, t.TempDir(), `{"code":"await page.click()","language":"typescript","model":"m"}`)
	c := NewAgentsClient(WithNpxPath(npx), WithTimeout(5*time.Second))
	got, err := c.Generate(context.Background(), "click", "http://x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Language != "typescript" || got.Code == "" {
		t.Errorf("unexpected: %+v", got)
	}
	if got.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestGenerate_BadJSON(t *testing.T) {
	npx := fakeNpx(t, t.TempDir(), `not-json`)
	c := NewAgentsClient(WithNpxPath(npx), WithTimeout(5*time.Second))
	if _, err := c.Generate(context.Background(), "go", "http://x"); err == nil {
		t.Error("expected JSON parse error")
	}
}

func TestGenerate_NpxFailureSurfacesStderr(t *testing.T) {
	npx := fakeNpxFailing(t, t.TempDir())
	c := NewAgentsClient(WithNpxPath(npx), WithTimeout(5*time.Second))
	_, err := c.Generate(context.Background(), "click", "http://x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "boom") {
		t.Errorf("error missing stderr: %v", err)
	}
}

func TestAvailable_TrueWhenScriptExists(t *testing.T) {
	npx := fakeNpx(t, t.TempDir(), `1.0.0`)
	c := NewAgentsClient(WithNpxPath(npx))
	if !c.Available() {
		t.Error("expected available=true for working script")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

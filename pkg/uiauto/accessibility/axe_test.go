package accessibility

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// mockEvaluator simulates a browser's JS evaluation.
type mockEvaluator struct {
	response string
	err      error
}

func (m *mockEvaluator) Evaluate(_ context.Context, _ string, result interface{}) error {
	if m.err != nil {
		return m.err
	}
	ptr, ok := result.(*string)
	if !ok {
		return fmt.Errorf("expected *string result")
	}
	*ptr = m.response
	return nil
}

func sampleAuditJSON() string {
	result := AuditResult{
		Violations: []Violation{
			{
				ID:          "color-contrast",
				Impact:      "serious",
				Tags:        []string{"wcag2aa", "cat.color"},
				Description: "Elements must have sufficient color contrast",
				Help:        "Elements must meet WCAG 2 AA contrast ratio thresholds",
				HelpURL:     "https://dequeuniversity.com/rules/axe/4.10/color-contrast",
				Nodes: []ViolationNode{
					{
						HTML:           `<span class="low-contrast">Hello</span>`,
						Impact:         "serious",
						Target:         []string{".low-contrast"},
						FailureSummary: "Fix any of the following: Element has insufficient color contrast",
					},
				},
			},
			{
				ID:          "image-alt",
				Impact:      "critical",
				Tags:        []string{"wcag2aa", "cat.text-alternatives"},
				Description: "Images must have alternate text",
				Help:        "Images must have alt text",
				Nodes: []ViolationNode{
					{HTML: `<img src="photo.jpg">`, Impact: "critical", Target: []string{"img"}},
					{HTML: `<img src="logo.png">`, Impact: "critical", Target: []string{"img:nth-child(2)"}},
				},
			},
		},
		Passes: []Violation{
			{ID: "html-lang-valid", Impact: "", Tags: []string{"wcag2aa"}},
		},
		URL:        "http://localhost:3000/",
		Timestamp:  "2026-03-20T10:00:00.000Z",
		TestEngine: TestEngine{Name: "axe-core", Version: "4.10.2"},
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func TestInjectAndRun(t *testing.T) {
	eval := &mockEvaluator{response: sampleAuditJSON()}
	result, err := InjectAndRun(context.Background(), eval)
	if err != nil {
		t.Fatalf("InjectAndRun: %v", err)
	}

	if len(result.Violations) != 2 {
		t.Errorf("expected 2 violations, got %d", len(result.Violations))
	}
	if result.URL != "http://localhost:3000/" {
		t.Errorf("URL = %q", result.URL)
	}
	if result.TestEngine.Version != "4.10.2" {
		t.Errorf("TestEngine.Version = %q", result.TestEngine.Version)
	}
}

func TestInjectAndRunError(t *testing.T) {
	eval := &mockEvaluator{err: fmt.Errorf("browser crashed")}
	_, err := InjectAndRun(context.Background(), eval)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInjectAndRunBadJSON(t *testing.T) {
	eval := &mockEvaluator{response: "not-valid-json"}
	_, err := InjectAndRun(context.Background(), eval)
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestAuditResultSummary(t *testing.T) {
	eval := &mockEvaluator{response: sampleAuditJSON()}
	result, _ := InjectAndRun(context.Background(), eval)

	summary := result.Summary()
	if summary["serious"] != 1 {
		t.Errorf("serious = %d, want 1", summary["serious"])
	}
	if summary["critical"] != 2 {
		t.Errorf("critical = %d, want 2", summary["critical"])
	}
}

func TestTotalViolations(t *testing.T) {
	eval := &mockEvaluator{response: sampleAuditJSON()}
	result, _ := InjectAndRun(context.Background(), eval)

	if total := result.TotalViolations(); total != 3 {
		t.Errorf("TotalViolations = %d, want 3", total)
	}
}

func TestCriticalAndSerious(t *testing.T) {
	eval := &mockEvaluator{response: sampleAuditJSON()}
	result, _ := InjectAndRun(context.Background(), eval)

	cs := result.CriticalAndSerious()
	if len(cs) != 2 {
		t.Errorf("CriticalAndSerious = %d, want 2", len(cs))
	}
}

func TestHasWCAG2AA(t *testing.T) {
	v1 := Violation{Tags: []string{"wcag2aa", "cat.color"}}
	if !v1.HasWCAG2AA() {
		t.Error("expected HasWCAG2AA = true for wcag2aa tag")
	}

	v2 := Violation{Tags: []string{"best-practice"}}
	if v2.HasWCAG2AA() {
		t.Error("expected HasWCAG2AA = false for best-practice tag")
	}
}

func TestRunOptions(t *testing.T) {
	t.Run("WithAxeSource", func(t *testing.T) {
		cfg := defaultRunConfig()
		WithAxeSource("https://custom.cdn/axe.js")(&cfg)
		if cfg.axeSource != "https://custom.cdn/axe.js" {
			t.Errorf("axeSource = %q", cfg.axeSource)
		}
	})

	t.Run("WithTags", func(t *testing.T) {
		cfg := defaultRunConfig()
		WithTags("wcag2a")(&cfg)
		if len(cfg.tags) != 1 || cfg.tags[0] != "wcag2a" {
			t.Errorf("tags = %v", cfg.tags)
		}
	})

	t.Run("WithContext", func(t *testing.T) {
		cfg := defaultRunConfig()
		WithContext("#main")(&cfg)
		if cfg.context != `{include: ['#main']}` {
			t.Errorf("context = %q", cfg.context)
		}
	})
}

func TestRunConfigJSON(t *testing.T) {
	cfg := defaultRunConfig()
	got := cfg.runConfigJSON()
	if got == "" {
		t.Error("runConfigJSON returned empty string")
	}
}

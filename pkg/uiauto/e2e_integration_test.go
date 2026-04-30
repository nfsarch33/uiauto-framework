package uiauto

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

// requireIntegration skips unless UIAUTO_INTEGRATION=1 is set.
func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("UIAUTO_INTEGRATION") != "1" {
		t.Skip("set UIAUTO_INTEGRATION=1 to run integration tests")
	}
	skipWithoutBrowser(t)
}

// --- Scenario: Clean (no drift, no healing needed) ---

func TestE2E_Clean_RegisterAndExecute(t *testing.T) {
	requireIntegration(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	if err := agent.RegisterPattern(ctx, "username", "#username", "text input"); err != nil {
		t.Fatalf("RegisterPattern username: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "password", "#password", "password input"); err != nil {
		t.Fatalf("RegisterPattern password: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"); err != nil {
		t.Fatalf("RegisterPattern submit: %v", err)
	}

	result := agent.RunTask(ctx, "login_clean", []Action{
		{TargetID: "username", Type: "click", Description: "focus username"},
		{TargetID: "submit", Type: "click", Description: "click submit"},
	})

	if result.Status != TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s; error=%v", result.Status, result.Error)
	}
	if len(result.HealResults) != 0 {
		t.Errorf("expected no healing in clean scenario, got %d heal results", len(result.HealResults))
	}

	metrics := agent.Metrics()
	if metrics.Executor.TotalActions < 2 {
		t.Errorf("expected at least 2 actions, got %d", metrics.Executor.TotalActions)
	}
}

// --- Scenario: Drifted (selectors changed, healing should recover) ---

func TestE2E_Drifted_HealingRecovers(t *testing.T) {
	requireIntegration(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Phase 1: register patterns against clean HTML
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate clean: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"); err != nil {
		t.Fatalf("RegisterPattern submit: %v", err)
	}

	// Phase 2: switch to drifted scenario
	fs.SetScenario(ScenarioDrifted)

	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate drifted: %v", err)
	}

	// Drift detection should fire
	heals := agent.DetectDriftAndHeal(ctx)
	t.Logf("heal results after drift: %d results", len(heals))
	for _, h := range heals {
		t.Logf("  target=%s old=%s new=%s method=%s success=%v",
			h.TargetID, h.OldSelector, h.NewSelector, h.Method, h.Success)
	}
}

// --- Scenario: Broken (page completely changed, healing should exhaust) ---

func TestE2E_Broken_AllHealingFails(t *testing.T) {
	requireIntegration(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Register against clean
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate clean: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Switch to broken (maintenance page)
	fs.SetScenario(ScenarioBroken)

	result := agent.RunTask(ctx, "login_broken", []Action{
		{TargetID: "submit", Type: "click", Description: "click submit on broken page"},
	})

	if result.Status == TaskCompleted {
		t.Error("expected task to fail on broken page, but it completed")
	}
	if result.Error == nil {
		t.Error("expected error on broken page, got nil")
	}
}

// --- Scenario: Unknown (completely new layout, structural match may help) ---

func TestE2E_Unknown_StructuralRecovery(t *testing.T) {
	requireIntegration(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Register against clean
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate clean: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Switch to unknown layout
	fs.SetScenario(ScenarioUnknown)
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate unknown: %v", err)
	}

	heals := agent.DetectDriftAndHeal(ctx)
	t.Logf("heal results for unknown layout: %d results", len(heals))
	for _, h := range heals {
		t.Logf("  target=%s method=%s success=%v confidence=%.2f new=%s",
			h.TargetID, h.Method, h.Success, h.Confidence, h.NewSelector)
	}
}

// --- Fixture Server Unit Tests ---

func TestFixtureServer_ScenarioSwitch(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	scenarios := []FixtureScenario{ScenarioClean, ScenarioDrifted, ScenarioBroken, ScenarioUnknown}
	for _, s := range scenarios {
		fs.SetScenario(s)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fs.URL()+"/login", nil)
		if err != nil {
			t.Fatalf("NewRequest (scenario=%s): %v", s, err)
		}
		resp, err := fs.Server.Client().Do(req)
		if err != nil {
			t.Fatalf("GET /login (scenario=%s): %v", s, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("scenario %s: expected 200, got %d", s, resp.StatusCode)
		}
	}
}

func TestFixtureServer_404(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fs.URL()+"/nonexistent", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := fs.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFixtureServer_FallbackToClean(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	// Dashboard has no "broken" variant, should fall back to clean
	fs.SetScenario(ScenarioBroken)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fs.URL()+"/dashboard", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := fs.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /dashboard: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 (fallback to clean), got %d", resp.StatusCode)
	}
}

// --- Mock LLM for integration tests ---

type mockIntegrationLLM struct {
	response string
	err      error
}

func (m *mockIntegrationLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: m.response}},
		},
	}, nil
}

var _ llm.Provider = (*mockIntegrationLLM)(nil)

// TestE2E_FullPipeline_WithMockLLM exercises the full pipeline with a mock LLM
// providing smart discovery responses.
func TestE2E_FullPipeline_WithMockLLM(t *testing.T) {
	requireIntegration(t)

	fs := NewFixtureServer()
	defer fs.Close()

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	mockLLM := &mockIntegrationLLM{
		response: `[{"selector": "[data-testid=\"auth-submit\"]", "description": "authentication submit button"}]`,
	}

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
		LLMProvider: mockLLM,
		SmartModels: []string{"mock-model"},
		Logger:      testDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Register on clean
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := agent.RegisterPattern(ctx, "submit", "#submit-btn", "submit button"); err != nil {
		t.Fatalf("RegisterPattern: %v", err)
	}

	// Switch to unknown - LLM should help discover new selector
	fs.SetScenario(ScenarioUnknown)
	if err := agent.Navigate(fs.URL() + "/login"); err != nil {
		t.Fatalf("Navigate unknown: %v", err)
	}

	heals := agent.DetectDriftAndHeal(ctx)
	t.Logf("full pipeline heal results: %d", len(heals))
	for _, h := range heals {
		t.Logf("  target=%s method=%s success=%v new=%s",
			h.TargetID, h.Method, h.Success, h.NewSelector)
	}

	metrics := agent.Metrics()
	t.Logf("metrics: executor.total=%d healer.attempts=%d healer.successes=%d",
		metrics.Executor.TotalActions, metrics.Healer.TotalAttempts, metrics.Healer.SuccessfulHeals)
}
